package api

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/odvcencio/gothub/internal/auth"
	"github.com/odvcencio/gothub/internal/models"
)

// authorizeRepoRequest resolves {owner}/{repo} and enforces repo visibility/access.
// write=true requires authenticated write access.
func (s *Server) authorizeRepoRequest(w http.ResponseWriter, r *http.Request, write bool) (*models.Repository, bool) {
	owner := r.PathValue("owner")
	repoName := r.PathValue("repo")

	repo, err := s.repoSvc.Get(r.Context(), owner, repoName)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			jsonError(w, "repository not found", http.StatusNotFound)
			return nil, false
		}
		jsonError(w, "internal error", http.StatusInternalServerError)
		return nil, false
	}

	claims := auth.GetClaims(r.Context())

	// Public read does not require authentication.
	if !write && !repo.IsPrivate && claims == nil {
		return repo, true
	}

	if claims == nil {
		if write {
			jsonError(w, "authentication required", http.StatusUnauthorized)
		} else {
			// Hide private repository existence.
			jsonError(w, "repository not found", http.StatusNotFound)
		}
		return nil, false
	}

	allowed, err := s.userHasRepoAccess(r.Context(), repo, claims.UserID, write)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return nil, false
	}
	if !allowed {
		if repo.IsPrivate {
			jsonError(w, "repository not found", http.StatusNotFound)
		} else {
			jsonError(w, "forbidden", http.StatusForbidden)
		}
		return nil, false
	}
	return repo, true
}
