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
	_ = s.notifySvc.NotifyIssueOpened(r.Context(), repo, issue, claims.UserID)

	go func(repoID int64, createdIssueID int, issueTitle string, issueBody string) {
		_ = s.webhookSvc.EmitIssueEvent(context.Background(), repoID, "opened", createdIssueID, issueTitle, issueBody, "open")
	}(repo.ID, issue.Number, issue.Title, issue.Body)

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
	issues, err := s.issueSvc.List(r.Context(), repo.ID, state)
	if err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}
	page, perPage := parsePagination(r, 30, 200)
	jsonResponse(w, http.StatusOK, paginateSlice(issues, page, perPage))
}

func (s *Server) handleGetIssue(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.authorizeRepoRequest(w, r, false)
	if !ok {
		return
	}
	number, _ := strconv.Atoi(r.PathValue("number"))
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
	number, _ := strconv.Atoi(r.PathValue("number"))
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
	go func(repoID int64, n int, title string, body string, state string, eventAction string) {
		_ = s.webhookSvc.EmitIssueEvent(context.Background(), repoID, eventAction, n, title, body, state)
	}(repo.ID, issue.Number, issue.Title, issue.Body, issue.State, action)

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
	number, _ := strconv.Atoi(r.PathValue("number"))
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
	_ = s.notifySvc.NotifyIssueComment(r.Context(), repo, issue, comment, claims.UserID)
	jsonResponse(w, http.StatusCreated, comment)
}

func (s *Server) handleListIssueComments(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.authorizeRepoRequest(w, r, false)
	if !ok {
		return
	}
	number, _ := strconv.Atoi(r.PathValue("number"))
	issue, err := s.issueSvc.Get(r.Context(), repo.ID, number)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			jsonError(w, "issue not found", http.StatusNotFound)
			return
		}
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	comments, err := s.issueSvc.ListComments(r.Context(), issue.ID)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	page, perPage := parsePagination(r, 50, 200)
	jsonResponse(w, http.StatusOK, paginateSlice(comments, page, perPage))
}
