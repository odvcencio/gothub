package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/odvcencio/gothub/internal/auth"
	"github.com/odvcencio/gothub/internal/models"
)

type createPRRequest struct {
	Title        string `json:"title"`
	Body         string `json:"body"`
	SourceBranch string `json:"source_branch"`
	TargetBranch string `json:"target_branch"`
}

func (s *Server) handleCreatePR(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	repo, ok := s.authorizeRepoRequest(w, r, true)
	if !ok {
		return
	}

	var req createPRRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Title == "" || req.SourceBranch == "" {
		jsonError(w, "title and source_branch are required", http.StatusBadRequest)
		return
	}
	if req.TargetBranch == "" {
		req.TargetBranch = repo.DefaultBranch
	}

	pr, err := s.prSvc.Create(r.Context(), repo.ID, claims.UserID, req.Title, req.Body, req.SourceBranch, req.TargetBranch)
	if err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.notifySvc.NotifyPullRequestOpened(r.Context(), repo, pr, claims.UserID); err != nil {
		slog.Error("notify pr opened", "error", err, "repo_id", repo.ID, "pr", pr.Number)
	}

	// Best-effort webhook emission; does not block PR creation success.
	s.runAsync(r.Context(), "webhook pr opened", []any{"repo_id", repo.ID, "pr", pr.Number}, func(ctx context.Context) error {
		return s.webhookSvc.EmitPullRequestEvent(ctx, repo.ID, "opened", pr)
	})

	jsonResponse(w, http.StatusCreated, pr)
}

func (s *Server) handleListPRs(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.authorizeRepoRequest(w, r, false)
	if !ok {
		return
	}

	state := r.URL.Query().Get("state")
	page, perPage := parsePagination(r, 30, 200)
	prs, err := s.prSvc.List(r.Context(), repo.ID, state, page, perPage)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	jsonResponse(w, http.StatusOK, prs)
}

func (s *Server) handleGetPR(w http.ResponseWriter, r *http.Request) {
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
	jsonResponse(w, http.StatusOK, pr)
}

func (s *Server) handlePRDiff(w http.ResponseWriter, r *http.Request) {
	owner := r.PathValue("owner")
	repoName := r.PathValue("repo")
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
		jsonError(w, "pull request not found", http.StatusNotFound)
		return
	}

	result, err := s.prSvc.Diff(r.Context(), owner, repoName, pr)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonResponse(w, http.StatusOK, result)
}

func (s *Server) handleMergePreview(w http.ResponseWriter, r *http.Request) {
	owner := r.PathValue("owner")
	repoName := r.PathValue("repo")
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
		jsonError(w, "pull request not found", http.StatusNotFound)
		return
	}

	preview, err := s.prSvc.MergePreview(r.Context(), owner, repoName, pr)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonResponse(w, http.StatusOK, preview)
}

func (s *Server) handlePRMergeGate(w http.ResponseWriter, r *http.Request) {
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
		jsonError(w, "pull request not found", http.StatusNotFound)
		return
	}

	gate, err := s.prSvc.EvaluateMergeGate(r.Context(), repo.ID, pr)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	jsonResponse(w, http.StatusOK, gate)
}

func (s *Server) handleMergePR(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	owner := r.PathValue("owner")
	repoName := r.PathValue("repo")
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
		jsonError(w, "pull request not found", http.StatusNotFound)
		return
	}
	if pr.State != "open" {
		jsonError(w, "pull request is not open", http.StatusBadRequest)
		return
	}

	gate, err := s.prSvc.EvaluateMergeGate(r.Context(), repo.ID, pr)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	if !gate.Allowed {
		jsonResponse(w, http.StatusConflict, map[string]any{
			"error":   "merge blocked by branch protection",
			"reasons": gate.Reasons,
			"detail":  strings.Join(gate.Reasons, "; "),
		})
		return
	}

	mergerName := strings.TrimSpace(claims.Username)
	if mergerName == "" {
		user, err := s.db.GetUserByID(r.Context(), claims.UserID)
		if err != nil {
			jsonError(w, "internal error", http.StatusInternalServerError)
			return
		}
		mergerName = user.Username
	}

	mergeHash, err := s.prSvc.Merge(r.Context(), owner, repoName, pr, mergerName)
	if err != nil {
		jsonError(w, err.Error(), http.StatusConflict)
		return
	}

	jsonResponse(w, http.StatusOK, map[string]string{
		"merge_commit": string(mergeHash),
		"status":       "merged",
	})

	// Best-effort webhook emission after merge.
	s.runAsync(r.Context(), "webhook pr merged", []any{"repo_id", repo.ID, "pr", pr.Number}, func(ctx context.Context) error {
		return s.webhookSvc.EmitPullRequestEvent(ctx, repo.ID, "merged", pr)
	})
}

func (s *Server) handleUpdatePR(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
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

	// Only the PR author or repo owner can edit
	if pr.AuthorID != claims.UserID {
		if repo.OwnerUserID == nil || *repo.OwnerUserID != claims.UserID {
			jsonError(w, "forbidden", http.StatusForbidden)
			return
		}
	}

	var req struct {
		Title *string `json:"title"`
		Body  *string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Title != nil {
		pr.Title = strings.TrimSpace(*req.Title)
	}
	if req.Body != nil {
		pr.Body = *req.Body
	}
	if err := s.db.UpdatePullRequest(r.Context(), pr); err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	jsonResponse(w, http.StatusOK, pr)
}

func (s *Server) handleDeletePRComment(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	commentID, _ := strconv.ParseInt(r.PathValue("comment_id"), 10, 64)
	if commentID == 0 {
		jsonError(w, "invalid comment id", http.StatusBadRequest)
		return
	}
	if err := s.db.DeletePRComment(r.Context(), commentID, claims.UserID); err != nil {
		jsonError(w, err.Error(), http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// PR Comments

type createPRCommentRequest struct {
	Body           string `json:"body"`
	FilePath       string `json:"file_path"`
	EntityKey      string `json:"entity_key"`
	EntityStableID string `json:"entity_stable_id"`
	LineNumber     *int   `json:"line_number"`
	CommitHash     string `json:"commit_hash"`
}

func (s *Server) handleCreatePRComment(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
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
		jsonError(w, "pull request not found", http.StatusNotFound)
		return
	}

	var req createPRCommentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Body == "" {
		jsonError(w, "body is required", http.StatusBadRequest)
		return
	}

	comment := &models.PRComment{
		PRID:           pr.ID,
		AuthorID:       claims.UserID,
		Body:           req.Body,
		FilePath:       req.FilePath,
		EntityKey:      req.EntityKey,
		EntityStableID: req.EntityStableID,
		LineNumber:     req.LineNumber,
		CommitHash:     req.CommitHash,
	}
	if err := s.prSvc.CreateComment(r.Context(), comment); err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	comment.AuthorName = claims.Username
	if err := s.notifySvc.NotifyPullRequestComment(r.Context(), repo, pr, comment, claims.UserID); err != nil {
		slog.Error("notify pr comment", "error", err, "repo_id", repo.ID, "pr", pr.Number)
	}
	jsonResponse(w, http.StatusCreated, comment)
}

func (s *Server) handleListPRComments(w http.ResponseWriter, r *http.Request) {
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
		jsonError(w, "pull request not found", http.StatusNotFound)
		return
	}

	page, perPage := parsePagination(r, 50, 200)
	comments, err := s.prSvc.ListComments(r.Context(), pr.ID, page, perPage)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	jsonResponse(w, http.StatusOK, comments)
}

// PR Reviews

type createPRReviewRequest struct {
	State      string `json:"state"` // "approved", "changes_requested", "commented"
	Body       string `json:"body"`
	CommitHash string `json:"commit_hash"`
}

func (s *Server) handleCreatePRReview(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
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
		jsonError(w, "pull request not found", http.StatusNotFound)
		return
	}

	var req createPRReviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	review := &models.PRReview{
		PRID:       pr.ID,
		AuthorID:   claims.UserID,
		State:      req.State,
		Body:       req.Body,
		CommitHash: req.CommitHash,
	}
	if err := s.prSvc.CreateReview(r.Context(), review); err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := s.notifySvc.NotifyPullRequestReview(r.Context(), repo, pr, review, claims.UserID); err != nil {
		slog.Error("notify pr review", "error", err, "repo_id", repo.ID, "pr", pr.Number)
	}
	jsonResponse(w, http.StatusCreated, review)
}

func (s *Server) handleListPRReviews(w http.ResponseWriter, r *http.Request) {
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
		jsonError(w, "pull request not found", http.StatusNotFound)
		return
	}

	page, perPage := parsePagination(r, 50, 200)
	reviews, err := s.prSvc.ListReviews(r.Context(), pr.ID, page, perPage)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	jsonResponse(w, http.StatusOK, reviews)
}
