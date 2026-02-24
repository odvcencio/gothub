package api_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
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

	"github.com/odvcencio/got/pkg/object"
	"github.com/odvcencio/gothub/internal/api"
	"github.com/odvcencio/gothub/internal/auth"
	"github.com/odvcencio/gothub/internal/database"
	"github.com/odvcencio/gothub/internal/gitinterop"
	"github.com/odvcencio/gothub/internal/gotstore"
	"github.com/odvcencio/gothub/internal/models"
	"github.com/odvcencio/gothub/internal/service"
	"golang.org/x/crypto/ssh"
)

func setupTestServer(t *testing.T) (*api.Server, database.DB) {
	return setupTestServerWithOptions(t, api.ServerOptions{
		EnablePasswordAuth: true,
	})
}

func setupTestServerWithOptions(t *testing.T, opts api.ServerOptions) (*api.Server, database.DB) {
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
	server := api.NewServerWithOptions(db, authSvc, repoSvc, opts)
	return server, db
}

func setupTestServerAsyncIndexing(t *testing.T) (*api.Server, database.DB) {
	return setupTestServerWithOptions(t, api.ServerOptions{
		EnablePasswordAuth:  true,
		EnableAsyncIndexing: true,
	})
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

func TestPasswordAuthDisabledByDefault(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := database.OpenSQLite(tmpDir + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	authSvc := auth.NewService("test-secret", 24*time.Hour)
	repoSvc := service.NewRepoService(db, tmpDir+"/repos")
	server := api.NewServer(db, authSvc, repoSvc)
	ts := httptest.NewServer(server)
	defer ts.Close()

	// Password registration should be rejected when password auth is disabled.
	resp, err := http.Post(ts.URL+"/api/v1/auth/register", "application/json", bytes.NewBufferString(`{"username":"pw","email":"pw@example.com","password":"secret123"}`))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("password register with auth disabled: expected 403, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Passwordless registration remains available.
	resp, err = http.Post(ts.URL+"/api/v1/auth/register", "application/json", bytes.NewBufferString(`{"username":"passless","email":"passless@example.com"}`))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("passwordless register with auth disabled: expected 201, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Password login endpoint should be disabled.
	resp, err = http.Post(ts.URL+"/api/v1/auth/login", "application/json", bytes.NewBufferString(`{"username":"passless","password":"irrelevant"}`))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("password login with auth disabled: expected 403, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	resp, err = http.Get(ts.URL + "/api/v1/auth/capabilities")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("auth capabilities: expected 200, got %d", resp.StatusCode)
	}
	var caps struct {
		PasswordAuthEnabled bool `json:"password_auth_enabled"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&caps); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if caps.PasswordAuthEnabled {
		t.Fatal("expected password_auth_enabled=false by default")
	}
}

func TestMagicLinkAuthFlow(t *testing.T) {
	server, _ := setupTestServer(t)
	ts := httptest.NewServer(server)
	defer ts.Close()

	regBody := `{"username":"magicuser","email":"magic@example.com","password":"secret123"}`
	resp, err := http.Post(ts.URL+"/api/v1/auth/register", "application/json", bytes.NewBufferString(regBody))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register: expected 201, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	reqBody := `{"email":"magic@example.com"}`
	resp, err = http.Post(ts.URL+"/api/v1/auth/magic/request", "application/json", bytes.NewBufferString(reqBody))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("magic request: expected 200, got %d", resp.StatusCode)
	}
	var requestResp struct {
		Sent  bool   `json:"sent"`
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&requestResp); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if !requestResp.Sent {
		t.Fatal("expected sent=true")
	}
	if requestResp.Token == "" {
		t.Fatal("expected magic token in response for local/dev mode")
	}

	verifyBody := fmt.Sprintf(`{"token":"%s"}`, requestResp.Token)
	resp, err = http.Post(ts.URL+"/api/v1/auth/magic/verify", "application/json", bytes.NewBufferString(verifyBody))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("magic verify: expected 200, got %d", resp.StatusCode)
	}
	var verifyResp struct {
		Token string `json:"token"`
		User  struct {
			Username string `json:"username"`
		} `json:"user"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&verifyResp); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if verifyResp.Token == "" {
		t.Fatal("expected jwt token from magic verify")
	}
	if verifyResp.User.Username != "magicuser" {
		t.Fatalf("expected username magicuser, got %q", verifyResp.User.Username)
	}

	// One-time token: replay must fail.
	resp, err = http.Post(ts.URL+"/api/v1/auth/magic/verify", "application/json", bytes.NewBufferString(verifyBody))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("magic verify replay: expected 401, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestSSHChallengeAuthFlow(t *testing.T) {
	server, _ := setupTestServer(t)
	ts := httptest.NewServer(server)
	defer ts.Close()

	signer, pubText, fingerprint := newTestSSHSigner(t)
	token := registerAndGetToken(t, ts.URL, "sshuser")

	addKeyBody := fmt.Sprintf(`{"name":"laptop","public_key":%q}`, strings.TrimSpace(pubText))
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/user/ssh-keys", bytes.NewBufferString(addKeyBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create ssh key: expected 201, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	challengeReq := fmt.Sprintf(`{"username":"sshuser","fingerprint":"%s"}`, fingerprint)
	resp, err = http.Post(ts.URL+"/api/v1/auth/ssh/challenge", "application/json", bytes.NewBufferString(challengeReq))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ssh challenge: expected 200, got %d", resp.StatusCode)
	}
	var challengeResp struct {
		ChallengeID string `json:"challenge_id"`
		Challenge   string `json:"challenge"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&challengeResp); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if challengeResp.ChallengeID == "" || challengeResp.Challenge == "" {
		t.Fatalf("invalid challenge response: %+v", challengeResp)
	}

	signature, err := signer.Sign(rand.Reader, []byte(challengeResp.Challenge))
	if err != nil {
		t.Fatal(err)
	}
	verifyReq := fmt.Sprintf(
		`{"challenge_id":"%s","signature":"%s","signature_format":"%s"}`,
		challengeResp.ChallengeID,
		base64.StdEncoding.EncodeToString(signature.Blob),
		signature.Format,
	)
	resp, err = http.Post(ts.URL+"/api/v1/auth/ssh/verify", "application/json", bytes.NewBufferString(verifyReq))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ssh verify: expected 200, got %d", resp.StatusCode)
	}
	var verifyResp struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&verifyResp); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if verifyResp.Token == "" {
		t.Fatal("expected jwt token from ssh verify")
	}

	// One-time challenge: replay must fail.
	resp, err = http.Post(ts.URL+"/api/v1/auth/ssh/verify", "application/json", bytes.NewBufferString(verifyReq))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("ssh verify replay: expected 401, got %d", resp.StatusCode)
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

func TestForkRepoCopiesStoreAndMetadata(t *testing.T) {
	server, db := setupTestServer(t)
	ts := httptest.NewServer(server)
	defer ts.Close()

	ownerToken := registerAndGetToken(t, ts.URL, "alice")
	forkerToken := registerAndGetToken(t, ts.URL, "bob")
	createRepo(t, ts.URL, ownerToken, "repo", false)

	ctx := context.Background()
	sourceRepo, err := db.GetRepository(ctx, "alice", "repo")
	if err != nil {
		t.Fatal(err)
	}

	sourceStore, err := gotstore.Open(sourceRepo.StoragePath)
	if err != nil {
		t.Fatal(err)
	}
	blobHash, err := sourceStore.Objects.WriteBlob(&object.Blob{Data: []byte("hello fork\n")})
	if err != nil {
		t.Fatal(err)
	}
	treeHash, err := sourceStore.Objects.WriteTree(&object.TreeObj{
		Entries: []object.TreeEntry{
			{Name: "README.md", BlobHash: blobHash},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	commitHash, err := sourceStore.Objects.WriteCommit(&object.CommitObj{
		TreeHash:           treeHash,
		Author:             "Alice <alice@example.com>",
		Timestamp:          1700000000,
		Message:            "initial commit",
		AuthorTimezone:     "+0000",
		Committer:          "Alice <alice@example.com>",
		CommitterTimestamp: 1700000000,
		CommitterTimezone:  "+0000",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := sourceStore.Refs.Set("heads/main", commitHash); err != nil {
		t.Fatal(err)
	}

	blobGit := strings.Repeat("1", 40)
	treeGit := strings.Repeat("2", 40)
	commitGit := strings.Repeat("3", 40)
	if err := db.SetHashMappings(ctx, []models.HashMapping{
		{RepoID: sourceRepo.ID, GotHash: string(blobHash), GitHash: blobGit, ObjectType: "blob"},
		{RepoID: sourceRepo.ID, GotHash: string(treeHash), GitHash: treeGit, ObjectType: "tree"},
		{RepoID: sourceRepo.ID, GotHash: string(commitHash), GitHash: commitGit, ObjectType: "commit"},
	}); err != nil {
		t.Fatal(err)
	}

	indexHash := strings.Repeat("a", 64)
	if err := db.SetCommitIndex(ctx, sourceRepo.ID, string(commitHash), indexHash); err != nil {
		t.Fatal(err)
	}
	if err := db.SetGitTreeEntryModes(ctx, sourceRepo.ID, string(treeHash), map[string]string{"README.md": "100644"}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertEntityIdentity(ctx, &models.EntityIdentity{
		RepoID:          sourceRepo.ID,
		StableID:        "ent-readme",
		Name:            "README",
		DeclKind:        "file",
		FirstSeenCommit: string(commitHash),
		LastSeenCommit:  string(commitHash),
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetEntityVersion(ctx, &models.EntityVersion{
		RepoID:     sourceRepo.ID,
		StableID:   "ent-readme",
		CommitHash: string(commitHash),
		Path:       "README.md",
		EntityHash: strings.Repeat("b", 64),
		BodyHash:   strings.Repeat("c", 64),
		Name:       "README",
		DeclKind:   "file",
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetEntityIndexEntries(ctx, sourceRepo.ID, string(commitHash), []models.EntityIndexEntry{
		{
			RepoID:     sourceRepo.ID,
			CommitHash: string(commitHash),
			FilePath:   "README.md",
			SymbolKey:  "sym-readme",
			StableID:   "ent-readme",
			Kind:       "constant",
			Name:       "README",
			Signature:  "const README = \"hello fork\"",
			StartLine:  1,
			EndLine:    1,
		},
	}); err != nil {
		t.Fatal(err)
	}

	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/repos/alice/repo/forks", bytes.NewBufferString(`{}`))
	req.Header.Set("Authorization", "Bearer "+forkerToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("fork repo: expected 201, got %d", resp.StatusCode)
	}
	var forkResp struct {
		ID           int64  `json:"id"`
		Name         string `json:"name"`
		ParentRepoID *int64 `json:"parent_repo_id"`
		ParentOwner  string `json:"parent_owner"`
		ParentName   string `json:"parent_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&forkResp); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if forkResp.Name != "repo" {
		t.Fatalf("expected fork name repo, got %q", forkResp.Name)
	}
	if forkResp.ParentRepoID == nil || *forkResp.ParentRepoID != sourceRepo.ID {
		t.Fatalf("expected parent_repo_id %d, got %+v", sourceRepo.ID, forkResp.ParentRepoID)
	}
	if forkResp.ParentOwner != "alice" {
		t.Fatalf("expected parent owner alice, got %q", forkResp.ParentOwner)
	}
	if forkResp.ParentName != "repo" {
		t.Fatalf("expected parent name repo, got %q", forkResp.ParentName)
	}

	forkRepo, err := db.GetRepository(ctx, "bob", "repo")
	if err != nil {
		t.Fatal(err)
	}
	if forkRepo.ParentRepoID == nil || *forkRepo.ParentRepoID != sourceRepo.ID {
		t.Fatalf("expected persisted parent repo id %d, got %+v", sourceRepo.ID, forkRepo.ParentRepoID)
	}

	forkStore, err := gotstore.Open(forkRepo.StoragePath)
	if err != nil {
		t.Fatal(err)
	}
	forkHead, err := forkStore.Refs.Get("heads/main")
	if err != nil {
		t.Fatal(err)
	}
	if forkHead != commitHash {
		t.Fatalf("expected fork head %q, got %q", commitHash, forkHead)
	}

	forkGitHash, err := db.GetGitHash(ctx, forkRepo.ID, string(commitHash))
	if err != nil {
		t.Fatal(err)
	}
	if forkGitHash != commitGit {
		t.Fatalf("expected git hash mapping %q, got %q", commitGit, forkGitHash)
	}
	forkIndex, err := db.GetCommitIndex(ctx, forkRepo.ID, string(commitHash))
	if err != nil {
		t.Fatal(err)
	}
	if forkIndex != indexHash {
		t.Fatalf("expected copied commit index %q, got %q", indexHash, forkIndex)
	}
	modes, err := db.GetGitTreeEntryModes(ctx, forkRepo.ID, string(treeHash))
	if err != nil {
		t.Fatal(err)
	}
	if modes["README.md"] != "100644" {
		t.Fatalf("expected copied mode 100644, got %#v", modes)
	}
	versions, err := db.ListEntityVersionsByCommit(ctx, forkRepo.ID, string(commitHash))
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 1 || versions[0].StableID != "ent-readme" {
		t.Fatalf("expected copied entity versions, got %+v", versions)
	}
	entries, err := db.ListEntityIndexEntriesByCommit(ctx, forkRepo.ID, string(commitHash), "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name != "README" {
		t.Fatalf("expected copied entity index entries, got %+v", entries)
	}

	resp, err = http.Get(ts.URL + "/api/v1/repos/bob/repo")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get fork repo: expected 200, got %d", resp.StatusCode)
	}
	var getForkResp struct {
		ParentRepoID *int64 `json:"parent_repo_id"`
		ParentOwner  string `json:"parent_owner"`
		ParentName   string `json:"parent_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&getForkResp); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if getForkResp.ParentRepoID == nil || *getForkResp.ParentRepoID != sourceRepo.ID {
		t.Fatalf("get fork repo: expected parent_repo_id %d, got %+v", sourceRepo.ID, getForkResp.ParentRepoID)
	}
	if getForkResp.ParentOwner != "alice" || getForkResp.ParentName != "repo" {
		t.Fatalf("get fork repo: expected parent alice/repo, got %q/%q", getForkResp.ParentOwner, getForkResp.ParentName)
	}

	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/api/v1/user/repos", nil)
	req.Header.Set("Authorization", "Bearer "+forkerToken)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list user repos: expected 200, got %d", resp.StatusCode)
	}
	var userRepos []struct {
		Name         string `json:"name"`
		ParentRepoID *int64 `json:"parent_repo_id"`
		ParentOwner  string `json:"parent_owner"`
		ParentName   string `json:"parent_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&userRepos); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(userRepos) != 1 {
		t.Fatalf("expected exactly one user repo before second fork, got %+v", userRepos)
	}
	if userRepos[0].ParentRepoID == nil || *userRepos[0].ParentRepoID != sourceRepo.ID {
		t.Fatalf("list user repos: expected parent_repo_id %d, got %+v", sourceRepo.ID, userRepos[0].ParentRepoID)
	}
	if userRepos[0].ParentOwner != "alice" || userRepos[0].ParentName != "repo" {
		t.Fatalf("list user repos: expected parent alice/repo, got %q/%q", userRepos[0].ParentOwner, userRepos[0].ParentName)
	}

	// A second fork by the same user should auto-suffix to avoid name collisions.
	req, _ = http.NewRequest("POST", ts.URL+"/api/v1/repos/alice/repo/forks", nil)
	req.Header.Set("Authorization", "Bearer "+forkerToken)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("second fork repo: expected 201, got %d", resp.StatusCode)
	}
	var secondFork struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&secondFork); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if secondFork.Name != "repo-fork-1" {
		t.Fatalf("expected auto-suffixed fork name repo-fork-1, got %q", secondFork.Name)
	}

	resp, err = http.Get(ts.URL + "/api/v1/repos/alice/repo/forks")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list forks: expected 200, got %d", resp.StatusCode)
	}
	var forks []struct {
		Name        string `json:"name"`
		OwnerName   string `json:"owner_name"`
		ParentRepo  *int64 `json:"parent_repo_id"`
		ParentOwner string `json:"parent_owner"`
		ParentName  string `json:"parent_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&forks); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(forks) != 2 {
		t.Fatalf("expected 2 forks, got %+v", forks)
	}
	if forks[0].ParentRepo == nil || *forks[0].ParentRepo != sourceRepo.ID {
		t.Fatalf("expected parent repo id %d on fork listing, got %+v", sourceRepo.ID, forks[0].ParentRepo)
	}
	for _, fork := range forks {
		if fork.ParentOwner != "alice" || fork.ParentName != "repo" {
			t.Fatalf("expected parent alice/repo on fork listing, got %q/%q", fork.ParentOwner, fork.ParentName)
		}
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

func TestAPIBodyLimitRejectsLargeJSON(t *testing.T) {
	server, _ := setupTestServer(t)
	ts := httptest.NewServer(server)
	defer ts.Close()

	token := registerAndGetToken(t, ts.URL, "alice")
	largeDescription := strings.Repeat("x", int((2<<20)+1024))
	body := fmt.Sprintf(`{"name":"bigrepo","description":"%s","private":false}`, largeDescription)

	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/repos", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("create repo with oversized body: expected 413, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestCORSPreflightReturnsHeaders(t *testing.T) {
	server, _ := setupTestServer(t)
	ts := httptest.NewServer(server)
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodOptions, ts.URL+"/api/v1/repos", nil)
	req.Header.Set("Origin", "https://example.test")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "Authorization, Content-Type")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("cors preflight: expected 204, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("cors preflight: expected allow-origin '*', got %q", got)
	}
	if got := resp.Header.Get("Access-Control-Allow-Methods"); !strings.Contains(got, "POST") {
		t.Fatalf("cors preflight: expected POST in allow-methods, got %q", got)
	}
	resp.Body.Close()
}

func TestCORSPreflightRespectsConfiguredAllowlist(t *testing.T) {
	server, _ := setupTestServerWithOptions(t, api.ServerOptions{
		EnablePasswordAuth: true,
		CORSAllowedOrigins: []string{"https://allowed.test"},
	})
	ts := httptest.NewServer(server)
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodOptions, ts.URL+"/api/v1/repos", nil)
	req.Header.Set("Origin", "https://allowed.test")
	req.Header.Set("Access-Control-Request-Method", "POST")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("cors preflight allowed: expected 204, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "https://allowed.test" {
		t.Fatalf("cors preflight allowed: expected reflected origin, got %q", got)
	}
	resp.Body.Close()

	req, _ = http.NewRequest(http.MethodOptions, ts.URL+"/api/v1/repos", nil)
	req.Header.Set("Origin", "https://blocked.test")
	req.Header.Set("Access-Control-Request-Method", "POST")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("cors preflight blocked: expected 204, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("cors preflight blocked: expected empty allow-origin, got %q", got)
	}
	resp.Body.Close()
}

func TestRateLimitMiddlewareBlocksAuthBurst(t *testing.T) {
	server, _ := setupTestServer(t)
	ts := httptest.NewServer(server)
	defer ts.Close()

	limited := false
	for i := 0; i < 80; i++ {
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/login", bytes.NewBufferString(`{"username":"","password":""}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Forwarded-For", "203.0.113.7")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			limited = true
			resp.Body.Close()
			break
		}
		resp.Body.Close()
	}
	if !limited {
		t.Fatal("expected at least one 429 from auth rate limiter burst")
	}
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

func TestProtocolAuthForbiddenForPrivateRepoNonMember(t *testing.T) {
	server, _ := setupTestServer(t)
	ts := httptest.NewServer(server)
	defer ts.Close()

	aliceToken := registerAndGetToken(t, ts.URL, "alice")
	bobToken := registerAndGetToken(t, ts.URL, "bob")

	createBody := `{"name":"private-repo","description":"","private":true}`
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

	reqBody := `{"wants":["` + strings.Repeat("0", 64) + `"]}`
	req, _ = http.NewRequest("POST", ts.URL+"/got/alice/private-repo/objects/batch", bytes.NewBufferString(reqBody))
	req.Header.Set("Authorization", "Bearer "+bobToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("non-member got batch fetch on private repo: expected 403, got %d", resp.StatusCode)
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

func TestGitUploadPackAdvertisementIncludesSidebandCapability(t *testing.T) {
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
	if !strings.Contains(bodyStr, "side-band-64k") {
		t.Fatalf("expected side-band-64k capability in advertisement: %q", bodyStr)
	}
	if !strings.Contains(bodyStr, "side-band-64k ofs-delta") {
		t.Fatalf("expected upload-pack capability set, got body %q", bodyStr)
	}
	if strings.Contains(bodyStr, "report-status") {
		t.Fatalf("upload-pack advertisement should not include receive-pack capability report-status: %q", bodyStr)
	}
}

func TestGitReceivePackAdvertisementIncludesReportStatusAndSideband(t *testing.T) {
	server, _ := setupTestServer(t)
	ts := httptest.NewServer(server)
	defer ts.Close()

	token := registerAndGetToken(t, ts.URL, "alice")
	createRepo(t, ts.URL, token, "repo", false)

	req, _ := http.NewRequest("GET", ts.URL+"/git/alice/repo/info/refs?service=git-receive-pack", nil)
	req.SetBasicAuth("alice", "secret123")
	resp, err := http.DefaultClient.Do(req)
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

	if !strings.Contains(bodyStr, "# service=git-receive-pack\n") {
		t.Fatalf("expected receive-pack service announcement, got body %q", bodyStr)
	}
	if !strings.Contains(bodyStr, "report-status delete-refs side-band-64k ofs-delta") {
		t.Fatalf("expected receive-pack capability set, got body %q", bodyStr)
	}
}

func TestGitUploadPackReturnsErrorOnCorruptObjectGraph(t *testing.T) {
	server, db := setupTestServer(t)
	ts := httptest.NewServer(server)
	defer ts.Close()

	token := registerAndGetToken(t, ts.URL, "owner")
	createRepo(t, ts.URL, token, "repo", false)

	repo, err := db.GetRepository(context.Background(), "owner", "repo")
	if err != nil {
		t.Fatal(err)
	}
	store, err := gotstore.Open(repo.StoragePath)
	if err != nil {
		t.Fatal(err)
	}

	commitHash, err := store.Objects.WriteCommit(&object.CommitObj{
		TreeHash:  object.Hash(strings.Repeat("a", 64)),
		Author:    "Owner <owner@example.com>",
		Timestamp: 1700000000,
		Message:   "broken commit",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Refs.Set("heads/main", commitHash); err != nil {
		t.Fatal(err)
	}

	gitHash := strings.Repeat("1", 40)
	if err := db.SetHashMapping(context.Background(), &models.HashMapping{
		RepoID:     repo.ID,
		GotHash:    string(commitHash),
		GitHash:    gitHash,
		ObjectType: "commit",
	}); err != nil {
		t.Fatal(err)
	}

	payload := append(pktLineForTest("want "+gitHash+"\n"), pktFlushForTest()...)
	payload = append(payload, pktLineForTest("done\n")...)
	payload = append(payload, pktFlushForTest()...)

	req, _ := http.NewRequest("POST", ts.URL+"/git/owner/repo/git-upload-pack", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/x-git-upload-pack-request")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("git upload-pack: expected 422 for corrupt graph, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestGitUploadPackUsesSidebandWhenRequested(t *testing.T) {
	server, db := setupTestServer(t)
	ts := httptest.NewServer(server)
	defer ts.Close()

	token := registerAndGetToken(t, ts.URL, "owner")
	createRepo(t, ts.URL, token, "repo", false)

	repo, err := db.GetRepository(context.Background(), "owner", "repo")
	if err != nil {
		t.Fatal(err)
	}
	store, err := gotstore.Open(repo.StoragePath)
	if err != nil {
		t.Fatal(err)
	}

	blobData := []byte("hello sideband\n")
	blobHash, err := store.Objects.WriteBlob(&object.Blob{Data: blobData})
	if err != nil {
		t.Fatal(err)
	}
	treeHash, err := store.Objects.WriteTree(&object.TreeObj{
		Entries: []object.TreeEntry{
			{
				Name:     "README.md",
				Mode:     object.TreeModeFile,
				BlobHash: blobHash,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	commitHash, err := store.Objects.WriteCommit(&object.CommitObj{
		TreeHash:  treeHash,
		Author:    "Owner <owner@example.com>",
		Timestamp: 1700000000,
		Message:   "seed",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Refs.Set("heads/main", commitHash); err != nil {
		t.Fatal(err)
	}

	gitCommitHash := strings.Repeat("1", 40)
	if err := db.SetHashMapping(context.Background(), &models.HashMapping{
		RepoID:     repo.ID,
		GotHash:    string(commitHash),
		GitHash:    gitCommitHash,
		ObjectType: "commit",
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetHashMapping(context.Background(), &models.HashMapping{
		RepoID:     repo.ID,
		GotHash:    string(treeHash),
		GitHash:    strings.Repeat("2", 40),
		ObjectType: "tree",
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetHashMapping(context.Background(), &models.HashMapping{
		RepoID:     repo.ID,
		GotHash:    string(blobHash),
		GitHash:    strings.Repeat("3", 40),
		ObjectType: "blob",
	}); err != nil {
		t.Fatal(err)
	}

	payload := append(pktLineForTest("want "+gitCommitHash+"\x00side-band-64k ofs-delta\n"), pktFlushForTest()...)
	payload = append(payload, pktLineForTest("done\n")...)
	payload = append(payload, pktFlushForTest()...)

	req, _ := http.NewRequest("POST", ts.URL+"/git/owner/repo/git-upload-pack", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/x-git-upload-pack-request")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("git upload-pack: expected 200, got %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(body, []byte("NAK\n")) {
		t.Fatalf("expected NAK pkt-line in upload-pack response, got body %q", string(body))
	}
	if !bytes.Contains(body, []byte{0x01, 'P', 'A', 'C', 'K'}) {
		t.Fatalf("expected side-band channel 1 PACK payload in upload-pack response, got %q", string(body))
	}
	if !bytes.HasSuffix(body, []byte("0000")) {
		t.Fatalf("expected pkt flush at end of side-band upload-pack response")
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

func TestReceivePackUsesSidebandForStatusWhenRequested(t *testing.T) {
	server, _ := setupTestServer(t)
	ts := httptest.NewServer(server)
	defer ts.Close()

	token := registerAndGetToken(t, ts.URL, "owner")
	createRepo(t, ts.URL, token, "repo", false)

	refName := "refs/heads/feature"
	line := fmt.Sprintf("%s %s %s\x00report-status side-band-64k\n", strings.Repeat("0", 40), strings.Repeat("1", 40), refName)
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

	if !bytes.Contains(body, []byte{0x01, 'u', 'n', 'p', 'a', 'c', 'k', ' ', 'o', 'k', '\n'}) {
		t.Fatalf("expected side-band framed unpack status, got body %q", string(body))
	}
	expectedNg := []byte{0x01, 'n', 'g', ' '}
	if !bytes.Contains(body, expectedNg) {
		t.Fatalf("expected side-band framed ng status, got body %q", string(body))
	}
}

func TestReceivePackRejectsStaleOldHash(t *testing.T) {
	server, db := setupTestServer(t)
	ts := httptest.NewServer(server)
	defer ts.Close()

	token := registerAndGetToken(t, ts.URL, "owner")
	createRepo(t, ts.URL, token, "repo", false)

	repo, err := db.GetRepository(context.Background(), "owner", "repo")
	if err != nil {
		t.Fatal(err)
	}
	store, err := gotstore.Open(repo.StoragePath)
	if err != nil {
		t.Fatal(err)
	}
	treeHash, err := store.Objects.WriteTree(&object.TreeObj{})
	if err != nil {
		t.Fatal(err)
	}
	commitHash, err := store.Objects.WriteCommit(&object.CommitObj{
		TreeHash:  treeHash,
		Author:    "Owner <owner@example.com>",
		Timestamp: 1700000000,
		Message:   "seed",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Refs.Set("heads/main", commitHash); err != nil {
		t.Fatal(err)
	}
	if err := db.SetHashMapping(context.Background(), &models.HashMapping{
		RepoID:     repo.ID,
		GotHash:    string(commitHash),
		GitHash:    strings.Repeat("a", 40),
		ObjectType: "commit",
	}); err != nil {
		t.Fatal(err)
	}

	updateLine := fmt.Sprintf("%s %s refs/heads/main\x00report-status\n", strings.Repeat("0", 40), strings.Repeat("0", 40))
	payload := append(pktLineForTest(updateLine), pktFlushForTest()...)

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
	expected := "ng refs/heads/main stale old hash"
	if !strings.Contains(bodyStr, expected) {
		t.Fatalf("expected stale old hash status %q, got body %q", expected, bodyStr)
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

func TestIndexStatusEndpointQueuedState(t *testing.T) {
	server, db := setupTestServerAsyncIndexing(t)
	ts := httptest.NewServer(server)
	defer ts.Close()

	token := registerAndGetToken(t, ts.URL, "owner")
	createRepo(t, ts.URL, token, "repo", false)

	gitCommitHash := pushSimpleGoCommit(t, ts.URL, "owner", "repo")

	ctx := context.Background()
	repo, err := db.GetRepository(ctx, "owner", "repo")
	if err != nil {
		t.Fatal(err)
	}
	gotCommitHash, err := db.GetGotHash(ctx, repo.ID, gitCommitHash)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := http.Get(ts.URL + "/api/v1/repos/owner/repo/index/status?ref=main")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("index status queued: expected 200, got %d", resp.StatusCode)
	}
	var payload struct {
		Ref         string    `json:"ref"`
		CommitHash  string    `json:"commit_hash"`
		Indexed     bool      `json:"indexed"`
		QueueStatus string    `json:"queue_status"`
		Attempts    int       `json:"attempts"`
		LastError   string    `json:"last_error"`
		UpdatedAt   time.Time `json:"updated_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if payload.Ref != "main" {
		t.Fatalf("expected ref main, got %q", payload.Ref)
	}
	if payload.CommitHash != gotCommitHash {
		t.Fatalf("expected commit hash %q, got %q", gotCommitHash, payload.CommitHash)
	}
	if payload.Indexed {
		t.Fatal("expected indexed=false for queued job")
	}
	if payload.QueueStatus != "queued" {
		t.Fatalf("expected queue_status queued, got %q", payload.QueueStatus)
	}
	if payload.Attempts != 0 {
		t.Fatalf("expected attempts 0 before claim, got %d", payload.Attempts)
	}
	if !payload.UpdatedAt.UTC().After(time.Time{}) {
		t.Fatalf("expected non-zero updated_at, got %s", payload.UpdatedAt)
	}
}

func TestIndexStatusEndpointCompletedState(t *testing.T) {
	server, db := setupTestServerAsyncIndexing(t)
	ts := httptest.NewServer(server)
	defer ts.Close()

	token := registerAndGetToken(t, ts.URL, "owner")
	createRepo(t, ts.URL, token, "repo", false)

	gitCommitHash := pushSimpleGoCommit(t, ts.URL, "owner", "repo")

	ctx := context.Background()
	repo, err := db.GetRepository(ctx, "owner", "repo")
	if err != nil {
		t.Fatal(err)
	}
	gotCommitHash, err := db.GetGotHash(ctx, repo.ID, gitCommitHash)
	if err != nil {
		t.Fatal(err)
	}

	claimed, err := db.ClaimIndexingJob(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if claimed == nil {
		t.Fatal("expected claimed indexing job")
	}
	if claimed.CommitHash != gotCommitHash {
		t.Fatalf("expected claimed commit hash %q, got %q", gotCommitHash, claimed.CommitHash)
	}

	if err := db.SetCommitIndex(ctx, repo.ID, gotCommitHash, strings.Repeat("d", 64)); err != nil {
		t.Fatal(err)
	}
	if err := db.CompleteIndexingJob(ctx, claimed.ID, models.IndexJobCompleted, ""); err != nil {
		t.Fatal(err)
	}

	resp, err := http.Get(ts.URL + "/api/v1/repos/owner/repo/index/status?ref=main")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("index status completed: expected 200, got %d", resp.StatusCode)
	}
	var payload struct {
		Ref         string    `json:"ref"`
		CommitHash  string    `json:"commit_hash"`
		Indexed     bool      `json:"indexed"`
		QueueStatus string    `json:"queue_status"`
		Attempts    int       `json:"attempts"`
		LastError   string    `json:"last_error"`
		UpdatedAt   time.Time `json:"updated_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if payload.Ref != "main" {
		t.Fatalf("expected ref main, got %q", payload.Ref)
	}
	if payload.CommitHash != gotCommitHash {
		t.Fatalf("expected commit hash %q, got %q", gotCommitHash, payload.CommitHash)
	}
	if !payload.Indexed {
		t.Fatal("expected indexed=true for completed job")
	}
	if payload.QueueStatus != "completed" {
		t.Fatalf("expected queue_status completed, got %q", payload.QueueStatus)
	}
	if payload.Attempts != 1 {
		t.Fatalf("expected attempts 1 after claim, got %d", payload.Attempts)
	}
	if !payload.UpdatedAt.UTC().After(time.Time{}) {
		t.Fatalf("expected non-zero updated_at, got %s", payload.UpdatedAt)
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

	resp, err = http.Get(ts.URL + "/api/v1/repos/owner/repo/symbols/main?q=ProcessOrder")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("symbols plain-text query: expected 200, got %d", resp.StatusCode)
	}
	var filtered []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&filtered); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(filtered) == 0 {
		t.Fatal("expected plain-text symbol search to return at least one result")
	}

	resp, err = http.Get(ts.URL + "/api/v1/repos/owner/repo/symbols/main?q=%2A%5Bname%3D%2FProcessOrder")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("symbols invalid selector: expected 400, got %d", resp.StatusCode)
	}
	resp.Body.Close()

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
		StableID   string `json:"stable_id"`
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
	seenStable := map[string]struct{}{}
	for i, hit := range hits {
		if strings.TrimSpace(hit.CommitHash) == "" {
			t.Fatalf("hit %d missing commit hash: %+v", i, hit)
		}
		if strings.TrimSpace(hit.StableID) == "" {
			t.Fatalf("hit %d missing stable id: %+v", i, hit)
		}
		if hit.Path != "main.go" {
			t.Fatalf("hit %d unexpected path: %+v", i, hit)
		}
		if hit.Name != "ProcessOrder" {
			t.Fatalf("hit %d unexpected entity name: %+v", i, hit)
		}
		seenStable[hit.StableID] = struct{}{}
	}
	if len(seenStable) != 1 {
		t.Fatalf("expected all hits to share one stable id, got %#v", seenStable)
	}

	resp, err = http.Get(ts.URL + "/api/v1/repos/owner/repo/entity-history/main?name=ProcessOrder&page=2&per_page=1")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("entity history page/per_page: expected 200, got %d", resp.StatusCode)
	}
	var pageTwo []struct {
		CommitHash string `json:"commit_hash"`
		StableID   string `json:"stable_id"`
		Path       string `json:"path"`
		Name       string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&pageTwo); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(pageTwo) != 1 {
		t.Fatalf("expected 1 paged entity history hit, got %+v", pageTwo)
	}
	if pageTwo[0].CommitHash != hits[1].CommitHash {
		t.Fatalf("expected paged hit commit %q, got %+v", hits[1].CommitHash, pageTwo[0])
	}
	if pageTwo[0].StableID != hits[1].StableID || pageTwo[0].Path != hits[1].Path || pageTwo[0].Name != hits[1].Name {
		t.Fatalf("expected paged hit to match baseline second hit, got %+v baseline=%+v", pageTwo[0], hits[1])
	}
}

func TestCommitsEndpointPaginationHonorsPageAndLimit(t *testing.T) {
	server, db := setupTestServer(t)
	ts := httptest.NewServer(server)
	defer ts.Close()

	token := registerAndGetToken(t, ts.URL, "owner")
	createRepo(t, ts.URL, token, "repo", false)

	repo, err := db.GetRepository(context.Background(), "owner", "repo")
	if err != nil {
		t.Fatal(err)
	}
	store, err := gotstore.Open(repo.StoragePath)
	if err != nil {
		t.Fatal(err)
	}

	hashes := make([]string, 0, 5)
	var parent object.Hash
	for i := 1; i <= 5; i++ {
		blobHash, err := store.Objects.WriteBlob(&object.Blob{
			Data: []byte(fmt.Sprintf("package main\n\nfunc v%d() {}\n", i)),
		})
		if err != nil {
			t.Fatal(err)
		}
		treeHash, err := store.Objects.WriteTree(&object.TreeObj{
			Entries: []object.TreeEntry{{Name: "main.go", BlobHash: blobHash}},
		})
		if err != nil {
			t.Fatal(err)
		}
		commit := &object.CommitObj{
			TreeHash:  treeHash,
			Author:    "Owner <owner@example.com>",
			Timestamp: 1700000000 + int64(i),
			Message:   fmt.Sprintf("commit-%d", i),
		}
		if parent != "" {
			commit.Parents = []object.Hash{parent}
		}
		hash, err := store.Objects.WriteCommit(commit)
		if err != nil {
			t.Fatal(err)
		}
		parent = hash
		hashes = append(hashes, string(hash))
	}
	if err := store.Refs.Set("heads/main", parent); err != nil {
		t.Fatal(err)
	}

	resp, err := http.Get(ts.URL + "/api/v1/repos/owner/repo/commits/main?page=2&per_page=2")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list commits page/per_page: expected 200, got %d", resp.StatusCode)
	}
	var paged []struct {
		Hash    string `json:"hash"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&paged); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(paged) != 2 {
		t.Fatalf("expected 2 paged commits, got %+v", paged)
	}
	if paged[0].Hash != hashes[2] || paged[0].Message != "commit-3" {
		t.Fatalf("expected first paged commit to be commit-3, got %+v", paged[0])
	}
	if paged[1].Hash != hashes[1] || paged[1].Message != "commit-2" {
		t.Fatalf("expected second paged commit to be commit-2, got %+v", paged[1])
	}

	resp, err = http.Get(ts.URL + "/api/v1/repos/owner/repo/commits/main?page=2&limit=2")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list commits with limit override: expected 200, got %d", resp.StatusCode)
	}
	var limited []struct {
		Hash string `json:"hash"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&limited); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(limited) != 2 {
		t.Fatalf("expected 2 commits with limit override, got %+v", limited)
	}
	if limited[0].Hash != hashes[2] || limited[1].Hash != hashes[1] {
		t.Fatalf("unexpected commits for limit override page 2: %+v", limited)
	}
}

func TestEntityHistoryEndpointRejectsInvalidLimit(t *testing.T) {
	server, _ := setupTestServer(t)
	ts := httptest.NewServer(server)
	defer ts.Close()

	token := registerAndGetToken(t, ts.URL, "owner")
	createRepo(t, ts.URL, token, "repo", false)

	resp, err := http.Get(ts.URL + "/api/v1/repos/owner/repo/entity-history/main?name=ProcessOrder&limit=abc")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("entity history invalid limit: expected 400, got %d", resp.StatusCode)
	}
}

func TestCallGraphEndpointRejectsInvalidDepth(t *testing.T) {
	server, _ := setupTestServer(t)
	ts := httptest.NewServer(server)
	defer ts.Close()

	token := registerAndGetToken(t, ts.URL, "owner")
	createRepo(t, ts.URL, token, "repo", false)

	resp, err := http.Get(ts.URL + "/api/v1/repos/owner/repo/callgraph/main?symbol=ProcessOrder&depth=abc")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("call graph invalid depth: expected 400, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	resp, err = http.Get(ts.URL + "/api/v1/repos/owner/repo/callgraph/main?symbol=ProcessOrder&depth=99")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("call graph depth above max: expected 400, got %d", resp.StatusCode)
	}
}

func TestImpactAnalysisEndpoint(t *testing.T) {
	server, db := setupTestServer(t)
	ts := httptest.NewServer(server)
	defer ts.Close()

	token := registerAndGetToken(t, ts.URL, "owner")
	createRepo(t, ts.URL, token, "repo", false)

	ctx := context.Background()
	repo, err := db.GetRepository(ctx, "owner", "repo")
	if err != nil {
		t.Fatal(err)
	}
	store, err := gotstore.Open(repo.StoragePath)
	if err != nil {
		t.Fatal(err)
	}

	blobHash, err := store.Objects.WriteBlob(&object.Blob{Data: []byte(
		"package main\n\n" +
			"func ProcessOrder() int { return 1 }\n\n" +
			"func Handle() int { return ProcessOrder() }\n",
	)})
	if err != nil {
		t.Fatal(err)
	}
	treeHash, err := store.Objects.WriteTree(&object.TreeObj{
		Entries: []object.TreeEntry{{Name: "main.go", BlobHash: blobHash}},
	})
	if err != nil {
		t.Fatal(err)
	}
	commitHash, err := store.Objects.WriteCommit(&object.CommitObj{
		TreeHash:  treeHash,
		Author:    "Owner <owner@example.com>",
		Timestamp: 1700000000,
		Message:   "seed impact fixture",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Refs.Set("heads/main", commitHash); err != nil {
		t.Fatal(err)
	}

	hasGraph, err := db.HasXRefGraphForCommit(ctx, repo.ID, string(commitHash))
	if err != nil {
		t.Fatal(err)
	}
	if hasGraph {
		t.Fatal("expected no persisted xref graph before impact analysis")
	}

	resp, err := http.Get(ts.URL + "/api/v1/repos/owner/repo/impact/main?symbol=ProcessOrder")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("impact endpoint: expected 200, got %d", resp.StatusCode)
	}
	var payload struct {
		MatchedDefinitions []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"matched_definitions"`
		DirectCallers []struct {
			Definition struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"definition"`
			CallCount int `json:"call_count"`
		} `json:"direct_callers"`
		Summary struct {
			MatchedDefinitions int `json:"matched_definitions"`
			DirectCallers      int `json:"direct_callers"`
			TotalIncomingCalls int `json:"total_incoming_calls"`
		} `json:"summary"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if len(payload.MatchedDefinitions) == 0 {
		t.Fatal("expected at least one matched definition")
	}
	foundProcessOrder := false
	for _, d := range payload.MatchedDefinitions {
		if d.Name == "ProcessOrder" {
			foundProcessOrder = true
			break
		}
	}
	if !foundProcessOrder {
		t.Fatalf("expected ProcessOrder in matched definitions, got %+v", payload.MatchedDefinitions)
	}

	if len(payload.DirectCallers) == 0 {
		t.Fatal("expected at least one direct caller")
	}
	foundHandle := false
	for _, caller := range payload.DirectCallers {
		if caller.Definition.Name == "Handle" {
			foundHandle = true
		}
		if caller.CallCount <= 0 {
			t.Fatalf("expected positive call count, got %+v", caller)
		}
	}
	if !foundHandle {
		t.Fatalf("expected Handle in direct callers, got %+v", payload.DirectCallers)
	}

	if payload.Summary.MatchedDefinitions != len(payload.MatchedDefinitions) {
		t.Fatalf("summary matched_definitions mismatch: %+v", payload.Summary)
	}
	if payload.Summary.DirectCallers != len(payload.DirectCallers) {
		t.Fatalf("summary direct_callers mismatch: %+v", payload.Summary)
	}
	if payload.Summary.TotalIncomingCalls < 1 {
		t.Fatalf("expected total incoming calls >= 1, got %+v", payload.Summary)
	}

	hasGraph, err = db.HasXRefGraphForCommit(ctx, repo.ID, string(commitHash))
	if err != nil {
		t.Fatal(err)
	}
	if !hasGraph {
		t.Fatal("expected impact analysis fallback to persist xref graph")
	}
}

func TestImpactAnalysisEndpointRequiresSymbol(t *testing.T) {
	server, _ := setupTestServer(t)
	ts := httptest.NewServer(server)
	defer ts.Close()

	token := registerAndGetToken(t, ts.URL, "owner")
	createRepo(t, ts.URL, token, "repo", false)

	resp, err := http.Get(ts.URL + "/api/v1/repos/owner/repo/impact/main")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("impact endpoint missing symbol: expected 400, got %d", resp.StatusCode)
	}
	if got := decodeAPIError(t, resp); got != "symbol query parameter is required" {
		t.Fatalf("unexpected missing symbol error: %q", got)
	}
}

func TestImpactAnalysisEndpointUnknownRefReturnsNotFound(t *testing.T) {
	server, _ := setupTestServer(t)
	ts := httptest.NewServer(server)
	defer ts.Close()

	token := registerAndGetToken(t, ts.URL, "owner")
	createRepo(t, ts.URL, token, "repo", false)

	resp, err := http.Get(ts.URL + "/api/v1/repos/owner/repo/impact/main?symbol=ProcessOrder")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("impact endpoint unknown ref: expected 404, got %d", resp.StatusCode)
	}
}

func TestSemverRecommendationEndpoint(t *testing.T) {
	server, db := setupTestServer(t)
	ts := httptest.NewServer(server)
	defer ts.Close()

	token := registerAndGetToken(t, ts.URL, "alice")
	createRepo(t, ts.URL, token, "repo", false)

	repo, err := db.GetRepository(context.Background(), "alice", "repo")
	if err != nil {
		t.Fatal(err)
	}
	store, err := gotstore.Open(repo.StoragePath)
	if err != nil {
		t.Fatal(err)
	}

	baseBlobHash, err := store.Objects.WriteBlob(&object.Blob{Data: []byte("package main\n\nfunc ProcessOrder(input string) int { return 1 }\n")})
	if err != nil {
		t.Fatal(err)
	}
	baseTreeHash, err := store.Objects.WriteTree(&object.TreeObj{
		Entries: []object.TreeEntry{{Name: "main.go", BlobHash: baseBlobHash}},
	})
	if err != nil {
		t.Fatal(err)
	}
	baseCommitHash, err := store.Objects.WriteCommit(&object.CommitObj{
		TreeHash:  baseTreeHash,
		Author:    "alice",
		Timestamp: 1700000000,
		Message:   "base",
	})
	if err != nil {
		t.Fatal(err)
	}

	headBlobHash, err := store.Objects.WriteBlob(&object.Blob{Data: []byte("package main\n\nfunc ProcessOrder(input string, retries int) int { return 1 }\n")})
	if err != nil {
		t.Fatal(err)
	}
	headTreeHash, err := store.Objects.WriteTree(&object.TreeObj{
		Entries: []object.TreeEntry{{Name: "main.go", BlobHash: headBlobHash}},
	})
	if err != nil {
		t.Fatal(err)
	}
	headCommitHash, err := store.Objects.WriteCommit(&object.CommitObj{
		TreeHash:  headTreeHash,
		Parents:   []object.Hash{baseCommitHash},
		Author:    "alice",
		Timestamp: 1700000100,
		Message:   "feature",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Refs.Set("heads/main", baseCommitHash); err != nil {
		t.Fatal(err)
	}
	if err := store.Refs.Set("heads/feature", headCommitHash); err != nil {
		t.Fatal(err)
	}

	resp, err := http.Get(ts.URL + "/api/v1/repos/alice/repo/semver/main...feature")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("semver endpoint: expected 200, got %d", resp.StatusCode)
	}
	var semverResp struct {
		Base            string   `json:"base"`
		Head            string   `json:"head"`
		Bump            string   `json:"bump"`
		BreakingChanges []string `json:"breaking_changes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&semverResp); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if semverResp.Bump != "major" {
		t.Fatalf("expected major bump recommendation, got %+v", semverResp)
	}
	if len(semverResp.BreakingChanges) == 0 {
		t.Fatalf("expected breaking change details, got %+v", semverResp)
	}
}

func TestDiffEndpointIncludesSemanticClassificationSummary(t *testing.T) {
	server, db := setupTestServer(t)
	ts := httptest.NewServer(server)
	defer ts.Close()

	token := registerAndGetToken(t, ts.URL, "alice")
	createRepo(t, ts.URL, token, "repo", false)
	seedSemanticDiffFixture(t, db, "alice", "repo")

	resp, err := http.Get(ts.URL + "/api/v1/repos/alice/repo/diff/main...feature")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("diff endpoint: expected 200, got %d", resp.StatusCode)
	}

	var diffResp semanticDiffTestResponse
	if err := json.NewDecoder(resp.Body).Decode(&diffResp); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	assertSemanticDiffSummary(t, diffResp)
	if diffResp.Semver == nil {
		t.Fatalf("expected semver recommendation in diff response")
	}
	if diffResp.Semver.Bump != "major" {
		t.Fatalf("expected major semver bump in diff response, got %+v", diffResp.Semver)
	}
}

func TestPRDiffEndpointIncludesSemanticClassificationSummary(t *testing.T) {
	server, db := setupTestServer(t)
	ts := httptest.NewServer(server)
	defer ts.Close()

	token := registerAndGetToken(t, ts.URL, "alice")
	createRepo(t, ts.URL, token, "repo", false)
	seedSemanticDiffFixture(t, db, "alice", "repo")

	prNumber := createPRNumber(t, ts.URL, token, "alice", "repo", "feature", "main")

	resp, err := http.Get(fmt.Sprintf("%s/api/v1/repos/alice/repo/pulls/%d/diff", ts.URL, prNumber))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("pr diff endpoint: expected 200, got %d", resp.StatusCode)
	}

	var diffResp semanticDiffTestResponse
	if err := json.NewDecoder(resp.Body).Decode(&diffResp); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	assertSemanticDiffSummary(t, diffResp)
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

func TestMergeGateRequiresEntityOwnerApproval(t *testing.T) {
	server, db := setupTestServer(t)
	ts := httptest.NewServer(server)
	defer ts.Close()

	ownerToken := registerAndGetToken(t, ts.URL, "alice")
	reviewerToken := registerAndGetToken(t, ts.URL, "bob")
	createRepo(t, ts.URL, ownerToken, "repo", false)

	repo, err := db.GetRepository(context.Background(), "alice", "repo")
	if err != nil {
		t.Fatal(err)
	}
	store, err := gotstore.Open(repo.StoragePath)
	if err != nil {
		t.Fatal(err)
	}

	gotOwnersHash, err := store.Objects.WriteBlob(&object.Blob{Data: []byte("func:ProcessOrder @bob\n")})
	if err != nil {
		t.Fatal(err)
	}
	baseBlobHash, err := store.Objects.WriteBlob(&object.Blob{Data: []byte("package main\n\nfunc ProcessOrder() int { return 1 }\n")})
	if err != nil {
		t.Fatal(err)
	}
	baseTreeHash, err := store.Objects.WriteTree(&object.TreeObj{
		Entries: []object.TreeEntry{
			{Name: ".gotowners", BlobHash: gotOwnersHash},
			{Name: "main.go", BlobHash: baseBlobHash},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	baseCommitHash, err := store.Objects.WriteCommit(&object.CommitObj{
		TreeHash:  baseTreeHash,
		Author:    "alice",
		Timestamp: 1700001000,
		Message:   "base",
	})
	if err != nil {
		t.Fatal(err)
	}

	featureBlobHash, err := store.Objects.WriteBlob(&object.Blob{Data: []byte("package main\n\nfunc ProcessOrder() int { return 2 }\n")})
	if err != nil {
		t.Fatal(err)
	}
	featureTreeHash, err := store.Objects.WriteTree(&object.TreeObj{
		Entries: []object.TreeEntry{
			{Name: ".gotowners", BlobHash: gotOwnersHash},
			{Name: "main.go", BlobHash: featureBlobHash},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	featureCommitHash, err := store.Objects.WriteCommit(&object.CommitObj{
		TreeHash:  featureTreeHash,
		Parents:   []object.Hash{baseCommitHash},
		Author:    "alice",
		Timestamp: 1700001100,
		Message:   "feature",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Refs.Set("heads/main", baseCommitHash); err != nil {
		t.Fatal(err)
	}
	if err := store.Refs.Set("heads/feature", featureCommitHash); err != nil {
		t.Fatal(err)
	}

	addCollabBody := `{"username":"bob","role":"write"}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/repos/alice/repo/collaborators", bytes.NewBufferString(addCollabBody))
	req.Header.Set("Authorization", "Bearer "+ownerToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("add collaborator: expected 201, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	prNumber := createPRNumber(t, ts.URL, ownerToken, "alice", "repo", "feature", "main")

	ruleBody := `{
		"enabled": true,
		"require_approvals": false,
		"require_status_checks": false,
		"require_entity_owner_approval": true
	}`
	req, _ = http.NewRequest("PUT", ts.URL+"/api/v1/repos/alice/repo/branch-protection/main", bytes.NewBufferString(ruleBody))
	req.Header.Set("Authorization", "Bearer "+ownerToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
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
	var blockedGate struct {
		Allowed bool     `json:"allowed"`
		Reasons []string `json:"reasons"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&blockedGate); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if blockedGate.Allowed {
		t.Fatalf("expected merge gate to block until owner approval, got %+v", blockedGate)
	}
	if !strings.Contains(strings.Join(blockedGate.Reasons, " "), "@bob") {
		t.Fatalf("expected entity owner reason to mention @bob, got %+v", blockedGate.Reasons)
	}

	reviewBody := `{"state":"approved","body":"owner-approved"}`
	req, _ = http.NewRequest("POST", fmt.Sprintf("%s/api/v1/repos/alice/repo/pulls/%d/reviews", ts.URL, prNumber), bytes.NewBufferString(reviewBody))
	req.Header.Set("Authorization", "Bearer "+reviewerToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create review: expected 201, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	req, _ = http.NewRequest("GET", fmt.Sprintf("%s/api/v1/repos/alice/repo/pulls/%d/merge-gate", ts.URL, prNumber), nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("merge gate endpoint after approval: expected 200, got %d", resp.StatusCode)
	}
	var allowedGate struct {
		Allowed bool     `json:"allowed"`
		Reasons []string `json:"reasons"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&allowedGate); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if !allowedGate.Allowed {
		t.Fatalf("expected merge gate to allow after owner approval, got %+v", allowedGate)
	}
}

func TestMergeGateRequiresLintPass(t *testing.T) {
	server, db := setupTestServer(t)
	ts := httptest.NewServer(server)
	defer ts.Close()

	token := registerAndGetToken(t, ts.URL, "alice")
	createRepo(t, ts.URL, token, "repo", false)

	repo, err := db.GetRepository(context.Background(), "alice", "repo")
	if err != nil {
		t.Fatal(err)
	}
	store, err := gotstore.Open(repo.StoragePath)
	if err != nil {
		t.Fatal(err)
	}

	baseBlobHash, err := store.Objects.WriteBlob(&object.Blob{Data: []byte("package main\n\nfunc ProcessOrder() int { return 1 }\n")})
	if err != nil {
		t.Fatal(err)
	}
	baseTreeHash, err := store.Objects.WriteTree(&object.TreeObj{
		Entries: []object.TreeEntry{{Name: "main.go", BlobHash: baseBlobHash}},
	})
	if err != nil {
		t.Fatal(err)
	}
	baseCommitHash, err := store.Objects.WriteCommit(&object.CommitObj{
		TreeHash:  baseTreeHash,
		Author:    "alice",
		Timestamp: 1700002000,
		Message:   "base",
	})
	if err != nil {
		t.Fatal(err)
	}

	brokenBlobHash, err := store.Objects.WriteBlob(&object.Blob{Data: []byte("package main\n\nfunc ProcessOrder( {\n")})
	if err != nil {
		t.Fatal(err)
	}
	brokenTreeHash, err := store.Objects.WriteTree(&object.TreeObj{
		Entries: []object.TreeEntry{{Name: "main.go", BlobHash: brokenBlobHash}},
	})
	if err != nil {
		t.Fatal(err)
	}
	brokenCommitHash, err := store.Objects.WriteCommit(&object.CommitObj{
		TreeHash:  brokenTreeHash,
		Parents:   []object.Hash{baseCommitHash},
		Author:    "alice",
		Timestamp: 1700002100,
		Message:   "broken",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Refs.Set("heads/main", baseCommitHash); err != nil {
		t.Fatal(err)
	}
	if err := store.Refs.Set("heads/feature", brokenCommitHash); err != nil {
		t.Fatal(err)
	}

	prNumber := createPRNumber(t, ts.URL, token, "alice", "repo", "feature", "main")

	ruleBody := `{
		"enabled": true,
		"require_lint_pass": true
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
	var blockedGate struct {
		Allowed bool     `json:"allowed"`
		Reasons []string `json:"reasons"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&blockedGate); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if blockedGate.Allowed {
		t.Fatalf("expected merge gate to block for lint failures, got %+v", blockedGate)
	}
	joinedReasons := strings.Join(blockedGate.Reasons, " ")
	if !strings.Contains(joinedReasons, "lint parse error") && !strings.Contains(joinedReasons, "lint symbol extraction failed") {
		t.Fatalf("expected lint gate reason, got %+v", blockedGate.Reasons)
	}

	fixedBlobHash, err := store.Objects.WriteBlob(&object.Blob{Data: []byte("package main\n\nfunc ProcessOrder() int { return 2 }\n")})
	if err != nil {
		t.Fatal(err)
	}
	fixedTreeHash, err := store.Objects.WriteTree(&object.TreeObj{
		Entries: []object.TreeEntry{{Name: "main.go", BlobHash: fixedBlobHash}},
	})
	if err != nil {
		t.Fatal(err)
	}
	fixedCommitHash, err := store.Objects.WriteCommit(&object.CommitObj{
		TreeHash:  fixedTreeHash,
		Parents:   []object.Hash{brokenCommitHash},
		Author:    "alice",
		Timestamp: 1700002200,
		Message:   "fix lint",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Refs.Set("heads/feature", fixedCommitHash); err != nil {
		t.Fatal(err)
	}

	req, _ = http.NewRequest("GET", fmt.Sprintf("%s/api/v1/repos/alice/repo/pulls/%d/merge-gate", ts.URL, prNumber), nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("merge gate endpoint after lint fix: expected 200, got %d", resp.StatusCode)
	}
	var allowedGate struct {
		Allowed bool     `json:"allowed"`
		Reasons []string `json:"reasons"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&allowedGate); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if !allowedGate.Allowed {
		t.Fatalf("expected merge gate to allow after lint fix, got %+v", allowedGate)
	}
}

func TestMergeGateRequiresNoNewDeadCode(t *testing.T) {
	server, db := setupTestServer(t)
	ts := httptest.NewServer(server)
	defer ts.Close()

	token := registerAndGetToken(t, ts.URL, "alice")
	createRepo(t, ts.URL, token, "repo", false)

	repo, err := db.GetRepository(context.Background(), "alice", "repo")
	if err != nil {
		t.Fatal(err)
	}
	store, err := gotstore.Open(repo.StoragePath)
	if err != nil {
		t.Fatal(err)
	}

	baseBlobHash, err := store.Objects.WriteBlob(&object.Blob{Data: []byte("package main\n\nfunc Existing() {}\n")})
	if err != nil {
		t.Fatal(err)
	}
	baseTreeHash, err := store.Objects.WriteTree(&object.TreeObj{
		Entries: []object.TreeEntry{{Name: "main.go", BlobHash: baseBlobHash}},
	})
	if err != nil {
		t.Fatal(err)
	}
	baseCommitHash, err := store.Objects.WriteCommit(&object.CommitObj{
		TreeHash:  baseTreeHash,
		Author:    "alice",
		Timestamp: 1700003000,
		Message:   "base",
	})
	if err != nil {
		t.Fatal(err)
	}

	deadBlobHash, err := store.Objects.WriteBlob(&object.Blob{Data: []byte("package main\n\nfunc Existing() {}\nfunc NewUnused() {}\n")})
	if err != nil {
		t.Fatal(err)
	}
	deadTreeHash, err := store.Objects.WriteTree(&object.TreeObj{
		Entries: []object.TreeEntry{{Name: "main.go", BlobHash: deadBlobHash}},
	})
	if err != nil {
		t.Fatal(err)
	}
	deadCommitHash, err := store.Objects.WriteCommit(&object.CommitObj{
		TreeHash:  deadTreeHash,
		Parents:   []object.Hash{baseCommitHash},
		Author:    "alice",
		Timestamp: 1700003100,
		Message:   "introduce dead code",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Refs.Set("heads/main", baseCommitHash); err != nil {
		t.Fatal(err)
	}
	if err := store.Refs.Set("heads/feature", deadCommitHash); err != nil {
		t.Fatal(err)
	}

	prNumber := createPRNumber(t, ts.URL, token, "alice", "repo", "feature", "main")

	ruleBody := `{
		"enabled": true,
		"require_no_new_dead_code": true
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
	var blockedGate struct {
		Allowed bool     `json:"allowed"`
		Reasons []string `json:"reasons"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&blockedGate); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if blockedGate.Allowed {
		t.Fatalf("expected merge gate to block for dead code, got %+v", blockedGate)
	}
	if !strings.Contains(strings.Join(blockedGate.Reasons, " "), "appears unused") {
		t.Fatalf("expected dead code reason, got %+v", blockedGate.Reasons)
	}

	fixedBlobHash, err := store.Objects.WriteBlob(&object.Blob{Data: []byte("package main\n\nfunc Existing() {}\nfunc NewUnused() {}\nfunc Use() { NewUnused() }\n")})
	if err != nil {
		t.Fatal(err)
	}
	fixedTreeHash, err := store.Objects.WriteTree(&object.TreeObj{
		Entries: []object.TreeEntry{{Name: "main.go", BlobHash: fixedBlobHash}},
	})
	if err != nil {
		t.Fatal(err)
	}
	fixedCommitHash, err := store.Objects.WriteCommit(&object.CommitObj{
		TreeHash:  fixedTreeHash,
		Parents:   []object.Hash{deadCommitHash},
		Author:    "alice",
		Timestamp: 1700003200,
		Message:   "use new symbol",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Refs.Set("heads/feature", fixedCommitHash); err != nil {
		t.Fatal(err)
	}

	req, _ = http.NewRequest("GET", fmt.Sprintf("%s/api/v1/repos/alice/repo/pulls/%d/merge-gate", ts.URL, prNumber), nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("merge gate endpoint after dead-code fix: expected 200, got %d", resp.StatusCode)
	}
	var allowedGate struct {
		Allowed bool     `json:"allowed"`
		Reasons []string `json:"reasons"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&allowedGate); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if !allowedGate.Allowed {
		t.Fatalf("expected merge gate to allow after dead-code fix, got %+v", allowedGate)
	}
}

func TestMergeGateRequiresSignedCommits(t *testing.T) {
	server, db := setupTestServer(t)
	ts := httptest.NewServer(server)
	defer ts.Close()

	token := registerAndGetToken(t, ts.URL, "alice")
	createRepo(t, ts.URL, token, "repo", false)

	ctx := context.Background()
	user, err := db.GetUserByUsername(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	repo, err := db.GetRepository(ctx, "alice", "repo")
	if err != nil {
		t.Fatal(err)
	}
	store, err := gotstore.Open(repo.StoragePath)
	if err != nil {
		t.Fatal(err)
	}

	signer, pubKeyText, fp := newTestSSHSigner(t)
	if err := db.CreateSSHKey(ctx, &models.SSHKey{
		UserID:      user.ID,
		Name:        "default",
		Fingerprint: fp,
		PublicKey:   pubKeyText,
		KeyType:     signer.PublicKey().Type(),
	}); err != nil {
		t.Fatal(err)
	}

	baseBlobHash, err := store.Objects.WriteBlob(&object.Blob{Data: []byte("package main\n\nfunc ProcessOrder() int { return 1 }\n")})
	if err != nil {
		t.Fatal(err)
	}
	baseTreeHash, err := store.Objects.WriteTree(&object.TreeObj{
		Entries: []object.TreeEntry{{Name: "main.go", BlobHash: baseBlobHash}},
	})
	if err != nil {
		t.Fatal(err)
	}
	baseCommitHash, err := store.Objects.WriteCommit(&object.CommitObj{
		TreeHash:  baseTreeHash,
		Author:    "alice",
		Timestamp: 1700004000,
		Message:   "base",
	})
	if err != nil {
		t.Fatal(err)
	}

	signedBlobHash, err := store.Objects.WriteBlob(&object.Blob{Data: []byte("package main\n\nfunc ProcessOrder() int { return 2 }\n")})
	if err != nil {
		t.Fatal(err)
	}
	signedTreeHash, err := store.Objects.WriteTree(&object.TreeObj{
		Entries: []object.TreeEntry{{Name: "main.go", BlobHash: signedBlobHash}},
	})
	if err != nil {
		t.Fatal(err)
	}
	signedCommit := &object.CommitObj{
		TreeHash:  signedTreeHash,
		Parents:   []object.Hash{baseCommitHash},
		Author:    "alice",
		Timestamp: 1700004100,
		Message:   "signed feature",
	}
	signedCommit.Signature = signTestCommit(t, signer, signedCommit)
	signedCommitHash, err := store.Objects.WriteCommit(signedCommit)
	if err != nil {
		t.Fatal(err)
	}

	if err := store.Refs.Set("heads/main", baseCommitHash); err != nil {
		t.Fatal(err)
	}
	if err := store.Refs.Set("heads/feature", signedCommitHash); err != nil {
		t.Fatal(err)
	}

	prNumber := createPRNumber(t, ts.URL, token, "alice", "repo", "feature", "main")

	ruleBody := `{
		"enabled": true,
		"require_signed_commits": true
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
	var signedGate struct {
		Allowed bool     `json:"allowed"`
		Reasons []string `json:"reasons"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&signedGate); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if !signedGate.Allowed {
		t.Fatalf("expected merge gate to allow signed-commit PR, got %+v", signedGate)
	}

	unsignedBlobHash, err := store.Objects.WriteBlob(&object.Blob{Data: []byte("package main\n\nfunc ProcessOrder() int { return 3 }\n")})
	if err != nil {
		t.Fatal(err)
	}
	unsignedTreeHash, err := store.Objects.WriteTree(&object.TreeObj{
		Entries: []object.TreeEntry{{Name: "main.go", BlobHash: unsignedBlobHash}},
	})
	if err != nil {
		t.Fatal(err)
	}
	unsignedCommitHash, err := store.Objects.WriteCommit(&object.CommitObj{
		TreeHash:  unsignedTreeHash,
		Parents:   []object.Hash{signedCommitHash},
		Author:    "alice",
		Timestamp: 1700004200,
		Message:   "unsigned feature",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Refs.Set("heads/feature", unsignedCommitHash); err != nil {
		t.Fatal(err)
	}

	req, _ = http.NewRequest("GET", fmt.Sprintf("%s/api/v1/repos/alice/repo/pulls/%d/merge-gate", ts.URL, prNumber), nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("merge gate endpoint with unsigned commit: expected 200, got %d", resp.StatusCode)
	}
	var blockedGate struct {
		Allowed bool     `json:"allowed"`
		Reasons []string `json:"reasons"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&blockedGate); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if blockedGate.Allowed {
		t.Fatalf("expected merge gate to block unsigned commit, got %+v", blockedGate)
	}
	if !strings.Contains(strings.Join(blockedGate.Reasons, " "), "not signed") {
		t.Fatalf("expected unsigned-commit reason, got %+v", blockedGate.Reasons)
	}

	if err := store.Refs.Set("heads/feature", signedCommitHash); err != nil {
		t.Fatal(err)
	}
	req, _ = http.NewRequest("GET", fmt.Sprintf("%s/api/v1/repos/alice/repo/pulls/%d/merge-gate", ts.URL, prNumber), nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("merge gate endpoint after signed reset: expected 200, got %d", resp.StatusCode)
	}
	var allowedAgain struct {
		Allowed bool     `json:"allowed"`
		Reasons []string `json:"reasons"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&allowedAgain); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if !allowedAgain.Allowed {
		t.Fatalf("expected merge gate to allow after removing unsigned commit, got %+v", allowedAgain)
	}
}

func TestMergeProducesEntityEnrichedTree(t *testing.T) {
	server, db := setupTestServer(t)
	ts := httptest.NewServer(server)
	defer ts.Close()

	token := registerAndGetToken(t, ts.URL, "alice")
	createRepo(t, ts.URL, token, "repo", false)

	repo, err := db.GetRepository(context.Background(), "alice", "repo")
	if err != nil {
		t.Fatal(err)
	}
	store, err := gotstore.Open(repo.StoragePath)
	if err != nil {
		t.Fatal(err)
	}

	baseBlobHash, err := store.Objects.WriteBlob(&object.Blob{Data: []byte("package main\n\nfunc ProcessOrder() int { return 1 }\n")})
	if err != nil {
		t.Fatal(err)
	}
	baseTreeHash, err := store.Objects.WriteTree(&object.TreeObj{
		Entries: []object.TreeEntry{
			{Name: "main.go", BlobHash: baseBlobHash},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	baseCommitHash, err := store.Objects.WriteCommit(&object.CommitObj{
		TreeHash:  baseTreeHash,
		Author:    "alice",
		Timestamp: 1700000000,
		Message:   "base",
	})
	if err != nil {
		t.Fatal(err)
	}

	featureBlobHash, err := store.Objects.WriteBlob(&object.Blob{Data: []byte("package main\n\nfunc ProcessOrder() int { return 2 }\n")})
	if err != nil {
		t.Fatal(err)
	}
	featureTreeHash, err := store.Objects.WriteTree(&object.TreeObj{
		Entries: []object.TreeEntry{
			{Name: "main.go", BlobHash: featureBlobHash},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	featureCommitHash, err := store.Objects.WriteCommit(&object.CommitObj{
		TreeHash:  featureTreeHash,
		Parents:   []object.Hash{baseCommitHash},
		Author:    "alice",
		Timestamp: 1700000100,
		Message:   "feature",
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := store.Refs.Set("heads/main", baseCommitHash); err != nil {
		t.Fatal(err)
	}
	if err := store.Refs.Set("heads/feature", featureCommitHash); err != nil {
		t.Fatal(err)
	}

	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/repos/alice/repo/pulls", bytes.NewBufferString(`{"title":"merge","source_branch":"feature","target_branch":"main"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
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
	if prResp.Number == 0 {
		t.Fatal("expected created PR number")
	}

	req, _ = http.NewRequest("POST", fmt.Sprintf("%s/api/v1/repos/alice/repo/pulls/%d/merge", ts.URL, prResp.Number), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("merge PR: expected 200, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	mergedHead, err := store.Refs.Get("heads/main")
	if err != nil {
		t.Fatal(err)
	}
	mergedCommit, err := store.Objects.ReadCommit(mergedHead)
	if err != nil {
		t.Fatal(err)
	}
	mergedTree, err := store.Objects.ReadTree(mergedCommit.TreeHash)
	if err != nil {
		t.Fatal(err)
	}
	if len(mergedTree.Entries) != 1 || mergedTree.Entries[0].Name != "main.go" {
		t.Fatalf("unexpected merged tree entries: %+v", mergedTree.Entries)
	}
	if mergedTree.Entries[0].EntityListHash == "" {
		t.Fatalf("expected merged tree entry to include entity list hash, got %+v", mergedTree.Entries[0])
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

func TestPullRequestWebhookIncludesEntityChanges(t *testing.T) {
	server, db := setupTestServer(t)
	ts := httptest.NewServer(server)
	defer ts.Close()

	token := registerAndGetToken(t, ts.URL, "alice")
	createRepo(t, ts.URL, token, "repo", false)

	repo, err := db.GetRepository(context.Background(), "alice", "repo")
	if err != nil {
		t.Fatal(err)
	}
	store, err := gotstore.Open(repo.StoragePath)
	if err != nil {
		t.Fatal(err)
	}

	baseBlobHash, err := store.Objects.WriteBlob(&object.Blob{Data: []byte("package main\n\nfunc ProcessOrder() int { return 1 }\n")})
	if err != nil {
		t.Fatal(err)
	}
	baseTreeHash, err := store.Objects.WriteTree(&object.TreeObj{
		Entries: []object.TreeEntry{{Name: "main.go", BlobHash: baseBlobHash}},
	})
	if err != nil {
		t.Fatal(err)
	}
	baseCommitHash, err := store.Objects.WriteCommit(&object.CommitObj{
		TreeHash:  baseTreeHash,
		Author:    "alice",
		Timestamp: 1700000000,
		Message:   "base",
	})
	if err != nil {
		t.Fatal(err)
	}

	headBlobHash, err := store.Objects.WriteBlob(&object.Blob{Data: []byte("package main\n\nfunc ProcessOrder() int { return 2 }\n")})
	if err != nil {
		t.Fatal(err)
	}
	headTreeHash, err := store.Objects.WriteTree(&object.TreeObj{
		Entries: []object.TreeEntry{{Name: "main.go", BlobHash: headBlobHash}},
	})
	if err != nil {
		t.Fatal(err)
	}
	headCommitHash, err := store.Objects.WriteCommit(&object.CommitObj{
		TreeHash:  headTreeHash,
		Parents:   []object.Hash{baseCommitHash},
		Author:    "alice",
		Timestamp: 1700000100,
		Message:   "feature",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Refs.Set("heads/main", baseCommitHash); err != nil {
		t.Fatal(err)
	}
	if err := store.Refs.Set("heads/feature", headCommitHash); err != nil {
		t.Fatal(err)
	}

	type prWebhookPayload struct {
		Action          string `json:"action"`
		EntitiesChanged []struct {
			File string `json:"file"`
			Type string `json:"type"`
			Key  string `json:"key"`
		} `json:"entities_changed"`
		EntitiesAdded    int `json:"entities_added"`
		EntitiesRemoved  int `json:"entities_removed"`
		EntitiesModified int `json:"entities_modified"`
	}

	payloadCh := make(chan prWebhookPayload, 1)
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Gothub-Event") != "pull_request" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		var payload prWebhookPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		select {
		case payloadCh <- payload:
		default:
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer receiver.Close()

	createHookBody := fmt.Sprintf(`{"url":"%s","secret":"topsecret","events":["pull_request"],"active":true}`, receiver.URL)
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
	resp.Body.Close()

	req, _ = http.NewRequest("POST", ts.URL+"/api/v1/repos/alice/repo/pulls", bytes.NewBufferString(`{"title":"entity webhook","source_branch":"feature","target_branch":"main"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create PR: expected 201, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	select {
	case payload := <-payloadCh:
		if payload.Action != "opened" {
			t.Fatalf("expected pull_request action opened, got %q", payload.Action)
		}
		if payload.EntitiesModified < 1 {
			t.Fatalf("expected at least one modified entity, got %+v", payload)
		}
		if len(payload.EntitiesChanged) == 0 {
			t.Fatalf("expected entities_changed entries, got %+v", payload)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("timed out waiting for pull_request webhook payload")
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
	commentReq := `{"body":"Looks good","entity_key":"ProcessOrder","entity_stable_id":"ent_process_order"}`
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
	var createdComment struct {
		EntityKey      string `json:"entity_key"`
		EntityStableID string `json:"entity_stable_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&createdComment); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if createdComment.EntityKey != "ProcessOrder" || createdComment.EntityStableID != "ent_process_order" {
		t.Fatalf("unexpected PR comment anchors: %+v", createdComment)
	}

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

func TestPRAndIssueEndpointsRejectInvalidNumbers(t *testing.T) {
	server, _ := setupTestServer(t)
	ts := httptest.NewServer(server)
	defer ts.Close()

	token := registerAndGetToken(t, ts.URL, "alice")
	createRepo(t, ts.URL, token, "repo", false)

	cases := []struct {
		name    string
		path    string
		wantErr string
	}{
		{
			name:    "pull request not a number",
			path:    "/api/v1/repos/alice/repo/pulls/not-a-number",
			wantErr: "invalid pull request number",
		},
		{
			name:    "pull request negative number",
			path:    "/api/v1/repos/alice/repo/pulls/-1",
			wantErr: "invalid pull request number",
		},
		{
			name:    "issue not a number",
			path:    "/api/v1/repos/alice/repo/issues/not-a-number",
			wantErr: "invalid issue number",
		},
		{
			name:    "issue zero",
			path:    "/api/v1/repos/alice/repo/issues/0",
			wantErr: "invalid issue number",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := http.Get(ts.URL + tc.path)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("expected 400 for %s, got %d", tc.path, resp.StatusCode)
			}
			if got := decodeAPIError(t, resp); got != tc.wantErr {
				t.Fatalf("expected error %q, got %q", tc.wantErr, got)
			}
		})
	}
}

func TestDeleteCommentEndpointsRejectInvalidCommentID(t *testing.T) {
	server, _ := setupTestServer(t)
	ts := httptest.NewServer(server)
	defer ts.Close()

	token := registerAndGetToken(t, ts.URL, "alice")
	createRepo(t, ts.URL, token, "repo", false)

	cases := []struct {
		name string
		path string
	}{
		{
			name: "pr comment negative id",
			path: "/api/v1/repos/alice/repo/pulls/1/comments/-1",
		},
		{
			name: "issue comment not a number",
			path: "/api/v1/repos/alice/repo/issues/1/comments/not-a-number",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodDelete, ts.URL+tc.path, nil)
			req.Header.Set("Authorization", "Bearer "+token)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("expected 400 for %s, got %d", tc.path, resp.StatusCode)
			}
			if got := decodeAPIError(t, resp); got != "invalid comment id" {
				t.Fatalf("expected non-leaky parse error, got %q", got)
			}
		})
	}
}

func pushSimpleGoCommit(t *testing.T, baseURL, owner, repo string) string {
	t.Helper()

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

	req, _ := http.NewRequest("POST", fmt.Sprintf("%s/git/%s/%s/git-receive-pack", baseURL, owner, repo), bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/x-git-receive-pack-request")
	req.SetBasicAuth(owner, "secret123")
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

	return string(commitHash)
}

func decodeAPIError(t *testing.T, resp *http.Response) string {
	t.Helper()
	var payload struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode error payload: %v", err)
	}
	return payload.Error
}

type semanticDiffTestResponse struct {
	Summary struct {
		TotalChanges     int `json:"total_changes"`
		Additions        int `json:"additions"`
		Removals         int `json:"removals"`
		SignatureChanges int `json:"signature_changes"`
		BodyOnlyChanges  int `json:"body_only_changes"`
		OtherChanges     int `json:"other_changes"`
	} `json:"summary"`
	Semver *struct {
		Bump string `json:"bump"`
	} `json:"semver,omitempty"`
	Files []struct {
		Changes []struct {
			Classification string `json:"classification"`
		} `json:"changes"`
	} `json:"files"`
}

func assertSemanticDiffSummary(t *testing.T, resp semanticDiffTestResponse) {
	t.Helper()

	classCounts := map[string]int{}
	totalChanges := 0
	for _, file := range resp.Files {
		for _, change := range file.Changes {
			classification := strings.TrimSpace(change.Classification)
			if classification == "" {
				t.Fatalf("expected classification on every change, got empty value")
			}
			classCounts[classification]++
			totalChanges++
		}
	}
	if totalChanges == 0 {
		t.Fatalf("expected at least one semantic change")
	}

	if resp.Summary.TotalChanges != totalChanges {
		t.Fatalf("summary total_changes mismatch: got %d want %d", resp.Summary.TotalChanges, totalChanges)
	}
	if resp.Summary.Additions != classCounts["addition"] {
		t.Fatalf("summary additions mismatch: got %d want %d", resp.Summary.Additions, classCounts["addition"])
	}
	if resp.Summary.Removals != classCounts["removal"] {
		t.Fatalf("summary removals mismatch: got %d want %d", resp.Summary.Removals, classCounts["removal"])
	}
	if resp.Summary.SignatureChanges != classCounts["signature_change"] {
		t.Fatalf("summary signature_changes mismatch: got %d want %d", resp.Summary.SignatureChanges, classCounts["signature_change"])
	}
	if resp.Summary.BodyOnlyChanges != classCounts["body_only_change"] {
		t.Fatalf("summary body_only_changes mismatch: got %d want %d", resp.Summary.BodyOnlyChanges, classCounts["body_only_change"])
	}

	if classCounts["signature_change"] == 0 {
		t.Fatalf("expected at least one signature_change classification, got %+v", classCounts)
	}
	if classCounts["body_only_change"] == 0 {
		t.Fatalf("expected at least one body_only_change classification, got %+v", classCounts)
	}
}

func seedSemanticDiffFixture(t *testing.T, db database.DB, owner, repo string) {
	t.Helper()

	repoModel, err := db.GetRepository(context.Background(), owner, repo)
	if err != nil {
		t.Fatal(err)
	}
	store, err := gotstore.Open(repoModel.StoragePath)
	if err != nil {
		t.Fatal(err)
	}

	baseContent := `package main

func ProcessOrder(input string) int {
	return 1
}

func applyDiscount(amount int) int {
	return amount
}

func LegacyEndpoint() int {
	return 1
}
`
	featureContent := `package main

func ProcessOrder(input string, retries int) int {
	return retries
}

func applyDiscount(amount int) int {
	return amount + 1
}

func NewFeature() int {
	return 42
}
`

	baseCommitHash := writeSemanticDiffCommit(t, store, "main.go", baseContent, nil, 1700005000, "base")
	featureCommitHash := writeSemanticDiffCommit(t, store, "main.go", featureContent, []object.Hash{baseCommitHash}, 1700005100, "feature")

	if err := store.Refs.Set("heads/main", baseCommitHash); err != nil {
		t.Fatal(err)
	}
	if err := store.Refs.Set("heads/feature", featureCommitHash); err != nil {
		t.Fatal(err)
	}
}

func writeSemanticDiffCommit(t *testing.T, store *gotstore.RepoStore, path, content string, parents []object.Hash, ts int64, message string) object.Hash {
	t.Helper()

	blobHash, err := store.Objects.WriteBlob(&object.Blob{Data: []byte(content)})
	if err != nil {
		t.Fatal(err)
	}
	treeHash, err := store.Objects.WriteTree(&object.TreeObj{
		Entries: []object.TreeEntry{{Name: path, BlobHash: blobHash}},
	})
	if err != nil {
		t.Fatal(err)
	}
	commitHash, err := store.Objects.WriteCommit(&object.CommitObj{
		TreeHash:  treeHash,
		Parents:   parents,
		Author:    "alice",
		Timestamp: ts,
		Message:   message,
	})
	if err != nil {
		t.Fatal(err)
	}
	return commitHash
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

func newTestSSHSigner(t *testing.T) (ssh.Signer, string, string) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	pubText := string(ssh.MarshalAuthorizedKey(signer.PublicKey()))
	fingerprint := fmt.Sprintf("%x", md5.Sum(signer.PublicKey().Marshal()))
	return signer, pubText, fingerprint
}

func signTestCommit(t *testing.T, signer ssh.Signer, commit *object.CommitObj) string {
	t.Helper()
	copyCommit := *commit
	copyCommit.Signature = ""
	payload := object.MarshalCommit(&copyCommit)
	sig, err := signer.Sign(rand.Reader, payload)
	if err != nil {
		t.Fatal(err)
	}
	pubB64 := base64.StdEncoding.EncodeToString(signer.PublicKey().Marshal())
	sigB64 := base64.StdEncoding.EncodeToString(sig.Blob)
	return fmt.Sprintf("sshsig-v1:%s:%s:%s", sig.Format, pubB64, sigB64)
}

func pktLineForTest(payload string) []byte {
	return []byte(fmt.Sprintf("%04x%s", len(payload)+4, payload))
}

func pktFlushForTest() []byte {
	return []byte("0000")
}
