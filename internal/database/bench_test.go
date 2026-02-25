package database

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/odvcencio/gothub/internal/models"
)

var benchmarkQueueReadyAt = time.Unix(946684800, 0).UTC()

func BenchmarkSQLiteEnqueueIndexingJob(b *testing.B) {
	ctx, db, repoID := setupSQLiteBenchmarkDB(b)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		job := &models.IndexingJob{
			RepoID:        repoID,
			CommitHash:    fmt.Sprintf("commit-%d", i),
			Status:        models.IndexJobQueued,
			MaxAttempts:   3,
			NextAttemptAt: benchmarkQueueReadyAt,
		}
		if err := db.EnqueueIndexingJob(ctx, job); err != nil {
			b.Fatalf("enqueue indexing job: %v", err)
		}
	}
}

func BenchmarkSQLiteIndexingQueueStats(b *testing.B) {
	ctx, db, repoID := setupSQLiteBenchmarkDB(b)
	for i := 0; i < 512; i++ {
		job := &models.IndexingJob{
			RepoID:        repoID,
			CommitHash:    fmt.Sprintf("seed-%d", i),
			Status:        models.IndexJobQueued,
			MaxAttempts:   3,
			NextAttemptAt: benchmarkQueueReadyAt,
		}
		if err := db.EnqueueIndexingJob(ctx, job); err != nil {
			b.Fatalf("seed enqueue indexing job: %v", err)
		}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := db.IndexingQueueStats(ctx); err != nil {
			b.Fatalf("indexing queue stats: %v", err)
		}
	}
}

func BenchmarkSQLiteMergeBaseCacheSetAndGet(b *testing.B) {
	ctx, db, repoID := setupSQLiteBenchmarkDB(b)

	type mergeBaseTriplet struct {
		left  string
		right string
		base  string
	}
	const keyCount = 1024
	keys := make([]mergeBaseTriplet, keyCount)
	for i := 0; i < keyCount; i++ {
		keys[i] = mergeBaseTriplet{
			left:  fmt.Sprintf("%064x", i+1),
			right: fmt.Sprintf("%064x", i+1+keyCount),
			base:  fmt.Sprintf("%064x", i+1+(2*keyCount)),
		}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := keys[i%keyCount]
		if err := db.SetMergeBaseCache(ctx, repoID, key.left, key.right, key.base); err != nil {
			b.Fatalf("set merge-base cache: %v", err)
		}
		base, ok, err := db.GetMergeBaseCache(ctx, repoID, key.right, key.left)
		if err != nil {
			b.Fatalf("get merge-base cache: %v", err)
		}
		if !ok || base != key.base {
			b.Fatalf("merge-base cache mismatch: ok=%v base=%q want=%q", ok, base, key.base)
		}
	}
}

func BenchmarkSQLiteClaimAndCompleteIndexingJobLoop(b *testing.B) {
	ctx, db, repoID := setupSQLiteBenchmarkDB(b)

	for i := 0; i < b.N; i++ {
		job := &models.IndexingJob{
			RepoID:        repoID,
			CommitHash:    fmt.Sprintf("claim-%08d", i),
			Status:        models.IndexJobQueued,
			MaxAttempts:   3,
			NextAttemptAt: benchmarkQueueReadyAt,
		}
		if err := db.EnqueueIndexingJob(ctx, job); err != nil {
			b.Fatalf("seed enqueue indexing job: %v", err)
		}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		claimed, err := db.ClaimIndexingJob(ctx)
		if err != nil {
			b.Fatalf("claim indexing job: %v", err)
		}
		if claimed == nil {
			b.Fatalf("expected claimable indexing job at iteration %d", i)
		}
		if err := db.CompleteIndexingJob(ctx, claimed.ID, models.IndexJobCompleted, ""); err != nil {
			b.Fatalf("complete indexing job: %v", err)
		}
	}
}

func setupSQLiteBenchmarkDB(b *testing.B) (context.Context, *SQLiteDB, int64) {
	b.Helper()

	ctx := context.Background()
	dbPath := filepath.Join(b.TempDir(), "bench.db")
	db, err := OpenSQLite(dbPath)
	if err != nil {
		b.Fatalf("open sqlite: %v", err)
	}
	b.Cleanup(func() {
		_ = db.Close()
	})

	if err := db.Migrate(ctx); err != nil {
		b.Fatalf("migrate sqlite: %v", err)
	}

	user := &models.User{
		Username:     "bench",
		Email:        "bench@example.com",
		PasswordHash: "x",
	}
	if err := db.CreateUser(ctx, user); err != nil {
		b.Fatalf("create benchmark user: %v", err)
	}

	repo := &models.Repository{
		OwnerUserID:   &user.ID,
		Name:          "bench-repo",
		DefaultBranch: "main",
		StoragePath:   filepath.Join(b.TempDir(), "repos"),
	}
	if err := db.CreateRepository(ctx, repo); err != nil {
		b.Fatalf("create benchmark repository: %v", err)
	}

	return ctx, db, repo.ID
}
