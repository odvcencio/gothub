package gitinterop

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/odvcencio/got/pkg/object"
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
