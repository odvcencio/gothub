package gotprotocol

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odvcencio/got/pkg/object"
	"github.com/odvcencio/gothub/internal/gotstore"
)

func TestPushObjectsRejectsCommitWithMissingTree(t *testing.T) {
	store, err := gotstore.Open(filepath.Join(t.TempDir(), "repo"))
	if err != nil {
		t.Fatal(err)
	}

	h := NewHandler(func(owner, repo string) (*gotstore.RepoStore, error) { return store, nil }, nil, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	commit := &object.CommitObj{
		TreeHash:  object.Hash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		Author:    "Alice <alice@example.com>",
		Timestamp: 1700000000,
		Message:   "bad commit",
	}
	commitData := object.MarshalCommit(commit)
	commitHash := object.HashObject(object.TypeCommit, commitData)

	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(map[string]any{
		"type": "commit",
		"data": commitData,
	}); err != nil {
		t.Fatal(err)
	}

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/got/alice/repo/objects", &body)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid commit push, got %d", resp.StatusCode)
	}
	if store.Objects.Has(commitHash) {
		t.Fatalf("invalid commit object should not be persisted")
	}
}

func TestPushObjectsRejectsProvidedHashMismatch(t *testing.T) {
	store, err := gotstore.Open(filepath.Join(t.TempDir(), "repo"))
	if err != nil {
		t.Fatal(err)
	}

	h := NewHandler(func(owner, repo string) (*gotstore.RepoStore, error) { return store, nil }, nil, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	blobData := object.MarshalBlob(&object.Blob{Data: []byte("hello\n")})
	computed := object.HashObject(object.TypeBlob, blobData)

	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(map[string]any{
		"hash": strings.Repeat("a", 64),
		"type": "blob",
		"data": blobData,
	}); err != nil {
		t.Fatal(err)
	}

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/got/alice/repo/objects", &body)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var respBody bytes.Buffer
	if _, err := respBody.ReadFrom(resp.Body); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for hash mismatch, got %d", resp.StatusCode)
	}
	if !strings.Contains(respBody.String(), "hash mismatch") {
		t.Fatalf("expected hash mismatch error, got %q", respBody.String())
	}
	if store.Objects.Has(computed) {
		t.Fatalf("mismatched object should not be persisted")
	}
}

func TestUpdateRefsExtractsEntitiesForCommit(t *testing.T) {
	store, err := gotstore.Open(filepath.Join(t.TempDir(), "repo"))
	if err != nil {
		t.Fatal(err)
	}

	h := NewHandler(func(owner, repo string) (*gotstore.RepoStore, error) { return store, nil }, nil, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	blobHash, err := store.Objects.WriteBlob(&object.Blob{Data: []byte("package main\n\nfunc ProcessOrder() int { return 1 }\n")})
	if err != nil {
		t.Fatal(err)
	}
	treeHash, err := store.Objects.WriteTree(&object.TreeObj{
		Entries: []object.TreeEntry{
			{Name: "main.go", BlobHash: blobHash},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	commitHash, err := store.Objects.WriteCommit(&object.CommitObj{
		TreeHash:  treeHash,
		Author:    "Alice <alice@example.com>",
		Timestamp: 1700000000,
		Message:   "initial",
	})
	if err != nil {
		t.Fatal(err)
	}

	updateBody, err := json.Marshal(map[string]string{"heads/main": string(commitHash)})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(ts.URL+"/got/alice/repo/refs", "application/json", bytes.NewReader(updateBody))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 updating refs, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	headHash, err := store.Refs.Get("heads/main")
	if err != nil {
		t.Fatal(err)
	}
	if headHash == commitHash {
		t.Fatalf("expected commit to be rewritten with entity lists")
	}
	updatedCommit, err := store.Objects.ReadCommit(headHash)
	if err != nil {
		t.Fatal(err)
	}
	updatedTree, err := store.Objects.ReadTree(updatedCommit.TreeHash)
	if err != nil {
		t.Fatal(err)
	}
	if len(updatedTree.Entries) != 1 {
		t.Fatalf("expected one tree entry, got %d", len(updatedTree.Entries))
	}
	if updatedTree.Entries[0].EntityListHash == "" {
		t.Fatalf("expected entity list hash to be populated on rewritten tree")
	}
}

func TestWalkObjectsIncludesEntitiesFromEntityList(t *testing.T) {
	store, err := gotstore.Open(filepath.Join(t.TempDir(), "repo"))
	if err != nil {
		t.Fatal(err)
	}

	entityBody := []byte("func ProcessOrder() int { return 1 }")
	entityHash, err := store.Objects.WriteEntity(&object.EntityObj{
		Kind:     "declaration",
		Name:     "ProcessOrder",
		DeclKind: "function",
		Body:     entityBody,
		BodyHash: object.HashBytes(entityBody),
	})
	if err != nil {
		t.Fatal(err)
	}
	entityListHash, err := store.Objects.WriteEntityList(&object.EntityListObj{
		Language:   "go",
		Path:       "main.go",
		EntityRefs: []object.Hash{entityHash},
	})
	if err != nil {
		t.Fatal(err)
	}
	blobHash, err := store.Objects.WriteBlob(&object.Blob{Data: []byte("package main\n")})
	if err != nil {
		t.Fatal(err)
	}
	treeHash, err := store.Objects.WriteTree(&object.TreeObj{
		Entries: []object.TreeEntry{
			{Name: "main.go", BlobHash: blobHash, EntityListHash: entityListHash},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	commitHash, err := store.Objects.WriteCommit(&object.CommitObj{
		TreeHash:  treeHash,
		Author:    "Alice <alice@example.com>",
		Timestamp: 1700000000,
		Message:   "initial",
	})
	if err != nil {
		t.Fatal(err)
	}

	all, err := WalkObjects(store.Objects, commitHash, func(object.Hash) bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	if !containsHash(all, entityHash) {
		t.Fatalf("expected walk to include entity object %s", entityHash)
	}
}

func TestBatchObjectsUsesWantHaveNegotiation(t *testing.T) {
	store, err := gotstore.Open(filepath.Join(t.TempDir(), "repo"))
	if err != nil {
		t.Fatal(err)
	}

	blobHash, err := store.Objects.WriteBlob(&object.Blob{Data: []byte("hello\n")})
	if err != nil {
		t.Fatal(err)
	}
	treeHash, err := store.Objects.WriteTree(&object.TreeObj{
		Entries: []object.TreeEntry{{Name: "README.md", BlobHash: blobHash}},
	})
	if err != nil {
		t.Fatal(err)
	}
	commitHash, err := store.Objects.WriteCommit(&object.CommitObj{
		TreeHash:  treeHash,
		Author:    "Alice <alice@example.com>",
		Timestamp: 1700000000,
		Message:   "init",
	})
	if err != nil {
		t.Fatal(err)
	}

	h := NewHandler(func(owner, repo string) (*gotstore.RepoStore, error) { return store, nil }, nil, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	reqBody, err := json.Marshal(map[string]any{
		"wants": []string{string(commitHash)},
	})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(ts.URL+"/got/alice/repo/objects/batch", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for batch fetch, got %d", resp.StatusCode)
	}
	var first struct {
		Objects []struct {
			Hash string `json:"hash"`
			Type string `json:"type"`
			Data []byte `json:"data"`
		} `json:"objects"`
		Truncated bool `json:"truncated"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&first); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if first.Truncated {
		t.Fatalf("did not expect truncated batch for tiny repo")
	}
	if !containsHashString(first.Objects, string(commitHash)) ||
		!containsHashString(first.Objects, string(treeHash)) ||
		!containsHashString(first.Objects, string(blobHash)) {
		t.Fatalf("expected batch objects to include commit/tree/blob, got %+v", first.Objects)
	}

	reqBody, err = json.Marshal(map[string]any{
		"wants": []string{string(commitHash)},
		"haves": []string{string(treeHash), string(blobHash)},
	})
	if err != nil {
		t.Fatal(err)
	}
	resp, err = http.Post(ts.URL+"/got/alice/repo/objects/batch", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for batch fetch with haves, got %d", resp.StatusCode)
	}
	var second struct {
		Objects []struct {
			Hash string `json:"hash"`
			Type string `json:"type"`
			Data []byte `json:"data"`
		} `json:"objects"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&second); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if !containsHashString(second.Objects, string(commitHash)) {
		t.Fatalf("expected commit hash in second batch, got %+v", second.Objects)
	}
	if containsHashString(second.Objects, string(treeHash)) || containsHashString(second.Objects, string(blobHash)) {
		t.Fatalf("expected haves to suppress tree/blob transfer, got %+v", second.Objects)
	}
}

func TestUpdateRefsCASAcceptsMatchingOldHash(t *testing.T) {
	store, err := gotstore.Open(filepath.Join(t.TempDir(), "repo"))
	if err != nil {
		t.Fatal(err)
	}

	treeHash, err := store.Objects.WriteTree(&object.TreeObj{})
	if err != nil {
		t.Fatal(err)
	}
	commitA, err := store.Objects.WriteCommit(&object.CommitObj{
		TreeHash:  treeHash,
		Author:    "Alice <alice@example.com>",
		Timestamp: 1700000000,
		Message:   "A",
	})
	if err != nil {
		t.Fatal(err)
	}
	commitB, err := store.Objects.WriteCommit(&object.CommitObj{
		TreeHash:  treeHash,
		Author:    "Alice <alice@example.com>",
		Timestamp: 1700000001,
		Message:   "B",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Refs.Set("heads/main", commitA); err != nil {
		t.Fatal(err)
	}

	h := NewHandler(func(owner, repo string) (*gotstore.RepoStore, error) { return store, nil }, nil, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	reqBody, err := json.Marshal(map[string]any{
		"updates": []map[string]string{
			{"name": "heads/main", "old": string(commitA), "new": string(commitB)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(ts.URL+"/got/alice/repo/refs", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for CAS ref update, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	head, err := store.Refs.Get("heads/main")
	if err != nil {
		t.Fatal(err)
	}
	if head != commitB {
		t.Fatalf("expected heads/main to point to %s, got %s", commitB, head)
	}
}

func TestUpdateRefsCASRejectsStaleOldHash(t *testing.T) {
	store, err := gotstore.Open(filepath.Join(t.TempDir(), "repo"))
	if err != nil {
		t.Fatal(err)
	}

	treeHash, err := store.Objects.WriteTree(&object.TreeObj{})
	if err != nil {
		t.Fatal(err)
	}
	commitA, err := store.Objects.WriteCommit(&object.CommitObj{
		TreeHash:  treeHash,
		Author:    "Alice <alice@example.com>",
		Timestamp: 1700000000,
		Message:   "A",
	})
	if err != nil {
		t.Fatal(err)
	}
	commitB, err := store.Objects.WriteCommit(&object.CommitObj{
		TreeHash:  treeHash,
		Author:    "Alice <alice@example.com>",
		Timestamp: 1700000001,
		Message:   "B",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Refs.Set("heads/main", commitA); err != nil {
		t.Fatal(err)
	}

	h := NewHandler(func(owner, repo string) (*gotstore.RepoStore, error) { return store, nil }, nil, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	reqBody, err := json.Marshal(map[string]any{
		"updates": []map[string]string{
			{"name": "heads/main", "old": strings.Repeat("f", 64), "new": string(commitB)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(ts.URL+"/got/alice/repo/refs", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 for stale CAS ref update, got %d", resp.StatusCode)
	}
	var body bytes.Buffer
	if _, err := body.ReadFrom(resp.Body); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if !strings.Contains(body.String(), "stale old hash") {
		t.Fatalf("expected stale old hash message, got %q", body.String())
	}

	head, err := store.Refs.Get("heads/main")
	if err != nil {
		t.Fatal(err)
	}
	if head != commitA {
		t.Fatalf("expected heads/main to remain %s, got %s", commitA, head)
	}
}

func containsHash(hashes []object.Hash, want object.Hash) bool {
	for _, h := range hashes {
		if h == want {
			return true
		}
	}
	return false
}

func containsHashString(objs []struct {
	Hash string `json:"hash"`
	Type string `json:"type"`
	Data []byte `json:"data"`
}, want string) bool {
	for _, obj := range objs {
		if obj.Hash == want {
			return true
		}
	}
	return false
}
