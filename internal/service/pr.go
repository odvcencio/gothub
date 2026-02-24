package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/odvcencio/got/pkg/diff"
	"github.com/odvcencio/got/pkg/merge"
	"github.com/odvcencio/got/pkg/object"
	"github.com/odvcencio/gothub/internal/database"
	"github.com/odvcencio/gothub/internal/entityutil"
	"github.com/odvcencio/gothub/internal/gotstore"
	"github.com/odvcencio/gothub/internal/models"
)

// MergePreviewResponse holds the result of a structural merge preview.
type MergePreviewResponse struct {
	HasConflicts  bool            `json:"has_conflicts"`
	ConflictCount int             `json:"conflict_count"`
	Stats         MergeStatsInfo  `json:"stats"`
	Files         []FileMergeInfo `json:"files"`
}

type MergeStatsInfo struct {
	TotalEntities  int `json:"total_entities"`
	Unchanged      int `json:"unchanged"`
	OursModified   int `json:"ours_modified"`
	TheirsModified int `json:"theirs_modified"`
	BothModified   int `json:"both_modified"`
	Added          int `json:"added"`
	Deleted        int `json:"deleted"`
	Conflicts      int `json:"conflicts"`
}

type FileMergeInfo struct {
	Path          string `json:"path"`
	Status        string `json:"status"` // "clean", "conflict", "added", "deleted"
	ConflictCount int    `json:"conflict_count"`
}

// MergeConflictError reports file paths that have unresolved merge conflicts.
type MergeConflictError struct {
	Paths []string
}

func (e *MergeConflictError) Error() string {
	if len(e.Paths) == 0 {
		return "merge has conflicts"
	}
	if len(e.Paths) == 1 {
		return fmt.Sprintf("merge conflict in %s", e.Paths[0])
	}
	return fmt.Sprintf("merge conflicts in %d files: %s", len(e.Paths), strings.Join(e.Paths, ", "))
}

// TargetBranchMovedError indicates the target branch changed during merge.
type TargetBranchMovedError struct {
	Branch   string
	Expected object.Hash
	Actual   object.Hash
}

func (e *TargetBranchMovedError) Error() string {
	return fmt.Sprintf(
		"target branch %s moved during merge (expected %s, got %s); refresh and retry",
		e.Branch, e.Expected, e.Actual,
	)
}

const (
	mergePreviewCacheTTL        = 30 * time.Second
	mergePreviewCacheMaxEntries = 256
	mergePreviewCleanupInterval = 2 * time.Second
)

type mergePreviewCacheEntry struct {
	resp     *MergePreviewResponse
	cachedAt time.Time
	expires  time.Time
}

type PRService struct {
	db           database.DB
	repoSvc      *RepoService
	browseSvc    *BrowseService
	codeIntelSvc *CodeIntelService
	lineageSvc   *EntityLineageService

	mergePreviewMu    sync.Mutex
	mergePreviewCache map[string]mergePreviewCacheEntry
	mergePreviewPrune time.Time
}

func NewPRService(db database.DB, repoSvc *RepoService, browseSvc *BrowseService) *PRService {
	return &PRService{
		db:                db,
		repoSvc:           repoSvc,
		browseSvc:         browseSvc,
		mergePreviewCache: make(map[string]mergePreviewCacheEntry),
	}
}

func (s *PRService) SetCodeIntelService(codeIntelSvc *CodeIntelService) {
	s.codeIntelSvc = codeIntelSvc
}

func (s *PRService) SetLineageService(lineageSvc *EntityLineageService) {
	s.lineageSvc = lineageSvc
}

func (s *PRService) Create(ctx context.Context, repoID, authorID int64, title, body, srcBranch, tgtBranch string) (*models.PullRequest, error) {
	pr := &models.PullRequest{
		RepoID:       repoID,
		Title:        title,
		Body:         body,
		State:        "open",
		AuthorID:     authorID,
		SourceBranch: srcBranch,
		TargetBranch: tgtBranch,
	}
	if err := s.db.CreatePullRequest(ctx, pr); err != nil {
		return nil, fmt.Errorf("create PR: %w", err)
	}
	return pr, nil
}

func (s *PRService) Get(ctx context.Context, repoID int64, number int) (*models.PullRequest, error) {
	return s.db.GetPullRequest(ctx, repoID, number)
}

func (s *PRService) List(ctx context.Context, repoID int64, state string) ([]models.PullRequest, error) {
	return s.db.ListPullRequests(ctx, repoID, state)
}

// Diff computes the entity-level diff for a PR.
func (s *PRService) Diff(ctx context.Context, owner, repo string, pr *models.PullRequest) (*DiffResponse, error) {
	store, err := s.repoSvc.OpenStore(ctx, owner, repo)
	if err != nil {
		return nil, err
	}

	srcHash, err := store.Refs.Get("heads/" + pr.SourceBranch)
	if err != nil {
		return nil, fmt.Errorf("source branch %s: %w", pr.SourceBranch, err)
	}
	tgtHash, err := store.Refs.Get("heads/" + pr.TargetBranch)
	if err != nil {
		return nil, fmt.Errorf("target branch %s: %w", pr.TargetBranch, err)
	}

	srcCommit, err := store.Objects.ReadCommit(srcHash)
	if err != nil {
		return nil, err
	}
	tgtCommit, err := store.Objects.ReadCommit(tgtHash)
	if err != nil {
		return nil, err
	}

	srcFiles, _ := flattenTree(store.Objects, srcCommit.TreeHash, "")
	tgtFiles, _ := flattenTree(store.Objects, tgtCommit.TreeHash, "")

	tgtMap := make(map[string]FileEntry, len(tgtFiles))
	for _, f := range tgtFiles {
		tgtMap[f.Path] = f
	}
	srcMap := make(map[string]FileEntry, len(srcFiles))
	for _, f := range srcFiles {
		srcMap[f.Path] = f
	}

	var fileDiffs []FileDiffResponse

	for path, srcEntry := range srcMap {
		tgtEntry, exists := tgtMap[path]
		if exists && tgtEntry.BlobHash == srcEntry.BlobHash {
			continue
		}
		var tgtData, srcData []byte
		if exists {
			tgtData, _ = readBlobData(store.Objects, object.Hash(tgtEntry.BlobHash))
		}
		srcData, _ = readBlobData(store.Objects, object.Hash(srcEntry.BlobHash))
		fd, err := diff.DiffFiles(path, tgtData, srcData)
		if err != nil || len(fd.Changes) == 0 {
			continue
		}
		fileDiffs = append(fileDiffs, fileDiffToResponse(fd))
	}

	for path, tgtEntry := range tgtMap {
		if _, exists := srcMap[path]; !exists {
			// File only in target (deleted in source branch perspective — or new in target)
			tgtData, _ := readBlobData(store.Objects, object.Hash(tgtEntry.BlobHash))
			fd, err := diff.DiffFiles(path, tgtData, nil)
			if err != nil || len(fd.Changes) == 0 {
				continue
			}
			fileDiffs = append(fileDiffs, fileDiffToResponse(fd))
		}
	}

	return &DiffResponse{
		Base:  string(tgtHash),
		Head:  string(srcHash),
		Files: fileDiffs,
	}, nil
}

// MergePreview computes a structural merge preview without committing.
func (s *PRService) MergePreview(ctx context.Context, owner, repo string, pr *models.PullRequest) (*MergePreviewResponse, error) {
	store, err := s.repoSvc.OpenStore(ctx, owner, repo)
	if err != nil {
		return nil, err
	}

	srcHash, err := store.Refs.Get("heads/" + pr.SourceBranch)
	if err != nil {
		return nil, fmt.Errorf("source branch: %w", err)
	}
	tgtHash, err := store.Refs.Get("heads/" + pr.TargetBranch)
	if err != nil {
		return nil, fmt.Errorf("target branch: %w", err)
	}
	cacheKey := fmt.Sprintf("%d:%s:%s", pr.RepoID, tgtHash, srcHash)
	if cached, ok := s.getCachedMergePreview(cacheKey); ok {
		return cached, nil
	}

	// Find merge base
	baseHash, err := FindMergeBase(store.Objects, tgtHash, srcHash)
	if err != nil {
		return nil, fmt.Errorf("find merge base: %w", err)
	}

	baseCommit, err := store.Objects.ReadCommit(baseHash)
	if err != nil {
		return nil, fmt.Errorf("read base commit: %w", err)
	}
	srcCommit, err := store.Objects.ReadCommit(srcHash)
	if err != nil {
		return nil, fmt.Errorf("read source commit: %w", err)
	}
	tgtCommit, err := store.Objects.ReadCommit(tgtHash)
	if err != nil {
		return nil, fmt.Errorf("read target commit: %w", err)
	}

	baseFiles, err := flattenTree(store.Objects, baseCommit.TreeHash, "")
	if err != nil {
		return nil, fmt.Errorf("flatten base tree: %w", err)
	}
	srcFiles, err := flattenTree(store.Objects, srcCommit.TreeHash, "")
	if err != nil {
		return nil, fmt.Errorf("flatten source tree: %w", err)
	}
	tgtFiles, err := flattenTree(store.Objects, tgtCommit.TreeHash, "")
	if err != nil {
		return nil, fmt.Errorf("flatten target tree: %w", err)
	}

	baseMap := indexFiles(baseFiles)
	srcMap := indexFiles(srcFiles)
	tgtMap := indexFiles(tgtFiles)

	// Collect all file paths
	allPaths := map[string]bool{}
	for p := range baseMap {
		allPaths[p] = true
	}
	for p := range srcMap {
		allPaths[p] = true
	}
	for p := range tgtMap {
		allPaths[p] = true
	}

	resp := &MergePreviewResponse{}
	var totalStats merge.MergeStats

	for path := range allPaths {
		baseEntry := baseMap[path]
		srcEntry := srcMap[path]
		tgtEntry := tgtMap[path]

		// Read blob data (empty if not present)
		var baseData, srcData, tgtData []byte
		if baseEntry.BlobHash != "" {
			baseData, err = readBlobData(store.Objects, object.Hash(baseEntry.BlobHash))
			if err != nil {
				return nil, fmt.Errorf("read base blob %s: %w", path, err)
			}
		}
		if srcEntry.BlobHash != "" {
			srcData, err = readBlobData(store.Objects, object.Hash(srcEntry.BlobHash))
			if err != nil {
				return nil, fmt.Errorf("read source blob %s: %w", path, err)
			}
		}
		if tgtEntry.BlobHash != "" {
			tgtData, err = readBlobData(store.Objects, object.Hash(tgtEntry.BlobHash))
			if err != nil {
				return nil, fmt.Errorf("read target blob %s: %w", path, err)
			}
		}

		// Skip files unchanged between all three
		if baseEntry.BlobHash == srcEntry.BlobHash && baseEntry.BlobHash == tgtEntry.BlobHash {
			continue
		}

		info := FileMergeInfo{Path: path}

		// Determine file status
		if srcEntry.BlobHash == "" && tgtEntry.BlobHash == "" {
			continue // deleted in both
		}
		if baseEntry.BlobHash == "" && srcEntry.BlobHash != "" {
			info.Status = "added"
			resp.Files = append(resp.Files, info)
			continue
		}
		if srcEntry.BlobHash == "" {
			info.Status = "deleted"
			resp.Files = append(resp.Files, info)
			continue
		}

		// Three-way merge
		result, err := merge.MergeFiles(path, baseData, tgtData, srcData)
		if err != nil {
			info.Status = "conflict"
			info.ConflictCount = 1
		} else {
			if result.HasConflicts {
				info.Status = "conflict"
				info.ConflictCount = result.ConflictCount
			} else {
				info.Status = "clean"
			}
			totalStats.TotalEntities += result.Stats.TotalEntities
			totalStats.Unchanged += result.Stats.Unchanged
			totalStats.OursModified += result.Stats.OursModified
			totalStats.TheirsModified += result.Stats.TheirsModified
			totalStats.BothModified += result.Stats.BothModified
			totalStats.Added += result.Stats.Added
			totalStats.Deleted += result.Stats.Deleted
			totalStats.Conflicts += result.Stats.Conflicts
		}

		resp.Files = append(resp.Files, info)
	}

	resp.HasConflicts = totalStats.Conflicts > 0
	resp.ConflictCount = totalStats.Conflicts
	resp.Stats = MergeStatsInfo{
		TotalEntities:  totalStats.TotalEntities,
		Unchanged:      totalStats.Unchanged,
		OursModified:   totalStats.OursModified,
		TheirsModified: totalStats.TheirsModified,
		BothModified:   totalStats.BothModified,
		Added:          totalStats.Added,
		Deleted:        totalStats.Deleted,
		Conflicts:      totalStats.Conflicts,
	}
	s.setCachedMergePreview(cacheKey, resp)
	return resp, nil
}

// Merge executes the structural merge, creating a merge commit.
func (s *PRService) Merge(ctx context.Context, owner, repo string, pr *models.PullRequest, mergerName string) (object.Hash, error) {
	store, err := s.repoSvc.OpenStore(ctx, owner, repo)
	if err != nil {
		return "", err
	}

	srcHash, err := store.Refs.Get("heads/" + pr.SourceBranch)
	if err != nil {
		return "", fmt.Errorf("source branch: %w", err)
	}
	tgtHash, err := store.Refs.Get("heads/" + pr.TargetBranch)
	if err != nil {
		return "", fmt.Errorf("target branch: %w", err)
	}

	baseHash, err := FindMergeBase(store.Objects, tgtHash, srcHash)
	if err != nil {
		return "", fmt.Errorf("find merge base: %w", err)
	}

	baseCommit, err := store.Objects.ReadCommit(baseHash)
	if err != nil {
		return "", fmt.Errorf("read base commit: %w", err)
	}
	srcCommit, err := store.Objects.ReadCommit(srcHash)
	if err != nil {
		return "", fmt.Errorf("read source commit: %w", err)
	}
	tgtCommit, err := store.Objects.ReadCommit(tgtHash)
	if err != nil {
		return "", fmt.Errorf("read target commit: %w", err)
	}

	baseFiles, err := flattenTree(store.Objects, baseCommit.TreeHash, "")
	if err != nil {
		return "", fmt.Errorf("flatten base tree: %w", err)
	}
	srcFiles, err := flattenTree(store.Objects, srcCommit.TreeHash, "")
	if err != nil {
		return "", fmt.Errorf("flatten source tree: %w", err)
	}
	tgtFiles, err := flattenTree(store.Objects, tgtCommit.TreeHash, "")
	if err != nil {
		return "", fmt.Errorf("flatten target tree: %w", err)
	}

	baseMap := indexFiles(baseFiles)
	srcMap := indexFiles(srcFiles)
	tgtMap := indexFiles(tgtFiles)

	allPaths := map[string]bool{}
	for p := range baseMap {
		allPaths[p] = true
	}
	for p := range srcMap {
		allPaths[p] = true
	}
	for p := range tgtMap {
		allPaths[p] = true
	}

	// Build merged tree entries
	mergedEntries := map[string]object.Hash{} // path -> blob hash
	conflictedPaths := make([]string, 0, 4)

	for path := range allPaths {
		baseEntry := baseMap[path]
		srcEntry := srcMap[path]
		tgtEntry := tgtMap[path]

		if srcEntry.BlobHash == "" {
			// Deleted in source
			continue
		}
		if tgtEntry.BlobHash == "" && baseEntry.BlobHash == "" {
			// New in source
			mergedEntries[path] = object.Hash(srcEntry.BlobHash)
			continue
		}
		if baseEntry.BlobHash == srcEntry.BlobHash {
			// Source unchanged, use target
			if tgtEntry.BlobHash != "" {
				mergedEntries[path] = object.Hash(tgtEntry.BlobHash)
			}
			continue
		}
		if baseEntry.BlobHash == tgtEntry.BlobHash {
			// Target unchanged, use source
			mergedEntries[path] = object.Hash(srcEntry.BlobHash)
			continue
		}

		// Both changed — three-way merge
		var baseData, srcData, tgtData []byte
		if baseEntry.BlobHash != "" {
			baseData, err = readBlobData(store.Objects, object.Hash(baseEntry.BlobHash))
			if err != nil {
				return "", fmt.Errorf("read base blob %s: %w", path, err)
			}
		}
		srcData, err = readBlobData(store.Objects, object.Hash(srcEntry.BlobHash))
		if err != nil {
			return "", fmt.Errorf("read source blob %s: %w", path, err)
		}
		tgtData, err = readBlobData(store.Objects, object.Hash(tgtEntry.BlobHash))
		if err != nil {
			return "", fmt.Errorf("read target blob %s: %w", path, err)
		}

		result, err := merge.MergeFiles(path, baseData, tgtData, srcData)
		if err != nil || result.HasConflicts {
			conflictedPaths = append(conflictedPaths, path)
			continue
		}

		blobHash, err := store.Objects.WriteBlob(&object.Blob{Data: result.Merged})
		if err != nil {
			return "", fmt.Errorf("write merged blob: %w", err)
		}
		mergedEntries[path] = blobHash
	}
	if len(conflictedPaths) > 0 {
		sort.Strings(conflictedPaths)
		return "", &MergeConflictError{Paths: conflictedPaths}
	}

	// Build tree from merged entries
	mergeTreeHash, err := buildTreeFromFiles(store.Objects, mergedEntries)
	if err != nil {
		return "", fmt.Errorf("build merge tree: %w", err)
	}
	mergeTreeHash, err = enrichTreeWithEntities(store.Objects, mergeTreeHash, "")
	if err != nil {
		return "", fmt.Errorf("enrich merge tree entities: %w", err)
	}

	// Create merge commit
	mergeCommit := &object.CommitObj{
		TreeHash:  mergeTreeHash,
		Parents:   []object.Hash{tgtHash, srcHash},
		Author:    mergerName,
		Timestamp: time.Now().Unix(),
		Message:   fmt.Sprintf("Merge pull request #%d: %s", pr.Number, pr.Title),
	}
	mergeCommitHash, err := store.Objects.WriteCommit(mergeCommit)
	if err != nil {
		return "", fmt.Errorf("write merge commit: %w", err)
	}

	// Update target branch ref with CAS to avoid clobbering concurrent pushes.
	if err := updateTargetBranchRef(store, pr.TargetBranch, tgtHash, mergeCommitHash); err != nil {
		return "", err
	}

	// Update PR state
	now := time.Now()
	pr.State = "merged"
	pr.MergeCommit = string(mergeCommitHash)
	pr.MergeMethod = "structural"
	pr.MergedAt = &now
	pr.SourceCommit = string(srcHash)
	pr.TargetCommit = string(tgtHash)
	if err := s.db.UpdatePullRequest(ctx, pr); err != nil {
		if rollbackErr := updateTargetBranchRef(store, pr.TargetBranch, mergeCommitHash, tgtHash); rollbackErr != nil {
			return "", fmt.Errorf("update pull request: %w (also failed to roll back target branch: %v)", err, rollbackErr)
		}
		return "", fmt.Errorf("update pull request: %w", err)
	}

	// Keep merge commits aligned with push paths by indexing lineage and code intel.
	if s.lineageSvc != nil {
		if err := s.lineageSvc.IndexCommit(ctx, pr.RepoID, store, mergeCommitHash); err != nil {
			return "", fmt.Errorf("index merge commit lineage: %w", err)
		}
	}
	if s.codeIntelSvc != nil {
		if err := s.codeIntelSvc.EnsureCommitIndexed(ctx, pr.RepoID, store, owner+"/"+repo, mergeCommitHash); err != nil {
			return "", fmt.Errorf("index merge commit codeintel: %w", err)
		}
	}

	return mergeCommitHash, nil
}

func updateTargetBranchRef(store *gotstore.RepoStore, branch string, expectedOld, newHash object.Hash) error {
	refName := "heads/" + branch
	if err := store.Refs.Update(refName, &expectedOld, &newHash); err != nil {
		var mismatch *gotstore.RefCASMismatchError
		if errors.As(err, &mismatch) {
			return &TargetBranchMovedError{
				Branch:   branch,
				Expected: expectedOld,
				Actual:   mismatch.Actual,
			}
		}
		return fmt.Errorf("update ref: %w", err)
	}
	return nil
}

func (s *PRService) getCachedMergePreview(key string) (*MergePreviewResponse, bool) {
	s.mergePreviewMu.Lock()
	defer s.mergePreviewMu.Unlock()

	now := time.Now()
	s.pruneExpiredMergePreviewCache(now)
	entry, ok := s.mergePreviewCache[key]
	if !ok || now.After(entry.expires) {
		if ok {
			delete(s.mergePreviewCache, key)
		}
		return nil, false
	}
	return cloneMergePreviewResponse(entry.resp), true
}

func (s *PRService) setCachedMergePreview(key string, resp *MergePreviewResponse) {
	s.mergePreviewMu.Lock()
	defer s.mergePreviewMu.Unlock()

	now := time.Now()
	s.pruneExpiredMergePreviewCache(now)
	if len(s.mergePreviewCache) >= mergePreviewCacheMaxEntries {
		s.evictOldestMergePreviewEntry()
	}
	s.mergePreviewCache[key] = mergePreviewCacheEntry{
		resp:     cloneMergePreviewResponse(resp),
		cachedAt: now,
		expires:  now.Add(mergePreviewCacheTTL),
	}
}

func (s *PRService) pruneExpiredMergePreviewCache(now time.Time) {
	if !s.mergePreviewPrune.IsZero() && now.Sub(s.mergePreviewPrune) < mergePreviewCleanupInterval {
		return
	}
	for key, entry := range s.mergePreviewCache {
		if now.After(entry.expires) {
			delete(s.mergePreviewCache, key)
		}
	}
	s.mergePreviewPrune = now
}

func (s *PRService) evictOldestMergePreviewEntry() {
	var (
		oldestKey string
		oldest    time.Time
		set       bool
	)
	for key, entry := range s.mergePreviewCache {
		if !set || entry.cachedAt.Before(oldest) {
			set = true
			oldest = entry.cachedAt
			oldestKey = key
		}
	}
	if set {
		delete(s.mergePreviewCache, oldestKey)
	}
}

func cloneMergePreviewResponse(in *MergePreviewResponse) *MergePreviewResponse {
	if in == nil {
		return nil
	}
	out := *in
	if len(in.Files) > 0 {
		out.Files = make([]FileMergeInfo, len(in.Files))
		copy(out.Files, in.Files)
	}
	return &out
}

// Comments
func (s *PRService) CreateComment(ctx context.Context, c *models.PRComment) error {
	return s.db.CreatePRComment(ctx, c)
}

func (s *PRService) ListComments(ctx context.Context, prID int64) ([]models.PRComment, error) {
	return s.db.ListPRComments(ctx, prID)
}

// Reviews
func (s *PRService) CreateReview(ctx context.Context, r *models.PRReview) error {
	return s.db.CreatePRReview(ctx, r)
}

func (s *PRService) ListReviews(ctx context.Context, prID int64) ([]models.PRReview, error) {
	return s.db.ListPRReviews(ctx, prID)
}

// --- helpers ---

func indexFiles(files []FileEntry) map[string]FileEntry {
	m := make(map[string]FileEntry, len(files))
	for _, f := range files {
		m[f.Path] = f
	}
	return m
}

// buildTreeFromFiles builds a hierarchical tree from flat file paths → blob hashes.
func buildTreeFromFiles(store *object.Store, files map[string]object.Hash) (object.Hash, error) {
	return buildTreeDir(store, files, "")
}

func buildTreeDir(store *object.Store, files map[string]object.Hash, prefix string) (object.Hash, error) {
	fileEntries := map[string]object.Hash{}
	subdirs := map[string]bool{}

	for path, hash := range files {
		var rel string
		if prefix == "" {
			rel = path
		} else {
			if len(path) <= len(prefix)+1 || path[:len(prefix)+1] != prefix+"/" {
				continue
			}
			rel = path[len(prefix)+1:]
		}
		slash := -1
		for i, c := range rel {
			if c == '/' {
				slash = i
				break
			}
		}
		if slash < 0 {
			fileEntries[rel] = hash
		} else {
			subdirs[rel[:slash]] = true
		}
	}

	var entries []object.TreeEntry
	// Add files
	for name, hash := range fileEntries {
		entries = append(entries, object.TreeEntry{
			Name:     name,
			IsDir:    false,
			BlobHash: hash,
		})
	}
	// Add subdirs
	for name := range subdirs {
		childPrefix := name
		if prefix != "" {
			childPrefix = prefix + "/" + name
		}
		subHash, err := buildTreeDir(store, files, childPrefix)
		if err != nil {
			return "", err
		}
		entries = append(entries, object.TreeEntry{
			Name:        name,
			IsDir:       true,
			SubtreeHash: subHash,
		})
	}

	// Keep deterministic tree ordering for stable hashes.
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })

	return store.WriteTree(&object.TreeObj{Entries: entries})
}

func enrichTreeWithEntities(store *object.Store, treeHash object.Hash, prefix string) (object.Hash, error) {
	tree, err := store.ReadTree(treeHash)
	if err != nil {
		return "", err
	}
	changed := false
	updated := make([]object.TreeEntry, len(tree.Entries))
	for i, e := range tree.Entries {
		entry := e
		fullPath := e.Name
		if prefix != "" {
			fullPath = prefix + "/" + e.Name
		}
		if e.IsDir {
			newSubtreeHash, err := enrichTreeWithEntities(store, e.SubtreeHash, fullPath)
			if err != nil {
				return "", err
			}
			if newSubtreeHash != e.SubtreeHash {
				entry.SubtreeHash = newSubtreeHash
				changed = true
			}
		} else if e.EntityListHash == "" {
			listHash, ok, err := entityutil.ExtractAndWriteEntityList(store, fullPath, e.BlobHash)
			if err != nil {
				return "", err
			}
			if ok {
				entry.EntityListHash = listHash
				changed = true
			}
		}
		updated[i] = entry
	}
	if !changed {
		return treeHash, nil
	}
	return store.WriteTree(&object.TreeObj{Entries: updated})
}
