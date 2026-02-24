package gitinterop

import (
	"bufio"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odvcencio/got/pkg/object"
	"github.com/odvcencio/gothub/internal/database"
	"github.com/odvcencio/gothub/internal/gotstore"
	"github.com/odvcencio/gothub/internal/models"
)

func TestUploadPackCorruptObjectGraphReturnsProtocolErrorPacket(t *testing.T) {
	store, db, repoID, owner, repo, cleanup := setupUploadPackTestRepo(t)
	defer cleanup()

	commitHash, err := store.Objects.WriteCommit(&object.CommitObj{
		TreeHash:  object.Hash(strings.Repeat("a", 64)),
		Author:    "Alice <alice@example.com>",
		Timestamp: 1700000000,
		Message:   "broken commit",
	})
	if err != nil {
		t.Fatal(err)
	}

	gitHash := strings.Repeat("1", 40)
	if err := db.SetHashMapping(context.Background(), &models.HashMapping{
		RepoID:     repoID,
		GotHash:    string(commitHash),
		GitHash:    gitHash,
		ObjectType: "commit",
	}); err != nil {
		t.Fatal(err)
	}

	h := NewSmartHTTPHandler(
		func(ownerArg, repoArg string) (*gotstore.RepoStore, error) {
			if ownerArg == owner && repoArg == repo {
				return store, nil
			}
			return nil, errRepoNotFound
		},
		db,
		func(ctx context.Context, ownerArg, repoArg string) (int64, error) {
			if ownerArg == owner && repoArg == repo {
				return repoID, nil
			}
			return 0, errRepoNotFound
		},
		nil,
		nil,
	)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	payload := append(pktLine("want "+gitHash+"\n"), pktFlush()...)
	payload = append(payload, pktLine("done\n")...)
	payload = append(payload, pktFlush()...)

	req, _ := http.NewRequest("POST", ts.URL+"/git/"+owner+"/"+repo+"/git-upload-pack", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/x-git-upload-pack-request")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for corrupt object graph, got %d", resp.StatusCode)
	}
	if !strings.Contains(resp.Header.Get("Content-Type"), "application/x-git-upload-pack-result") {
		t.Fatalf("expected upload-pack result content type, got %q", resp.Header.Get("Content-Type"))
	}
	var body bytes.Buffer
	if _, err := body.ReadFrom(resp.Body); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	line, err := readPktLine(bufio.NewReader(bytes.NewReader(body.Bytes())))
	if err != nil {
		t.Fatalf("read protocol error line: %v", err)
	}
	if line == nil {
		t.Fatal("expected ERR pkt-line, got flush")
	}
	if !strings.HasPrefix(string(line), "ERR ") {
		t.Fatalf("expected ERR pkt-line, got %q", string(line))
	}
	if !strings.Contains(string(line), "invalid object graph") {
		t.Fatalf("expected object graph detail in ERR pkt-line, got %q", string(line))
	}
}

func TestUploadPackCorruptObjectGraphReturnsSidebandError(t *testing.T) {
	store, db, repoID, owner, repo, cleanup := setupUploadPackTestRepo(t)
	defer cleanup()

	commitHash, err := store.Objects.WriteCommit(&object.CommitObj{
		TreeHash:  object.Hash(strings.Repeat("b", 64)),
		Author:    "Alice <alice@example.com>",
		Timestamp: 1700000000,
		Message:   "broken commit",
	})
	if err != nil {
		t.Fatal(err)
	}

	gitHash := strings.Repeat("2", 40)
	if err := db.SetHashMapping(context.Background(), &models.HashMapping{
		RepoID:     repoID,
		GotHash:    string(commitHash),
		GitHash:    gitHash,
		ObjectType: "commit",
	}); err != nil {
		t.Fatal(err)
	}

	h := NewSmartHTTPHandler(
		func(ownerArg, repoArg string) (*gotstore.RepoStore, error) {
			if ownerArg == owner && repoArg == repo {
				return store, nil
			}
			return nil, errRepoNotFound
		},
		db,
		func(ctx context.Context, ownerArg, repoArg string) (int64, error) {
			if ownerArg == owner && repoArg == repo {
				return repoID, nil
			}
			return 0, errRepoNotFound
		},
		nil,
		nil,
	)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	payload := append(pktLine("want "+gitHash+"\x00side-band-64k ofs-delta\n"), pktFlush()...)
	payload = append(payload, pktLine("done\n")...)
	payload = append(payload, pktFlush()...)

	req, _ := http.NewRequest("POST", ts.URL+"/git/"+owner+"/"+repo+"/git-upload-pack", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/x-git-upload-pack-request")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for corrupt object graph, got %d", resp.StatusCode)
	}
	var body bytes.Buffer
	if _, err := body.ReadFrom(resp.Body); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	rd := bufio.NewReader(bytes.NewReader(body.Bytes()))
	line, err := readPktLine(rd)
	if err != nil {
		t.Fatalf("read sideband error line: %v", err)
	}
	if len(line) < 2 || line[0] != 3 {
		t.Fatalf("expected sideband channel 3 error packet, got %q", string(line))
	}
	if !strings.Contains(string(line[1:]), "invalid object graph") {
		t.Fatalf("expected object graph detail in sideband error packet, got %q", string(line[1:]))
	}
	flush, err := readPktLine(rd)
	if err != nil {
		t.Fatalf("read trailing flush: %v", err)
	}
	if flush != nil {
		t.Fatalf("expected trailing flush packet, got payload %q", string(flush))
	}
}

var errRepoNotFound = &repoNotFoundError{}

type repoNotFoundError struct{}

func (e *repoNotFoundError) Error() string { return "repository not found" }

func setupUploadPackTestRepo(t *testing.T) (*gotstore.RepoStore, database.DB, int64, string, string, func()) {
	t.Helper()

	ctx := context.Background()
	tmpDir := t.TempDir()

	db, err := database.OpenSQLite(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	owner := "alice"
	repoName := "repo"
	user := &models.User{Username: owner, Email: owner + "@example.com", PasswordHash: "x"}
	if err := db.CreateUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	repo := &models.Repository{
		OwnerUserID:   &user.ID,
		Name:          repoName,
		DefaultBranch: "main",
		StoragePath:   filepath.Join(tmpDir, "repo"),
	}
	if err := db.CreateRepository(ctx, repo); err != nil {
		t.Fatal(err)
	}

	store, err := gotstore.Open(repo.StoragePath)
	if err != nil {
		t.Fatal(err)
	}

	cleanup := func() {
		_ = db.Close()
	}
	return store, db, repo.ID, owner, repoName, cleanup
}
