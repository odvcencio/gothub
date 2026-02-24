package database

import "time"

// IndexingQueueStats summarizes indexing queue status for health and observability endpoints.
type IndexingQueueStats struct {
	Queued         int64
	InProgress     int64
	Failed         int64
	OldestQueuedAt *time.Time
}
