package api

import (
	"database/sql"
	"net/http"
	"strings"
	"time"

	"github.com/odvcencio/gothub/internal/models"
)

const indexQueueStatusNotFound = "not_found"

type indexStatusResponse struct {
	Ref         string    `json:"ref"`
	CommitHash  string    `json:"commit_hash"`
	Indexed     bool      `json:"indexed"`
	QueueStatus string    `json:"queue_status"`
	Attempts    int       `json:"attempts"`
	LastError   string    `json:"last_error,omitempty"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (s *Server) handleGetIndexStatus(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.authorizeRepoRequest(w, r, false)
	if !ok {
		return
	}

	ref := strings.TrimSpace(r.URL.Query().Get("ref"))
	if ref == "" {
		jsonError(w, "ref query parameter is required", http.StatusBadRequest)
		return
	}

	commitHash, err := s.browseSvc.ResolveRef(r.Context(), r.PathValue("owner"), r.PathValue("repo"), ref)
	if err != nil {
		if strings.Contains(err.Error(), "ref not found") {
			jsonError(w, "ref not found", http.StatusNotFound)
			return
		}
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	resp := indexStatusResponse{
		Ref:         ref,
		CommitHash:  string(commitHash),
		QueueStatus: indexQueueStatusNotFound,
		UpdatedAt:   time.Now().UTC(),
	}

	job, err := s.db.GetIndexingJobStatus(r.Context(), repo.ID, string(commitHash))
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	if job != nil {
		resp.QueueStatus = normalizeIndexQueueStatus(job.Status)
		resp.Attempts = job.AttemptCount
		resp.LastError = strings.TrimSpace(job.LastError)
		resp.UpdatedAt = job.UpdatedAt
	}

	_, err = s.db.GetCommitIndex(r.Context(), repo.ID, string(commitHash))
	switch {
	case err == nil:
		resp.Indexed = true
	case err == sql.ErrNoRows:
	default:
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	if resp.QueueStatus == indexQueueStatusNotFound && resp.Indexed {
		resp.QueueStatus = string(models.IndexJobCompleted)
	}

	jsonResponse(w, http.StatusOK, resp)
}

func normalizeIndexQueueStatus(status models.IndexJobStatus) string {
	switch status {
	case models.IndexJobQueued, models.IndexJobInProgress, models.IndexJobFailed, models.IndexJobCompleted:
		return string(status)
	default:
		return indexQueueStatusNotFound
	}
}
