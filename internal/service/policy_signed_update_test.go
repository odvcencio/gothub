package service

import (
	"context"
	"crypto/md5"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odvcencio/got/pkg/object"
	"github.com/odvcencio/gothub/internal/database"
	"github.com/odvcencio/gothub/internal/models"
	"golang.org/x/crypto/ssh"
)

func TestEvaluateBranchUpdateGateRequiresSignedCommits(t *testing.T) {
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
	repoSvc := NewRepoService(db, filepath.Join(tmpDir, "repos"))
	repo, err := repoSvc.Create(ctx, user.ID, "repo", "", false)
	if err != nil {
		t.Fatal(err)
	}
	store, err := repoSvc.OpenStore(ctx, "alice", "repo")
	if err != nil {
		t.Fatal(err)
	}

	pubKeyText, signer, err := generateTestSigner()
	if err != nil {
		t.Fatal(err)
	}
	pubKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(pubKeyText))
	if err != nil {
		t.Fatal(err)
	}
	fp := fmt.Sprintf("%x", md5.Sum(pubKey.Marshal()))
	if err := db.CreateSSHKey(ctx, &models.SSHKey{
		UserID:      user.ID,
		Name:        "default",
		Fingerprint: fp,
		PublicKey:   pubKeyText,
		KeyType:     pubKey.Type(),
	}); err != nil {
		t.Fatal(err)
	}

	baseBlobHash, err := store.Objects.WriteBlob(&object.Blob{Data: []byte("package main\n\nfunc ProcessOrder() int { return 1 }\n")})
	if err != nil {
		t.Fatal(err)
	}
	baseTreeHash, err := store.Objects.WriteTree(&object.TreeObj{Entries: []object.TreeEntry{{Name: "main.go", BlobHash: baseBlobHash}}})
	if err != nil {
		t.Fatal(err)
	}
	baseCommitHash, err := store.Objects.WriteCommit(&object.CommitObj{
		TreeHash:  baseTreeHash,
		Author:    "alice",
		Timestamp: 1700005000,
		Message:   "base",
	})
	if err != nil {
		t.Fatal(err)
	}

	signedBlobHash, err := store.Objects.WriteBlob(&object.Blob{Data: []byte("package main\n\nfunc ProcessOrder() int { return 2 }\n")})
	if err != nil {
		t.Fatal(err)
	}
	signedTreeHash, err := store.Objects.WriteTree(&object.TreeObj{Entries: []object.TreeEntry{{Name: "main.go", BlobHash: signedBlobHash}}})
	if err != nil {
		t.Fatal(err)
	}
	signedCommit := &object.CommitObj{
		TreeHash:  signedTreeHash,
		Parents:   []object.Hash{baseCommitHash},
		Author:    "alice",
		Timestamp: 1700005100,
		Message:   "signed",
	}
	sig, err := signCommitForTest(signedCommit, signer)
	if err != nil {
		t.Fatal(err)
	}
	signedCommit.Signature = sig
	signedCommitHash, err := store.Objects.WriteCommit(signedCommit)
	if err != nil {
		t.Fatal(err)
	}

	unsignedBlobHash, err := store.Objects.WriteBlob(&object.Blob{Data: []byte("package main\n\nfunc ProcessOrder() int { return 3 }\n")})
	if err != nil {
		t.Fatal(err)
	}
	unsignedTreeHash, err := store.Objects.WriteTree(&object.TreeObj{Entries: []object.TreeEntry{{Name: "main.go", BlobHash: unsignedBlobHash}}})
	if err != nil {
		t.Fatal(err)
	}
	unsignedCommitHash, err := store.Objects.WriteCommit(&object.CommitObj{
		TreeHash:  unsignedTreeHash,
		Parents:   []object.Hash{signedCommitHash},
		Author:    "alice",
		Timestamp: 1700005200,
		Message:   "unsigned",
	})
	if err != nil {
		t.Fatal(err)
	}

	prSvc := NewPRService(db, repoSvc, NewBrowseService(repoSvc))
	if err := prSvc.UpsertBranchProtectionRule(ctx, &models.BranchProtectionRule{
		RepoID:               repo.ID,
		Branch:               "main",
		Enabled:              true,
		RequireSignedCommits: true,
	}); err != nil {
		t.Fatal(err)
	}

	allowedReasons, err := prSvc.EvaluateBranchUpdateGate(ctx, repo.ID, "main", baseCommitHash, signedCommitHash)
	if err != nil {
		t.Fatal(err)
	}
	if len(allowedReasons) != 0 {
		t.Fatalf("expected signed branch update to pass, got reasons %+v", allowedReasons)
	}

	blockedReasons, err := prSvc.EvaluateBranchUpdateGate(ctx, repo.ID, "main", signedCommitHash, unsignedCommitHash)
	if err != nil {
		t.Fatal(err)
	}
	if len(blockedReasons) == 0 {
		t.Fatalf("expected unsigned branch update to be blocked")
	}
	if !strings.Contains(strings.Join(blockedReasons, " "), "not signed") {
		t.Fatalf("expected unsigned-commit reason, got %+v", blockedReasons)
	}
}
