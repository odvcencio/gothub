package service

import (
	"context"
	"fmt"
	"time"

	"github.com/odvcencio/got/pkg/diff"
	"github.com/odvcencio/got/pkg/merge"
	"github.com/odvcencio/got/pkg/object"
	"github.com/odvcencio/gothub/internal/database"
	"github.com/odvcencio/gothub/internal/models"
)

// MergePreviewResponse holds the result of a structural merge preview.
type MergePreviewResponse struct {
	HasConflicts  bool              `json:"has_conflicts"`
	ConflictCount int               `json:"conflict_count"`
	Stats         MergeStatsInfo    `json:"stats"`
	Files         []FileMergeInfo   `json:"files"`
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

type PRService struct {
	db        database.DB
	repoSvc   *RepoService
	browseSvc *BrowseService
}

func NewPRService(db database.DB, repoSvc *RepoService, browseSvc *BrowseService) *PRService {
	return &PRService{db: db, repoSvc: repoSvc, browseSvc: browseSvc}
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

	// Find merge base
	baseHash, err := FindMergeBase(store.Objects, tgtHash, srcHash)
	if err != nil {
		return nil, fmt.Errorf("find merge base: %w", err)
	}

	baseCommit, _ := store.Objects.ReadCommit(baseHash)
	srcCommit, _ := store.Objects.ReadCommit(srcHash)
	tgtCommit, _ := store.Objects.ReadCommit(tgtHash)

	baseFiles, _ := flattenTree(store.Objects, baseCommit.TreeHash, "")
	srcFiles, _ := flattenTree(store.Objects, srcCommit.TreeHash, "")
	tgtFiles, _ := flattenTree(store.Objects, tgtCommit.TreeHash, "")

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
			baseData, _ = readBlobData(store.Objects, object.Hash(baseEntry.BlobHash))
		}
		if srcEntry.BlobHash != "" {
			srcData, _ = readBlobData(store.Objects, object.Hash(srcEntry.BlobHash))
		}
		if tgtEntry.BlobHash != "" {
			tgtData, _ = readBlobData(store.Objects, object.Hash(tgtEntry.BlobHash))
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

	baseCommit, _ := store.Objects.ReadCommit(baseHash)
	srcCommit, _ := store.Objects.ReadCommit(srcHash)
	tgtCommit, _ := store.Objects.ReadCommit(tgtHash)

	baseFiles, _ := flattenTree(store.Objects, baseCommit.TreeHash, "")
	srcFiles, _ := flattenTree(store.Objects, srcCommit.TreeHash, "")
	tgtFiles, _ := flattenTree(store.Objects, tgtCommit.TreeHash, "")

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
			baseData, _ = readBlobData(store.Objects, object.Hash(baseEntry.BlobHash))
		}
		srcData, _ = readBlobData(store.Objects, object.Hash(srcEntry.BlobHash))
		tgtData, _ = readBlobData(store.Objects, object.Hash(tgtEntry.BlobHash))

		result, err := merge.MergeFiles(path, baseData, tgtData, srcData)
		if err != nil || result.HasConflicts {
			return "", fmt.Errorf("merge conflict in %s", path)
		}

		blobHash, err := store.Objects.WriteBlob(&object.Blob{Data: result.Merged})
		if err != nil {
			return "", fmt.Errorf("write merged blob: %w", err)
		}
		mergedEntries[path] = blobHash
	}

	// Build tree from merged entries
	mergeTreeHash, err := buildTreeFromFiles(store.Objects, mergedEntries)
	if err != nil {
		return "", fmt.Errorf("build merge tree: %w", err)
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

	// Update target branch ref
	if err := store.Refs.Set("heads/"+pr.TargetBranch, mergeCommitHash); err != nil {
		return "", fmt.Errorf("update ref: %w", err)
	}

	// Update PR state
	now := time.Now()
	pr.State = "merged"
	pr.MergeCommit = string(mergeCommitHash)
	pr.MergeMethod = "structural"
	pr.MergedAt = &now
	pr.SourceCommit = string(srcHash)
	pr.TargetCommit = string(tgtHash)
	s.db.UpdatePullRequest(ctx, pr)

	return mergeCommitHash, nil
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
	type dirChild struct {
		name string
	}
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

	// Sort entries by name
	for i := 0; i < len(entries); i++ {
		for j := i + 1; j < len(entries); j++ {
			if entries[j].Name < entries[i].Name {
				entries[i], entries[j] = entries[j], entries[i]
			}
		}
	}

	return store.WriteTree(&object.TreeObj{Entries: entries})
}
