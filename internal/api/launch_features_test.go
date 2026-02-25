package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/odvcencio/gothub/internal/api"
	"github.com/odvcencio/gothub/internal/models"
)

func TestCreateRepoEnforcesPublicLaunchLimits(t *testing.T) {
	server, _ := setupTestServerWithOptions(t, api.ServerOptions{
		EnablePasswordAuth: true,
		RestrictToPublic:   true,
		MaxPublicRepos:     1,
	})
	ts := httptest.NewServer(server)
	defer ts.Close()

	token := registerAndGetToken(t, ts.URL, "alice")
	createRepo(t, ts.URL, token, "one", false)

	secondPublicReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/repos", bytes.NewBufferString(`{"name":"two","private":false}`))
	secondPublicReq.Header.Set("Authorization", "Bearer "+token)
	secondPublicReq.Header.Set("Content-Type", "application/json")
	secondPublicResp, err := http.DefaultClient.Do(secondPublicReq)
	if err != nil {
		t.Fatal(err)
	}
	defer secondPublicResp.Body.Close()
	if secondPublicResp.StatusCode != http.StatusForbidden {
		t.Fatalf("second public repo status = %d, want %d", secondPublicResp.StatusCode, http.StatusForbidden)
	}

	privateReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/repos", bytes.NewBufferString(`{"name":"private-one","private":true}`))
	privateReq.Header.Set("Authorization", "Bearer "+token)
	privateReq.Header.Set("Content-Type", "application/json")
	privateResp, err := http.DefaultClient.Do(privateReq)
	if err != nil {
		t.Fatal(err)
	}
	defer privateResp.Body.Close()
	if privateResp.StatusCode != http.StatusForbidden {
		t.Fatalf("private repo status = %d, want %d", privateResp.StatusCode, http.StatusForbidden)
	}
}

func TestRunnerTokenCanUpsertPRCheckRun(t *testing.T) {
	server, db := setupTestServer(t)
	ts := httptest.NewServer(server)
	defer ts.Close()

	token := registerAndGetToken(t, ts.URL, "alice")
	createRepo(t, ts.URL, token, "repo", false)

	ctx := context.Background()
	repo, err := db.GetRepository(ctx, "alice", "repo")
	if err != nil {
		t.Fatalf("GetRepository: %v", err)
	}
	user, err := db.GetUserByUsername(ctx, "alice")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	pr := &models.PullRequest{
		RepoID:       repo.ID,
		Title:        "runner checks",
		Body:         "",
		State:        models.PullRequestStateOpen,
		AuthorID:     user.ID,
		SourceBranch: "feature",
		TargetBranch: "main",
	}
	if err := db.CreatePullRequest(ctx, pr); err != nil {
		t.Fatalf("CreatePullRequest: %v", err)
	}

	createTokenReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/repos/alice/repo/runners/tokens", bytes.NewBufferString(`{"name":"local-runner","expires_in_hours":24}`))
	createTokenReq.Header.Set("Authorization", "Bearer "+token)
	createTokenReq.Header.Set("Content-Type", "application/json")
	createTokenResp, err := http.DefaultClient.Do(createTokenReq)
	if err != nil {
		t.Fatal(err)
	}
	defer createTokenResp.Body.Close()
	if createTokenResp.StatusCode != http.StatusCreated {
		t.Fatalf("create runner token status = %d, want %d", createTokenResp.StatusCode, http.StatusCreated)
	}
	var tokenPayload struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(createTokenResp.Body).Decode(&tokenPayload); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(tokenPayload.Token) == "" {
		t.Fatal("runner token response missing token")
	}

	upsertReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/repos/alice/repo/pulls/1/checks/runner", bytes.NewBufferString(`{"name":"ci/build","status":"completed","conclusion":"success"}`))
	upsertReq.Header.Set("Authorization", "Runner "+tokenPayload.Token)
	upsertReq.Header.Set("Content-Type", "application/json")
	upsertResp, err := http.DefaultClient.Do(upsertReq)
	if err != nil {
		t.Fatal(err)
	}
	defer upsertResp.Body.Close()
	if upsertResp.StatusCode != http.StatusOK {
		t.Fatalf("runner upsert check status = %d, want %d", upsertResp.StatusCode, http.StatusOK)
	}

	listResp, err := http.Get(ts.URL + "/api/v1/repos/alice/repo/pulls/1/checks")
	if err != nil {
		t.Fatal(err)
	}
	defer listResp.Body.Close()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("list checks status = %d, want %d", listResp.StatusCode, http.StatusOK)
	}
	var checks []map[string]any
	if err := json.NewDecoder(listResp.Body).Decode(&checks); err != nil {
		t.Fatal(err)
	}
	if len(checks) != 1 {
		t.Fatalf("checks length = %d, want 1", len(checks))
	}
}

func TestInterestSignupEndpoint(t *testing.T) {
	server, _ := setupTestServer(t)
	ts := httptest.NewServer(server)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/v1/interest-signups", "application/json", bytes.NewBufferString(`{"email":"dev@example.com","name":"Dev","source":"landing"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("interest signup status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}

	badResp, err := http.Post(ts.URL+"/api/v1/interest-signups", "application/json", bytes.NewBufferString(`{"email":"not-an-email"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer badResp.Body.Close()
	if badResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid interest signup status = %d, want %d", badResp.StatusCode, http.StatusBadRequest)
	}
}
