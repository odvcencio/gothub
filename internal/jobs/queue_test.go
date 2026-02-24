package jobs

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/odvcencio/gothub/internal/database"
	"github.com/odvcencio/gothub/internal/models"
)

func TestQueueEnqueueClaimAndComplete(t *testing.T) {
	db, repoID := setupQueueTestDB(t)
	q := NewQueue(db, QueueOptions{MaxAttempts: 2})

	ctx := context.Background()
	job, err := q.EnqueueCommitIndex(ctx, repoID, strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	if job.ID == 0 {
		t.Fatal("expected persisted job id")
	}
	if job.Status != models.IndexJobQueued {
		t.Fatalf("expected queued status, got %q", job.Status)
	}

	claimed, err := q.Claim(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if claimed == nil {
		t.Fatal("expected claimed job")
	}
	if claimed.ID != job.ID {
		t.Fatalf("expected claimed id %d, got %d", job.ID, claimed.ID)
	}
	if claimed.Status != models.IndexJobInProgress {
		t.Fatalf("expected in_progress status, got %q", claimed.Status)
	}

	if err := q.Complete(ctx, claimed.ID); err != nil {
		t.Fatal(err)
	}
	status, err := q.Status(ctx, repoID, job.CommitHash)
	if err != nil {
		t.Fatal(err)
	}
	if status == nil || status.Status != models.IndexJobCompleted {
		t.Fatalf("expected completed status, got %+v", status)
	}
}

func TestQueueRetryOrFailTransitions(t *testing.T) {
	db, repoID := setupQueueTestDB(t)
	q := NewQueue(db, QueueOptions{RetryDelay: 5 * time.Millisecond, MaxAttempts: 2})

	ctx := context.Background()
	job, err := q.EnqueueCommitIndex(ctx, repoID, strings.Repeat("b", 64))
	if err != nil {
		t.Fatal(err)
	}

	first, err := q.Claim(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if first == nil {
		t.Fatal("expected first claim")
	}
	if err := q.RetryOrFail(ctx, first, errors.New("temporary")); err != nil {
		t.Fatal(err)
	}

	time.Sleep(10 * time.Millisecond)
	second, err := q.Claim(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if second == nil {
		t.Fatal("expected second claim after retry delay")
	}
	if second.AttemptCount != 2 {
		t.Fatalf("expected attempt_count 2, got %d", second.AttemptCount)
	}
	if err := q.RetryOrFail(ctx, second, errors.New("terminal")); err != nil {
		t.Fatal(err)
	}

	status, err := q.Status(ctx, repoID, job.CommitHash)
	if err != nil {
		t.Fatal(err)
	}
	if status == nil {
		t.Fatal("expected persisted status")
	}
	if status.Status != models.IndexJobFailed {
		t.Fatalf("expected failed status, got %q", status.Status)
	}
	if status.LastError != "terminal" {
		t.Fatalf("expected terminal error message, got %q", status.LastError)
	}
}

func setupQueueTestDB(t *testing.T) (database.DB, int64) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := database.OpenSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	user := &models.User{Username: "queue-owner", Email: "queue-owner@example.com", PasswordHash: "x"}
	if err := db.CreateUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	repo := &models.Repository{
		OwnerUserID:   &user.ID,
		Name:          "queue-repo",
		DefaultBranch: "main",
		StoragePath:   "pending",
	}
	if err := db.CreateRepository(ctx, repo); err != nil {
		t.Fatal(err)
	}

	return db, repo.ID
}
