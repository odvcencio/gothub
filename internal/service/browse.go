package service

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/odvcencio/got/pkg/object"
	"github.com/odvcencio/gothub/internal/gotstore"
)

// TreeEntry represents a file or directory in a tree listing.
type TreeEntry struct {
	Name           string `json:"name"`
	IsDir          bool   `json:"is_dir"`
	BlobHash       string `json:"blob_hash,omitempty"`
	EntityListHash string `json:"entity_list_hash,omitempty"`
	SubtreeHash    string `json:"subtree_hash,omitempty"`
}

// FileEntry represents a file with its full path (flattened tree).
type FileEntry struct {
	Path           string `json:"path"`
	BlobHash       string `json:"blob_hash"`
	EntityListHash string `json:"entity_list_hash,omitempty"`
}

// CommitInfo is a summary of a commit for API responses.
type CommitInfo struct {
	Hash      string   `json:"hash"`
	TreeHash  string   `json:"tree_hash"`
	Parents   []string `json:"parents"`
	Author    string   `json:"author"`
	Timestamp int64    `json:"timestamp"`
	Message   string   `json:"message"`
	Signature string   `json:"signature,omitempty"`
	Verified  bool     `json:"verified"`
	Signer    string   `json:"signer,omitempty"`
}

// BlobContent holds file content for API responses.
type BlobContent struct {
	Hash string `json:"hash"`
	Data []byte `json:"data"`
	Size int    `json:"size"`
}

type BrowseService struct {
	repoSvc *RepoService
}

func NewBrowseService(repoSvc *RepoService) *BrowseService {
	return &BrowseService{repoSvc: repoSvc}
}

// ResolveRef resolves a branch/tag name to a commit hash.
func (s *BrowseService) ResolveRef(ctx context.Context, owner, repo, ref string) (object.Hash, error) {
	store, err := s.repoSvc.OpenStore(ctx, owner, repo)
	if err != nil {
		return "", err
	}
	// Try branches first, then tags, then treat as raw hash
	if h, err := store.Refs.Get("heads/" + ref); err == nil {
		return h, nil
	}
	if h, err := store.Refs.Get("tags/" + ref); err == nil {
		return h, nil
	}
	// Assume it's a raw commit hash
	if store.Objects.Has(object.Hash(ref)) {
		return object.Hash(ref), nil
	}
	return "", fmt.Errorf("ref not found: %s", ref)
}

// ListBranches returns all branch names (without the heads/ prefix).
func (s *BrowseService) ListBranches(ctx context.Context, owner, repo string) ([]string, error) {
	store, err := s.repoSvc.OpenStore(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	refs, err := store.Refs.List("heads")
	if err != nil {
		return nil, err
	}
	branches := make([]string, 0, len(refs))
	for refName := range refs {
		branches = append(branches, strings.TrimPrefix(refName, "heads/"))
	}
	sort.Strings(branches)
	return branches, nil
}

// ListTree returns the entries of a directory at the given path within a commit.
func (s *BrowseService) ListTree(ctx context.Context, owner, repo, ref, dirPath string) ([]TreeEntry, error) {
	store, err := s.repoSvc.OpenStore(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	commitHash, err := s.ResolveRef(ctx, owner, repo, ref)
	if err != nil {
		return nil, err
	}
	commit, err := store.Objects.ReadCommit(commitHash)
	if err != nil {
		return nil, fmt.Errorf("read commit: %w", err)
	}

	treeHash := commit.TreeHash
	// Walk down to the target directory
	if dirPath != "" && dirPath != "." {
		var walkErr error
		treeHash, walkErr = walkToDir(store.Objects, treeHash, dirPath)
		if walkErr != nil {
			return nil, walkErr
		}
	}

	tree, err := store.Objects.ReadTree(treeHash)
	if err != nil {
		return nil, fmt.Errorf("read tree: %w", err)
	}

	entries := make([]TreeEntry, len(tree.Entries))
	for i, e := range tree.Entries {
		entries[i] = TreeEntry{
			Name:           e.Name,
			IsDir:          e.IsDir,
			BlobHash:       string(e.BlobHash),
			EntityListHash: string(e.EntityListHash),
			SubtreeHash:    string(e.SubtreeHash),
		}
	}
	return entries, nil
}

// GetBlob returns the content of a file at the given path within a commit.
func (s *BrowseService) GetBlob(ctx context.Context, owner, repo, ref, filePath string) (*BlobContent, error) {
	store, err := s.repoSvc.OpenStore(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	commitHash, err := s.ResolveRef(ctx, owner, repo, ref)
	if err != nil {
		return nil, err
	}
	commit, err := store.Objects.ReadCommit(commitHash)
	if err != nil {
		return nil, fmt.Errorf("read commit: %w", err)
	}

	blobHash, err := findBlob(store.Objects, commit.TreeHash, filePath)
	if err != nil {
		return nil, err
	}

	blob, err := store.Objects.ReadBlob(blobHash)
	if err != nil {
		return nil, fmt.Errorf("read blob: %w", err)
	}
	return &BlobContent{
		Hash: string(blobHash),
		Data: blob.Data,
		Size: len(blob.Data),
	}, nil
}

// GetCommit returns info about a single commit.
func (s *BrowseService) GetCommit(ctx context.Context, owner, repo, hash string) (*CommitInfo, error) {
	store, err := s.repoSvc.OpenStore(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	commit, err := store.Objects.ReadCommit(object.Hash(hash))
	if err != nil {
		return nil, fmt.Errorf("read commit: %w", err)
	}
	info := commitToInfo(hash, commit)
	verified, signer, _ := verifyCommitSignature(ctx, s.repoSvc.db, commit)
	info.Verified = verified
	info.Signer = signer
	return info, nil
}

// ListCommits returns the commit log starting from a ref, walking parents.
func (s *BrowseService) ListCommits(ctx context.Context, owner, repo, ref string, limit int) ([]CommitInfo, error) {
	store, err := s.repoSvc.OpenStore(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	head, err := s.ResolveRef(ctx, owner, repo, ref)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 30
	}

	var commits []CommitInfo
	queue := []object.Hash{head}
	seen := map[object.Hash]bool{}

	for len(queue) > 0 && len(commits) < limit {
		h := queue[0]
		queue = queue[1:]
		if seen[h] {
			continue
		}
		seen[h] = true

		commit, err := store.Objects.ReadCommit(h)
		if err != nil {
			continue
		}
		info := commitToInfo(string(h), commit)
		verified, signer, _ := verifyCommitSignature(ctx, s.repoSvc.db, commit)
		info.Verified = verified
		info.Signer = signer
		commits = append(commits, *info)
		for _, p := range commit.Parents {
			if !seen[p] {
				queue = append(queue, p)
			}
		}
	}
	return commits, nil
}

// FlattenTree returns all files recursively under a commit's tree.
func (s *BrowseService) FlattenTree(ctx context.Context, owner, repo, ref string) ([]FileEntry, error) {
	store, err := s.repoSvc.OpenStore(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	commitHash, err := s.ResolveRef(ctx, owner, repo, ref)
	if err != nil {
		return nil, err
	}
	commit, err := store.Objects.ReadCommit(commitHash)
	if err != nil {
		return nil, fmt.Errorf("read commit: %w", err)
	}
	return flattenTree(store.Objects, commit.TreeHash, "")
}

// --- helpers ---

func walkToDir(store *object.Store, treeHash object.Hash, dirPath string) (object.Hash, error) {
	parts := strings.Split(strings.Trim(dirPath, "/"), "/")
	current := treeHash
	for _, part := range parts {
		tree, err := store.ReadTree(current)
		if err != nil {
			return "", fmt.Errorf("read tree: %w", err)
		}
		found := false
		for _, e := range tree.Entries {
			if e.Name == part && e.IsDir {
				current = e.SubtreeHash
				found = true
				break
			}
		}
		if !found {
			return "", fmt.Errorf("directory not found: %s", dirPath)
		}
	}
	return current, nil
}

func findBlob(store *object.Store, treeHash object.Hash, filePath string) (object.Hash, error) {
	dir := path.Dir(filePath)
	name := path.Base(filePath)

	targetTree := treeHash
	if dir != "." && dir != "" {
		var err error
		targetTree, err = walkToDir(store, treeHash, dir)
		if err != nil {
			return "", err
		}
	}

	tree, err := store.ReadTree(targetTree)
	if err != nil {
		return "", fmt.Errorf("read tree: %w", err)
	}
	for _, e := range tree.Entries {
		if e.Name == name && !e.IsDir {
			return e.BlobHash, nil
		}
	}
	return "", fmt.Errorf("file not found: %s", filePath)
}

func flattenTree(store *object.Store, treeHash object.Hash, prefix string) ([]FileEntry, error) {
	tree, err := store.ReadTree(treeHash)
	if err != nil {
		return nil, fmt.Errorf("read tree: %w", err)
	}
	var result []FileEntry
	for _, e := range tree.Entries {
		fullPath := e.Name
		if prefix != "" {
			fullPath = prefix + "/" + e.Name
		}
		if e.IsDir {
			sub, err := flattenTree(store, e.SubtreeHash, fullPath)
			if err != nil {
				return nil, err
			}
			result = append(result, sub...)
		} else {
			result = append(result, FileEntry{
				Path:           fullPath,
				BlobHash:       string(e.BlobHash),
				EntityListHash: string(e.EntityListHash),
			})
		}
	}
	return result, nil
}

func commitToInfo(hash string, c *object.CommitObj) *CommitInfo {
	parents := make([]string, len(c.Parents))
	for i, p := range c.Parents {
		parents[i] = string(p)
	}
	return &CommitInfo{
		Hash:      hash,
		TreeHash:  string(c.TreeHash),
		Parents:   parents,
		Author:    c.Author,
		Timestamp: c.Timestamp,
		Message:   c.Message,
		Signature: c.Signature,
	}
}

// FindMergeBase finds the first common ancestor of two commits using BFS.
func FindMergeBase(store *object.Store, a, b object.Hash) (object.Hash, error) {
	visitedA := map[object.Hash]bool{a: true}
	visitedB := map[object.Hash]bool{b: true}
	queueA := []object.Hash{a}
	queueB := []object.Hash{b}

	for len(queueA) > 0 || len(queueB) > 0 {
		if len(queueA) > 0 {
			cur := queueA[0]
			queueA = queueA[1:]
			if visitedB[cur] {
				return cur, nil
			}
			commit, err := store.ReadCommit(cur)
			if err != nil {
				continue
			}
			for _, p := range commit.Parents {
				if !visitedA[p] {
					visitedA[p] = true
					queueA = append(queueA, p)
				}
			}
		}
		if len(queueB) > 0 {
			cur := queueB[0]
			queueB = queueB[1:]
			if visitedA[cur] {
				return cur, nil
			}
			commit, err := store.ReadCommit(cur)
			if err != nil {
				continue
			}
			for _, p := range commit.Parents {
				if !visitedB[p] {
					visitedB[p] = true
					queueB = append(queueB, p)
				}
			}
		}
	}
	return "", fmt.Errorf("no common ancestor")
}

// unused import guard
var _ = (*gotstore.RepoStore)(nil)
