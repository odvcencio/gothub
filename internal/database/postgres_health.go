package database

import (
	"context"
	"database/sql"

	"github.com/odvcencio/gothub/internal/models"
)

func (p *PostgresDB) IndexingQueueStats(ctx context.Context) (IndexingQueueStats, error) {
	var stats IndexingQueueStats
	var oldestQueued sql.NullTime
	err := p.db.QueryRowContext(ctx,
		`SELECT
			 COALESCE(SUM(CASE WHEN status = $1 THEN 1 ELSE 0 END), 0) AS queued,
			 COALESCE(SUM(CASE WHEN status = $2 THEN 1 ELSE 0 END), 0) AS in_progress,
			 COALESCE(SUM(CASE WHEN status = $3 THEN 1 ELSE 0 END), 0) AS failed,
			 MIN(CASE WHEN status = $1 THEN next_attempt_at END) AS oldest_queued_at
		 FROM indexing_jobs`,
		models.IndexJobQueued,
		models.IndexJobInProgress,
		models.IndexJobFailed,
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

func (p *PostgresDB) DBStats() sql.DBStats {
	return p.db.Stats()
}
