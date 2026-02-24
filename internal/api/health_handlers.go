package api

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"github.com/odvcencio/gothub/internal/database"
)

type indexingQueueStatsProvider interface {
	IndexingQueueStats(ctx context.Context) (database.IndexingQueueStats, error)
}

type dbStatsProvider interface {
	DBStats() sql.DBStats
}

type adminHealthResponse struct {
	Status    string              `json:"status"`
	Timestamp time.Time           `json:"timestamp"`
	Queue     adminHealthQueue    `json:"queue"`
	Workers   adminHealthWorkers  `json:"workers"`
	Cache     adminHealthCache    `json:"cache"`
	Database  adminHealthDatabase `json:"database"`
	Errors    []string            `json:"errors,omitempty"`
}

type adminHealthQueue struct {
	Depth                 int64   `json:"depth"`
	InProgress            int64   `json:"in_progress"`
	Failed                int64   `json:"failed"`
	OldestQueuedAgeSecond float64 `json:"oldest_queued_age_seconds"`
}

type adminHealthWorkers struct {
	AsyncIndexingEnabled bool `json:"async_indexing_enabled"`
	Configured           int  `json:"configured"`
	Active               int  `json:"active"`
}

type adminHealthCache struct {
	CodeIntel adminHealthCodeIntelCache `json:"codeintel"`
}

type adminHealthCodeIntelCache struct {
	Entries    int   `json:"entries"`
	MaxItems   int   `json:"max_items"`
	TTLSeconds int64 `json:"ttl_seconds"`
}

type adminHealthDatabase struct {
	OpenConnections int   `json:"open_connections"`
	InUse           int   `json:"in_use"`
	Idle            int   `json:"idle"`
	WaitCount       int64 `json:"wait_count"`
	WaitDurationMS  int64 `json:"wait_duration_ms"`
	MaxIdleClosed   int64 `json:"max_idle_closed"`
	MaxLifetime     int64 `json:"max_lifetime_closed"`
	MaxIdleTime     int64 `json:"max_idle_time_closed"`
}

func (s *Server) handleAdminHealth(w http.ResponseWriter, r *http.Request) {
	resp := adminHealthResponse{
		Status:    "ok",
		Timestamp: time.Now().UTC(),
		Workers: adminHealthWorkers{
			AsyncIndexingEnabled: s.asyncIndex,
		},
	}
	if s.asyncIndex {
		resp.Workers.Configured = 1
	}

	cacheStats := s.codeIntelSvc.CacheStats()
	resp.Cache.CodeIntel = adminHealthCodeIntelCache{
		Entries:    cacheStats.Entries,
		MaxItems:   cacheStats.MaxItems,
		TTLSeconds: cacheStats.TTLSeconds,
	}

	if queueProvider, ok := s.db.(indexingQueueStatsProvider); ok {
		stats, err := queueProvider.IndexingQueueStats(r.Context())
		if err != nil {
			resp.Errors = append(resp.Errors, "indexing_queue_stats")
		} else {
			resp.Queue.Depth = stats.Queued
			resp.Queue.InProgress = stats.InProgress
			resp.Queue.Failed = stats.Failed
			if stats.OldestQueuedAt != nil {
				age := time.Since(stats.OldestQueuedAt.UTC()).Seconds()
				if age < 0 {
					age = 0
				}
				resp.Queue.OldestQueuedAgeSecond = age
			}
		}
	}

	if poolProvider, ok := s.db.(dbStatsProvider); ok {
		stats := poolProvider.DBStats()
		resp.Database = adminHealthDatabase{
			OpenConnections: stats.OpenConnections,
			InUse:           stats.InUse,
			Idle:            stats.Idle,
			WaitCount:       stats.WaitCount,
			WaitDurationMS:  stats.WaitDuration.Milliseconds(),
			MaxIdleClosed:   stats.MaxIdleClosed,
			MaxLifetime:     stats.MaxLifetimeClosed,
			MaxIdleTime:     stats.MaxIdleTimeClosed,
		}
	}

	resp.Workers.Active = int(resp.Queue.InProgress)
	if len(resp.Errors) > 0 {
		resp.Status = "degraded"
		jsonResponse(w, http.StatusServiceUnavailable, resp)
		return
	}
	jsonResponse(w, http.StatusOK, resp)
}
