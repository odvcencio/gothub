package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/odvcencio/gothub/internal/models"
)

const (
	defaultWorkerCount  = 2
	defaultPollInterval = 250 * time.Millisecond
)

type JobProcessor func(ctx context.Context, job *models.IndexingJob) error

type WorkerPoolOptions struct {
	Workers      int
	PollInterval time.Duration
	Logger       *slog.Logger
}

// WorkerPool claims indexing jobs from Queue and executes them with JobProcessor.
type WorkerPool struct {
	queue        *Queue
	process      JobProcessor
	workers      int
	pollInterval time.Duration
	logger       *slog.Logger

	mu      sync.Mutex
	cancel  context.CancelFunc
	done    chan struct{}
	started bool
}

func NewWorkerPool(queue *Queue, process JobProcessor, opts WorkerPoolOptions) *WorkerPool {
	workers := opts.Workers
	if workers <= 0 {
		workers = defaultWorkerCount
	}
	pollInterval := opts.PollInterval
	if pollInterval <= 0 {
		pollInterval = defaultPollInterval
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &WorkerPool{
		queue:        queue,
		process:      process,
		workers:      workers,
		pollInterval: pollInterval,
		logger:       logger,
	}
}

func (w *WorkerPool) Start(parent context.Context) error {
	if w == nil || w.queue == nil || w.process == nil {
		return fmt.Errorf("worker pool is not configured")
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	if w.started {
		return nil
	}

	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	w.cancel = cancel
	w.done = done
	w.started = true

	go w.run(ctx, done)
	return nil
}

func (w *WorkerPool) Stop(ctx context.Context) error {
	if w == nil {
		return nil
	}

	w.mu.Lock()
	if !w.started {
		w.mu.Unlock()
		return nil
	}
	cancel := w.cancel
	done := w.done
	w.mu.Unlock()

	cancel()

	select {
	case <-done:
	case <-ctx.Done():
		return ctx.Err()
	}

	w.mu.Lock()
	w.started = false
	w.cancel = nil
	w.done = nil
	w.mu.Unlock()
	return nil
}

func (w *WorkerPool) run(ctx context.Context, done chan<- struct{}) {
	defer close(done)

	var wg sync.WaitGroup
	for i := 0; i < w.workers; i++ {
		workerID := i + 1
		wg.Add(1)
		go func() {
			defer wg.Done()
			w.runWorker(ctx, workerID)
		}()
	}
	wg.Wait()
}

func (w *WorkerPool) runWorker(ctx context.Context, workerID int) {
	for {
		if err := ctx.Err(); err != nil {
			return
		}

		job, err := w.queue.Claim(ctx)
		if err != nil {
			w.logger.Warn("index worker claim failed", "worker_id", workerID, "error", err)
			if !sleepOrDone(ctx, w.pollInterval) {
				return
			}
			continue
		}
		if job == nil {
			if !sleepOrDone(ctx, w.pollInterval) {
				return
			}
			continue
		}

		if err := w.process(ctx, job); err != nil {
			if retryErr := w.queue.RetryOrFail(ctx, job, err); retryErr != nil {
				w.logger.Error("index worker retry/fail update failed", "worker_id", workerID, "job_id", job.ID, "error", retryErr)
			}
			continue
		}

		if err := w.queue.Complete(ctx, job.ID); err != nil {
			w.logger.Error("index worker complete failed", "worker_id", workerID, "job_id", job.ID, "error", err)
		}
	}
}

func sleepOrDone(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		select {
		case <-ctx.Done():
			return false
		default:
			return true
		}
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
