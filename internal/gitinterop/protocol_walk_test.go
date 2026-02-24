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

func TestWalkGotObjectsTraversesTagTarget(t *testing.T) {
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
	tagHash, err := store.Objects.WriteTag(&object.TagObj{
		TargetHash: commitHash,
		Data: []byte("object " + string(commitHash) + "\n" +
			"type commit\n" +
			"tag v1.0.0\n\nrelease\n"),
	})
	if err != nil {
		t.Fatal(err)
	}

	objs, err := walkGotObjects(store.Objects, tagHash, func(object.Hash) bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []object.Hash{tagHash, commitHash, treeHash, blobHash} {
		if !containsHash(objs, want) {
			t.Fatalf("expected walk to include %s", want)
		}
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
