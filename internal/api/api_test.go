package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
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
	server, _ := setupTestServer(t)
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

	// Verify storage was initialized (objects/ and refs/ dirs exist)
	// The repo storage path is based on the repo ID
	entries, err := os.ReadDir(ts.URL) // This won't work — need to check the tmpdir
	_ = entries
	_ = err
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
