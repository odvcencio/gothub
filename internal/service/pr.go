package service

import (
	"container/heap"
	"context"
	"errors"
	"fmt"
	"runtime"
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
	"golang.org/x/sync/errgroup"
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

// PRMergeStateSyncStatus reports the durable outcome when PR state persistence fails.
type PRMergeStateSyncStatus string

const (
	PRMergeStateSyncRolledBack PRMergeStateSyncStatus = "rolled_back"
	PRMergeStateSyncDesynced   PRMergeStateSyncStatus = "desynced"
)

// PRMergeStateSyncError indicates merge ref updates and PR-state persistence diverged.
type PRMergeStateSyncError struct {
	Status       PRMergeStateSyncStatus
	Branch       string
	MergeCommit  object.Hash
	TargetBefore object.Hash
	Cause        error
	RollbackErr  error
}

func (e *PRMergeStateSyncError) Error() string {
	if e == nil {
		return "merge state sync failed"
	}
	if e.RollbackErr != nil {
		return fmt.Sprintf(
			"merge state sync failed (status=%s): updated %s to %s but failed to persist PR state and rollback to %s failed: %v (rollback error: %v)",
			e.Status, e.Branch, e.MergeCommit, e.TargetBefore, e.Cause, e.RollbackErr,
		)
	}
	return fmt.Sprintf(
		"merge state sync failed (status=%s): updated %s to %s but failed to persist PR state; rolled back to %s: %v",
		e.Status, e.Branch, e.MergeCommit, e.TargetBefore, e.Cause,
	)
}

func (e *PRMergeStateSyncError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

const (
	mergePreviewCacheTTL        = 30 * time.Second
	mergePreviewCacheMaxEntries = 256
	mergePathWorkersMax         = 16
)

type mergePreviewCacheEntry struct {
	key       string
	resp      *MergePreviewResponse
	expires   time.Time
	heapIndex int
}

type mergePreviewExpiryHeap []*mergePreviewCacheEntry

func (h mergePreviewExpiryHeap) Len() int { return len(h) }

func (h mergePreviewExpiryHeap) Less(i, j int) bool {
	if h[i].expires.Equal(h[j].expires) {
		return h[i].key < h[j].key
	}
	return h[i].expires.Before(h[j].expires)
}

func (h mergePreviewExpiryHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].heapIndex = i
	h[j].heapIndex = j
}

func (h *mergePreviewExpiryHeap) Push(x any) {
	entry := x.(*mergePreviewCacheEntry)
	entry.heapIndex = len(*h)
	*h = append(*h, entry)
}

func (h *mergePreviewExpiryHeap) Pop() any {
	old := *h
	n := len(old)
	entry := old[n-1]
	old[n-1] = nil
	entry.heapIndex = -1
	*h = old[:n-1]
	return entry
}

type PRService struct {
	db           database.DB
	repoSvc      *RepoService
	browseSvc    *BrowseService
	codeIntelSvc *CodeIntelService
	lineageSvc   *EntityLineageService

	mergePreviewMu     sync.RWMutex
	mergePreviewCache  map[string]*mergePreviewCacheEntry
	mergePreviewExpiry mergePreviewExpiryHeap
}

func NewPRService(db database.DB, repoSvc *RepoService, browseSvc *BrowseService) *PRService {
	return &PRService{
		db:                db,
		repoSvc:           repoSvc,
		browseSvc:         browseSvc,
		mergePreviewCache: make(map[string]*mergePreviewCacheEntry),
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
		State:        models.PullRequestStateOpen,
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

func (s *PRService) List(ctx context.Context, repoID int64, state string, page, perPage int) ([]models.PullRequest, error) {
	limit, offset := normalizePage(page, perPage, 30, 200)
	return s.db.ListPullRequestsPage(ctx, repoID, state, limit, offset)
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
	baseHash, err := s.findMergeBaseCached(ctx, pr.RepoID, store.Objects, tgtHash, srcHash)
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

	paths := collectAllPaths(baseMap, srcMap, tgtMap)
	results := make([]mergePreviewPathResult, len(paths))
	if err := runPathWorkers(ctx, paths, func(i int, path string) error {
		result, err := computeMergePreviewPath(store.Objects, path, baseMap[path], srcMap[path], tgtMap[path])
		if err != nil {
			return err
		}
		results[i] = result
		return nil
	}); err != nil {
		return nil, err
	}

	resp := &MergePreviewResponse{
		Files: make([]FileMergeInfo, 0, len(paths)),
	}
	var totalStats merge.MergeStats

	for _, result := range results {
		if !result.include {
			continue
		}
		resp.Files = append(resp.Files, result.info)
		accumulateMergeStats(&totalStats, result.stats)
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

	baseHash, err := s.findMergeBaseCached(ctx, pr.RepoID, store.Objects, tgtHash, srcHash)
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
	paths := collectAllPaths(baseMap, srcMap, tgtMap)

	// Build merged tree entries
	mergedEntries := make(map[string]object.Hash, len(paths))
	conflictedPaths := make([]string, 0, 4)
	decisions := make([]mergePathDecision, len(paths))
	if err := runPathWorkers(ctx, paths, func(i int, path string) error {
		decision, err := computeMergePathDecision(store.Objects, path, baseMap[path], srcMap[path], tgtMap[path])
		if err != nil {
			return err
		}
		decisions[i] = decision
		return nil
	}); err != nil {
		return "", err
	}

	for _, decision := range decisions {
		if !decision.include {
			continue
		}
		if decision.conflict {
			conflictedPaths = append(conflictedPaths, decision.path)
			continue
		}
		if decision.directBlobHash != "" {
			mergedEntries[decision.path] = decision.directBlobHash
			continue
		}
		if !decision.writeMergedBlob {
			continue
		}
		blobHash, err := store.Objects.WriteBlob(&object.Blob{Data: decision.mergedData})
		if err != nil {
			return "", fmt.Errorf("write merged blob: %w", err)
		}
		mergedEntries[decision.path] = blobHash
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
	mergedPR := *pr
	now := time.Now()
	mergedPR.State = models.PullRequestStateMerged
	mergedPR.MergeCommit = string(mergeCommitHash)
	mergedPR.MergeMethod = "structural"
	mergedPR.MergedAt = &now
	mergedPR.SourceCommit = string(srcHash)
	mergedPR.TargetCommit = string(tgtHash)
	if err := s.db.UpdatePullRequest(ctx, &mergedPR); err != nil {
		syncErr := &PRMergeStateSyncError{
			Status:       PRMergeStateSyncRolledBack,
			Branch:       pr.TargetBranch,
			MergeCommit:  mergeCommitHash,
			TargetBefore: tgtHash,
			Cause:        err,
		}
		if rollbackErr := updateTargetBranchRef(store, pr.TargetBranch, mergeCommitHash, tgtHash); rollbackErr != nil {
			syncErr.Status = PRMergeStateSyncDesynced
			syncErr.RollbackErr = rollbackErr
		}
		return "", syncErr
	}
	*pr = mergedPR

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

func (s *PRService) findMergeBaseCached(ctx context.Context, repoID int64, store *object.Store, left, right object.Hash) (object.Hash, error) {
	if store == nil {
		return "", fmt.Errorf("object store is nil")
	}
	if repoID > 0 && s.db != nil {
		if cached, ok, err := s.db.GetMergeBaseCache(ctx, repoID, string(left), string(right)); err == nil && ok {
			cachedHash := object.Hash(strings.TrimSpace(cached))
			if cachedHash != "" && store.Has(cachedHash) {
				return cachedHash, nil
			}
		}
	}

	var opts MergeBaseOptions
	if repoID > 0 && s.db != nil {
		type generationLookupResult struct {
			generation uint64
			ok         bool
			err        error
		}
		lookupCache := make(map[object.Hash]generationLookupResult)
		opts.GenerationLookup = func(hash object.Hash) (uint64, bool, error) {
			if cached, ok := lookupCache[hash]; ok {
				return cached.generation, cached.ok, cached.err
			}
			meta, ok, err := s.db.GetCommitMetadata(ctx, repoID, string(hash))
			result := generationLookupResult{ok: ok, err: err}
			if err == nil && ok && meta.Generation > 0 {
				result.generation = uint64(meta.Generation)
			} else {
				result.ok = false
			}
			lookupCache[hash] = result
			return result.generation, result.ok, result.err
		}
	}

	baseHash, err := FindMergeBaseWithOptions(store, left, right, opts)
	if err != nil {
		return "", err
	}
	if repoID > 0 && s.db != nil {
		_ = s.db.SetMergeBaseCache(ctx, repoID, string(left), string(right), string(baseHash))
	}
	return baseHash, nil
}

type mergePreviewPathResult struct {
	include bool
	info    FileMergeInfo
	stats   merge.MergeStats
}

func computeMergePreviewPath(store *object.Store, path string, baseEntry, srcEntry, tgtEntry FileEntry) (mergePreviewPathResult, error) {
	var (
		baseData []byte
		srcData  []byte
		tgtData  []byte
		err      error
	)
	if baseEntry.BlobHash != "" {
		baseData, err = readBlobData(store, object.Hash(baseEntry.BlobHash))
		if err != nil {
			return mergePreviewPathResult{}, fmt.Errorf("read base blob %s: %w", path, err)
		}
	}
	if srcEntry.BlobHash != "" {
		srcData, err = readBlobData(store, object.Hash(srcEntry.BlobHash))
		if err != nil {
			return mergePreviewPathResult{}, fmt.Errorf("read source blob %s: %w", path, err)
		}
	}
	if tgtEntry.BlobHash != "" {
		tgtData, err = readBlobData(store, object.Hash(tgtEntry.BlobHash))
		if err != nil {
			return mergePreviewPathResult{}, fmt.Errorf("read target blob %s: %w", path, err)
		}
	}

	// Skip files unchanged between all three.
	if baseEntry.BlobHash == srcEntry.BlobHash && baseEntry.BlobHash == tgtEntry.BlobHash {
		return mergePreviewPathResult{}, nil
	}

	info := FileMergeInfo{Path: path}
	// Determine file status.
	if srcEntry.BlobHash == "" && tgtEntry.BlobHash == "" {
		return mergePreviewPathResult{}, nil // deleted in both
	}
	if baseEntry.BlobHash == "" && srcEntry.BlobHash != "" {
		info.Status = "added"
		return mergePreviewPathResult{include: true, info: info}, nil
	}
	if srcEntry.BlobHash == "" {
		info.Status = "deleted"
		return mergePreviewPathResult{include: true, info: info}, nil
	}

	// Three-way merge.
	result, err := merge.MergeFiles(path, baseData, tgtData, srcData)
	if err != nil {
		info.Status = "conflict"
		info.ConflictCount = 1
		return mergePreviewPathResult{
			include: true,
			info:    info,
			stats:   merge.MergeStats{Conflicts: 1},
		}, nil
	}
	if result.HasConflicts {
		info.Status = "conflict"
		info.ConflictCount = result.ConflictCount
	} else {
		info.Status = "clean"
	}

	return mergePreviewPathResult{
		include: true,
		info:    info,
		stats:   result.Stats,
	}, nil
}

type mergePathDecision struct {
	include         bool
	path            string
	conflict        bool
	directBlobHash  object.Hash
	mergedData      []byte
	writeMergedBlob bool
}

func computeMergePathDecision(store *object.Store, path string, baseEntry, srcEntry, tgtEntry FileEntry) (mergePathDecision, error) {
	if srcEntry.BlobHash == "" {
		// Deleted in source.
		return mergePathDecision{}, nil
	}
	if tgtEntry.BlobHash == "" && baseEntry.BlobHash == "" {
		// New in source.
		return mergePathDecision{
			include:        true,
			path:           path,
			directBlobHash: object.Hash(srcEntry.BlobHash),
		}, nil
	}
	if baseEntry.BlobHash == srcEntry.BlobHash {
		// Source unchanged, use target.
		if tgtEntry.BlobHash == "" {
			return mergePathDecision{}, nil
		}
		return mergePathDecision{
			include:        true,
			path:           path,
			directBlobHash: object.Hash(tgtEntry.BlobHash),
		}, nil
	}
	if baseEntry.BlobHash == tgtEntry.BlobHash {
		// Target unchanged, use source.
		return mergePathDecision{
			include:        true,
			path:           path,
			directBlobHash: object.Hash(srcEntry.BlobHash),
		}, nil
	}

	// Both changed — three-way merge.
	var (
		baseData []byte
		srcData  []byte
		tgtData  []byte
		err      error
	)
	if baseEntry.BlobHash != "" {
		baseData, err = readBlobData(store, object.Hash(baseEntry.BlobHash))
		if err != nil {
			return mergePathDecision{}, fmt.Errorf("read base blob %s: %w", path, err)
		}
	}
	srcData, err = readBlobData(store, object.Hash(srcEntry.BlobHash))
	if err != nil {
		return mergePathDecision{}, fmt.Errorf("read source blob %s: %w", path, err)
	}
	tgtData, err = readBlobData(store, object.Hash(tgtEntry.BlobHash))
	if err != nil {
		return mergePathDecision{}, fmt.Errorf("read target blob %s: %w", path, err)
	}

	result, err := merge.MergeFiles(path, baseData, tgtData, srcData)
	if err != nil || result.HasConflicts {
		return mergePathDecision{
			include:  true,
			path:     path,
			conflict: true,
		}, nil
	}

	return mergePathDecision{
		include:         true,
		path:            path,
		mergedData:      result.Merged,
		writeMergedBlob: true,
	}, nil
}

func runPathWorkers(ctx context.Context, paths []string, fn func(i int, path string) error) error {
	if len(paths) == 0 {
		return nil
	}
	workerCount := mergePathWorkerCount(len(paths))
	if workerCount <= 1 {
		for i, path := range paths {
			if err := fn(i, path); err != nil {
				return err
			}
		}
		return nil
	}

	group, _ := errgroup.WithContext(ctx)
	group.SetLimit(workerCount)
	for i, path := range paths {
		i, path := i, path
		group.Go(func() error {
			return fn(i, path)
		})
	}
	return group.Wait()
}

func mergePathWorkerCount(pathCount int) int {
	if pathCount <= 1 {
		return 1
	}
	workers := runtime.GOMAXPROCS(0)
	if workers < 1 {
		workers = 1
	}
	if workers > mergePathWorkersMax {
		workers = mergePathWorkersMax
	}
	if workers > pathCount {
		workers = pathCount
	}
	return workers
}

func (s *PRService) getCachedMergePreview(key string) (*MergePreviewResponse, bool) {
	now := time.Now()
	s.mergePreviewMu.RLock()
	entry, ok := s.mergePreviewCache[key]
	if ok && now.Before(entry.expires) {
		cloned := cloneMergePreviewResponse(entry.resp)
		s.mergePreviewMu.RUnlock()
		return cloned, true
	}
	s.mergePreviewMu.RUnlock()

	if ok {
		s.deleteExpiredMergePreviewEntry(key, now)
	}
	return nil, false
}

func (s *PRService) setCachedMergePreview(key string, resp *MergePreviewResponse) {
	now := time.Now()
	cloned := cloneMergePreviewResponse(resp)
	expires := now.Add(mergePreviewCacheTTL)

	s.mergePreviewMu.Lock()
	defer s.mergePreviewMu.Unlock()

	if s.mergePreviewCache == nil {
		s.mergePreviewCache = make(map[string]*mergePreviewCacheEntry)
	}
	if len(s.mergePreviewCache) != len(s.mergePreviewExpiry) {
		s.rebuildMergePreviewExpiryHeapLocked()
	}
	s.pruneExpiredMergePreviewCacheLocked(now)

	if entry, exists := s.mergePreviewCache[key]; exists {
		entry.resp = cloned
		entry.expires = expires
		heap.Fix(&s.mergePreviewExpiry, entry.heapIndex)
		return
	}

	for len(s.mergePreviewCache) >= mergePreviewCacheMaxEntries {
		s.evictOldestMergePreviewEntryLocked()
	}

	entry := &mergePreviewCacheEntry{
		key:       key,
		resp:      cloned,
		expires:   expires,
		heapIndex: -1,
	}
	s.mergePreviewCache[key] = entry
	heap.Push(&s.mergePreviewExpiry, entry)
}

func (s *PRService) deleteExpiredMergePreviewEntry(key string, now time.Time) {
	s.mergePreviewMu.Lock()
	defer s.mergePreviewMu.Unlock()

	entry, ok := s.mergePreviewCache[key]
	if !ok || now.Before(entry.expires) {
		return
	}
	s.removeMergePreviewEntryLocked(entry)
}

func (s *PRService) removeMergePreviewEntryLocked(entry *mergePreviewCacheEntry) {
	if entry == nil {
		return
	}

	if current, ok := s.mergePreviewCache[entry.key]; ok && current == entry {
		delete(s.mergePreviewCache, entry.key)
	}

	if idx := entry.heapIndex; idx >= 0 && idx < len(s.mergePreviewExpiry) && s.mergePreviewExpiry[idx] == entry {
		heap.Remove(&s.mergePreviewExpiry, idx)
		return
	}
	for i := range s.mergePreviewExpiry {
		if s.mergePreviewExpiry[i] == entry {
			heap.Remove(&s.mergePreviewExpiry, i)
			return
		}
	}
}

func (s *PRService) rebuildMergePreviewExpiryHeapLocked() {
	rebuilt := make(mergePreviewExpiryHeap, 0, len(s.mergePreviewCache))
	for key, entry := range s.mergePreviewCache {
		if entry == nil {
			delete(s.mergePreviewCache, key)
			continue
		}
		entry.key = key
		entry.heapIndex = len(rebuilt)
		rebuilt = append(rebuilt, entry)
	}
	heap.Init(&rebuilt)
	s.mergePreviewExpiry = rebuilt
}

func (s *PRService) pruneExpiredMergePreviewCacheLocked(now time.Time) {
	if len(s.mergePreviewExpiry) == 0 {
		return
	}

	for len(s.mergePreviewExpiry) > 0 {
		top := s.mergePreviewExpiry[0]
		if now.Before(top.expires) {
			return
		}
		s.removeMergePreviewEntryLocked(top)
	}
}

func (s *PRService) evictOldestMergePreviewEntryLocked() {
	for len(s.mergePreviewExpiry) > 0 {
		entry := heap.Pop(&s.mergePreviewExpiry).(*mergePreviewCacheEntry)
		current, ok := s.mergePreviewCache[entry.key]
		if !ok || current != entry {
			continue
		}
		delete(s.mergePreviewCache, entry.key)
		return
	}

	for key := range s.mergePreviewCache {
		delete(s.mergePreviewCache, key)
		return
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

func (s *PRService) ListComments(ctx context.Context, prID int64, page, perPage int) ([]models.PRComment, error) {
	limit, offset := normalizePage(page, perPage, 50, 200)
	return s.db.ListPRCommentsPage(ctx, prID, limit, offset)
}

// Reviews
func (s *PRService) CreateReview(ctx context.Context, r *models.PRReview) error {
	return s.db.CreatePRReview(ctx, r)
}

func (s *PRService) ListReviews(ctx context.Context, prID int64, page, perPage int) ([]models.PRReview, error) {
	limit, offset := normalizePage(page, perPage, 50, 200)
	return s.db.ListPRReviewsPage(ctx, prID, limit, offset)
}

func normalizePage(page, perPage, defaultPerPage, maxPerPage int) (limit, offset int) {
	if page <= 0 {
		page = 1
	}
	if perPage <= 0 {
		perPage = defaultPerPage
	}
	if perPage > maxPerPage {
		perPage = maxPerPage
	}
	return perPage, (page - 1) * perPage
}

// --- helpers ---

func collectAllPaths(baseMap, srcMap, tgtMap map[string]FileEntry) []string {
	allPaths := make(map[string]struct{}, len(baseMap)+len(srcMap)+len(tgtMap))
	for path := range baseMap {
		allPaths[path] = struct{}{}
	}
	for path := range srcMap {
		allPaths[path] = struct{}{}
	}
	for path := range tgtMap {
		allPaths[path] = struct{}{}
	}

	paths := make([]string, 0, len(allPaths))
	for path := range allPaths {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func accumulateMergeStats(dst *merge.MergeStats, src merge.MergeStats) {
	dst.TotalEntities += src.TotalEntities
	dst.Unchanged += src.Unchanged
	dst.OursModified += src.OursModified
	dst.TheirsModified += src.TheirsModified
	dst.BothModified += src.BothModified
	dst.Added += src.Added
	dst.Deleted += src.Deleted
	dst.Conflicts += src.Conflicts
}

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
