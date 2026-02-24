package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/odvcencio/gothub/internal/database"
	"github.com/odvcencio/gothub/internal/models"
)

type WebhookService struct {
	db     database.DB
	client *http.Client
}

func NewWebhookService(db database.DB) *WebhookService {
	return &WebhookService{
		db: db,
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

func (s *WebhookService) CreateWebhook(ctx context.Context, hook *models.Webhook) error {
	hook.URL = strings.TrimSpace(hook.URL)
	if hook.URL == "" {
		return fmt.Errorf("url is required")
	}
	hook.Events = normalizeWebhookEvents(hook.Events)
	hook.EventsCSV = strings.Join(hook.Events, ",")
	if hook.EventsCSV == "" {
		hook.EventsCSV = "*"
	}
	if err := s.db.CreateWebhook(ctx, hook); err != nil {
		return err
	}
	hook.Events = parseWebhookEvents(hook.EventsCSV)
	return nil
}

func (s *WebhookService) GetWebhook(ctx context.Context, repoID, webhookID int64) (*models.Webhook, error) {
	hook, err := s.db.GetWebhook(ctx, repoID, webhookID)
	if err != nil {
		return nil, err
	}
	hook.Events = parseWebhookEvents(hook.EventsCSV)
	return hook, nil
}

func (s *WebhookService) ListWebhooks(ctx context.Context, repoID int64) ([]models.Webhook, error) {
	hooks, err := s.db.ListWebhooks(ctx, repoID)
	if err != nil {
		return nil, err
	}
	for i := range hooks {
		hooks[i].Events = parseWebhookEvents(hooks[i].EventsCSV)
	}
	return hooks, nil
}

func (s *WebhookService) DeleteWebhook(ctx context.Context, repoID, webhookID int64) error {
	return s.db.DeleteWebhook(ctx, repoID, webhookID)
}

func (s *WebhookService) ListWebhookDeliveries(ctx context.Context, repoID, webhookID int64) ([]models.WebhookDelivery, error) {
	return s.db.ListWebhookDeliveries(ctx, repoID, webhookID)
}

func (s *WebhookService) Redeliver(ctx context.Context, repoID, webhookID, deliveryID int64) (*models.WebhookDelivery, error) {
	hook, err := s.db.GetWebhook(ctx, repoID, webhookID)
	if err != nil {
		return nil, err
	}
	prev, err := s.db.GetWebhookDelivery(ctx, repoID, webhookID, deliveryID)
	if err != nil {
		return nil, err
	}
	return s.deliverWithRetry(ctx, hook, prev.Event, []byte(prev.RequestBody), &prev.ID)
}

func (s *WebhookService) PingWebhook(ctx context.Context, repoID, webhookID int64) (*models.WebhookDelivery, error) {
	hook, err := s.db.GetWebhook(ctx, repoID, webhookID)
	if err != nil {
		return nil, err
	}
	payload := map[string]any{
		"zen":        "Keep it logically awesome.",
		"hook_id":    hook.ID,
		"repo_id":    repoID,
		"active":     hook.Active,
		"emitted_at": time.Now().UTC().Format(time.RFC3339),
	}
	body, _ := json.Marshal(payload)
	return s.deliverWithRetry(ctx, hook, "ping", body, nil)
}

func (s *WebhookService) EmitPullRequestEvent(ctx context.Context, repoID int64, action string, pr *models.PullRequest) error {
	payload := map[string]any{
		"action": action,
		"number": pr.Number,
		"pull_request": map[string]any{
			"id":            pr.ID,
			"number":        pr.Number,
			"title":         pr.Title,
			"body":          pr.Body,
			"state":         pr.State,
			"author_id":     pr.AuthorID,
			"author_name":   pr.AuthorName,
			"source_branch": pr.SourceBranch,
			"target_branch": pr.TargetBranch,
			"merge_commit":  pr.MergeCommit,
			"merge_method":  pr.MergeMethod,
			"created_at":    pr.CreatedAt,
			"merged_at":     pr.MergedAt,
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = s.emitRepoEvent(ctx, repoID, "pull_request", body)
	return err
}

func (s *WebhookService) EmitIssueEvent(ctx context.Context, repoID int64, action string, number int, title, body, state string) error {
	payload := map[string]any{
		"action": action,
		"number": number,
		"issue": map[string]any{
			"number": number,
			"title":  title,
			"body":   body,
			"state":  state,
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = s.emitRepoEvent(ctx, repoID, "issues", data)
	return err
}

func (s *WebhookService) emitRepoEvent(ctx context.Context, repoID int64, event string, body []byte) ([]*models.WebhookDelivery, error) {
	hooks, err := s.db.ListWebhooks(ctx, repoID)
	if err != nil {
		return nil, err
	}
	var deliveries []*models.WebhookDelivery
	for i := range hooks {
		h := hooks[i]
		if !h.Active {
			continue
		}
		if !webhookEventMatches(h.EventsCSV, event) {
			continue
		}
		d, err := s.deliverWithRetry(ctx, &h, event, body, nil)
		if err != nil {
			// Keep dispatching to other webhooks.
			continue
		}
		deliveries = append(deliveries, d)
	}
	return deliveries, nil
}

func (s *WebhookService) deliverWithRetry(ctx context.Context, hook *models.Webhook, event string, body []byte, redeliveryOf *int64) (*models.WebhookDelivery, error) {
	const maxAttempts = 3
	deliveryUID := randomDeliveryUID()
	var last *models.WebhookDelivery

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		start := time.Now()
		statusCode := 0
		respBody := ""
		errText := ""
		success := false

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, hook.URL, bytes.NewReader(body))
		if err != nil {
			errText = err.Error()
		} else {
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("User-Agent", "gothub-webhook/1.0")
			req.Header.Set("X-Gothub-Event", event)
			req.Header.Set("X-Gothub-Delivery", deliveryUID)
			if hook.Secret != "" {
				req.Header.Set("X-Hub-Signature-256", signBody(hook.Secret, body))
			}

			resp, err := s.client.Do(req)
			if err != nil {
				errText = err.Error()
			} else {
				statusCode = resp.StatusCode
				respBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 32*1024))
				resp.Body.Close()
				respBody = string(respBytes)
				success = statusCode >= 200 && statusCode < 300
				if !success && errText == "" {
					errText = fmt.Sprintf("unexpected status code %d", statusCode)
				}
			}
		}

		delivery := &models.WebhookDelivery{
			RepoID:         hook.RepoID,
			WebhookID:      hook.ID,
			Event:          event,
			DeliveryUID:    deliveryUID,
			Attempt:        attempt,
			StatusCode:     statusCode,
			Success:        success,
			Error:          errText,
			RequestBody:    string(body),
			ResponseBody:   respBody,
			DurationMS:     time.Since(start).Milliseconds(),
			RedeliveryOfID: redeliveryOf,
		}
		if err := s.db.CreateWebhookDelivery(ctx, delivery); err != nil {
			return nil, err
		}
		last = delivery
		if success {
			return delivery, nil
		}
		if attempt < maxAttempts {
			time.Sleep(time.Duration(attempt*100) * time.Millisecond)
		}
	}

	if last == nil {
		return nil, fmt.Errorf("delivery failed")
	}
	return last, fmt.Errorf("delivery failed after retries: %s", last.Error)
}

func normalizeWebhookEvents(events []string) []string {
	if len(events) == 0 {
		return []string{"*"}
	}
	seen := make(map[string]bool, len(events))
	out := make([]string, 0, len(events))
	for _, e := range events {
		e = strings.TrimSpace(strings.ToLower(e))
		if e == "" || seen[e] {
			continue
		}
		seen[e] = true
		out = append(out, e)
	}
	if len(out) == 0 {
		return []string{"*"}
	}
	return out
}

func parseWebhookEvents(csv string) []string {
	if strings.TrimSpace(csv) == "" {
		return []string{"*"}
	}
	return normalizeWebhookEvents(strings.Split(csv, ","))
}

func webhookEventMatches(eventsCSV, event string) bool {
	events := parseWebhookEvents(eventsCSV)
	event = strings.TrimSpace(strings.ToLower(event))
	for _, e := range events {
		if e == "*" || e == event {
			return true
		}
	}
	return false
}

func signBody(secret string, body []byte) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write(body)
	return "sha256=" + hex.EncodeToString(m.Sum(nil))
}

func randomDeliveryUID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("gothub-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}
