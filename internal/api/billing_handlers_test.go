package api_test

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/odvcencio/gothub/internal/api"
)

func TestPolarWebhookGrantsAndRevokesPrivateRepoEntitlement(t *testing.T) {
	const secret = "polar-test-secret-123"
	server, _ := setupTestServerWithOptions(t, api.ServerOptions{

		RequirePrivatePlan: true,
		PolarWebhookSecret: secret,
		PolarProductIDs:    []string{"prod_private"},
	})
	ts := httptest.NewServer(server)
	defer ts.Close()

	token := registerAndGetToken(t, ts.URL, "alice")

	if code := createRepoExpectStatus(t, ts.URL, token, "private-before", true); code != http.StatusForbidden {
		t.Fatalf("create private repo before entitlement status = %d, want %d", code, http.StatusForbidden)
	}

	activePayload := map[string]any{
		"type": "customer.state_changed",
		"data": map[string]any{
			"customer": map[string]any{
				"email": "alice@example.com",
			},
			"active_subscriptions": []any{
				map[string]any{
					"status":             "active",
					"product_id":         "prod_private",
					"current_period_end": "2030-01-01T00:00:00Z",
				},
			},
		},
	}
	sendPolarWebhook(t, ts.URL, secret, activePayload, http.StatusOK)

	if code := createRepoExpectStatus(t, ts.URL, token, "private-after-grant", true); code != http.StatusCreated {
		t.Fatalf("create private repo after entitlement status = %d, want %d", code, http.StatusCreated)
	}

	inactivePayload := map[string]any{
		"type": "customer.state_changed",
		"data": map[string]any{
			"customer": map[string]any{
				"email": "alice@example.com",
			},
			"active_subscriptions": []any{},
		},
	}
	sendPolarWebhook(t, ts.URL, secret, inactivePayload, http.StatusOK)

	if code := createRepoExpectStatus(t, ts.URL, token, "private-after-revoke", true); code != http.StatusForbidden {
		t.Fatalf("create private repo after entitlement revoke status = %d, want %d", code, http.StatusForbidden)
	}
}

func TestPolarWebhookRejectsInvalidSignature(t *testing.T) {
	const secret = "polar-test-secret-456"
	server, _ := setupTestServerWithOptions(t, api.ServerOptions{

		RequirePrivatePlan: true,
		PolarWebhookSecret: secret,
	})
	ts := httptest.NewServer(server)
	defer ts.Close()

	payload := map[string]any{
		"type": "customer.state_changed",
		"data": map[string]any{
			"customer": map[string]any{
				"email": "alice@example.com",
			},
			"active_subscriptions": []any{},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/billing/polar/webhook", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("webhook-id", "evt_invalid")
	req.Header.Set("webhook-timestamp", strconv.FormatInt(time.Now().Unix(), 10))
	req.Header.Set("webhook-signature", "v1,invalid")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("send webhook request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("invalid signature webhook status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func createRepoExpectStatus(t *testing.T, baseURL, token, name string, isPrivate bool) int {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"name":    name,
		"private": isPrivate,
	})
	req, _ := http.NewRequest(http.MethodPost, baseURL+"/api/v1/repos", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create repo request: %v", err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

func sendPolarWebhook(t *testing.T, baseURL, secret string, payload map[string]any, wantStatus int) {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	msgID := "evt_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	ts := time.Now().UTC()
	signature := signPolarWebhookTestPayload(t, secret, msgID, ts, body)

	req, _ := http.NewRequest(http.MethodPost, baseURL+"/api/v1/billing/polar/webhook", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("webhook-id", msgID)
	req.Header.Set("webhook-timestamp", strconv.FormatInt(ts.Unix(), 10))
	req.Header.Set("webhook-signature", signature)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("send webhook request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != wantStatus {
		t.Fatalf("webhook status = %d, want %d", resp.StatusCode, wantStatus)
	}
}

func signPolarWebhookTestPayload(t *testing.T, secret, msgID string, ts time.Time, body []byte) string {
	t.Helper()
	key := []byte(strings.TrimSpace(secret))
	if strings.HasPrefix(secret, "whsec_") {
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(strings.TrimSpace(secret), "whsec_"))
		if err != nil {
			t.Fatalf("decode webhook secret: %v", err)
		}
		key = decoded
	}
	canonical := fmt.Sprintf("%s.%d.%s", msgID, ts.Unix(), body)
	h := hmac.New(sha256.New, key)
	_, _ = h.Write([]byte(canonical))
	sig := make([]byte, base64.StdEncoding.EncodedLen(h.Size()))
	base64.StdEncoding.Encode(sig, h.Sum(nil))
	return "v1," + string(sig)
}
