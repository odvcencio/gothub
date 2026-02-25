package jobs

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/odvcencio/gothub/internal/models"
)

func TestWorkerPoolProcessesJobs(t *testing.T) {
	db, repoID := setupQueueTestDB(t)
	q := NewQueue(db, QueueOptions{RetryDelay: 5 * time.Millisecond, MaxAttempts: 2})

	const commitHash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	job, err := q.EnqueueCommitIndex(context.Background(), repoID, commitHash)
	if err != nil {
		t.Fatal(err)
	}

	var processed atomic.Int32
	pool := NewWorkerPool(q, func(ctx context.Context, claimed *models.IndexingJob) error {
		if claimed == nil {
			return errors.New("claimed job is nil")
		}
		if claimed.CommitHash != commitHash {
			return errors.New("unexpected commit hash")
		}
		processed.Add(1)
		return nil
	}, WorkerPoolOptions{
		Workers:      1,
		PollInterval: 5 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := pool.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
		defer stopCancel()
		if err := pool.Stop(stopCtx); err != nil {
			t.Fatalf("stop worker pool: %v", err)
		}
	}()

	waitForJobStatus(t, q, repoID, commitHash, models.IndexJobCompleted, 2*time.Second)
	if got := processed.Load(); got != 1 {
		t.Fatalf("processed count = %d, want 1", got)
	}

	status, err := q.Status(context.Background(), repoID, commitHash)
	if err != nil {
		t.Fatal(err)
	}
	if status == nil {
		t.Fatal("expected persisted status")
	}
	if status.ID != job.ID {
		t.Fatalf("status job id = %d, want %d", status.ID, job.ID)
	}
}

func TestWorkerPoolRetriesAndFailsAfterMaxAttempts(t *testing.T) {
	db, repoID := setupQueueTestDB(t)
	q := NewQueue(db, QueueOptions{RetryDelay: 5 * time.Millisecond, MaxAttempts: 2})

	const commitHash = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if _, err := q.EnqueueCommitIndex(context.Background(), repoID, commitHash); err != nil {
		t.Fatal(err)
	}

	var attempts atomic.Int32
	pool := NewWorkerPool(q, func(ctx context.Context, claimed *models.IndexingJob) error {
		attempts.Add(1)
		return errors.New("boom")
	}, WorkerPoolOptions{
		Workers:      1,
		PollInterval: 5 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := pool.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
		defer stopCancel()
		if err := pool.Stop(stopCtx); err != nil {
			t.Fatalf("stop worker pool: %v", err)
		}
	}()

	waitForJobStatus(t, q, repoID, commitHash, models.IndexJobFailed, 2*time.Second)
	if got := attempts.Load(); got < 2 {
		t.Fatalf("attempts = %d, want >= 2", got)
	}

	status, err := q.Status(context.Background(), repoID, commitHash)
	if err != nil {
		t.Fatal(err)
	}
	if status == nil {
		t.Fatal("expected persisted status")
	}
	if status.AttemptCount != 2 {
		t.Fatalf("attempt count = %d, want 2", status.AttemptCount)
	}
	if !strings.Contains(status.LastError, "boom") {
		t.Fatalf("last error = %q, want to contain boom", status.LastError)
	}
}

func waitForJobStatus(t *testing.T, q *Queue, repoID int64, commitHash string, want models.IndexJobStatus, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		status, err := q.Status(context.Background(), repoID, commitHash)
		if err != nil {
			t.Fatal(err)
		}
		if status != nil && status.Status == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for status %q", want)
}
