package database

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/odvcencio/gothub/internal/models"
)

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
			NextAttemptAt: time.Now().UTC(),
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
			NextAttemptAt: time.Now().UTC(),
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
