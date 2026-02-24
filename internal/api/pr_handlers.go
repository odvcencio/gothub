package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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
	_ = s.notifySvc.NotifyPullRequestOpened(r.Context(), repo, pr, claims.UserID)

	// Best-effort webhook emission; does not block PR creation success.
	go func(repoID int64, createdPR *models.PullRequest) {
		_ = s.webhookSvc.EmitPullRequestEvent(context.Background(), repoID, "opened", createdPR)
	}(repo.ID, pr)

	jsonResponse(w, http.StatusCreated, pr)
}

func (s *Server) handleListPRs(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.authorizeRepoRequest(w, r, false)
	if !ok {
		return
	}

	state := r.URL.Query().Get("state")
	prs, err := s.prSvc.List(r.Context(), repo.ID, state)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	page, perPage := parsePagination(r, 30, 200)
	jsonResponse(w, http.StatusOK, paginateSlice(prs, page, perPage))
}

func (s *Server) handleGetPR(w http.ResponseWriter, r *http.Request) {
	number, _ := strconv.Atoi(r.PathValue("number"))

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
	number, _ := strconv.Atoi(r.PathValue("number"))

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
	number, _ := strconv.Atoi(r.PathValue("number"))

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
	number, _ := strconv.Atoi(r.PathValue("number"))

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
	number, _ := strconv.Atoi(r.PathValue("number"))

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

	user, _ := s.db.GetUserByID(r.Context(), claims.UserID)
	mergerName := user.Username

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
	go func(repoID int64, mergedPR *models.PullRequest) {
		_ = s.webhookSvc.EmitPullRequestEvent(context.Background(), repoID, "merged", mergedPR)
	}(repo.ID, pr)
}

// PR Comments

type createPRCommentRequest struct {
	Body       string `json:"body"`
	FilePath   string `json:"file_path"`
	EntityKey  string `json:"entity_key"`
	LineNumber *int   `json:"line_number"`
	CommitHash string `json:"commit_hash"`
}

func (s *Server) handleCreatePRComment(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	number, _ := strconv.Atoi(r.PathValue("number"))

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
		PRID:       pr.ID,
		AuthorID:   claims.UserID,
		Body:       req.Body,
		FilePath:   req.FilePath,
		EntityKey:  req.EntityKey,
		LineNumber: req.LineNumber,
		CommitHash: req.CommitHash,
	}
	if err := s.prSvc.CreateComment(r.Context(), comment); err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	comment.AuthorName = claims.Username
	_ = s.notifySvc.NotifyPullRequestComment(r.Context(), repo, pr, comment, claims.UserID)
	jsonResponse(w, http.StatusCreated, comment)
}

func (s *Server) handleListPRComments(w http.ResponseWriter, r *http.Request) {
	number, _ := strconv.Atoi(r.PathValue("number"))

	repo, ok := s.authorizeRepoRequest(w, r, false)
	if !ok {
		return
	}

	pr, err := s.prSvc.Get(r.Context(), repo.ID, number)
	if err != nil {
		jsonError(w, "pull request not found", http.StatusNotFound)
		return
	}

	comments, err := s.prSvc.ListComments(r.Context(), pr.ID)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	page, perPage := parsePagination(r, 50, 200)
	jsonResponse(w, http.StatusOK, paginateSlice(comments, page, perPage))
}

// PR Reviews

type createPRReviewRequest struct {
	State      string `json:"state"` // "approved", "changes_requested", "commented"
	Body       string `json:"body"`
	CommitHash string `json:"commit_hash"`
}

func (s *Server) handleCreatePRReview(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	number, _ := strconv.Atoi(r.PathValue("number"))

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
	_ = s.notifySvc.NotifyPullRequestReview(r.Context(), repo, pr, review, claims.UserID)
	jsonResponse(w, http.StatusCreated, review)
}

func (s *Server) handleListPRReviews(w http.ResponseWriter, r *http.Request) {
	number, _ := strconv.Atoi(r.PathValue("number"))

	repo, ok := s.authorizeRepoRequest(w, r, false)
	if !ok {
		return
	}

	pr, err := s.prSvc.Get(r.Context(), repo.ID, number)
	if err != nil {
		jsonError(w, "pull request not found", http.StatusNotFound)
		return
	}

	reviews, err := s.prSvc.ListReviews(r.Context(), pr.ID)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	page, perPage := parsePagination(r, 50, 200)
	jsonResponse(w, http.StatusOK, paginateSlice(reviews, page, perPage))
}
