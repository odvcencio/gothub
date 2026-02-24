package jobs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/odvcencio/gothub/internal/database"
	"github.com/odvcencio/gothub/internal/models"
)

const (
	defaultRetryDelay = 5 * time.Second
	defaultMaxRetries = 3
)

// Queue persists indexing jobs and status transitions in the database.
type Queue struct {
	db          database.DB
	retryDelay  time.Duration
	maxAttempts int
	jobType     models.IndexJobType
}

type QueueOptions struct {
	RetryDelay  time.Duration
	MaxAttempts int
	JobType     models.IndexJobType
}

func NewQueue(db database.DB, opts QueueOptions) *Queue {
	retryDelay := opts.RetryDelay
	if retryDelay <= 0 {
		retryDelay = defaultRetryDelay
	}
	maxAttempts := opts.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = defaultMaxRetries
	}
	jobType := opts.JobType
	if jobType == "" {
		jobType = models.IndexJobTypeCommitIndex
	}
	return &Queue{
		db:          db,
		retryDelay:  retryDelay,
		maxAttempts: maxAttempts,
		jobType:     jobType,
	}
}

func (q *Queue) EnqueueCommitIndex(ctx context.Context, repoID int64, commitHash string) (*models.IndexingJob, error) {
	if strings.TrimSpace(commitHash) == "" {
		return nil, fmt.Errorf("commit hash is required")
	}
	job := &models.IndexingJob{
		RepoID:        repoID,
		CommitHash:    commitHash,
		JobType:       q.jobType,
		Status:        models.IndexJobQueued,
		MaxAttempts:   q.maxAttempts,
		NextAttemptAt: time.Now().UTC(),
	}
	if err := q.db.EnqueueIndexingJob(ctx, job); err != nil {
		return nil, err
	}
	return job, nil
}

func (q *Queue) Claim(ctx context.Context) (*models.IndexingJob, error) {
	return q.db.ClaimIndexingJob(ctx)
}

func (q *Queue) Complete(ctx context.Context, jobID int64) error {
	return q.db.CompleteIndexingJob(ctx, jobID, models.IndexJobCompleted, "")
}

func (q *Queue) Fail(ctx context.Context, jobID int64, runErr error) error {
	return q.db.CompleteIndexingJob(ctx, jobID, models.IndexJobFailed, failureMessage(runErr))
}

func (q *Queue) RetryOrFail(ctx context.Context, job *models.IndexingJob, runErr error) error {
	if job == nil {
		return fmt.Errorf("indexing job is nil")
	}
	message := failureMessage(runErr)
	if job.MaxAttempts > 0 && job.AttemptCount >= job.MaxAttempts {
		return q.db.CompleteIndexingJob(ctx, job.ID, models.IndexJobFailed, message)
	}
	nextAttempt := time.Now().UTC().Add(q.retryDelay)
	return q.db.RequeueIndexingJob(ctx, job.ID, message, nextAttempt)
}

func (q *Queue) Status(ctx context.Context, repoID int64, commitHash string) (*models.IndexingJob, error) {
	job, err := q.db.GetIndexingJobStatus(ctx, repoID, commitHash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return job, nil
}

func failureMessage(err error) string {
	if err == nil {
		return "job failed"
	}
	msg := strings.TrimSpace(err.Error())
	if msg == "" {
		return "job failed"
	}
	return msg
}
