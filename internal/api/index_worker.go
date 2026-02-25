package api

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/odvcencio/got/pkg/object"
	"github.com/odvcencio/gothub/internal/jobs"
	"github.com/odvcencio/gothub/internal/models"
)

const (
	defaultIndexWorkerStopTimeout = 5 * time.Second
)

func (s *Server) newIndexWorker(workerCount int, pollInterval time.Duration) *jobs.WorkerPool {
	return jobs.NewWorkerPool(s.indexQueue, s.processIndexingJob, jobs.WorkerPoolOptions{
		Workers:      workerCount,
		PollInterval: pollInterval,
		Logger:       slog.Default(),
	})
}

func (s *Server) processIndexingJob(ctx context.Context, job *models.IndexingJob) error {
	if job == nil {
		return fmt.Errorf("indexing job is nil")
	}
	if job.JobType != models.IndexJobTypeCommitIndex {
		return fmt.Errorf("unsupported indexing job type %q", job.JobType)
	}

	commitHash := object.Hash(strings.TrimSpace(job.CommitHash))
	if commitHash == "" {
		return fmt.Errorf("commit hash is required")
	}

	store, err := s.repoSvc.OpenStoreByID(ctx, job.RepoID)
	if err != nil {
		return err
	}
	if err := s.lineageSvc.IndexCommit(ctx, job.RepoID, store, commitHash); err != nil {
		return err
	}
	return s.codeIntelSvc.EnsureCommitIndexed(ctx, job.RepoID, store, "", commitHash)
}

func (s *Server) StartBackgroundWorkers(ctx context.Context) error {
	if !s.asyncIndex || s.indexWorker == nil {
		return nil
	}
	return s.indexWorker.Start(ctx)
}

func (s *Server) StopBackgroundWorkers(ctx context.Context) error {
	if s.indexWorker == nil {
		return nil
	}
	return s.indexWorker.Stop(ctx)
}

func (s *Server) StopBackgroundWorkersNow() {
	stopCtx, cancel := context.WithTimeout(context.Background(), defaultIndexWorkerStopTimeout)
	defer cancel()
	if err := s.StopBackgroundWorkers(stopCtx); err != nil {
		slog.Warn("stop background workers", "error", err)
	}
}
