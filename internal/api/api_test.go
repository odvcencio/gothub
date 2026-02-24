package api_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/odvcencio/gothub/internal/api"
	"github.com/odvcencio/gothub/internal/auth"
	"github.com/odvcencio/gothub/internal/database"
	"github.com/odvcencio/gothub/internal/gitinterop"
	"github.com/odvcencio/gothub/internal/service"
)

func setupTestServer(t *testing.T) (*api.Server, database.DB) {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test.db"
	storagePath := tmpDir + "/repos"

	db, err := database.OpenSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	authSvc := auth.NewService("test-secret", 24*time.Hour)
	repoSvc := service.NewRepoService(db, storagePath)
	server := api.NewServer(db, authSvc, repoSvc)
	return server, db
}

func TestRegisterAndLogin(t *testing.T) {
	server, _ := setupTestServer(t)
	ts := httptest.NewServer(server)
	defer ts.Close()

	// Register
	body := `{"username":"alice","email":"alice@example.com","password":"secret123"}`
	resp, err := http.Post(ts.URL+"/api/v1/auth/register", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register: expected 201, got %d", resp.StatusCode)
	}
	var regResp struct {
		Token string `json:"token"`
		User  struct {
			ID       int64  `json:"id"`
			Username string `json:"username"`
		} `json:"user"`
	}
	json.NewDecoder(resp.Body).Decode(&regResp)
	resp.Body.Close()

	if regResp.Token == "" {
		t.Fatal("expected token in register response")
	}
	if regResp.User.Username != "alice" {
		t.Fatalf("expected username alice, got %s", regResp.User.Username)
	}

	// Login
	body = `{"username":"alice","password":"secret123"}`
	resp, err = http.Post(ts.URL+"/api/v1/auth/login", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login: expected 200, got %d", resp.StatusCode)
	}
	var loginResp struct {
		Token string `json:"token"`
	}
	json.NewDecoder(resp.Body).Decode(&loginResp)
	resp.Body.Close()
	if loginResp.Token == "" {
		t.Fatal("expected token in login response")
	}

	// Get current user with token
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/user", nil)
	req.Header.Set("Authorization", "Bearer "+loginResp.Token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get user: expected 200, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestCreateAndGetRepo(t *testing.T) {
	server, db := setupTestServer(t)
	ts := httptest.NewServer(server)
	defer ts.Close()

	// Register user
	body := `{"username":"bob","email":"bob@example.com","password":"secret123"}`
	resp, _ := http.Post(ts.URL+"/api/v1/auth/register", "application/json", bytes.NewBufferString(body))
	var regResp struct {
		Token string `json:"token"`
	}
	json.NewDecoder(resp.Body).Decode(&regResp)
	resp.Body.Close()

	// Create repo
	body = `{"name":"myrepo","description":"A test repo"}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/repos", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+regResp.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create repo: expected 201, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Get repo
	resp, err = http.Get(ts.URL + "/api/v1/repos/bob/myrepo")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get repo: expected 200, got %d", resp.StatusCode)
	}
	var repoResp struct {
		Name          string `json:"name"`
		DefaultBranch string `json:"default_branch"`
	}
	json.NewDecoder(resp.Body).Decode(&repoResp)
	resp.Body.Close()
	if repoResp.Name != "myrepo" {
		t.Fatalf("expected repo name myrepo, got %s", repoResp.Name)
	}
	if repoResp.DefaultBranch != "main" {
		t.Fatalf("expected default branch main, got %s", repoResp.DefaultBranch)
	}

	// Verify storage path was persisted and not left as "pending".
	storedRepo, err := db.GetRepository(context.Background(), "bob", "myrepo")
	if err != nil {
		t.Fatalf("get repo from db: %v", err)
	}
	if storedRepo.StoragePath == "" || storedRepo.StoragePath == "pending" {
		t.Fatalf("expected persisted storage path, got %q", storedRepo.StoragePath)
	}
}

func TestUnauthenticatedAccess(t *testing.T) {
	server, _ := setupTestServer(t)
	ts := httptest.NewServer(server)
	defer ts.Close()

	// Accessing protected endpoint without token should return 401
	resp, err := http.Get(ts.URL + "/api/v1/user")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Creating repo without auth should fail
	body := `{"name":"nope"}`
	resp, err = http.Post(ts.URL+"/api/v1/repos", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestProtocolAuthForPrivateRepo(t *testing.T) {
	server, _ := setupTestServer(t)
	ts := httptest.NewServer(server)
	defer ts.Close()

	// Owner account + private repo.
	regBody := `{"username":"alice","email":"alice@example.com","password":"secret123"}`
	resp, err := http.Post(ts.URL+"/api/v1/auth/register", "application/json", bytes.NewBufferString(regBody))
	if err != nil {
		t.Fatal(err)
	}
	var regResp struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&regResp); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if regResp.Token == "" {
		t.Fatal("expected token in register response")
	}

	createBody := `{"name":"private-repo","description":"","private":true}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/repos", bytes.NewBufferString(createBody))
	req.Header.Set("Authorization", "Bearer "+regResp.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create private repo: expected 201, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Anonymous protocol read on private repo must be rejected.
	resp, err = http.Get(ts.URL + "/git/alice/private-repo/info/refs?service=git-upload-pack")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous git info/refs: expected 401, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Owner can read via Basic auth.
	req, _ = http.NewRequest("GET", ts.URL+"/git/alice/private-repo/info/refs?service=git-upload-pack", nil)
	req.SetBasicAuth("alice", "secret123")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("owner git info/refs: expected 200, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Anonymous got-protocol read on private repo must be rejected.
	resp, err = http.Get(ts.URL + "/got/alice/private-repo/refs")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous got refs: expected 401, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestPrivateRepoReadAccessAcrossAPI(t *testing.T) {
	server, _ := setupTestServer(t)
	ts := httptest.NewServer(server)
	defer ts.Close()

	aliceToken := registerAndGetToken(t, ts.URL, "alice")
	bobToken := registerAndGetToken(t, ts.URL, "bob")

	// Alice creates a private repo.
	createBody := `{"name":"secret","description":"","private":true}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/repos", bytes.NewBufferString(createBody))
	req.Header.Set("Authorization", "Bearer "+aliceToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create private repo: expected 201, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Anonymous cannot discover/read private repo metadata.
	resp, err = http.Get(ts.URL + "/api/v1/repos/alice/secret")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("anonymous get private repo: expected 404, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Unrelated authenticated user also cannot read it.
	req, _ = http.NewRequest("GET", ts.URL+"/api/v1/repos/alice/secret", nil)
	req.Header.Set("Authorization", "Bearer "+bobToken)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("non-member get private repo: expected 404, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Owner can read metadata.
	req, _ = http.NewRequest("GET", ts.URL+"/api/v1/repos/alice/secret", nil)
	req.Header.Set("Authorization", "Bearer "+aliceToken)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("owner get private repo: expected 200, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// PR listing is also protected.
	resp, err = http.Get(ts.URL + "/api/v1/repos/alice/secret/pulls")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("anonymous list private PRs: expected 404, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	req, _ = http.NewRequest("GET", ts.URL+"/api/v1/repos/alice/secret/pulls", nil)
	req.Header.Set("Authorization", "Bearer "+bobToken)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("non-member list private PRs: expected 404, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	req, _ = http.NewRequest("GET", ts.URL+"/api/v1/repos/alice/secret/pulls", nil)
	req.Header.Set("Authorization", "Bearer "+aliceToken)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("owner list private PRs: expected 200, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestWriteAccessRequiresRepoPermission(t *testing.T) {
	server, _ := setupTestServer(t)
	ts := httptest.NewServer(server)
	defer ts.Close()

	aliceToken := registerAndGetToken(t, ts.URL, "alice")
	bobToken := registerAndGetToken(t, ts.URL, "bob")

	// Alice creates a public repo.
	createRepoReq := `{"name":"pub","description":"","private":false}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/repos", bytes.NewBufferString(createRepoReq))
	req.Header.Set("Authorization", "Bearer "+aliceToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create repo: expected 201, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Anonymous can read PR list on public repo.
	resp, err = http.Get(ts.URL + "/api/v1/repos/alice/pub/pulls")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("anonymous list public PRs: expected 200, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Unrelated authenticated user cannot create PR (write access denied).
	createPRReq := `{"title":"test","source_branch":"feat","target_branch":"main"}`
	req, _ = http.NewRequest("POST", ts.URL+"/api/v1/repos/alice/pub/pulls", bytes.NewBufferString(createPRReq))
	req.Header.Set("Authorization", "Bearer "+bobToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("non-member create PR on public repo: expected 403, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestCollaboratorWriteAccessLifecycle(t *testing.T) {
	server, _ := setupTestServer(t)
	ts := httptest.NewServer(server)
	defer ts.Close()

	aliceToken := registerAndGetToken(t, ts.URL, "alice")
	bobToken := registerAndGetToken(t, ts.URL, "bob")

	createRepo(t, ts.URL, aliceToken, "repo", false)

	// Bob cannot create PR before being added.
	prBody := `{"title":"test","source_branch":"feature","target_branch":"main"}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/repos/alice/repo/pulls", bytes.NewBufferString(prBody))
	req.Header.Set("Authorization", "Bearer "+bobToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("pre-collab create PR: expected 403, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	addBody := `{"username":"bob","role":"write"}`
	req, _ = http.NewRequest("POST", ts.URL+"/api/v1/repos/alice/repo/collaborators", bytes.NewBufferString(addBody))
	req.Header.Set("Authorization", "Bearer "+aliceToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("add collaborator: expected 201, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	resp, err = http.Get(ts.URL + "/api/v1/repos/alice/repo/collaborators")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list collaborators: expected 200, got %d", resp.StatusCode)
	}
	var collabs []struct {
		Username string `json:"username"`
		Role     string `json:"role"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&collabs); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(collabs) == 0 || collabs[0].Username != "bob" || collabs[0].Role != "write" {
		t.Fatalf("unexpected collaborators response: %+v", collabs)
	}

	// Bob can now create PR.
	req, _ = http.NewRequest("POST", ts.URL+"/api/v1/repos/alice/repo/pulls", bytes.NewBufferString(prBody))
	req.Header.Set("Authorization", "Bearer "+bobToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("post-collab create PR: expected 201, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	req, _ = http.NewRequest("DELETE", ts.URL+"/api/v1/repos/alice/repo/collaborators/bob", nil)
	req.Header.Set("Authorization", "Bearer "+aliceToken)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("remove collaborator: expected 204, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Bob is blocked again after removal.
	req, _ = http.NewRequest("POST", ts.URL+"/api/v1/repos/alice/repo/pulls", bytes.NewBufferString(prBody))
	req.Header.Set("Authorization", "Bearer "+bobToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("post-removal create PR: expected 403, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestIssueLifecycleAndComments(t *testing.T) {
	server, _ := setupTestServer(t)
	ts := httptest.NewServer(server)
	defer ts.Close()

	token := registerAndGetToken(t, ts.URL, "alice")
	createRepo(t, ts.URL, token, "repo", false)

	createIssueBody := `{"title":"Issue one","body":"description"}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/repos/alice/repo/issues", bytes.NewBufferString(createIssueBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create issue: expected 201, got %d", resp.StatusCode)
	}
	var issue struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		State  string `json:"state"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&issue); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if issue.Number == 0 || issue.Title != "Issue one" || issue.State != "open" {
		t.Fatalf("unexpected created issue: %+v", issue)
	}

	resp, err = http.Get(ts.URL + "/api/v1/repos/alice/repo/issues?state=open")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list issues: expected 200, got %d", resp.StatusCode)
	}
	var issues []struct {
		Number int `json:"number"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&issues); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(issues) != 1 || issues[0].Number != issue.Number {
		t.Fatalf("unexpected issues list: %+v", issues)
	}

	resp, err = http.Get(fmt.Sprintf("%s/api/v1/repos/alice/repo/issues/%d", ts.URL, issue.Number))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get issue: expected 200, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	updateBody := `{"state":"closed","title":"Issue one closed"}`
	req, _ = http.NewRequest("PATCH", fmt.Sprintf("%s/api/v1/repos/alice/repo/issues/%d", ts.URL, issue.Number), bytes.NewBufferString(updateBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update issue: expected 200, got %d", resp.StatusCode)
	}
	var updated struct {
		State string `json:"state"`
		Title string `json:"title"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&updated); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if updated.State != "closed" || updated.Title != "Issue one closed" {
		t.Fatalf("unexpected updated issue: %+v", updated)
	}

	commentBody := `{"body":"first comment"}`
	req, _ = http.NewRequest("POST", fmt.Sprintf("%s/api/v1/repos/alice/repo/issues/%d/comments", ts.URL, issue.Number), bytes.NewBufferString(commentBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create issue comment: expected 201, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	resp, err = http.Get(fmt.Sprintf("%s/api/v1/repos/alice/repo/issues/%d/comments", ts.URL, issue.Number))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list issue comments: expected 200, got %d", resp.StatusCode)
	}
	var comments []struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&comments); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(comments) != 1 || comments[0].Body != "first comment" {
		t.Fatalf("unexpected issue comments: %+v", comments)
	}
}

func TestRepositoryStarLifecycle(t *testing.T) {
	server, _ := setupTestServer(t)
	ts := httptest.NewServer(server)
	defer ts.Close()

	aliceToken := registerAndGetToken(t, ts.URL, "alice")
	bobToken := registerAndGetToken(t, ts.URL, "bob")
	createRepo(t, ts.URL, aliceToken, "repo", false)

	resp, err := http.Get(ts.URL + "/api/v1/repos/alice/repo/stars")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get stars: expected 200, got %d", resp.StatusCode)
	}
	var starsResp struct {
		Count   int  `json:"count"`
		Starred bool `json:"starred"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&starsResp); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if starsResp.Count != 0 || starsResp.Starred {
		t.Fatalf("expected empty star state, got %+v", starsResp)
	}

	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/repos/alice/repo/star", nil)
	req.Header.Set("Authorization", "Bearer "+bobToken)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("star repo: expected 200, got %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&starsResp); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if starsResp.Count != 1 || !starsResp.Starred {
		t.Fatalf("expected starred=true with count=1, got %+v", starsResp)
	}

	req, _ = http.NewRequest("GET", ts.URL+"/api/v1/repos/alice/repo/stars", nil)
	req.Header.Set("Authorization", "Bearer "+bobToken)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get stars (bob): expected 200, got %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&starsResp); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if starsResp.Count != 1 || !starsResp.Starred {
		t.Fatalf("expected bob to see starred=true with count=1, got %+v", starsResp)
	}

	req, _ = http.NewRequest("GET", ts.URL+"/api/v1/repos/alice/repo/stargazers", nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list stargazers: expected 200, got %d", resp.StatusCode)
	}
	var stargazers []struct {
		ID       int64  `json:"id"`
		Username string `json:"username"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&stargazers); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(stargazers) != 1 || stargazers[0].Username != "bob" {
		t.Fatalf("expected bob as only stargazer, got %+v", stargazers)
	}

	req, _ = http.NewRequest("GET", ts.URL+"/api/v1/user/starred", nil)
	req.Header.Set("Authorization", "Bearer "+bobToken)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list user starred repos: expected 200, got %d", resp.StatusCode)
	}
	var starredRepos []struct {
		OwnerName string `json:"owner_name"`
		Name      string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&starredRepos); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(starredRepos) != 1 || starredRepos[0].OwnerName != "alice" || starredRepos[0].Name != "repo" {
		t.Fatalf("unexpected starred repos response: %+v", starredRepos)
	}

	req, _ = http.NewRequest("DELETE", ts.URL+"/api/v1/repos/alice/repo/star", nil)
	req.Header.Set("Authorization", "Bearer "+bobToken)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unstar repo: expected 200, got %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&starsResp); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if starsResp.Count != 0 || starsResp.Starred {
		t.Fatalf("expected starred=false with count=0 after unstar, got %+v", starsResp)
	}
}

func TestBranchesEndpointAndPRAuthorName(t *testing.T) {
	server, _ := setupTestServer(t)
	ts := httptest.NewServer(server)
	defer ts.Close()

	aliceToken := registerAndGetToken(t, ts.URL, "alice")

	// Create public repo.
	createRepoReq := `{"name":"repo","description":"","private":false}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/repos", bytes.NewBufferString(createRepoReq))
	req.Header.Set("Authorization", "Bearer "+aliceToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create repo: expected 201, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Branch list should work and return JSON array.
	resp, err = http.Get(ts.URL + "/api/v1/repos/alice/repo/branches")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list branches: expected 200, got %d", resp.StatusCode)
	}
	var branches []string
	if err := json.NewDecoder(resp.Body).Decode(&branches); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	// Create PR as owner and verify author_name is present in list response.
	createPRReq := `{"title":"My PR","source_branch":"feature","target_branch":"main"}`
	req, _ = http.NewRequest("POST", ts.URL+"/api/v1/repos/alice/repo/pulls", bytes.NewBufferString(createPRReq))
	req.Header.Set("Authorization", "Bearer "+aliceToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create PR: expected 201, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	resp, err = http.Get(ts.URL + "/api/v1/repos/alice/repo/pulls")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list PRs: expected 200, got %d", resp.StatusCode)
	}
	var prs []struct {
		AuthorName string `json:"author_name"`
		Title      string `json:"title"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&prs); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(prs) == 0 {
		t.Fatal("expected at least one PR in list")
	}
	if prs[0].AuthorName != "alice" {
		t.Fatalf("expected author_name alice, got %q", prs[0].AuthorName)
	}
}

func TestGitUploadPackAdvertisementNoSidebandCapability(t *testing.T) {
	server, _ := setupTestServer(t)
	ts := httptest.NewServer(server)
	defer ts.Close()

	token := registerAndGetToken(t, ts.URL, "alice")
	createRepo(t, ts.URL, token, "repo", false)

	resp, err := http.Get(ts.URL + "/git/alice/repo/info/refs?service=git-upload-pack")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("git info/refs: expected 200, got %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	bodyStr := string(body)

	if !strings.Contains(bodyStr, "# service=git-upload-pack\n") {
		t.Fatalf("expected upload-pack service announcement, got body %q", bodyStr)
	}
	if strings.Contains(bodyStr, "side-band-64k") {
		t.Fatalf("did not expect side-band-64k capability in advertisement: %q", bodyStr)
	}
	if !strings.Contains(bodyStr, "report-status delete-refs ofs-delta") {
		t.Fatalf("expected base capabilities, got body %q", bodyStr)
	}
}

func TestReceivePackReportsActualRefStatus(t *testing.T) {
	server, _ := setupTestServer(t)
	ts := httptest.NewServer(server)
	defer ts.Close()

	token := registerAndGetToken(t, ts.URL, "owner")
	createRepo(t, ts.URL, token, "repo", false)

	refName := "refs/heads/feature"
	line := fmt.Sprintf("%s %s %s\x00report-status\n", strings.Repeat("0", 40), strings.Repeat("1", 40), refName)
	payload := append(pktLineForTest(line), pktFlushForTest()...)

	req, _ := http.NewRequest("POST", ts.URL+"/git/owner/repo/git-receive-pack", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/x-git-receive-pack-request")
	req.SetBasicAuth("owner", "secret123")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("git receive-pack: expected 200, got %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	bodyStr := string(body)

	if !strings.Contains(bodyStr, "unpack ok\n") {
		t.Fatalf("expected unpack ok in receive-pack result, got body %q", bodyStr)
	}
	expected := "ng " + refName + " missing object mapping\n"
	if !strings.Contains(bodyStr, expected) {
		t.Fatalf("expected ref-specific ng status %q, got body %q", expected, bodyStr)
	}
	if strings.Contains(bodyStr, "ok refs/heads/main\n") {
		t.Fatalf("unexpected hardcoded main ref status in response: %q", bodyStr)
	}
}

func TestGitReceivePackPersistsEntityListHash(t *testing.T) {
	server, _ := setupTestServer(t)
	ts := httptest.NewServer(server)
	defer ts.Close()

	token := registerAndGetToken(t, ts.URL, "owner")
	createRepo(t, ts.URL, token, "repo", false)

	blobData := []byte("package main\n\nfunc ProcessOrder() int { return 1 }\n")
	blobHash := gitinterop.GitHashBytes(gitinterop.GitTypeBlob, blobData)

	var treeBuf bytes.Buffer
	fmt.Fprintf(&treeBuf, "100644 main.go\x00")
	blobRaw, err := hex.DecodeString(string(blobHash))
	if err != nil {
		t.Fatal(err)
	}
	treeBuf.Write(blobRaw)
	treeData := treeBuf.Bytes()
	treeHash := gitinterop.GitHashBytes(gitinterop.GitTypeTree, treeData)

	commitData := []byte(fmt.Sprintf(
		"tree %s\nauthor Owner <owner@example.com> 1700000000 +0000\ncommitter Owner <owner@example.com> 1700000000 +0000\n\ninitial\n",
		treeHash,
	))
	commitHash := gitinterop.GitHashBytes(gitinterop.GitTypeCommit, commitData)

	packData, err := gitinterop.BuildPackfile([]gitinterop.PackfileObject{
		{Type: gitinterop.OBJ_BLOB, Data: blobData},
		{Type: gitinterop.OBJ_TREE, Data: treeData},
		{Type: gitinterop.OBJ_COMMIT, Data: commitData},
	})
	if err != nil {
		t.Fatal(err)
	}

	updateLine := fmt.Sprintf("%s %s refs/heads/main\x00report-status\n", strings.Repeat("0", 40), commitHash)
	payload := append(pktLineForTest(updateLine), pktFlushForTest()...)
	payload = append(payload, packData...)

	req, _ := http.NewRequest("POST", ts.URL+"/git/owner/repo/git-receive-pack", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/x-git-receive-pack-request")
	req.SetBasicAuth("owner", "secret123")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("git receive-pack: expected 200, got %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "ok refs/heads/main\n") {
		t.Fatalf("expected ok status for refs/heads/main, got body %q", string(body))
	}

	resp, err = http.Get(ts.URL + "/api/v1/repos/owner/repo/tree/main")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list tree: expected 200, got %d", resp.StatusCode)
	}
	var entries []struct {
		Name           string `json:"name"`
		EntityListHash string `json:"entity_list_hash"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(entries) != 1 || entries[0].Name != "main.go" {
		t.Fatalf("expected single main.go entry, got %+v", entries)
	}
	if entries[0].EntityListHash == "" {
		t.Fatalf("expected persisted entity_list_hash on main.go, got %+v", entries[0])
	}
}

func TestGitReceivePackSupportsSubmoduleTreeEntries(t *testing.T) {
	server, db := setupTestServer(t)
	ts := httptest.NewServer(server)
	defer ts.Close()

	token := registerAndGetToken(t, ts.URL, "owner")
	createRepo(t, ts.URL, token, "repo", false)

	submoduleGitHash := strings.Repeat("2", 40)
	submoduleRaw, err := hex.DecodeString(submoduleGitHash)
	if err != nil {
		t.Fatal(err)
	}

	var treeBuf bytes.Buffer
	fmt.Fprintf(&treeBuf, "160000 module\x00")
	treeBuf.Write(submoduleRaw)
	treeData := treeBuf.Bytes()
	treeHash := gitinterop.GitHashBytes(gitinterop.GitTypeTree, treeData)

	commitData := []byte(fmt.Sprintf(
		"tree %s\nauthor Owner <owner@example.com> 1700000000 +0000\ncommitter Owner <owner@example.com> 1700000000 +0000\n\ninitial\n",
		treeHash,
	))
	commitHash := gitinterop.GitHashBytes(gitinterop.GitTypeCommit, commitData)

	packData, err := gitinterop.BuildPackfile([]gitinterop.PackfileObject{
		{Type: gitinterop.OBJ_TREE, Data: treeData},
		{Type: gitinterop.OBJ_COMMIT, Data: commitData},
	})
	if err != nil {
		t.Fatal(err)
	}

	updateLine := fmt.Sprintf("%s %s refs/heads/main\x00report-status\n", strings.Repeat("0", 40), commitHash)
	payload := append(pktLineForTest(updateLine), pktFlushForTest()...)
	payload = append(payload, packData...)

	req, _ := http.NewRequest("POST", ts.URL+"/git/owner/repo/git-receive-pack", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/x-git-receive-pack-request")
	req.SetBasicAuth("owner", "secret123")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("git receive-pack: expected 200, got %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "ok refs/heads/main\n") {
		t.Fatalf("expected ok status for refs/heads/main, got body %q", string(body))
	}

	repo, err := db.GetRepository(context.Background(), "owner", "repo")
	if err != nil {
		t.Fatal(err)
	}
	gotHash, err := db.GetGotHash(context.Background(), repo.ID, submoduleGitHash)
	if err != nil {
		t.Fatalf("expected synthetic mapping for submodule git hash, got error: %v", err)
	}
	if strings.TrimSpace(gotHash) == "" {
		t.Fatal("expected non-empty got hash for submodule mapping")
	}
}

func TestCodeIntelPersistsCommitIndex(t *testing.T) {
	server, db := setupTestServer(t)
	ts := httptest.NewServer(server)
	defer ts.Close()

	token := registerAndGetToken(t, ts.URL, "owner")
	createRepo(t, ts.URL, token, "repo", false)

	blobData := []byte("package main\n\nfunc ProcessOrder() int { return 1 }\n")
	blobHash := gitinterop.GitHashBytes(gitinterop.GitTypeBlob, blobData)

	var treeBuf bytes.Buffer
	fmt.Fprintf(&treeBuf, "100644 main.go\x00")
	blobRaw, err := hex.DecodeString(string(blobHash))
	if err != nil {
		t.Fatal(err)
	}
	treeBuf.Write(blobRaw)
	treeData := treeBuf.Bytes()
	treeHash := gitinterop.GitHashBytes(gitinterop.GitTypeTree, treeData)

	commitData := []byte(fmt.Sprintf(
		"tree %s\nauthor Owner <owner@example.com> 1700000000 +0000\ncommitter Owner <owner@example.com> 1700000000 +0000\n\ninitial\n",
		treeHash,
	))
	commitHash := gitinterop.GitHashBytes(gitinterop.GitTypeCommit, commitData)

	packData, err := gitinterop.BuildPackfile([]gitinterop.PackfileObject{
		{Type: gitinterop.OBJ_BLOB, Data: blobData},
		{Type: gitinterop.OBJ_TREE, Data: treeData},
		{Type: gitinterop.OBJ_COMMIT, Data: commitData},
	})
	if err != nil {
		t.Fatal(err)
	}

	updateLine := fmt.Sprintf("%s %s refs/heads/main\x00report-status\n", strings.Repeat("0", 40), commitHash)
	payload := append(pktLineForTest(updateLine), pktFlushForTest()...)
	payload = append(payload, packData...)

	req, _ := http.NewRequest("POST", ts.URL+"/git/owner/repo/git-receive-pack", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/x-git-receive-pack-request")
	req.SetBasicAuth("owner", "secret123")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("git receive-pack: expected 200, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	resp, err = http.Get(ts.URL + "/api/v1/repos/owner/repo/symbols/main")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("symbols endpoint: expected 200, got %d", resp.StatusCode)
	}
	var symbols []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&symbols); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(symbols) == 0 {
		t.Fatal("expected at least one symbol from indexed go file")
	}

	repo, err := db.GetRepository(context.Background(), "owner", "repo")
	if err != nil {
		t.Fatal(err)
	}
	gotCommitHash, err := db.GetGotHash(context.Background(), repo.ID, string(commitHash))
	if err != nil {
		t.Fatal(err)
	}
	indexHash, err := db.GetCommitIndex(context.Background(), repo.ID, gotCommitHash)
	if err != nil {
		t.Fatalf("expected persisted commit index mapping, got error: %v", err)
	}
	if strings.TrimSpace(indexHash) == "" {
		t.Fatal("expected non-empty persisted index hash")
	}
}

func TestEntityHistoryEndpointReturnsMatchesAcrossCommitHistory(t *testing.T) {
	server, _ := setupTestServer(t)
	ts := httptest.NewServer(server)
	defer ts.Close()

	token := registerAndGetToken(t, ts.URL, "owner")
	createRepo(t, ts.URL, token, "repo", false)

	blobData := []byte("package main\n\nfunc ProcessOrder() int { return 1 }\n")
	blobHash := gitinterop.GitHashBytes(gitinterop.GitTypeBlob, blobData)

	var treeBuf bytes.Buffer
	fmt.Fprintf(&treeBuf, "100644 main.go\x00")
	blobRaw, err := hex.DecodeString(string(blobHash))
	if err != nil {
		t.Fatal(err)
	}
	treeBuf.Write(blobRaw)
	treeData := treeBuf.Bytes()
	treeHash := gitinterop.GitHashBytes(gitinterop.GitTypeTree, treeData)

	commit1Data := []byte(fmt.Sprintf(
		"tree %s\nauthor Owner <owner@example.com> 1700000000 +0000\ncommitter Owner <owner@example.com> 1700000000 +0000\n\ninitial\n",
		treeHash,
	))
	commit1Hash := gitinterop.GitHashBytes(gitinterop.GitTypeCommit, commit1Data)

	commit2Data := []byte(fmt.Sprintf(
		"tree %s\nparent %s\nauthor Owner <owner@example.com> 1700000100 +0000\ncommitter Owner <owner@example.com> 1700000100 +0000\n\nsecond\n",
		treeHash, commit1Hash,
	))
	commit2Hash := gitinterop.GitHashBytes(gitinterop.GitTypeCommit, commit2Data)

	packData, err := gitinterop.BuildPackfile([]gitinterop.PackfileObject{
		{Type: gitinterop.OBJ_BLOB, Data: blobData},
		{Type: gitinterop.OBJ_TREE, Data: treeData},
		{Type: gitinterop.OBJ_COMMIT, Data: commit1Data},
		{Type: gitinterop.OBJ_COMMIT, Data: commit2Data},
	})
	if err != nil {
		t.Fatal(err)
	}

	updateLine := fmt.Sprintf("%s %s refs/heads/main\x00report-status\n", strings.Repeat("0", 40), commit2Hash)
	payload := append(pktLineForTest(updateLine), pktFlushForTest()...)
	payload = append(payload, packData...)

	req, _ := http.NewRequest("POST", ts.URL+"/git/owner/repo/git-receive-pack", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/x-git-receive-pack-request")
	req.SetBasicAuth("owner", "secret123")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("git receive-pack: expected 200, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	resp, err = http.Get(ts.URL + "/api/v1/repos/owner/repo/entity-history/main?name=ProcessOrder&limit=10")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("entity history: expected 200, got %d", resp.StatusCode)
	}
	var hits []struct {
		CommitHash string `json:"commit_hash"`
		Path       string `json:"path"`
		Name       string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&hits); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if len(hits) < 2 {
		t.Fatalf("expected at least 2 entity history hits, got %+v", hits)
	}
	for i, hit := range hits {
		if strings.TrimSpace(hit.CommitHash) == "" {
			t.Fatalf("hit %d missing commit hash: %+v", i, hit)
		}
		if hit.Path != "main.go" {
			t.Fatalf("hit %d unexpected path: %+v", i, hit)
		}
		if hit.Name != "ProcessOrder" {
			t.Fatalf("hit %d unexpected entity name: %+v", i, hit)
		}
	}
}

func TestMergeBlockedByBranchProtectionApprovalRequirement(t *testing.T) {
	server, _ := setupTestServer(t)
	ts := httptest.NewServer(server)
	defer ts.Close()

	token := registerAndGetToken(t, ts.URL, "alice")
	createRepo(t, ts.URL, token, "repo", false)
	prNumber := createPRNumber(t, ts.URL, token, "alice", "repo", "feature", "main")

	ruleBody := `{
		"enabled": true,
		"require_approvals": true,
		"required_approvals": 1,
		"require_status_checks": false
	}`
	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/repos/alice/repo/branch-protection/main", bytes.NewBufferString(ruleBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("set branch protection: expected 200, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	req, _ = http.NewRequest("POST", fmt.Sprintf("%s/api/v1/repos/alice/repo/pulls/%d/merge", ts.URL, prNumber), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("merge should be blocked: expected 409, got %d", resp.StatusCode)
	}
	var mergeResp struct {
		Error   string   `json:"error"`
		Reasons []string `json:"reasons"`
		Detail  string   `json:"detail"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&mergeResp); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if mergeResp.Error != "merge blocked by branch protection" {
		t.Fatalf("unexpected merge error: %q", mergeResp.Error)
	}
	if !strings.Contains(strings.Join(mergeResp.Reasons, " "), "approving review") {
		t.Fatalf("expected approval requirement reason, got %+v", mergeResp.Reasons)
	}
}

func TestMergeGatePassesAfterRequiredCheckSucceeds(t *testing.T) {
	server, _ := setupTestServer(t)
	ts := httptest.NewServer(server)
	defer ts.Close()

	token := registerAndGetToken(t, ts.URL, "alice")
	createRepo(t, ts.URL, token, "repo", false)
	prNumber := createPRNumber(t, ts.URL, token, "alice", "repo", "feature", "main")

	ruleBody := `{
		"enabled": true,
		"require_approvals": false,
		"require_status_checks": true,
		"required_checks": ["ci/test"]
	}`
	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/repos/alice/repo/branch-protection/main", bytes.NewBufferString(ruleBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("set branch protection: expected 200, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Missing required check should block merge.
	req, _ = http.NewRequest("POST", fmt.Sprintf("%s/api/v1/repos/alice/repo/pulls/%d/merge", ts.URL, prNumber), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("merge should be blocked without check: expected 409, got %d", resp.StatusCode)
	}
	var blockedResp struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&blockedResp); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if blockedResp.Error != "merge blocked by branch protection" {
		t.Fatalf("expected branch protection block, got %q", blockedResp.Error)
	}

	checkBody := `{"name":"ci/test","status":"completed","conclusion":"success"}`
	req, _ = http.NewRequest("POST", fmt.Sprintf("%s/api/v1/repos/alice/repo/pulls/%d/checks", ts.URL, prNumber), bytes.NewBufferString(checkBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("upsert check run: expected 200, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Merge now gets past policy and fails later (branches do not exist in this test fixture).
	req, _ = http.NewRequest("POST", fmt.Sprintf("%s/api/v1/repos/alice/repo/pulls/%d/merge", ts.URL, prNumber), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("merge should still fail due to missing branches: expected 409, got %d", resp.StatusCode)
	}
	var afterCheckResp struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&afterCheckResp); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if afterCheckResp.Error == "merge blocked by branch protection" {
		t.Fatalf("expected merge to pass policy gate after check run, got %q", afterCheckResp.Error)
	}
}

func TestPRMergeGateEndpoint(t *testing.T) {
	server, _ := setupTestServer(t)
	ts := httptest.NewServer(server)
	defer ts.Close()

	token := registerAndGetToken(t, ts.URL, "alice")
	createRepo(t, ts.URL, token, "repo", false)
	prNumber := createPRNumber(t, ts.URL, token, "alice", "repo", "feature", "main")

	ruleBody := `{
		"enabled": true,
		"require_approvals": false,
		"require_status_checks": true,
		"required_checks": ["ci/test"]
	}`
	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/repos/alice/repo/branch-protection/main", bytes.NewBufferString(ruleBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("set branch protection: expected 200, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	req, _ = http.NewRequest("GET", fmt.Sprintf("%s/api/v1/repos/alice/repo/pulls/%d/merge-gate", ts.URL, prNumber), nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("merge gate endpoint: expected 200, got %d", resp.StatusCode)
	}
	var gateResp struct {
		Allowed bool     `json:"allowed"`
		Reasons []string `json:"reasons"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&gateResp); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if gateResp.Allowed {
		t.Fatal("expected merge gate to block without required check")
	}
	if len(gateResp.Reasons) == 0 {
		t.Fatal("expected merge gate reasons when blocked")
	}

	checkBody := `{"name":"ci/test","status":"completed","conclusion":"success"}`
	req, _ = http.NewRequest("POST", fmt.Sprintf("%s/api/v1/repos/alice/repo/pulls/%d/checks", ts.URL, prNumber), bytes.NewBufferString(checkBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("upsert check run: expected 200, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	req, _ = http.NewRequest("GET", fmt.Sprintf("%s/api/v1/repos/alice/repo/pulls/%d/merge-gate", ts.URL, prNumber), nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("merge gate endpoint after check: expected 200, got %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&gateResp); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if !gateResp.Allowed {
		t.Fatalf("expected merge gate to allow after required check, got reasons %+v", gateResp.Reasons)
	}
}

func TestWebhookPingRetriesAndRedelivery(t *testing.T) {
	server, _ := setupTestServer(t)
	ts := httptest.NewServer(server)
	defer ts.Close()

	token := registerAndGetToken(t, ts.URL, "alice")
	createRepo(t, ts.URL, token, "repo", false)

	var recvCount int32
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&recvCount, 1)
		if r.Header.Get("X-Gothub-Event") != "ping" {
			t.Errorf("expected X-Gothub-Event ping, got %q", r.Header.Get("X-Gothub-Event"))
		}
		if r.Header.Get("X-Gothub-Delivery") == "" {
			t.Errorf("expected X-Gothub-Delivery header")
		}
		if !strings.HasPrefix(r.Header.Get("X-Hub-Signature-256"), "sha256=") {
			t.Errorf("expected HMAC signature header, got %q", r.Header.Get("X-Hub-Signature-256"))
		}
		if atomic.LoadInt32(&recvCount) < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("temporary failure"))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer receiver.Close()

	createHookBody := fmt.Sprintf(`{"url":"%s","secret":"topsecret","events":["ping"],"active":true}`, receiver.URL)
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/repos/alice/repo/webhooks", bytes.NewBufferString(createHookBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create webhook: expected 201, got %d", resp.StatusCode)
	}
	var hook struct {
		ID        int64 `json:"id"`
		HasSecret bool  `json:"has_secret"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&hook); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if hook.ID == 0 {
		t.Fatal("expected webhook id")
	}
	if !hook.HasSecret {
		t.Fatal("expected has_secret=true")
	}

	req, _ = http.NewRequest("POST", fmt.Sprintf("%s/api/v1/repos/alice/repo/webhooks/%d/ping", ts.URL, hook.ID), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ping webhook: expected 200, got %d", resp.StatusCode)
	}
	var pingDelivery struct {
		ID      int64 `json:"id"`
		Attempt int   `json:"attempt"`
		Success bool  `json:"success"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&pingDelivery); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if pingDelivery.ID == 0 || !pingDelivery.Success || pingDelivery.Attempt != 3 {
		t.Fatalf("unexpected ping delivery: %+v", pingDelivery)
	}
	if atomic.LoadInt32(&recvCount) != 3 {
		t.Fatalf("expected 3 delivery attempts, got %d", atomic.LoadInt32(&recvCount))
	}

	resp, err = http.Get(fmt.Sprintf("%s/api/v1/repos/alice/repo/webhooks/%d/deliveries", ts.URL, hook.ID))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list deliveries: expected 200, got %d", resp.StatusCode)
	}
	var deliveries []struct {
		ID             int64  `json:"id"`
		Attempt        int    `json:"attempt"`
		DeliveryUID    string `json:"delivery_uid"`
		Success        bool   `json:"success"`
		RedeliveryOfID *int64 `json:"redelivery_of_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&deliveries); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(deliveries) != 3 {
		t.Fatalf("expected 3 delivery rows, got %d", len(deliveries))
	}
	var originalID int64
	for _, d := range deliveries {
		if d.Attempt == 1 {
			originalID = d.ID
			break
		}
	}
	if originalID == 0 {
		t.Fatalf("expected attempt 1 delivery in %+v", deliveries)
	}

	req, _ = http.NewRequest("POST", fmt.Sprintf("%s/api/v1/repos/alice/repo/webhooks/%d/deliveries/%d/redeliver", ts.URL, hook.ID, originalID), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("redeliver: expected 200, got %d", resp.StatusCode)
	}
	var redelivery struct {
		ID             int64  `json:"id"`
		Attempt        int    `json:"attempt"`
		RedeliveryOfID *int64 `json:"redelivery_of_id"`
		Success        bool   `json:"success"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&redelivery); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if redelivery.ID == 0 || !redelivery.Success || redelivery.RedeliveryOfID == nil || *redelivery.RedeliveryOfID != originalID {
		t.Fatalf("unexpected redelivery response: %+v", redelivery)
	}

	resp, err = http.Get(fmt.Sprintf("%s/api/v1/repos/alice/repo/webhooks/%d/deliveries", ts.URL, hook.ID))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list deliveries after redelivery: expected 200, got %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&deliveries); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(deliveries) != 4 {
		t.Fatalf("expected 4 delivery rows after redelivery, got %d", len(deliveries))
	}
}

func TestNotificationsLifecycle(t *testing.T) {
	server, _ := setupTestServer(t)
	ts := httptest.NewServer(server)
	defer ts.Close()

	aliceToken := registerAndGetToken(t, ts.URL, "alice")
	bobToken := registerAndGetToken(t, ts.URL, "bob")
	createRepo(t, ts.URL, aliceToken, "repo", false)

	// Give bob write access so he can open PRs/issues.
	addCollabBody := `{"username":"bob","role":"write"}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/repos/alice/repo/collaborators", bytes.NewBufferString(addCollabBody))
	req.Header.Set("Authorization", "Bearer "+aliceToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("add collaborator: expected 201, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Bob opens PR -> Alice gets notification.
	createPRReq := `{"title":"PR from bob","source_branch":"feature","target_branch":"main"}`
	req, _ = http.NewRequest("POST", ts.URL+"/api/v1/repos/alice/repo/pulls", bytes.NewBufferString(createPRReq))
	req.Header.Set("Authorization", "Bearer "+bobToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create PR: expected 201, got %d", resp.StatusCode)
	}
	var prResp struct {
		Number int `json:"number"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&prResp); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	req, _ = http.NewRequest("GET", ts.URL+"/api/v1/notifications/unread-count", nil)
	req.Header.Set("Authorization", "Bearer "+aliceToken)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unread count: expected 200, got %d", resp.StatusCode)
	}
	var unreadCount struct {
		Count int `json:"count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&unreadCount); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if unreadCount.Count != 1 {
		t.Fatalf("expected 1 unread notification for alice, got %d", unreadCount.Count)
	}

	req, _ = http.NewRequest("GET", ts.URL+"/api/v1/notifications?unread=true", nil)
	req.Header.Set("Authorization", "Bearer "+aliceToken)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list notifications: expected 200, got %d", resp.StatusCode)
	}
	var notifications []struct {
		ID    int64  `json:"id"`
		Type  string `json:"type"`
		Title string `json:"title"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&notifications); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(notifications) != 1 {
		t.Fatalf("expected 1 unread notification, got %d", len(notifications))
	}
	if notifications[0].Type != "pull_request.opened" {
		t.Fatalf("unexpected notification type %q", notifications[0].Type)
	}

	// Mark one as read.
	req, _ = http.NewRequest("POST", fmt.Sprintf("%s/api/v1/notifications/%d/read", ts.URL, notifications[0].ID), nil)
	req.Header.Set("Authorization", "Bearer "+aliceToken)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("mark notification read: expected 204, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	req, _ = http.NewRequest("GET", ts.URL+"/api/v1/notifications/unread-count", nil)
	req.Header.Set("Authorization", "Bearer "+aliceToken)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewDecoder(resp.Body).Decode(&unreadCount); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if unreadCount.Count != 0 {
		t.Fatalf("expected 0 unread notifications after mark read, got %d", unreadCount.Count)
	}

	// Alice comments on bob's PR -> Bob gets notified.
	commentReq := `{"body":"Looks good"}`
	req, _ = http.NewRequest("POST", fmt.Sprintf("%s/api/v1/repos/alice/repo/pulls/%d/comments", ts.URL, prResp.Number), bytes.NewBufferString(commentReq))
	req.Header.Set("Authorization", "Bearer "+aliceToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create PR comment: expected 201, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	req, _ = http.NewRequest("GET", ts.URL+"/api/v1/notifications/unread-count", nil)
	req.Header.Set("Authorization", "Bearer "+bobToken)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewDecoder(resp.Body).Decode(&unreadCount); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if unreadCount.Count != 1 {
		t.Fatalf("expected 1 unread notification for bob, got %d", unreadCount.Count)
	}
}

func registerAndGetToken(t *testing.T, baseURL, username string) string {
	t.Helper()
	body := fmt.Sprintf(`{"username":"%s","email":"%s@example.com","password":"secret123"}`, username, username)
	resp, err := http.Post(baseURL+"/api/v1/auth/register", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register %s: expected 201, got %d", username, resp.StatusCode)
	}
	var regResp struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&regResp); err != nil {
		t.Fatal(err)
	}
	if regResp.Token == "" {
		t.Fatalf("register %s: missing token", username)
	}
	return regResp.Token
}

func createRepo(t *testing.T, baseURL, token, name string, isPrivate bool) {
	t.Helper()
	body := fmt.Sprintf(`{"name":"%s","description":"","private":%t}`, name, isPrivate)
	req, _ := http.NewRequest("POST", baseURL+"/api/v1/repos", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create repo %s: expected 201, got %d", name, resp.StatusCode)
	}
}

func createPRNumber(t *testing.T, baseURL, token, owner, repo, sourceBranch, targetBranch string) int {
	t.Helper()
	body := fmt.Sprintf(`{"title":"test pr","body":"","source_branch":"%s","target_branch":"%s"}`, sourceBranch, targetBranch)
	req, _ := http.NewRequest("POST", fmt.Sprintf("%s/api/v1/repos/%s/%s/pulls", baseURL, owner, repo), bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create PR: expected 201, got %d", resp.StatusCode)
	}
	var pr struct {
		Number int `json:"number"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
		t.Fatal(err)
	}
	if pr.Number == 0 {
		t.Fatal("create PR: missing number in response")
	}
	return pr.Number
}

func pktLineForTest(payload string) []byte {
	return []byte(fmt.Sprintf("%04x%s", len(payload)+4, payload))
}

func pktFlushForTest() []byte {
	return []byte("0000")
}
