package service

import (
	"context"
	"crypto/ed25519"
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odvcencio/got/pkg/object"
	"github.com/odvcencio/gothub/internal/database"
	"github.com/odvcencio/gothub/internal/models"
	"golang.org/x/crypto/ssh"
)

func TestBrowseServiceGetCommitVerifiesSignedCommit(t *testing.T) {
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
	_, err = repoSvc.Create(ctx, user.ID, "repo", "", false)
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
		Name:        "test",
		Fingerprint: fp,
		PublicKey:   pubKeyText,
		KeyType:     pubKey.Type(),
	}); err != nil {
		t.Fatal(err)
	}

	blobHash, err := store.Objects.WriteBlob(&object.Blob{Data: []byte("package main\n\nfunc main() {}\n")})
	if err != nil {
		t.Fatal(err)
	}
	treeHash, err := store.Objects.WriteTree(&object.TreeObj{Entries: []object.TreeEntry{{Name: "main.go", BlobHash: blobHash}}})
	if err != nil {
		t.Fatal(err)
	}

	commit := &object.CommitObj{
		TreeHash:  treeHash,
		Author:    "alice",
		Timestamp: 1700000000,
		Message:   "signed",
	}
	sig, err := signCommitForTest(commit, signer)
	if err != nil {
		t.Fatal(err)
	}
	commit.Signature = sig

	commitHash, err := store.Objects.WriteCommit(commit)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Refs.Set("heads/main", commitHash); err != nil {
		t.Fatal(err)
	}

	browse := NewBrowseService(repoSvc)
	info, err := browse.GetCommit(ctx, "alice", "repo", string(commitHash))
	if err != nil {
		t.Fatal(err)
	}
	if !info.Verified {
		t.Fatalf("expected verified commit")
	}
	if info.Signer != "alice" {
		t.Fatalf("expected signer alice, got %q", info.Signer)
	}
	if strings.TrimSpace(info.Signature) == "" {
		t.Fatalf("expected signature in commit info")
	}

	commits, err := browse.ListCommits(ctx, "alice", "repo", "main", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) == 0 {
		t.Fatalf("expected commit in list")
	}
	if !commits[0].Verified {
		t.Fatalf("expected listed commit to be verified")
	}
}

func TestBrowseServiceGetCommitUnverifiedWhenKeyUnknown(t *testing.T) {
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
	_, err = repoSvc.Create(ctx, user.ID, "repo", "", false)
	if err != nil {
		t.Fatal(err)
	}
	store, err := repoSvc.OpenStore(ctx, "alice", "repo")
	if err != nil {
		t.Fatal(err)
	}

	_, signer, err := generateTestSigner()
	if err != nil {
		t.Fatal(err)
	}
	blobHash, err := store.Objects.WriteBlob(&object.Blob{Data: []byte("package main\n")})
	if err != nil {
		t.Fatal(err)
	}
	treeHash, err := store.Objects.WriteTree(&object.TreeObj{Entries: []object.TreeEntry{{Name: "main.go", BlobHash: blobHash}}})
	if err != nil {
		t.Fatal(err)
	}
	commit := &object.CommitObj{TreeHash: treeHash, Author: "alice", Timestamp: 1700000001, Message: "signed"}
	sig, err := signCommitForTest(commit, signer)
	if err != nil {
		t.Fatal(err)
	}
	commit.Signature = sig

	commitHash, err := store.Objects.WriteCommit(commit)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Refs.Set("heads/main", commitHash); err != nil {
		t.Fatal(err)
	}

	browse := NewBrowseService(repoSvc)
	info, err := browse.GetCommit(ctx, "alice", "repo", string(commitHash))
	if err != nil {
		t.Fatal(err)
	}
	if info.Verified {
		t.Fatalf("expected unverified commit when signing key is unknown")
	}
}

func generateTestSigner() (string, ssh.Signer, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", nil, err
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		return "", nil, err
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return "", nil, err
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub))), signer, nil
}

func signCommitForTest(commit *object.CommitObj, signer ssh.Signer) (string, error) {
	payload := commitSigningPayloadForVerification(commit)
	sig, err := signer.Sign(rand.Reader, payload)
	if err != nil {
		return "", err
	}
	pubB64 := base64.StdEncoding.EncodeToString(signer.PublicKey().Marshal())
	sigB64 := base64.StdEncoding.EncodeToString(sig.Blob)
	return fmt.Sprintf("sshsig-v1:%s:%s:%s", sig.Format, pubB64, sigB64), nil
}
