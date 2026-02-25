package service

import (
	"context"
	"os"
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

func TestEntityLineageIndexCommitCopiesParentVersionsWhenTreeUnchanged(t *testing.T) {
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
	commit1Hash, err := store.Objects.WriteCommit(&object.CommitObj{
		TreeHash:  treeHash,
		Author:    "Alice <alice@example.com>",
		Timestamp: 1700000000,
		Message:   "initial",
	})
	if err != nil {
		t.Fatal(err)
	}
	commit2Hash, err := store.Objects.WriteCommit(&object.CommitObj{
		TreeHash:  treeHash,
		Parents:   []object.Hash{commit1Hash},
		Author:    "Alice <alice@example.com>",
		Timestamp: 1700000100,
		Message:   "no code changes",
	})
	if err != nil {
		t.Fatal(err)
	}

	lineage := NewEntityLineageService(db)
	if err := lineage.IndexCommit(ctx, repo.ID, store, commit2Hash); err != nil {
		t.Fatalf("index lineage: %v", err)
	}

	versions1, err := db.ListEntityVersionsByCommit(ctx, repo.ID, string(commit1Hash))
	if err != nil {
		t.Fatal(err)
	}
	versions2, err := db.ListEntityVersionsByCommit(ctx, repo.ID, string(commit2Hash))
	if err != nil {
		t.Fatal(err)
	}

	if len(versions1) == 0 {
		t.Fatal("expected parent commit to have entity versions")
	}
	if len(versions2) != len(versions1) {
		t.Fatalf("expected copied versions count %d, got %d", len(versions1), len(versions2))
	}

	stable1, err := processOrderStableID(ctx, db, repo.ID, string(commit1Hash))
	if err != nil {
		t.Fatal(err)
	}
	stable2, err := processOrderStableID(ctx, db, repo.ID, string(commit2Hash))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(stable1) == "" || strings.TrimSpace(stable2) == "" {
		t.Fatalf("expected non-empty stable IDs, got %q and %q", stable1, stable2)
	}
	if stable1 != stable2 {
		t.Fatalf("expected unchanged-tree commit to keep stable id, got %q vs %q", stable1, stable2)
	}
}

func TestEntityLineageIndexCommitSingleParentIncrementalOnlyReprocessesChangedFiles(t *testing.T) {
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

	keepBlobHash, err := store.Objects.WriteBlob(&object.Blob{
		Data: []byte("package main\n\nfunc KeepAlive() int { return 1 }\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	orderBlobV1Hash, err := store.Objects.WriteBlob(&object.Blob{
		Data: []byte("package main\n\nfunc ProcessOrder() int { return 1 }\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	tree1Hash, err := store.Objects.WriteTree(&object.TreeObj{
		Entries: []object.TreeEntry{
			{Name: "keep.go", BlobHash: keepBlobHash},
			{Name: "order.go", BlobHash: orderBlobV1Hash},
		},
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

	orderBlobV2Hash, err := store.Objects.WriteBlob(&object.Blob{
		Data: []byte("package main\n\nfunc ProcessOrder() int { return 2 }\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	tree2Hash, err := store.Objects.WriteTree(&object.TreeObj{
		Entries: []object.TreeEntry{
			{Name: "keep.go", BlobHash: keepBlobHash},
			{Name: "order.go", BlobHash: orderBlobV2Hash},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	commit2Hash, err := store.Objects.WriteCommit(&object.CommitObj{
		TreeHash:  tree2Hash,
		Parents:   []object.Hash{commit1Hash},
		Author:    "Alice <alice@example.com>",
		Timestamp: 1700000100,
		Message:   "modify changed file only",
	})
	if err != nil {
		t.Fatal(err)
	}

	lineage := NewEntityLineageService(db)
	if err := lineage.IndexCommit(ctx, repo.ID, store, commit1Hash); err != nil {
		t.Fatalf("index commit1 lineage: %v", err)
	}

	versions1, err := db.ListEntityVersionsByCommit(ctx, repo.ID, string(commit1Hash))
	if err != nil {
		t.Fatal(err)
	}
	keepV1, ok := findEntityVersionByName(versions1, "KeepAlive")
	if !ok {
		t.Fatalf("expected KeepAlive in commit1 versions")
	}
	orderV1, ok := findEntityVersionByName(versions1, "ProcessOrder")
	if !ok {
		t.Fatalf("expected ProcessOrder in commit1 versions")
	}

	// Remove the unchanged file's blob object. Incremental lineage must carry it
	// forward from parent versions instead of re-reading the blob.
	if err := os.Remove(looseObjectPath(repo.StoragePath, keepBlobHash)); err != nil {
		t.Fatalf("remove keep blob object: %v", err)
	}

	if err := lineage.IndexCommit(ctx, repo.ID, store, commit2Hash); err != nil {
		t.Fatalf("index commit2 lineage: %v", err)
	}

	versions2, err := db.ListEntityVersionsByCommit(ctx, repo.ID, string(commit2Hash))
	if err != nil {
		t.Fatal(err)
	}
	keepV2, ok := findEntityVersionByName(versions2, "KeepAlive")
	if !ok {
		t.Fatalf("expected unchanged KeepAlive to be carried forward in commit2")
	}
	orderV2, ok := findEntityVersionByName(versions2, "ProcessOrder")
	if !ok {
		t.Fatalf("expected changed ProcessOrder in commit2 versions")
	}

	if keepV2.StableID != keepV1.StableID {
		t.Fatalf("expected unchanged file stable id to be copied, got %q vs %q", keepV2.StableID, keepV1.StableID)
	}
	if keepV2.BodyHash != keepV1.BodyHash {
		t.Fatalf("expected unchanged file body hash to be copied, got %q vs %q", keepV2.BodyHash, keepV1.BodyHash)
	}
	if keepV2.EntityHash != keepV1.EntityHash {
		t.Fatalf("expected unchanged file entity hash to be copied, got %q vs %q", keepV2.EntityHash, keepV1.EntityHash)
	}
	if keepV2.Path != keepV1.Path {
		t.Fatalf("expected unchanged file path to be copied, got %q vs %q", keepV2.Path, keepV1.Path)
	}

	if orderV2.StableID != orderV1.StableID {
		t.Fatalf("expected changed file to keep stable id matching behavior, got %q vs %q", orderV2.StableID, orderV1.StableID)
	}
	if orderV2.BodyHash == orderV1.BodyHash {
		t.Fatalf("expected changed file to be reprocessed with new body hash, both were %q", orderV2.BodyHash)
	}
	if orderV2.EntityHash == orderV1.EntityHash {
		t.Fatalf("expected changed file to be reprocessed with new entity hash, both were %q", orderV2.EntityHash)
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

func findEntityVersionByName(versions []models.EntityVersion, name string) (models.EntityVersion, bool) {
	for i := range versions {
		if versions[i].Name == name {
			return versions[i], true
		}
	}
	return models.EntityVersion{}, false
}

func looseObjectPath(repoPath string, hash object.Hash) string {
	h := string(hash)
	return filepath.Join(repoPath, "objects", "objects", h[:2], h[2:])
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
