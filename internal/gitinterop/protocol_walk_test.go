package gitinterop

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/odvcencio/got/pkg/object"
	"github.com/odvcencio/gothub/internal/gotstore"
)

func TestWalkGotObjectsFailsOnMissingTree(t *testing.T) {
	store, err := gotstore.Open(filepath.Join(t.TempDir(), "repo"))
	if err != nil {
		t.Fatal(err)
	}

	missingTree := object.Hash(strings.Repeat("a", 64))
	commitHash, err := store.Objects.WriteCommit(&object.CommitObj{
		TreeHash:  missingTree,
		Author:    "Alice <alice@example.com>",
		Timestamp: 1700000000,
		Message:   "broken",
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = walkGotObjects(store.Objects, commitHash, func(object.Hash) bool { return false })
	if err == nil {
		t.Fatal("expected walkGotObjects to fail for missing tree")
	}
	if !strings.Contains(err.Error(), string(missingTree)) {
		t.Fatalf("expected error to mention missing tree hash %s, got %v", missingTree, err)
	}
}
