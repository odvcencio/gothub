package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/odvcencio/gothub/internal/auth"
)

type createIssueRequest struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

func (s *Server) handleCreateIssue(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	repo, ok := s.authorizeRepoRequest(w, r, true)
	if !ok {
		return
	}

	var req createIssueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	issue, err := s.issueSvc.Create(r.Context(), repo.ID, claims.UserID, req.Title, req.Body)
	if err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}
	issue.AuthorName = claims.Username
	if err := s.notifySvc.NotifyIssueOpened(r.Context(), repo, issue, claims.UserID); err != nil {
		slog.Error("notify issue opened", "error", err, "repo_id", repo.ID, "issue", issue.Number)
	}

	s.runWebhookAsync(r.Context(), "webhook issue opened", []any{"repo_id", repo.ID, "issue", issue.Number}, func(ctx context.Context) error {
		return s.webhookSvc.EmitIssueEvent(ctx, repo.ID, "opened", issue.Number, issue.Title, issue.Body, "open")
	})
	s.publishRepoEvent(repo.ID, "issue.opened", map[string]any{
		"number": issue.Number,
		"title":  issue.Title,
		"state":  issue.State,
	})

	jsonResponse(w, http.StatusCreated, issue)
}

func (s *Server) handleListIssues(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.authorizeRepoRequest(w, r, false)
	if !ok {
		return
	}
	state := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("state")))
	if state == "all" {
		state = ""
	}
	page, perPage := parsePagination(r, 30, 200)
	issues, err := s.issueSvc.List(r.Context(), repo.ID, state, page, perPage)
	if err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}
	jsonResponse(w, http.StatusOK, issues)
}

func (s *Server) handleGetIssue(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.authorizeRepoRequest(w, r, false)
	if !ok {
		return
	}
	number, ok := parsePathPositiveInt(w, r, "number", "issue number")
	if !ok {
		return
	}
	issue, err := s.issueSvc.Get(r.Context(), repo.ID, number)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			jsonError(w, "issue not found", http.StatusNotFound)
			return
		}
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	jsonResponse(w, http.StatusOK, issue)
}

type updateIssueRequest struct {
	Title *string `json:"title"`
	Body  *string `json:"body"`
	State *string `json:"state"` // "open"|"closed"
}

func (s *Server) handleUpdateIssue(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.authorizeRepoRequest(w, r, true)
	if !ok {
		return
	}
	number, ok := parsePathPositiveInt(w, r, "number", "issue number")
	if !ok {
		return
	}
	issue, err := s.issueSvc.Get(r.Context(), repo.ID, number)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			jsonError(w, "issue not found", http.StatusNotFound)
			return
		}
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	beforeState := issue.State

	var req updateIssueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Title != nil {
		issue.Title = *req.Title
	}
	if req.Body != nil {
		issue.Body = *req.Body
	}
	if req.State != nil {
		issue.State = strings.ToLower(strings.TrimSpace(*req.State))
	}

	if err := s.issueSvc.Update(r.Context(), issue); err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}

	action := "edited"
	if beforeState != issue.State {
		if issue.State == "closed" {
			action = "closed"
		} else if issue.State == "open" {
			action = "reopened"
		}
	}
	s.runWebhookAsync(r.Context(), "webhook issue event", []any{"repo_id", repo.ID, "issue", issue.Number, "action", action}, func(ctx context.Context) error {
		return s.webhookSvc.EmitIssueEvent(ctx, repo.ID, action, issue.Number, issue.Title, issue.Body, issue.State)
	})
	s.publishRepoEvent(repo.ID, "issue."+action, map[string]any{
		"number": issue.Number,
		"title":  issue.Title,
		"state":  issue.State,
	})

	jsonResponse(w, http.StatusOK, issue)
}

type createIssueCommentRequest struct {
	Body string `json:"body"`
}

func (s *Server) handleCreateIssueComment(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	repo, ok := s.authorizeRepoRequest(w, r, true)
	if !ok {
		return
	}
	number, ok := parsePathPositiveInt(w, r, "number", "issue number")
	if !ok {
		return
	}
	issue, err := s.issueSvc.Get(r.Context(), repo.ID, number)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			jsonError(w, "issue not found", http.StatusNotFound)
			return
		}
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	var req createIssueCommentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	comment, err := s.issueSvc.CreateComment(r.Context(), issue.ID, claims.UserID, req.Body)
	if err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}
	comment.AuthorName = claims.Username
	if err := s.notifySvc.NotifyIssueComment(r.Context(), repo, issue, comment, claims.UserID); err != nil {
		slog.Error("notify issue comment", "error", err, "repo_id", repo.ID, "issue", issue.Number)
	}
	jsonResponse(w, http.StatusCreated, comment)
}

func (s *Server) handleDeleteIssueComment(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	commentID, ok := parsePathPositiveInt64(w, r, "comment_id", "comment id")
	if !ok {
		return
	}
	if err := s.db.DeleteIssueComment(r.Context(), commentID, claims.UserID); err != nil {
		jsonError(w, err.Error(), http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListIssueComments(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.authorizeRepoRequest(w, r, false)
	if !ok {
		return
	}
	number, ok := parsePathPositiveInt(w, r, "number", "issue number")
	if !ok {
		return
	}
	issue, err := s.issueSvc.Get(r.Context(), repo.ID, number)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			jsonError(w, "issue not found", http.StatusNotFound)
			return
		}
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	page, perPage := parsePagination(r, 50, 200)
	comments, err := s.issueSvc.ListComments(r.Context(), issue.ID, page, perPage)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	jsonResponse(w, http.StatusOK, comments)
}
