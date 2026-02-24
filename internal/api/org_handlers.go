package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/odvcencio/gothub/internal/auth"
	"github.com/odvcencio/gothub/internal/models"
)

func (s *Server) handleCreateOrg(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		DisplayName string `json:"display_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		jsonError(w, "name is required", http.StatusBadRequest)
		return
	}

	claims := auth.GetClaims(r.Context())
	org := &models.Org{Name: req.Name, DisplayName: req.DisplayName}
	if err := s.db.CreateOrg(r.Context(), org); err != nil {
		jsonError(w, "failed to create org", http.StatusInternalServerError)
		return
	}

	// Creator becomes owner
	if err := s.db.AddOrgMember(r.Context(), &models.OrgMember{
		OrgID:  org.ID,
		UserID: claims.UserID,
		Role:   "owner",
	}); err != nil {
		slog.Error("add org owner", "error", err, "org_id", org.ID, "user_id", claims.UserID)
	}

	jsonResponse(w, http.StatusCreated, org)
}

func (s *Server) handleGetOrg(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("org")
	org, err := s.db.GetOrg(r.Context(), name)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			jsonError(w, "org not found", http.StatusNotFound)
			return
		}
		jsonError(w, "failed to get org", http.StatusInternalServerError)
		return
	}
	jsonResponse(w, http.StatusOK, org)
}

func (s *Server) handleListUserOrgs(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	orgs, err := s.db.ListUserOrgs(r.Context(), claims.UserID)
	if err != nil {
		jsonError(w, "failed to list orgs", http.StatusInternalServerError)
		return
	}
	if orgs == nil {
		orgs = []models.Org{}
	}
	page, perPage := parsePagination(r, 30, 200)
	jsonResponse(w, http.StatusOK, paginateSlice(orgs, page, perPage))
}

func (s *Server) handleDeleteOrg(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("org")
	claims := auth.GetClaims(r.Context())

	org, err := s.db.GetOrg(r.Context(), name)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			jsonError(w, "org not found", http.StatusNotFound)
			return
		}
		jsonError(w, "failed to get org", http.StatusInternalServerError)
		return
	}

	// Only org owners can delete
	member, err := s.db.GetOrgMember(r.Context(), org.ID, claims.UserID)
	if err != nil || member.Role != "owner" {
		jsonError(w, "only org owners can delete organizations", http.StatusForbidden)
		return
	}

	if err := s.db.DeleteOrg(r.Context(), org.ID); err != nil {
		jsonError(w, "failed to delete org", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListOrgMembers(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("org")
	org, err := s.db.GetOrg(r.Context(), name)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			jsonError(w, "org not found", http.StatusNotFound)
			return
		}
		jsonError(w, "failed to get org", http.StatusInternalServerError)
		return
	}

	members, err := s.db.ListOrgMembers(r.Context(), org.ID)
	if err != nil {
		jsonError(w, "failed to list members", http.StatusInternalServerError)
		return
	}
	if members == nil {
		members = []models.OrgMember{}
	}
	page, perPage := parsePagination(r, 50, 200)
	jsonResponse(w, http.StatusOK, paginateSlice(members, page, perPage))
}

func (s *Server) handleAddOrgMember(w http.ResponseWriter, r *http.Request) {
	orgName := r.PathValue("org")
	claims := auth.GetClaims(r.Context())

	org, err := s.db.GetOrg(r.Context(), orgName)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			jsonError(w, "org not found", http.StatusNotFound)
			return
		}
		jsonError(w, "failed to get org", http.StatusInternalServerError)
		return
	}

	// Only owners can add members
	member, err := s.db.GetOrgMember(r.Context(), org.ID, claims.UserID)
	if err != nil || member.Role != "owner" {
		jsonError(w, "only org owners can manage members", http.StatusForbidden)
		return
	}

	var req struct {
		Username string `json:"username"`
		Role     string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}
	if req.Role == "" {
		req.Role = "member"
	}
	switch req.Role {
	case "owner", "member":
	default:
		jsonError(w, "role must be 'owner' or 'member'", http.StatusBadRequest)
		return
	}

	user, err := s.db.GetUserByUsername(r.Context(), req.Username)
	if err != nil {
		jsonError(w, "user not found", http.StatusNotFound)
		return
	}

	if err := s.db.AddOrgMember(r.Context(), &models.OrgMember{
		OrgID:  org.ID,
		UserID: user.ID,
		Role:   req.Role,
	}); err != nil {
		jsonError(w, "failed to add member", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRemoveOrgMember(w http.ResponseWriter, r *http.Request) {
	orgName := r.PathValue("org")
	username := r.PathValue("username")
	claims := auth.GetClaims(r.Context())

	org, err := s.db.GetOrg(r.Context(), orgName)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			jsonError(w, "org not found", http.StatusNotFound)
			return
		}
		jsonError(w, "failed to get org", http.StatusInternalServerError)
		return
	}

	// Only owners can remove members
	member, err := s.db.GetOrgMember(r.Context(), org.ID, claims.UserID)
	if err != nil || member.Role != "owner" {
		jsonError(w, "only org owners can manage members", http.StatusForbidden)
		return
	}

	user, err := s.db.GetUserByUsername(r.Context(), username)
	if err != nil {
		jsonError(w, "user not found", http.StatusNotFound)
		return
	}

	if err := s.db.RemoveOrgMember(r.Context(), org.ID, user.ID); err != nil {
		jsonError(w, "failed to remove member", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListOrgRepos(w http.ResponseWriter, r *http.Request) {
	orgName := r.PathValue("org")
	org, err := s.db.GetOrg(r.Context(), orgName)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			jsonError(w, "org not found", http.StatusNotFound)
			return
		}
		jsonError(w, "failed to get org", http.StatusInternalServerError)
		return
	}

	repos, err := s.db.ListOrgRepositories(r.Context(), org.ID)
	if err != nil {
		jsonError(w, "failed to list repos", http.StatusInternalServerError)
		return
	}
	if repos == nil {
		repos = []models.Repository{}
	}
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		visible := repos[:0]
		for _, repo := range repos {
			if !repo.IsPrivate {
				visible = append(visible, repo)
			}
		}
		repos = visible
	} else if _, err := s.db.GetOrgMember(r.Context(), org.ID, claims.UserID); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			jsonError(w, "failed to verify org membership", http.StatusInternalServerError)
			return
		}
		visible := repos[:0]
		for _, repo := range repos {
			if !repo.IsPrivate {
				visible = append(visible, repo)
			}
		}
		repos = visible
	}

	page, perPage := parsePagination(r, 30, 200)
	jsonResponse(w, http.StatusOK, paginateSlice(repos, page, perPage))
}
