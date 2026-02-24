package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/odvcencio/gothub/internal/auth"
)

type createRepoRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Private     bool   `json:"private"`
}

type forkRepoRequest struct {
	Name string `json:"name"`
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
	repo, ok := s.authorizeRepoRequest(w, r, false)
	if !ok {
		return
	}
	if repo.ParentRepoID != nil && *repo.ParentRepoID > 0 && (repo.ParentOwner == "" || repo.ParentName == "") {
		if parent, err := s.repoSvc.GetByID(r.Context(), *repo.ParentRepoID); err == nil {
			repo.ParentOwner = parent.OwnerName
			repo.ParentName = parent.Name
		}
	}
	jsonResponse(w, http.StatusOK, repo)
}

func (s *Server) handleListUserRepos(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	page, perPage := parsePagination(r, 30, 200)
	repos, err := s.repoSvc.ListPage(r.Context(), claims.UserID, page, perPage)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	jsonResponse(w, http.StatusOK, repos)
}

func (s *Server) handleDeleteRepo(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	repo, ok := s.authorizeRepoRequest(w, r, true)
	if !ok {
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

func (s *Server) handleForkRepo(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	sourceRepo, ok := s.authorizeRepoRequest(w, r, false)
	if !ok {
		return
	}

	req := forkRepoRequest{}
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			jsonError(w, "invalid request body", http.StatusBadRequest)
			return
		}
	}

	fork, err := s.repoSvc.Fork(r.Context(), sourceRepo.ID, claims.UserID, req.Name)
	if err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}
	fork.OwnerName = claims.Username
	fork.ParentOwner = sourceRepo.OwnerName
	fork.ParentName = sourceRepo.Name
	jsonResponse(w, http.StatusCreated, fork)
}

func (s *Server) handleListRepoForks(w http.ResponseWriter, r *http.Request) {
	sourceRepo, ok := s.authorizeRepoRequest(w, r, false)
	if !ok {
		return
	}
	page, perPage := parsePagination(r, 30, 200)
	forks, err := s.repoSvc.ListForksPage(r.Context(), sourceRepo.ID, page, perPage)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	jsonResponse(w, http.StatusOK, forks)
}
