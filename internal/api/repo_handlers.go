package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/odvcencio/gothub/internal/auth"
)

type createRepoRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Private     bool   `json:"private"`
}

func (s *Server) handleCreateRepo(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	var req createRepoRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		jsonError(w, "name is required", http.StatusBadRequest)
		return
	}

	repo, err := s.repoSvc.Create(r.Context(), claims.UserID, req.Name, req.Description, req.Private)
	if err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}
	jsonResponse(w, http.StatusCreated, repo)
}

func (s *Server) handleGetRepo(w http.ResponseWriter, r *http.Request) {
	owner := r.PathValue("owner")
	name := r.PathValue("repo")
	repo, err := s.repoSvc.Get(r.Context(), owner, name)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			jsonError(w, "repository not found", http.StatusNotFound)
			return
		}
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Check access for private repos
	if repo.IsPrivate {
		claims := auth.GetClaims(r.Context())
		if claims == nil || (repo.OwnerUserID != nil && claims.UserID != *repo.OwnerUserID) {
			jsonError(w, "repository not found", http.StatusNotFound)
			return
		}
	}

	jsonResponse(w, http.StatusOK, repo)
}

func (s *Server) handleListUserRepos(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	repos, err := s.repoSvc.List(r.Context(), claims.UserID)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	jsonResponse(w, http.StatusOK, repos)
}

func (s *Server) handleDeleteRepo(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	owner := r.PathValue("owner")
	name := r.PathValue("repo")

	repo, err := s.repoSvc.Get(r.Context(), owner, name)
	if err != nil {
		jsonError(w, "repository not found", http.StatusNotFound)
		return
	}

	// Only owner can delete
	if repo.OwnerUserID == nil || claims.UserID != *repo.OwnerUserID {
		jsonError(w, "forbidden", http.StatusForbidden)
		return
	}

	if err := s.repoSvc.Delete(r.Context(), repo.ID); err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
