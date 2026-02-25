package service

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
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

func TestPRMergeStateSyncErrorRollsBackTargetRefOnDBFailure(t *testing.T) {
	ctx, prSvc, store, repo := setupPRMergeTestService(t)

	base := writeMainCommit(t, store, "package main\n\nfunc A() int { return 0 }\n", nil, "base", 1700002000)
	mainHead := writeMainCommit(t, store, "package main\n\nfunc A() int { return 1 }\n", []object.Hash{base}, "main", 1700002010)
	featureHead := base

	if err := store.Refs.Set("heads/main", mainHead); err != nil {
		t.Fatal(err)
	}
	if err := store.Refs.Set("heads/feature", featureHead); err != nil {
		t.Fatal(err)
	}

	pr := &models.PullRequest{
		RepoID:       repo.ID,
		Title:        "merge db failure",
		Body:         "",
		State:        "open",
		AuthorID:     1,
		SourceBranch: "feature",
		TargetBranch: "main",
	}
	if err := prSvc.db.CreatePullRequest(ctx, pr); err != nil {
		t.Fatal(err)
	}

	dbErr := errors.New("update failed")
	prSvc.db = &updatePullRequestHookDB{
		DB:        prSvc.db,
		updateErr: dbErr,
	}

	mergeHash, err := prSvc.Merge(ctx, "alice", "repo", pr, "alice")
	if err == nil {
		t.Fatal("expected merge to fail when UpdatePullRequest fails")
	}
	if mergeHash != "" {
		t.Fatalf("expected empty merge hash on failure, got %s", mergeHash)
	}
	if !errors.Is(err, dbErr) {
		t.Fatalf("expected wrapped db error, got %v", err)
	}
	if !strings.Contains(err.Error(), "status=rolled_back") {
		t.Fatalf("expected rolled_back status in error, got %q", err)
	}

	var syncErr *PRMergeStateSyncError
	if !errors.As(err, &syncErr) {
		t.Fatalf("expected PRMergeStateSyncError, got %T: %v", err, err)
	}
	if syncErr.Status != PRMergeStateSyncRolledBack {
		t.Fatalf("expected status %q, got %q", PRMergeStateSyncRolledBack, syncErr.Status)
	}
	if syncErr.RollbackErr != nil {
		t.Fatalf("expected rollback to succeed, got rollback error: %v", syncErr.RollbackErr)
	}

	headAfter, err := store.Refs.Get("heads/main")
	if err != nil {
		t.Fatal(err)
	}
	if headAfter != mainHead {
		t.Fatalf("target branch should roll back to original head: got %s want %s", headAfter, mainHead)
	}

	persisted, err := prSvc.db.GetPullRequest(ctx, repo.ID, pr.Number)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.State != "open" {
		t.Fatalf("db PR state should remain open, got %q", persisted.State)
	}
	if persisted.MergeCommit != "" {
		t.Fatalf("db PR merge_commit should remain empty, got %q", persisted.MergeCommit)
	}

	if pr.State != "open" {
		t.Fatalf("input PR state should remain open on failed state sync, got %q", pr.State)
	}
	if pr.MergeCommit != "" {
		t.Fatalf("input PR merge_commit should remain empty on failed state sync, got %q", pr.MergeCommit)
	}
}

func TestPRMergeStateSyncErrorSurfacesDesyncWhenRollbackFails(t *testing.T) {
	ctx, prSvc, store, repo := setupPRMergeTestService(t)

	base := writeMainCommit(t, store, "package main\n\nfunc A() int { return 0 }\n", nil, "base", 1700003000)
	mainHead := writeMainCommit(t, store, "package main\n\nfunc A() int { return 1 }\n", []object.Hash{base}, "main", 1700003010)
	featureHead := base

	if err := store.Refs.Set("heads/main", mainHead); err != nil {
		t.Fatal(err)
	}
	if err := store.Refs.Set("heads/feature", featureHead); err != nil {
		t.Fatal(err)
	}

	pr := &models.PullRequest{
		RepoID:       repo.ID,
		Title:        "merge db failure rollback failure",
		Body:         "",
		State:        "open",
		AuthorID:     1,
		SourceBranch: "feature",
		TargetBranch: "main",
	}
	if err := prSvc.db.CreatePullRequest(ctx, pr); err != nil {
		t.Fatal(err)
	}

	dbErr := errors.New("update failed")
	var concurrentHead object.Hash
	prSvc.db = &updatePullRequestHookDB{
		DB:        prSvc.db,
		updateErr: dbErr,
		onUpdate: func(pr *models.PullRequest) error {
			mergeHead := object.Hash(pr.MergeCommit)
			if mergeHead == "" {
				return fmt.Errorf("missing merge commit in update payload")
			}
			concurrentHead = writeMainCommit(
				t,
				store,
				"package main\n\nfunc A() int { return 3 }\n",
				[]object.Hash{mergeHead},
				"concurrent push",
				1700003030,
			)
			if err := store.Refs.Update("heads/main", &mergeHead, &concurrentHead); err != nil {
				return fmt.Errorf("move target ref: %w", err)
			}
			return nil
		},
	}

	_, err := prSvc.Merge(ctx, "alice", "repo", pr, "alice")
	if err == nil {
		t.Fatal("expected merge to fail when UpdatePullRequest fails")
	}
	if !errors.Is(err, dbErr) {
		t.Fatalf("expected wrapped db error, got %v", err)
	}
	if !strings.Contains(err.Error(), "status=desynced") {
		t.Fatalf("expected desynced status in error, got %q", err)
	}

	var syncErr *PRMergeStateSyncError
	if !errors.As(err, &syncErr) {
		t.Fatalf("expected PRMergeStateSyncError, got %T: %v", err, err)
	}
	if syncErr.Status != PRMergeStateSyncDesynced {
		t.Fatalf("expected status %q, got %q", PRMergeStateSyncDesynced, syncErr.Status)
	}
	if syncErr.RollbackErr == nil {
		t.Fatal("expected rollback error when target ref moves before rollback")
	}

	var moved *TargetBranchMovedError
	if !errors.As(syncErr.RollbackErr, &moved) {
		t.Fatalf("expected rollback error to be TargetBranchMovedError, got %T: %v", syncErr.RollbackErr, syncErr.RollbackErr)
	}
	if moved.Branch != "main" {
		t.Fatalf("unexpected moved branch %q", moved.Branch)
	}

	headAfter, err := store.Refs.Get("heads/main")
	if err != nil {
		t.Fatal(err)
	}
	if headAfter != concurrentHead {
		t.Fatalf("target branch should remain at concurrent head after rollback failure: got %s want %s", headAfter, concurrentHead)
	}

	persisted, err := prSvc.db.GetPullRequest(ctx, repo.ID, pr.Number)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.State != "open" {
		t.Fatalf("db PR state should remain open, got %q", persisted.State)
	}
	if persisted.MergeCommit != "" {
		t.Fatalf("db PR merge_commit should remain empty, got %q", persisted.MergeCommit)
	}

	if pr.State != "open" {
		t.Fatalf("input PR state should remain open on failed state sync, got %q", pr.State)
	}
	if pr.MergeCommit != "" {
		t.Fatalf("input PR merge_commit should remain empty on failed state sync, got %q", pr.MergeCommit)
	}
}

func TestMergePreviewCacheClonesResponses(t *testing.T) {
	svc := &PRService{mergePreviewCache: make(map[string]*mergePreviewCacheEntry)}
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
	entry := &mergePreviewCacheEntry{
		key:       "k",
		resp:      &MergePreviewResponse{Files: []FileMergeInfo{{Path: "main.go"}}},
		expires:   time.Now().Add(-time.Second),
		heapIndex: 0,
	}
	svc := &PRService{
		mergePreviewCache: map[string]*mergePreviewCacheEntry{
			"k": entry,
		},
		mergePreviewExpiry: mergePreviewExpiryHeap{entry},
	}

	if _, ok := svc.getCachedMergePreview("k"); ok {
		t.Fatal("expected expired cache entry to be treated as miss")
	}
	if len(svc.mergePreviewCache) != 0 {
		t.Fatalf("expected expired cache entry to be pruned, got %d entries", len(svc.mergePreviewCache))
	}
}

func TestMergePreviewCacheEvictsOldestAtCapacity(t *testing.T) {
	svc := &PRService{mergePreviewCache: make(map[string]*mergePreviewCacheEntry)}
	resp := &MergePreviewResponse{Files: []FileMergeInfo{{Path: "main.go", Status: "clean"}}}

	svc.setCachedMergePreview("a-oldest", resp)
	for i := 0; i < mergePreviewCacheMaxEntries-1; i++ {
		svc.setCachedMergePreview(fmt.Sprintf("b-%03d", i), resp)
	}
	svc.setCachedMergePreview("z-new", resp)

	if _, ok := svc.getCachedMergePreview("a-oldest"); ok {
		t.Fatal("expected oldest cache entry to be evicted at capacity")
	}
	if _, ok := svc.getCachedMergePreview("z-new"); !ok {
		t.Fatal("expected newest cache entry to remain after eviction")
	}
	assertMergePreviewCacheInvariant(t, svc)
}

func TestMergePreviewCacheRemainsBounded(t *testing.T) {
	svc := &PRService{mergePreviewCache: make(map[string]*mergePreviewCacheEntry)}

	total := mergePreviewCacheMaxEntries + 64
	for i := 0; i < total; i++ {
		svc.setCachedMergePreview(fmt.Sprintf("k-%03d", i), &MergePreviewResponse{
			Files: []FileMergeInfo{{Path: fmt.Sprintf("file-%03d.go", i), Status: "clean"}},
		})
	}

	if got := len(svc.mergePreviewCache); got != mergePreviewCacheMaxEntries {
		t.Fatalf("expected bounded cache size %d, got %d", mergePreviewCacheMaxEntries, got)
	}
	assertMergePreviewCacheInvariant(t, svc)
}

func TestMergePreviewCacheRepeatedKeyUpdatesKeepSingleHeapEntry(t *testing.T) {
	svc := &PRService{mergePreviewCache: make(map[string]*mergePreviewCacheEntry)}

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
	if heapSize != 1 {
		t.Fatalf("expected single heap entry after repeated key updates, got %d", heapSize)
	}
	assertMergePreviewCacheInvariant(t, svc)
}

func TestMergePreviewCacheConcurrentAccessMaintainsInvariants(t *testing.T) {
	svc := &PRService{mergePreviewCache: make(map[string]*mergePreviewCacheEntry)}

	const (
		workers    = 8
		iterations = 500
		keySpace   = 96
	)

	start := make(chan struct{})
	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for j := 0; j < iterations; j++ {
				key := fmt.Sprintf("writer-%02d-%03d", i, j%keySpace)
				svc.setCachedMergePreview(key, &MergePreviewResponse{
					Files: []FileMergeInfo{{Path: fmt.Sprintf("file-%03d.go", j), Status: "clean"}},
				})
			}
		}()
	}

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			<-start
			for j := 0; j < iterations; j++ {
				key := fmt.Sprintf("writer-%02d-%03d", (id+j)%workers, j%keySpace)
				_, _ = svc.getCachedMergePreview(key)
			}
		}(i)
	}

	close(start)
	wg.Wait()
	assertMergePreviewCacheInvariant(t, svc)
}

func assertMergePreviewCacheInvariant(t *testing.T, svc *PRService) {
	t.Helper()

	svc.mergePreviewMu.RLock()
	defer svc.mergePreviewMu.RUnlock()

	if len(svc.mergePreviewCache) > mergePreviewCacheMaxEntries {
		t.Fatalf("cache size exceeded max entries: got %d want <= %d", len(svc.mergePreviewCache), mergePreviewCacheMaxEntries)
	}
	if len(svc.mergePreviewCache) != len(svc.mergePreviewExpiry) {
		t.Fatalf("cache/heap size mismatch: cache=%d heap=%d", len(svc.mergePreviewCache), len(svc.mergePreviewExpiry))
	}

	for i, entry := range svc.mergePreviewExpiry {
		if entry == nil {
			t.Fatalf("heap entry %d is nil", i)
		}
		if entry.heapIndex != i {
			t.Fatalf("heap entry %q has index %d, want %d", entry.key, entry.heapIndex, i)
		}
		cached, ok := svc.mergePreviewCache[entry.key]
		if !ok {
			t.Fatalf("heap entry %q missing from cache map", entry.key)
		}
		if cached != entry {
			t.Fatalf("heap entry %q pointer mismatch between heap and cache map", entry.key)
		}
	}

	for key, entry := range svc.mergePreviewCache {
		if entry == nil {
			t.Fatalf("cache entry %q is nil", key)
		}
		if entry.key != key {
			t.Fatalf("cache entry key mismatch: map key %q entry key %q", key, entry.key)
		}
		if entry.heapIndex < 0 || entry.heapIndex >= len(svc.mergePreviewExpiry) {
			t.Fatalf("cache entry %q has invalid heap index %d", key, entry.heapIndex)
		}
		if svc.mergePreviewExpiry[entry.heapIndex] != entry {
			t.Fatalf("cache entry %q heap pointer mismatch at index %d", key, entry.heapIndex)
		}
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

func TestFindMergeBaseCachedPersistsAndRecoversFromStaleEntry(t *testing.T) {
	ctx, prSvc, store, repo := setupPRMergeTestService(t)

	root := writeMainCommit(t, store, "package main\n\nfunc V() int { return 0 }\n", nil, "root", 1700001000)
	left := writeMainCommit(t, store, "package main\n\nfunc V() int { return 1 }\n", []object.Hash{root}, "left", 1700001010)
	right := writeMainCommit(t, store, "package main\n\nfunc V() int { return 2 }\n", []object.Hash{root}, "right", 1700001020)

	// Seed a stale cache entry whose base does not exist in the object store.
	staleBase := strings.Repeat("f", 64)
	if err := prSvc.db.SetMergeBaseCache(ctx, repo.ID, string(left), string(right), staleBase); err != nil {
		t.Fatal(err)
	}

	base, err := prSvc.findMergeBaseCached(ctx, repo.ID, store.Objects, left, right)
	if err != nil {
		t.Fatalf("findMergeBaseCached: %v", err)
	}
	if base != root {
		t.Fatalf("merge base = %s, want %s", base, root)
	}

	cachedBase, ok, err := prSvc.db.GetMergeBaseCache(ctx, repo.ID, string(left), string(right))
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected merge-base cache hit")
	}
	if cachedBase != string(root) {
		t.Fatalf("cached merge base = %s, want %s", cachedBase, root)
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

type updatePullRequestHookDB struct {
	database.DB
	onUpdate  func(pr *models.PullRequest) error
	updateErr error
}

func (d *updatePullRequestHookDB) UpdatePullRequest(ctx context.Context, pr *models.PullRequest) error {
	if d.onUpdate != nil {
		if err := d.onUpdate(pr); err != nil {
			return err
		}
	}
	if d.updateErr != nil {
		return d.updateErr
	}
	return d.DB.UpdatePullRequest(ctx, pr)
}
