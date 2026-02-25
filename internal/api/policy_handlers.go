package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/odvcencio/gothub/internal/models"
)

type upsertBranchProtectionRequest struct {
	Enabled                    *bool    `json:"enabled"`
	RequireApprovals           bool     `json:"require_approvals"`
	RequiredApprovals          int      `json:"required_approvals"`
	RequireStatusChecks        bool     `json:"require_status_checks"`
	RequireEntityOwnerApproval bool     `json:"require_entity_owner_approval"`
	RequireLintPass            bool     `json:"require_lint_pass"`
	RequireNoNewDeadCode       bool     `json:"require_no_new_dead_code"`
	RequireSignedCommits       bool     `json:"require_signed_commits"`
	RequiredChecks             []string `json:"required_checks"`
}

func (s *Server) handleUpsertBranchProtection(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.authorizeRepoRequest(w, r, true)
	if !ok {
		return
	}
	branch := strings.TrimSpace(r.PathValue("branch"))
	if branch == "" {
		jsonError(w, "branch is required", http.StatusBadRequest)
		return
	}

	var req upsertBranchProtectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	requiredApprovals := req.RequiredApprovals
	if requiredApprovals <= 0 {
		requiredApprovals = 1
	}

	rule := &models.BranchProtectionRule{
		RepoID:                     repo.ID,
		Branch:                     branch,
		Enabled:                    enabled,
		RequireApprovals:           req.RequireApprovals,
		RequiredApprovals:          requiredApprovals,
		RequireStatusChecks:        req.RequireStatusChecks,
		RequireEntityOwnerApproval: req.RequireEntityOwnerApproval,
		RequireLintPass:            req.RequireLintPass,
		RequireNoNewDeadCode:       req.RequireNoNewDeadCode,
		RequireSignedCommits:       req.RequireSignedCommits,
		RequiredChecks:             req.RequiredChecks,
	}
	if err := s.prSvc.UpsertBranchProtectionRule(r.Context(), rule); err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	jsonResponse(w, http.StatusOK, rule)
}

func (s *Server) handleGetBranchProtection(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.authorizeRepoRequest(w, r, false)
	if !ok {
		return
	}
	branch := strings.TrimSpace(r.PathValue("branch"))
	if branch == "" {
		jsonError(w, "branch is required", http.StatusBadRequest)
		return
	}

	rule, err := s.prSvc.GetBranchProtectionRule(r.Context(), repo.ID, branch)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			jsonError(w, "branch protection rule not found", http.StatusNotFound)
			return
		}
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	jsonResponse(w, http.StatusOK, rule)
}

func (s *Server) handleDeleteBranchProtection(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.authorizeRepoRequest(w, r, true)
	if !ok {
		return
	}
	branch := strings.TrimSpace(r.PathValue("branch"))
	if branch == "" {
		jsonError(w, "branch is required", http.StatusBadRequest)
		return
	}
	if err := s.prSvc.DeleteBranchProtectionRule(r.Context(), repo.ID, branch); err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type upsertPRCheckRunRequest struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	DetailsURL string `json:"details_url"`
	ExternalID string `json:"external_id"`
	HeadCommit string `json:"head_commit"`
}

func (s *Server) handleUpsertPRCheckRun(w http.ResponseWriter, r *http.Request) {
	number, ok := parsePathPositiveInt(w, r, "number", "pull request number")
	if !ok {
		return
	}
	repo, ok := s.authorizeRepoRequest(w, r, true)
	if !ok {
		return
	}

	pr, err := s.prSvc.Get(r.Context(), repo.ID, number)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			jsonError(w, "pull request not found", http.StatusNotFound)
			return
		}
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	run, statusCode, message := decodePRCheckRunUpsertRequest(r, pr.ID)
	if statusCode != 0 {
		jsonError(w, message, statusCode)
		return
	}
	if err := s.prSvc.UpsertPRCheckRun(r.Context(), run); err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	jsonResponse(w, http.StatusOK, run)
}

func (s *Server) handleListPRCheckRuns(w http.ResponseWriter, r *http.Request) {
	number, ok := parsePathPositiveInt(w, r, "number", "pull request number")
	if !ok {
		return
	}
	repo, ok := s.authorizeRepoRequest(w, r, false)
	if !ok {
		return
	}

	pr, err := s.prSvc.Get(r.Context(), repo.ID, number)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			jsonError(w, "pull request not found", http.StatusNotFound)
			return
		}
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	page, perPage := parsePagination(r, 50, 200)
	runs, err := s.prSvc.ListPRCheckRunsPage(r.Context(), pr.ID, page, perPage)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	jsonResponse(w, http.StatusOK, runs)
}
