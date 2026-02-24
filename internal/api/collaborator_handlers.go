package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/odvcencio/gothub/internal/models"
)

type addCollaboratorRequest struct {
	Username string `json:"username"`
	Role     string `json:"role"` // "read", "write", "admin"
}

type collaboratorResponse struct {
	UserID   int64  `json:"user_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

func (s *Server) handleAddCollaborator(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.authorizeRepoRequest(w, r, true)
	if !ok {
		return
	}

	var req addCollaboratorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" {
		jsonError(w, "username is required", http.StatusBadRequest)
		return
	}
	role := strings.ToLower(strings.TrimSpace(req.Role))
	if role == "" {
		role = "read"
	}
	if role != "read" && role != "write" && role != "admin" {
		jsonError(w, "role must be one of read, write, admin", http.StatusBadRequest)
		return
	}

	user, err := s.db.GetUserByUsername(r.Context(), req.Username)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			jsonError(w, "user not found", http.StatusNotFound)
			return
		}
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	c := &models.Collaborator{RepoID: repo.ID, UserID: user.ID, Role: role}
	if err := s.db.AddCollaborator(r.Context(), c); err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	jsonResponse(w, http.StatusCreated, collaboratorResponse{
		UserID:   user.ID,
		Username: user.Username,
		Role:     role,
	})
}

func (s *Server) handleListCollaborators(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.authorizeRepoRequest(w, r, false)
	if !ok {
		return
	}
	page, perPage := parsePagination(r, 50, 200)
	limit := perPage
	offset := (page - 1) * perPage
	collabs, err := s.db.ListCollaboratorsPage(r.Context(), repo.ID, limit, offset)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	resp := make([]collaboratorResponse, 0, len(collabs))
	for _, c := range collabs {
		u, err := s.db.GetUserByID(r.Context(), c.UserID)
		if err != nil {
			continue
		}
		resp = append(resp, collaboratorResponse{
			UserID:   c.UserID,
			Username: u.Username,
			Role:     c.Role,
		})
	}
	jsonResponse(w, http.StatusOK, resp)
}

func (s *Server) handleRemoveCollaborator(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.authorizeRepoRequest(w, r, true)
	if !ok {
		return
	}
	username := strings.TrimSpace(r.PathValue("username"))
	if username == "" {
		jsonError(w, "username is required", http.StatusBadRequest)
		return
	}

	user, err := s.db.GetUserByUsername(r.Context(), username)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			jsonError(w, "user not found", http.StatusNotFound)
			return
		}
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	if err := s.db.RemoveCollaborator(r.Context(), repo.ID, user.ID); err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
