package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/odvcencio/gothub/internal/models"
)

type createWebhookRequest struct {
	URL    string   `json:"url"`
	Secret string   `json:"secret"`
	Events []string `json:"events"`
	Active *bool    `json:"active"`
}

type webhookResponse struct {
	ID        int64     `json:"id"`
	RepoID    int64     `json:"repo_id"`
	URL       string    `json:"url"`
	Events    []string  `json:"events,omitempty"`
	Active    bool      `json:"active"`
	HasSecret bool      `json:"has_secret"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (s *Server) handleCreateWebhook(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.authorizeRepoRequest(w, r, true)
	if !ok {
		return
	}

	var req createWebhookRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.URL) == "" {
		jsonError(w, "url is required", http.StatusBadRequest)
		return
	}

	active := true
	if req.Active != nil {
		active = *req.Active
	}

	hook := &models.Webhook{
		RepoID: repo.ID,
		URL:    req.URL,
		Secret: req.Secret,
		Events: req.Events,
		Active: active,
	}
	if err := s.webhookSvc.CreateWebhook(r.Context(), hook); err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}

	jsonResponse(w, http.StatusCreated, webhookToResponse(hook))
}

func (s *Server) handleListWebhooks(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.authorizeRepoRequest(w, r, false)
	if !ok {
		return
	}

	hooks, err := s.webhookSvc.ListWebhooks(r.Context(), repo.ID)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	resp := make([]webhookResponse, 0, len(hooks))
	for i := range hooks {
		resp = append(resp, webhookToResponse(&hooks[i]))
	}
	page, perPage := parsePagination(r, 50, 200)
	jsonResponse(w, http.StatusOK, paginateSlice(resp, page, perPage))
}

func (s *Server) handleGetWebhook(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.authorizeRepoRequest(w, r, false)
	if !ok {
		return
	}
	webhookID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || webhookID <= 0 {
		jsonError(w, "invalid webhook id", http.StatusBadRequest)
		return
	}

	hook, err := s.webhookSvc.GetWebhook(r.Context(), repo.ID, webhookID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			jsonError(w, "webhook not found", http.StatusNotFound)
			return
		}
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	jsonResponse(w, http.StatusOK, webhookToResponse(hook))
}

func (s *Server) handleDeleteWebhook(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.authorizeRepoRequest(w, r, true)
	if !ok {
		return
	}
	webhookID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || webhookID <= 0 {
		jsonError(w, "invalid webhook id", http.StatusBadRequest)
		return
	}

	if err := s.webhookSvc.DeleteWebhook(r.Context(), repo.ID, webhookID); err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListWebhookDeliveries(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.authorizeRepoRequest(w, r, false)
	if !ok {
		return
	}
	webhookID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || webhookID <= 0 {
		jsonError(w, "invalid webhook id", http.StatusBadRequest)
		return
	}
	if _, err := s.webhookSvc.GetWebhook(r.Context(), repo.ID, webhookID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			jsonError(w, "webhook not found", http.StatusNotFound)
			return
		}
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	deliveries, err := s.webhookSvc.ListWebhookDeliveries(r.Context(), repo.ID, webhookID)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	page, perPage := parsePagination(r, 50, 200)
	jsonResponse(w, http.StatusOK, paginateSlice(deliveries, page, perPage))
}

func (s *Server) handleRedeliverWebhookDelivery(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.authorizeRepoRequest(w, r, true)
	if !ok {
		return
	}
	webhookID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || webhookID <= 0 {
		jsonError(w, "invalid webhook id", http.StatusBadRequest)
		return
	}
	deliveryID, err := strconv.ParseInt(r.PathValue("delivery_id"), 10, 64)
	if err != nil || deliveryID <= 0 {
		jsonError(w, "invalid delivery id", http.StatusBadRequest)
		return
	}

	delivery, err := s.webhookSvc.Redeliver(r.Context(), repo.ID, webhookID, deliveryID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			jsonError(w, "delivery not found", http.StatusNotFound)
			return
		}
		jsonError(w, err.Error(), http.StatusBadGateway)
		return
	}
	jsonResponse(w, http.StatusOK, delivery)
}

func (s *Server) handlePingWebhook(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.authorizeRepoRequest(w, r, true)
	if !ok {
		return
	}
	webhookID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || webhookID <= 0 {
		jsonError(w, "invalid webhook id", http.StatusBadRequest)
		return
	}

	delivery, err := s.webhookSvc.PingWebhook(r.Context(), repo.ID, webhookID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			jsonError(w, "webhook not found", http.StatusNotFound)
			return
		}
		jsonError(w, err.Error(), http.StatusBadGateway)
		return
	}
	jsonResponse(w, http.StatusOK, delivery)
}

func webhookToResponse(hook *models.Webhook) webhookResponse {
	return webhookResponse{
		ID:        hook.ID,
		RepoID:    hook.RepoID,
		URL:       hook.URL,
		Events:    hook.Events,
		Active:    hook.Active,
		HasSecret: hook.Secret != "",
		CreatedAt: hook.CreatedAt,
		UpdatedAt: hook.UpdatedAt,
	}
}
