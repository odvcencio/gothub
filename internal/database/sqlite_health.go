package database

import (
	"context"
	"database/sql"

	"github.com/odvcencio/gothub/internal/models"
)

func (s *SQLiteDB) IndexingQueueStats(ctx context.Context) (IndexingQueueStats, error) {
	var stats IndexingQueueStats
	var oldestQueued sql.NullTime
	err := s.db.QueryRowContext(ctx,
		`SELECT
			 COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0) AS queued,
			 COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0) AS in_progress,
			 COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0) AS failed,
			 MIN(CASE WHEN status = ? THEN next_attempt_at END) AS oldest_queued_at
		 FROM indexing_jobs`,
		models.IndexJobQueued,
		models.IndexJobInProgress,
		models.IndexJobFailed,
		models.IndexJobQueued,
	).Scan(&stats.Queued, &stats.InProgress, &stats.Failed, &oldestQueued)
	if err != nil {
		return IndexingQueueStats{}, err
	}
	if oldestQueued.Valid {
		t := oldestQueued.Time.UTC()
		stats.OldestQueuedAt = &t
	}
	return stats, nil
}

func (s *SQLiteDB) DBStats() sql.DBStats {
	return s.db.Stats()
}
