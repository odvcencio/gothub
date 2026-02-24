package gitinterop

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odvcencio/got/pkg/object"
	"github.com/odvcencio/gothub/internal/database"
	"github.com/odvcencio/gothub/internal/gotstore"
	"github.com/odvcencio/gothub/internal/models"
)

func TestExtractEntitiesForCommitsRewritesCommitTree(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	db, err := database.OpenSQLite(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	user := &models.User{Username: "alice", Email: "alice@example.com", PasswordHash: "x"}
	if err := db.CreateUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	repo := &models.Repository{
		OwnerUserID:   &user.ID,
		Name:          "repo",
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
		Parents:   nil,
		Author:    "Alice <alice@example.com>",
		Timestamp: 1700000000,
		Message:   "initial",
	})
	if err != nil {
		t.Fatal(err)
	}

	gitCommitHash := strings.Repeat("1", 40)
	if err := db.SetHashMapping(ctx, &models.HashMapping{
		RepoID: repo.ID, GitHash: gitCommitHash, GotHash: string(commitHash), ObjectType: "commit",
	}); err != nil {
		t.Fatal(err)
	}

	handler := &SmartHTTPHandler{db: db}
	updates := []refUpdate{{newHash: GitHash(gitCommitHash), refName: "refs/heads/main"}}
	overrides, err := handler.extractEntitiesForCommits(ctx, store, repo.ID, updates)
	if err != nil {
		t.Fatalf("extract entities: %v", err)
	}
	if len(overrides) != 1 {
		t.Fatalf("expected one commit mapping override, got %d", len(overrides))
	}
	if overrides[0].GitHash != gitCommitHash {
		t.Fatalf("unexpected override git hash: %s", overrides[0].GitHash)
	}
	if overrides[0].GotHash == string(commitHash) {
		t.Fatal("expected rewritten commit hash to differ from original")
	}

	if err := db.SetHashMappings(ctx, overrides); err != nil {
		t.Fatalf("persist overrides: %v", err)
	}
	mappedHash, err := db.GetGotHash(ctx, repo.ID, gitCommitHash)
	if err != nil {
		t.Fatal(err)
	}
	newCommit, err := store.Objects.ReadCommit(object.Hash(mappedHash))
	if err != nil {
		t.Fatal(err)
	}
	newTree, err := store.Objects.ReadTree(newCommit.TreeHash)
	if err != nil {
		t.Fatal(err)
	}
	if len(newTree.Entries) != 1 {
		t.Fatalf("expected one tree entry, got %d", len(newTree.Entries))
	}
	if newTree.Entries[0].EntityListHash == "" {
		t.Fatal("expected entity list hash to be linked in rewritten tree")
	}
	entityList, err := store.Objects.ReadEntityList(newTree.Entries[0].EntityListHash)
	if err != nil {
		t.Fatal(err)
	}
	if len(entityList.EntityRefs) == 0 {
		t.Fatal("expected non-empty entity refs in entity list")
	}
}
