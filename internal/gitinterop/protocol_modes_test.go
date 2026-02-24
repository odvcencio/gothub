package gitinterop

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/odvcencio/got/pkg/object"
	"github.com/odvcencio/gothub/internal/database"
	"github.com/odvcencio/gothub/internal/gotstore"
	"github.com/odvcencio/gothub/internal/models"
)

func TestGitTreeModeRoundTripPreservesExecutableBit(t *testing.T) {
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

	blobData := []byte("#!/bin/sh\necho hi\n")
	blobGitHash := GitHashBytes(GitTypeBlob, blobData)
	blobGotHash, err := store.Objects.WriteBlob(&object.Blob{Data: blobData})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetHashMapping(ctx, &models.HashMapping{
		RepoID: repo.ID, GitHash: string(blobGitHash), GotHash: string(blobGotHash), ObjectType: "blob",
	}); err != nil {
		t.Fatal(err)
	}

	var gitTreeBuf bytes.Buffer
	fmt.Fprintf(&gitTreeBuf, "100755 script.sh\x00")
	blobRaw, err := hex.DecodeString(string(blobGitHash))
	if err != nil {
		t.Fatal(err)
	}
	gitTreeBuf.Write(blobRaw)
	gitTreeData := gitTreeBuf.Bytes()

	gotTree, modes, err := parseGitTree(gitTreeData, func(gitHash, _ string) (string, error) {
		if gitHash == string(blobGitHash) {
			return string(blobGotHash), nil
		}
		return "", sql.ErrNoRows
	})
	if err != nil {
		t.Fatalf("parse git tree: %v", err)
	}
	if modes["script.sh"] != "100755" {
		t.Fatalf("expected parsed mode 100755, got %q", modes["script.sh"])
	}

	gotTreeHash, err := store.Objects.WriteTree(gotTree)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetGitTreeEntryModes(ctx, repo.ID, string(gotTreeHash), modes); err != nil {
		t.Fatal(err)
	}

	objType, treePayload, err := store.Objects.Read(gotTreeHash)
	if err != nil {
		t.Fatal(err)
	}
	converted, err := convertGotToGitData(gotTreeHash, objType, treePayload, store.Objects, ctx, db, repo.ID)
	if err != nil {
		t.Fatalf("convert got tree to git data: %v", err)
	}

	if !bytes.Equal(converted, gitTreeData) {
		t.Fatalf("expected tree bytes to round-trip unchanged\nexpected=%x\ngot=%x", gitTreeData, converted)
	}
}
