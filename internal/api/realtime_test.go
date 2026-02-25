package api_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRepoEventsStreamEmitsIssueOpened(t *testing.T) {
	server, _ := setupTestServer(t)
	ts := httptest.NewServer(server)
	defer ts.Close()

	token := registerAndGetToken(t, ts.URL, "alice")
	createRepo(t, ts.URL, token, "repo", false)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/v1/repos/alice/repo/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("events stream: expected 200, got %d", resp.StatusCode)
	}
	if !strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("events stream: expected text/event-stream, got %q", resp.Header.Get("Content-Type"))
	}

	createIssueBody := `{"title":"SSE issue","body":"check stream"}`
	createIssueReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/repos/alice/repo/issues", bytes.NewBufferString(createIssueBody))
	createIssueReq.Header.Set("Authorization", "Bearer "+token)
	createIssueReq.Header.Set("Content-Type", "application/json")
	createIssueResp, err := http.DefaultClient.Do(createIssueReq)
	if err != nil {
		t.Fatal(err)
	}
	createIssueResp.Body.Close()
	if createIssueResp.StatusCode != http.StatusCreated {
		t.Fatalf("create issue: expected 201, got %d", createIssueResp.StatusCode)
	}

	eventType, eventData := readSSEEvent(t, bufio.NewReader(resp.Body), 5*time.Second)
	if eventType != "issue.opened" {
		t.Fatalf("expected event type issue.opened, got %q", eventType)
	}

	var payload struct {
		Type    string         `json:"type"`
		RepoID  int64          `json:"repo_id"`
		Payload map[string]any `json:"payload"`
	}
	if err := json.Unmarshal(eventData, &payload); err != nil {
		t.Fatalf("decode event payload: %v", err)
	}
	if payload.Type != "issue.opened" {
		t.Fatalf("payload type: expected issue.opened, got %q", payload.Type)
	}
	if payload.RepoID == 0 {
		t.Fatal("payload repo_id: expected non-zero")
	}
	if got := payload.Payload["title"]; got != "SSE issue" {
		t.Fatalf("payload title: expected %q, got %#v", "SSE issue", got)
	}
}

func TestRepoEventsPrivateRepoAccessControl(t *testing.T) {
	server, _ := setupTestServer(t)
	ts := httptest.NewServer(server)
	defer ts.Close()

	aliceToken := registerAndGetToken(t, ts.URL, "alice")
	bobToken := registerAndGetToken(t, ts.URL, "bob")
	createRepo(t, ts.URL, aliceToken, "private-repo", true)

	anonResp, err := http.Get(ts.URL + "/api/v1/repos/alice/private-repo/events")
	if err != nil {
		t.Fatal(err)
	}
	anonResp.Body.Close()
	if anonResp.StatusCode != http.StatusNotFound {
		t.Fatalf("anonymous private events: expected 404, got %d", anonResp.StatusCode)
	}

	bobReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/repos/alice/private-repo/events", nil)
	bobReq.Header.Set("Authorization", "Bearer "+bobToken)
	bobResp, err := http.DefaultClient.Do(bobReq)
	if err != nil {
		t.Fatal(err)
	}
	bobResp.Body.Close()
	if bobResp.StatusCode != http.StatusNotFound {
		t.Fatalf("non-member private events: expected 404, got %d", bobResp.StatusCode)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	aliceReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/v1/repos/alice/private-repo/events", nil)
	aliceReq.Header.Set("Authorization", "Bearer "+aliceToken)
	aliceResp, err := http.DefaultClient.Do(aliceReq)
	if err != nil {
		t.Fatal(err)
	}
	defer aliceResp.Body.Close()
	if aliceResp.StatusCode != http.StatusOK {
		t.Fatalf("owner private events: expected 200, got %d", aliceResp.StatusCode)
	}
	if !strings.Contains(aliceResp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("owner private events content-type: expected text/event-stream, got %q", aliceResp.Header.Get("Content-Type"))
	}
}

func readSSEEvent(t *testing.T, reader *bufio.Reader, timeout time.Duration) (string, []byte) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var eventType string
	var dataLines []string

	for time.Now().Before(deadline) {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read SSE line: %v", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			if eventType != "" && len(dataLines) > 0 {
				return eventType, []byte(strings.Join(dataLines, "\n"))
			}
			eventType = ""
			dataLines = dataLines[:0]
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		if strings.HasPrefix(line, "event:") {
			eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}

	t.Fatalf("timed out waiting for SSE event after %s", timeout)
	return "", nil
}
