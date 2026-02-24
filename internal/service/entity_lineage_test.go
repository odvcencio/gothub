package service

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

func TestEntityLineageIndexCommitKeepsStableIDAcrossMoveAndEdit(t *testing.T) {
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

	blob1Hash, err := store.Objects.WriteBlob(&object.Blob{
		Data: []byte("package main\n\nfunc ProcessOrder() int { return 1 }\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	tree1Hash, err := store.Objects.WriteTree(&object.TreeObj{
		Entries: []object.TreeEntry{{Name: "main.go", BlobHash: blob1Hash}},
	})
	if err != nil {
		t.Fatal(err)
	}
	commit1Hash, err := store.Objects.WriteCommit(&object.CommitObj{
		TreeHash:  tree1Hash,
		Author:    "Alice <alice@example.com>",
		Timestamp: 1700000000,
		Message:   "initial",
	})
	if err != nil {
		t.Fatal(err)
	}

	blob2Hash, err := store.Objects.WriteBlob(&object.Blob{
		Data: []byte("package main\n\nfunc ProcessOrder() int { return 1 }\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	tree2Hash, err := store.Objects.WriteTree(&object.TreeObj{
		Entries: []object.TreeEntry{{Name: "order.go", BlobHash: blob2Hash}},
	})
	if err != nil {
		t.Fatal(err)
	}
	commit2Hash, err := store.Objects.WriteCommit(&object.CommitObj{
		TreeHash:  tree2Hash,
		Parents:   []object.Hash{commit1Hash},
		Author:    "Alice <alice@example.com>",
		Timestamp: 1700000100,
		Message:   "move file",
	})
	if err != nil {
		t.Fatal(err)
	}

	blob3Hash, err := store.Objects.WriteBlob(&object.Blob{
		Data: []byte("package main\n\nfunc ProcessOrder() int { return 2 }\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	tree3Hash, err := store.Objects.WriteTree(&object.TreeObj{
		Entries: []object.TreeEntry{{Name: "order.go", BlobHash: blob3Hash}},
	})
	if err != nil {
		t.Fatal(err)
	}
	commit3Hash, err := store.Objects.WriteCommit(&object.CommitObj{
		TreeHash:  tree3Hash,
		Parents:   []object.Hash{commit2Hash},
		Author:    "Alice <alice@example.com>",
		Timestamp: 1700000200,
		Message:   "edit function",
	})
	if err != nil {
		t.Fatal(err)
	}

	lineage := NewEntityLineageService(db)
	if err := lineage.IndexCommit(ctx, repo.ID, store, commit3Hash); err != nil {
		t.Fatalf("index lineage: %v", err)
	}

	stable1, err := processOrderStableID(ctx, db, repo.ID, string(commit1Hash))
	if err != nil {
		t.Fatal(err)
	}
	stable2, err := processOrderStableID(ctx, db, repo.ID, string(commit2Hash))
	if err != nil {
		t.Fatal(err)
	}
	stable3, err := processOrderStableID(ctx, db, repo.ID, string(commit3Hash))
	if err != nil {
		t.Fatal(err)
	}

	if strings.TrimSpace(stable1) == "" || strings.TrimSpace(stable2) == "" || strings.TrimSpace(stable3) == "" {
		t.Fatalf("expected non-empty stable IDs, got %q %q %q", stable1, stable2, stable3)
	}
	if stable1 != stable2 || stable2 != stable3 {
		t.Fatalf("expected same stable ID across commits, got %q %q %q", stable1, stable2, stable3)
	}
}

func processOrderStableID(ctx context.Context, db database.DB, repoID int64, commitHash string) (string, error) {
	versions, err := db.ListEntityVersionsByCommit(ctx, repoID, commitHash)
	if err != nil {
		return "", err
	}
	for _, v := range versions {
		if v.Name == "ProcessOrder" {
			return v.StableID, nil
		}
	}
	return "", nil
}

func TestEntityHistoryLazyIndexesLineageWhenMissing(t *testing.T) {
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

	blobHash, err := store.Objects.WriteBlob(&object.Blob{
		Data: []byte("package main\n\nfunc ProcessOrder() int { return 1 }\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	treeHash, err := store.Objects.WriteTree(&object.TreeObj{
		Entries: []object.TreeEntry{{Name: "main.go", BlobHash: blobHash}},
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
	if err := store.Refs.Set("heads/main", commitHash); err != nil {
		t.Fatal(err)
	}

	// Ensure lineage DB starts empty.
	has, err := db.HasEntityVersionsForCommit(ctx, repo.ID, string(commitHash))
	if err != nil {
		t.Fatal(err)
	}
	if has {
		t.Fatalf("expected no lineage versions before lazy indexing")
	}

	browseSvc := NewBrowseService(repoSvc)
	lineageSvc := NewEntityLineageService(db)
	diffSvc := NewDiffService(repoSvc, browseSvc, db, lineageSvc)

	hits, err := diffSvc.EntityHistory(ctx, "alice", "repo", "main", "", "ProcessOrder", "", 10, 0)
	if err != nil {
		t.Fatalf("entity history: %v", err)
	}
	if len(hits) == 0 {
		t.Fatalf("expected history hits after lazy indexing")
	}
	if strings.TrimSpace(hits[0].StableID) == "" {
		t.Fatalf("expected stable id in entity history hit")
	}

	has, err = db.HasEntityVersionsForCommit(ctx, repo.ID, string(commitHash))
	if err != nil {
		t.Fatal(err)
	}
	if !has {
		t.Fatalf("expected lazy indexing to persist entity versions")
	}
}
