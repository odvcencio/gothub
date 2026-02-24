package service

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/odvcencio/got/pkg/object"
	"github.com/odvcencio/gothub/internal/database"
	"github.com/odvcencio/gothub/internal/gotstore"
	"github.com/odvcencio/gothub/internal/models"
)

func TestPRMergeReturnsStructuredConflictAndPreservesTargetRef(t *testing.T) {
	ctx, prSvc, store, repo := setupPRMergeTestService(t)

	base := writeMainCommit(t, store, "package main\n\nfunc A() int { return 0 }\n", nil, "base", 1700000000)
	mainHead := writeMainCommit(t, store, "package main\n\nfunc A() int { return 1 }\n", []object.Hash{base}, "main", 1700000010)
	featureHead := writeMainCommit(t, store, "package main\n\nfunc A() int { return 2 }\n", []object.Hash{base}, "feature", 1700000020)

	if err := store.Refs.Set("heads/main", mainHead); err != nil {
		t.Fatal(err)
	}
	if err := store.Refs.Set("heads/feature", featureHead); err != nil {
		t.Fatal(err)
	}

	pr := &models.PullRequest{
		RepoID:       repo.ID,
		Number:       1,
		Title:        "conflict",
		SourceBranch: "feature",
		TargetBranch: "main",
	}

	_, err := prSvc.Merge(ctx, "alice", "repo", pr, "alice")
	if err == nil {
		t.Fatal("expected merge conflict error")
	}

	var mergeConflict *MergeConflictError
	if !errors.As(err, &mergeConflict) {
		t.Fatalf("expected MergeConflictError, got %T: %v", err, err)
	}
	if len(mergeConflict.Paths) != 1 || mergeConflict.Paths[0] != "main.go" {
		t.Fatalf("unexpected conflict paths: %+v", mergeConflict.Paths)
	}

	headAfter, err := store.Refs.Get("heads/main")
	if err != nil {
		t.Fatal(err)
	}
	if headAfter != mainHead {
		t.Fatalf("target branch should remain unchanged on conflict: got %s want %s", headAfter, mainHead)
	}
}

func TestUpdateTargetBranchRefReportsCASMismatch(t *testing.T) {
	_, _, store, _ := setupPRMergeTestService(t)

	current := writeMainCommit(t, store, "package main\n\nfunc A() int { return 1 }\n", nil, "current", 1700000100)
	next := writeMainCommit(t, store, "package main\n\nfunc A() int { return 2 }\n", []object.Hash{current}, "next", 1700000200)

	if err := store.Refs.Set("heads/main", current); err != nil {
		t.Fatal(err)
	}

	err := updateTargetBranchRef(store, "main", next, next)
	if err == nil {
		t.Fatal("expected CAS mismatch error")
	}

	var moved *TargetBranchMovedError
	if !errors.As(err, &moved) {
		t.Fatalf("expected TargetBranchMovedError, got %T: %v", err, err)
	}
	if moved.Branch != "main" {
		t.Fatalf("unexpected branch in moved error: %q", moved.Branch)
	}
	if moved.Expected != next {
		t.Fatalf("unexpected expected hash: got %s want %s", moved.Expected, next)
	}
	if moved.Actual != current {
		t.Fatalf("unexpected actual hash: got %s want %s", moved.Actual, current)
	}

	headAfter, err := store.Refs.Get("heads/main")
	if err != nil {
		t.Fatal(err)
	}
	if headAfter != current {
		t.Fatalf("main ref should remain unchanged after CAS mismatch: got %s want %s", headAfter, current)
	}
}

func TestMergePreviewCacheClonesResponses(t *testing.T) {
	svc := &PRService{mergePreviewCache: make(map[string]mergePreviewCacheEntry)}
	original := &MergePreviewResponse{
		HasConflicts: false,
		Files:        []FileMergeInfo{{Path: "main.go", Status: "clean"}},
	}

	svc.setCachedMergePreview("k", original)
	original.Files[0].Path = "mutated.go"

	got1, ok := svc.getCachedMergePreview("k")
	if !ok {
		t.Fatal("expected cached preview entry")
	}
	if got1.Files[0].Path != "main.go" {
		t.Fatalf("expected cached copy to preserve original path, got %q", got1.Files[0].Path)
	}

	got1.Files[0].Path = "changed-again.go"
	got2, ok := svc.getCachedMergePreview("k")
	if !ok {
		t.Fatal("expected cached preview entry on second read")
	}
	if got2.Files[0].Path != "main.go" {
		t.Fatalf("expected cached value to be isolated from caller mutation, got %q", got2.Files[0].Path)
	}
}

func TestMergePreviewCacheExpiresEntries(t *testing.T) {
	svc := &PRService{
		mergePreviewCache: map[string]mergePreviewCacheEntry{
			"k": {
				resp:    &MergePreviewResponse{Files: []FileMergeInfo{{Path: "main.go"}}},
				expires: time.Now().Add(-time.Second),
			},
		},
	}

	if _, ok := svc.getCachedMergePreview("k"); ok {
		t.Fatal("expected expired cache entry to be treated as miss")
	}
	if len(svc.mergePreviewCache) != 0 {
		t.Fatalf("expected expired cache entry to be pruned, got %d entries", len(svc.mergePreviewCache))
	}
}

func TestMergePreviewCacheRemainsBounded(t *testing.T) {
	svc := &PRService{mergePreviewCache: make(map[string]mergePreviewCacheEntry)}

	total := mergePreviewCacheMaxEntries + 64
	for i := 0; i < total; i++ {
		svc.setCachedMergePreview(fmt.Sprintf("k-%03d", i), &MergePreviewResponse{
			Files: []FileMergeInfo{{Path: fmt.Sprintf("file-%03d.go", i), Status: "clean"}},
		})
	}

	if got := len(svc.mergePreviewCache); got != mergePreviewCacheMaxEntries {
		t.Fatalf("expected bounded cache size %d, got %d", mergePreviewCacheMaxEntries, got)
	}
}

func TestMergePreviewCacheCompactsStaleHeapEntries(t *testing.T) {
	svc := &PRService{mergePreviewCache: make(map[string]mergePreviewCacheEntry)}

	updates := mergePreviewCacheMaxEntries * 5
	for i := 0; i < updates; i++ {
		svc.setCachedMergePreview("shared", &MergePreviewResponse{
			Files: []FileMergeInfo{{Path: fmt.Sprintf("file-%03d.go", i), Status: "clean"}},
		})
	}

	svc.mergePreviewMu.RLock()
	cacheSize := len(svc.mergePreviewCache)
	heapSize := len(svc.mergePreviewExpiry)
	svc.mergePreviewMu.RUnlock()

	if cacheSize != 1 {
		t.Fatalf("expected single cache entry after repeated key updates, got %d", cacheSize)
	}
	if heapSize > mergePreviewCacheMaxEntries {
		t.Fatalf("expected heap compaction to cap stale metadata, got heap size %d", heapSize)
	}
}

func TestRunPathWorkersExecutesConcurrently(t *testing.T) {
	prev := runtime.GOMAXPROCS(4)
	defer runtime.GOMAXPROCS(prev)

	paths := []string{"a.go", "b.go", "c.go", "d.go"}
	entered := make(chan struct{}, len(paths))
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseWorkers := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseWorkers)
	done := make(chan error, 1)

	go func() {
		done <- runPathWorkers(context.Background(), paths, func(_ int, _ string) error {
			entered <- struct{}{}
			<-release
			return nil
		})
	}()

	timeout := time.After(2 * time.Second)
	for i := 0; i < 2; i++ {
		select {
		case <-entered:
		case <-timeout:
			t.Fatal("expected at least two workers to run concurrently")
		}
	}

	releaseWorkers()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runPathWorkers returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for path workers to complete")
	}
}

func setupPRMergeTestService(t *testing.T) (context.Context, *PRService, *gotstore.RepoStore, *models.Repository) {
	t.Helper()

	ctx := context.Background()
	tmpDir := t.TempDir()

	db, err := database.OpenSQLite(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	user := &models.User{Username: "alice", Email: "alice@example.com", PasswordHash: "x"}
	if err := db.CreateUser(ctx, user); err != nil {
		t.Fatal(err)
	}

	repoSvc := NewRepoService(db, filepath.Join(tmpDir, "repos"))
	repo, err := repoSvc.Create(ctx, user.ID, "repo", "", false)
	if err != nil {
		t.Fatal(err)
	}
	store, err := repoSvc.OpenStore(ctx, "alice", "repo")
	if err != nil {
		t.Fatal(err)
	}

	prSvc := NewPRService(db, repoSvc, nil)
	return ctx, prSvc, store, repo
}

func writeMainCommit(t *testing.T, store *gotstore.RepoStore, content string, parents []object.Hash, message string, ts int64) object.Hash {
	t.Helper()

	blobHash, err := store.Objects.WriteBlob(&object.Blob{Data: []byte(content)})
	if err != nil {
		t.Fatal(err)
	}
	treeHash, err := store.Objects.WriteTree(&object.TreeObj{
		Entries: []object.TreeEntry{
			{
				Name:     "main.go",
				BlobHash: blobHash,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	commitHash, err := store.Objects.WriteCommit(&object.CommitObj{
		TreeHash:  treeHash,
		Parents:   parents,
		Author:    "alice",
		Timestamp: ts,
		Message:   message,
	})
	if err != nil {
		t.Fatal(err)
	}
	return commitHash
}
