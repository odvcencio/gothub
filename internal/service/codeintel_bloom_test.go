package service

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/odvcencio/got/pkg/object"
	"github.com/odvcencio/gothub/internal/database"
	"github.com/odvcencio/gothub/internal/models"
)

type countingSearchEntityDB struct {
	database.DB
	searchCalls int
}

func (d *countingSearchEntityDB) SearchEntityIndexEntries(ctx context.Context, repoID int64, commitHash, textQuery, kind string, limit int) ([]models.EntityIndexEntry, error) {
	d.searchCalls++
	return d.DB.SearchEntityIndexEntries(ctx, repoID, commitHash, textQuery, kind, limit)
}

func TestSymbolSearchBloomFilterHitAndMiss(t *testing.T) {
	entries := []models.EntityIndexEntry{
		{
			Name:      "ProcessOrder",
			Signature: "func ProcessOrder(ctx context.Context) error",
		},
		{
			Name:      "ValidateOrder",
			Signature: "func (s *OrderService) ValidateOrder() bool",
			Receiver:  "*OrderService",
		},
	}

	filter := buildSymbolSearchBloomFromEntries(entries)
	if filter == nil {
		t.Fatal("expected bloom filter")
	}
	if !filter.supportsEntityTextSearch() {
		t.Fatal("expected bloom filter to cover entity text fields")
	}
	if !bloomMightContainPlainTextQuery(filter, "ProcessOrder") {
		t.Fatal("expected bloom hit for symbol name")
	}
	if !bloomMightContainPlainTextQuery(filter, "OrderService") {
		t.Fatal("expected bloom hit for signature/receiver token")
	}
	if bloomMightContainPlainTextQuery(filter, "QwxjkvmissToken") {
		t.Fatal("expected bloom miss for absent query")
	}
}

func TestSearchSymbolsBloomMissBypassesEntityTextQuery(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	rawDB, err := database.OpenSQLite(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rawDB.Close() })
	if err := rawDB.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	db := &countingSearchEntityDB{DB: rawDB}
	user := &models.User{Username: "alice", Email: "alice@example.com", PasswordHash: "x"}
	if err := db.CreateUser(ctx, user); err != nil {
		t.Fatal(err)
	}

	repoSvc := NewRepoService(db, filepath.Join(tmpDir, "repos"))
	if _, err := repoSvc.Create(ctx, user.ID, "repo", "", false); err != nil {
		t.Fatal(err)
	}

	store, err := repoSvc.OpenStore(ctx, "alice", "repo")
	if err != nil {
		t.Fatal(err)
	}
	blobHash, err := store.Objects.WriteBlob(&object.Blob{Data: []byte("package main\n\nfunc ProcessOrder() {}\n")})
	if err != nil {
		t.Fatal(err)
	}
	treeHash, err := store.Objects.WriteTree(&object.TreeObj{Entries: []object.TreeEntry{{Name: "main.go", BlobHash: blobHash}}})
	if err != nil {
		t.Fatal(err)
	}
	commitHash, err := store.Objects.WriteCommit(&object.CommitObj{TreeHash: treeHash, Author: "alice", Timestamp: 1700000000, Message: "init"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Refs.Set("heads/main", commitHash); err != nil {
		t.Fatal(err)
	}

	browseSvc := NewBrowseService(repoSvc)
	codeIntelSvc := NewCodeIntelService(db, repoSvc, browseSvc)

	results, err := codeIntelSvc.SearchSymbols(ctx, "alice", "repo", "main", "NoMatchQwxJkvToken")
	if err != nil {
		t.Fatalf("search symbols: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected no symbol results, got %+v", results)
	}
	if db.searchCalls != 0 {
		t.Fatalf("expected bloom miss to bypass SearchEntityIndexEntries, got %d calls", db.searchCalls)
	}
}
