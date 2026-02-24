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
	"net/url"
	"sort"
	"strings"
	"time"

	gitdiff "github.com/odvcencio/got/pkg/diff"
	"github.com/odvcencio/got/pkg/object"
	"github.com/odvcencio/gothub/internal/database"
	"github.com/odvcencio/gothub/internal/gotstore"
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
	u, err := url.Parse(hook.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("url must be a valid HTTP or HTTPS URL")
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

func (s *WebhookService) ListWebhooksPage(ctx context.Context, repoID int64, page, perPage int) ([]models.Webhook, error) {
	limit, offset := normalizePage(page, perPage, 50, 200)
	hooks, err := s.db.ListWebhooksPage(ctx, repoID, limit, offset)
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

func (s *WebhookService) ListWebhookDeliveriesPage(ctx context.Context, repoID, webhookID int64, page, perPage int) ([]models.WebhookDelivery, error) {
	limit, offset := normalizePage(page, perPage, 50, 200)
	return s.db.ListWebhookDeliveriesPage(ctx, repoID, webhookID, limit, offset)
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
		"entities_changed":  []map[string]string{},
		"entities_added":    0,
		"entities_removed":  0,
		"entities_modified": 0,
	}
	if summary, err := s.computePREntityChanges(ctx, repoID, pr); err == nil {
		payload["entities_changed"] = summary.Changes
		payload["entities_added"] = summary.Added
		payload["entities_removed"] = summary.Removed
		payload["entities_modified"] = summary.Modified
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = s.emitRepoEvent(ctx, repoID, "pull_request", body)
	return err
}

type prEntityChangeSummary struct {
	Changes  []map[string]string
	Added    int
	Removed  int
	Modified int
}

func (s *WebhookService) computePREntityChanges(ctx context.Context, repoID int64, pr *models.PullRequest) (*prEntityChangeSummary, error) {
	repo, err := s.db.GetRepositoryByID(ctx, repoID)
	if err != nil {
		return nil, err
	}
	if repo.StoragePath == "" || repo.StoragePath == "pending" {
		return nil, fmt.Errorf("repo storage path unavailable")
	}
	store, err := gotstore.Open(repo.StoragePath)
	if err != nil {
		return nil, err
	}

	srcHash, err := store.Refs.Get("heads/" + pr.SourceBranch)
	if err != nil {
		return nil, err
	}
	tgtHash, err := store.Refs.Get("heads/" + pr.TargetBranch)
	if err != nil {
		return nil, err
	}
	srcCommit, err := store.Objects.ReadCommit(srcHash)
	if err != nil {
		return nil, err
	}
	tgtCommit, err := store.Objects.ReadCommit(tgtHash)
	if err != nil {
		return nil, err
	}

	srcFiles, err := flattenTree(store.Objects, srcCommit.TreeHash, "")
	if err != nil {
		return nil, err
	}
	tgtFiles, err := flattenTree(store.Objects, tgtCommit.TreeHash, "")
	if err != nil {
		return nil, err
	}

	srcMap := make(map[string]FileEntry, len(srcFiles))
	for _, f := range srcFiles {
		srcMap[f.Path] = f
	}
	tgtMap := make(map[string]FileEntry, len(tgtFiles))
	for _, f := range tgtFiles {
		tgtMap[f.Path] = f
	}

	pathSet := make(map[string]struct{}, len(srcMap)+len(tgtMap))
	for path, srcEntry := range srcMap {
		if tgtEntry, ok := tgtMap[path]; !ok || tgtEntry.BlobHash != srcEntry.BlobHash {
			pathSet[path] = struct{}{}
		}
	}
	for path := range tgtMap {
		if _, ok := srcMap[path]; !ok {
			pathSet[path] = struct{}{}
		}
	}

	paths := make([]string, 0, len(pathSet))
	for path := range pathSet {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	summary := &prEntityChangeSummary{
		Changes: make([]map[string]string, 0),
	}
	for _, path := range paths {
		srcEntry, hasSrc := srcMap[path]
		tgtEntry, hasTgt := tgtMap[path]
		var srcData, tgtData []byte
		if hasSrc && srcEntry.BlobHash != "" {
			srcData, err = readBlobData(store.Objects, object.Hash(srcEntry.BlobHash))
			if err != nil {
				continue
			}
		}
		if hasTgt && tgtEntry.BlobHash != "" {
			tgtData, err = readBlobData(store.Objects, object.Hash(tgtEntry.BlobHash))
			if err != nil {
				continue
			}
		}
		fd, err := gitdiff.DiffFiles(path, tgtData, srcData)
		if err != nil {
			continue
		}
		for _, c := range fd.Changes {
			changeType := changeTypeNames[c.Type]
			switch changeType {
			case "added":
				summary.Added++
			case "removed":
				summary.Removed++
			case "modified":
				summary.Modified++
			}
			summary.Changes = append(summary.Changes, map[string]string{
				"file": path,
				"type": changeType,
				"key":  c.Key,
			})
		}
	}
	return summary, nil
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
			time.Sleep(time.Duration(1<<uint(attempt-1)) * time.Second)
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
