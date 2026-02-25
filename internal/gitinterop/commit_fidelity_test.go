package gitinterop

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odvcencio/got/pkg/object"
	"github.com/odvcencio/gothub/internal/database"
	"github.com/odvcencio/gothub/internal/gotstore"
	"github.com/odvcencio/gothub/internal/models"
)

func TestParseGitCommitPreservesAuthorAndCommitterMetadata(t *testing.T) {
	treeGitHash := strings.Repeat("1", 40)
	gotTreeHash := strings.Repeat("a", 64)
	resolve := func(gitHash string) (string, error) {
		if gitHash == treeGitHash {
			return gotTreeHash, nil
		}
		return "", fmt.Errorf("missing mapping for %s", gitHash)
	}

	raw := []byte("tree " + treeGitHash + "\n" +
		"author Alice <alice@example.com> 1700000000 +0200\n" +
		"committer Bob <bob@example.com> 1700000100 -0700\n" +
		"\nmessage\n")

	commit, err := parseGitCommit(raw, resolve)
	if err != nil {
		t.Fatalf("parseGitCommit: %v", err)
	}
	if commit.Author != "Alice <alice@example.com>" {
		t.Fatalf("unexpected author: %q", commit.Author)
	}
	if commit.Timestamp != 1700000000 {
		t.Fatalf("unexpected author timestamp: %d", commit.Timestamp)
	}
	if commit.AuthorTimezone != "+0200" {
		t.Fatalf("unexpected author timezone: %q", commit.AuthorTimezone)
	}
	if commit.Committer != "Bob <bob@example.com>" {
		t.Fatalf("unexpected committer: %q", commit.Committer)
	}
	if commit.CommitterTimestamp != 1700000100 {
		t.Fatalf("unexpected committer timestamp: %d", commit.CommitterTimestamp)
	}
	if commit.CommitterTimezone != "-0700" {
		t.Fatalf("unexpected committer timezone: %q", commit.CommitterTimezone)
	}
}

func TestGotToGitCommitUsesCommitterMetadata(t *testing.T) {
	commit := &object.CommitObj{
		TreeHash:           object.Hash(strings.Repeat("a", 64)),
		Author:             "Alice <alice@example.com>",
		Timestamp:          1700000000,
		AuthorTimezone:     "+0200",
		Committer:          "Bob <bob@example.com>",
		CommitterTimestamp: 1700000100,
		CommitterTimezone:  "-0700",
		Message:            "test",
	}

	_, data := GotToGitCommit(commit, GitHash(strings.Repeat("1", 40)), nil)
	if !bytes.Contains(data, []byte("author Alice <alice@example.com> 1700000000 +0200\n")) {
		t.Fatalf("expected author line with timezone, got %q", string(data))
	}
	if !bytes.Contains(data, []byte("committer Bob <bob@example.com> 1700000100 -0700\n")) {
		t.Fatalf("expected committer line with metadata, got %q", string(data))
	}
}

func TestParseGitCommitDoesNotSynthesizeCommitterOrTimezone(t *testing.T) {
	treeGitHash := strings.Repeat("1", 40)
	gotTreeHash := strings.Repeat("a", 64)
	resolve := func(gitHash string) (string, error) {
		if gitHash == treeGitHash {
			return gotTreeHash, nil
		}
		return "", fmt.Errorf("missing mapping for %s", gitHash)
	}

	raw := []byte("tree " + treeGitHash + "\n" +
		"author Alice <alice@example.com> 1700000000\n" +
		"\nmessage\n")

	commit, err := parseGitCommit(raw, resolve)
	if err != nil {
		t.Fatalf("parseGitCommit: %v", err)
	}
	if commit.AuthorTimezone != "" {
		t.Fatalf("unexpected synthesized author timezone: %q", commit.AuthorTimezone)
	}
	if commit.Committer != "" {
		t.Fatalf("unexpected synthesized committer: %q", commit.Committer)
	}
	if commit.CommitterTimezone != "" {
		t.Fatalf("unexpected synthesized committer timezone: %q", commit.CommitterTimezone)
	}
}

func TestGotToGitCommitExplicitCommitterDoesNotInheritAuthorTimestampOrTimezone(t *testing.T) {
	commit := &object.CommitObj{
		TreeHash:           object.Hash(strings.Repeat("a", 64)),
		Author:             "Alice <alice@example.com>",
		Timestamp:          1700000000,
		AuthorTimezone:     "+0200",
		Committer:          "Bob <bob@example.com>",
		CommitterTimestamp: 0,
		CommitterTimezone:  "",
		Message:            "test",
	}

	_, data := GotToGitCommit(commit, GitHash(strings.Repeat("1", 40)), nil)
	if !bytes.Contains(data, []byte("committer Bob <bob@example.com> 0 +0000\n")) {
		t.Fatalf("expected explicit committer defaults, got %q", string(data))
	}
	if bytes.Contains(data, []byte("committer Bob <bob@example.com> 1700000000 +0200\n")) {
		t.Fatalf("committer metadata incorrectly inherited from author: %q", string(data))
	}
}

func TestGitCommitMetadataRoundTripPreservesAuthorCommitterAndTimezones(t *testing.T) {
	treeGitHash := strings.Repeat("1", 40)
	gotTreeHash := strings.Repeat("a", 64)
	resolve := func(gitHash string) (string, error) {
		if gitHash == treeGitHash {
			return gotTreeHash, nil
		}
		return "", fmt.Errorf("missing mapping for %s", gitHash)
	}

	raw := []byte("tree " + treeGitHash + "\n" +
		"author Alice Builder 1700000000 +0230\n" +
		"committer Bob Reviewer 1700000100 -0715\n" +
		"\nmessage\n")

	parsed, err := parseGitCommit(raw, resolve)
	if err != nil {
		t.Fatalf("parseGitCommit: %v", err)
	}
	if parsed.Author != "Alice Builder" {
		t.Fatalf("unexpected author identity: %q", parsed.Author)
	}
	if parsed.Committer != "Bob Reviewer" {
		t.Fatalf("unexpected committer identity: %q", parsed.Committer)
	}
	if parsed.AuthorTimezone != "+0230" {
		t.Fatalf("unexpected author timezone: %q", parsed.AuthorTimezone)
	}
	if parsed.CommitterTimezone != "-0715" {
		t.Fatalf("unexpected committer timezone: %q", parsed.CommitterTimezone)
	}

	stored := object.MarshalCommit(parsed)
	reloaded, err := object.UnmarshalCommit(stored)
	if err != nil {
		t.Fatalf("UnmarshalCommit: %v", err)
	}

	_, roundTrip := GotToGitCommit(reloaded, GitHash(treeGitHash), nil)
	if !bytes.Contains(roundTrip, []byte("author Alice Builder 1700000000 +0230\n")) {
		t.Fatalf("expected author line to round-trip, got %q", string(roundTrip))
	}
	if !bytes.Contains(roundTrip, []byte("committer Bob Reviewer 1700000100 -0715\n")) {
		t.Fatalf("expected committer line to round-trip, got %q", string(roundTrip))
	}
}

func TestParseGitTagRewritesObjectHeaderToGotHash(t *testing.T) {
	gitTargetHash := strings.Repeat("1", 40)
	gotTargetHash := strings.Repeat("a", 64)

	raw := []byte("object " + gitTargetHash + "\n" +
		"type commit\n" +
		"tag v1.0.0\n" +
		"tagger Alice <alice@example.com> 1700000000 +0000\n\n" +
		"release\n")

	tag, err := parseGitTag(raw, func(gitHash string) (string, error) {
		if gitHash == gitTargetHash {
			return gotTargetHash, nil
		}
		return "", fmt.Errorf("missing mapping for %s", gitHash)
	})
	if err != nil {
		t.Fatalf("parseGitTag: %v", err)
	}
	if tag.TargetHash != object.Hash(gotTargetHash) {
		t.Fatalf("unexpected target hash: got %q want %q", tag.TargetHash, gotTargetHash)
	}
	if !bytes.Contains(tag.Data, []byte("object "+gotTargetHash+"\n")) {
		t.Fatalf("expected rewritten object header, got %q", string(tag.Data))
	}
	if bytes.Contains(tag.Data, []byte("object "+gitTargetHash+"\n")) {
		t.Fatalf("expected git hash to be rewritten, got %q", string(tag.Data))
	}
}

func TestConvertGotToGitTagRestoresObjectHeader(t *testing.T) {
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

	gitTargetHash := strings.Repeat("1", 40)
	gotTargetHash := strings.Repeat("a", 64)
	if err := db.SetHashMapping(ctx, &models.HashMapping{
		RepoID:     repo.ID,
		GitHash:    gitTargetHash,
		GotHash:    gotTargetHash,
		ObjectType: "commit",
	}); err != nil {
		t.Fatal(err)
	}

	tagPayload := object.MarshalTag(&object.TagObj{
		TargetHash: object.Hash(gotTargetHash),
		Data: []byte("object " + gotTargetHash + "\n" +
			"type commit\n" +
			"tag v1.0.0\n\nrelease\n"),
	})
	tagHash, err := store.Objects.WriteTag(&object.TagObj{
		TargetHash: object.Hash(gotTargetHash),
		Data: []byte("object " + gotTargetHash + "\n" +
			"type commit\n" +
			"tag v1.0.0\n\nrelease\n"),
	})
	if err != nil {
		t.Fatal(err)
	}

	converted, err := convertGotToGitData(tagHash, object.TypeTag, tagPayload, store.Objects, ctx, db, repo.ID)
	if err != nil {
		t.Fatalf("convertGotToGitData: %v", err)
	}
	if !bytes.Contains(converted, []byte("object "+gitTargetHash+"\n")) {
		t.Fatalf("expected tag object header to use git hash, got %q", string(converted))
	}
	if bytes.Contains(converted, []byte("object "+gotTargetHash+"\n")) {
		t.Fatalf("expected got hash to be rewritten, got %q", string(converted))
	}
}
