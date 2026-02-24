package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/odvcencio/gothub/internal/api"
	"github.com/odvcencio/gothub/internal/auth"
	"github.com/odvcencio/gothub/internal/database"
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

func pktLineForTest(payload string) []byte {
	return []byte(fmt.Sprintf("%04x%s", len(payload)+4, payload))
}

func pktFlushForTest() []byte {
	return []byte("0000")
}
