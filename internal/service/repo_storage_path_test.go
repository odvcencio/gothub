package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/odvcencio/gothub/internal/database"
	"github.com/odvcencio/gothub/internal/gotstore"
	"github.com/odvcencio/gothub/internal/models"
)

func TestCreateStoragePathDerivationTenancyOff(t *testing.T) {
	ctx, db, storageRoot := setupRepoStoragePathTestDB(t)
	owner := createRepoStoragePathTestUser(t, ctx, db, "owner-off")

	repoSvc := NewRepoService(db, storageRoot)
	repo, err := repoSvc.Create(ctx, owner.ID, "repo-off", "", false)
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}

	expected := filepath.Join(storageRoot, fmt.Sprintf("%d", repo.ID))
	if repo.StoragePath != expected {
		t.Fatalf("storage path = %q, want %q", repo.StoragePath, expected)
	}
	if _, err := os.Stat(expected); err != nil {
		t.Fatalf("expected storage path to exist: %v", err)
	}
}

func TestCreateStoragePathDerivationTenancyOn(t *testing.T) {
	ctx, db, storageRoot := setupRepoStoragePathTestDB(t)
	owner := createRepoStoragePathTestUser(t, ctx, db, "owner-on")
	tenantCtx := database.WithTenantID(ctx, "tenant-a")

	repoSvc := NewRepoService(db, storageRoot)
	repo, err := repoSvc.Create(tenantCtx, owner.ID, "repo-on", "", false)
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}

	expected := filepath.Join(storageRoot, "tenant-a", fmt.Sprintf("%d", repo.ID))
	if repo.StoragePath != expected {
		t.Fatalf("storage path = %q, want %q", repo.StoragePath, expected)
	}
	if _, err := os.Stat(expected); err != nil {
		t.Fatalf("expected tenant storage path to exist: %v", err)
	}

	legacyPath := filepath.Join(storageRoot, fmt.Sprintf("%d", repo.ID))
	if _, err := os.Stat(legacyPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected legacy storage path to be absent, got err=%v", err)
	}
}

func TestForkStoragePathDerivationTenancyOn(t *testing.T) {
	ctx, db, storageRoot := setupRepoStoragePathTestDB(t)
	sourceOwner := createRepoStoragePathTestUser(t, ctx, db, "source-owner")
	forkOwner := createRepoStoragePathTestUser(t, ctx, db, "fork-owner")
	tenantCtx := database.WithTenantID(ctx, "tenant-a")

	repoSvc := NewRepoService(db, storageRoot)
	sourceRepo, err := repoSvc.Create(tenantCtx, sourceOwner.ID, "source", "source repo", false)
	if err != nil {
		t.Fatalf("create source repo: %v", err)
	}

	fork, err := repoSvc.Fork(tenantCtx, sourceRepo.ID, forkOwner.ID, "")
	if err != nil {
		t.Fatalf("fork repo: %v", err)
	}

	expected := filepath.Join(storageRoot, "tenant-a", fmt.Sprintf("%d", fork.ID))
	if fork.StoragePath != expected {
		t.Fatalf("fork storage path = %q, want %q", fork.StoragePath, expected)
	}
	if _, err := os.Stat(expected); err != nil {
		t.Fatalf("expected fork storage path to exist: %v", err)
	}
}

func TestOpenStoreByIDPendingStoragePathPrefersLegacyPath(t *testing.T) {
	ctx, db, storageRoot := setupRepoStoragePathTestDB(t)
	owner := createRepoStoragePathTestUser(t, ctx, db, "owner-legacy")
	tenantCtx := database.WithTenantID(ctx, "tenant-a")

	repo := &models.Repository{
		OwnerUserID:   &owner.ID,
		Name:          "legacy-pending",
		Description:   "",
		DefaultBranch: "main",
		IsPrivate:     false,
		StoragePath:   "pending",
	}
	if err := db.CreateRepository(ctx, repo); err != nil {
		t.Fatalf("seed repository: %v", err)
	}

	legacyPath := filepath.Join(storageRoot, fmt.Sprintf("%d", repo.ID))
	if _, err := gotstore.Open(legacyPath); err != nil {
		t.Fatalf("seed legacy store: %v", err)
	}

	repoSvc := NewRepoService(db, storageRoot)
	store, err := repoSvc.OpenStoreByID(tenantCtx, repo.ID)
	if err != nil {
		t.Fatalf("open store by id: %v", err)
	}
	if store.Root() != legacyPath {
		t.Fatalf("store root = %q, want %q", store.Root(), legacyPath)
	}

	tenantPath := filepath.Join(storageRoot, "tenant-a", fmt.Sprintf("%d", repo.ID))
	if _, err := os.Stat(tenantPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected tenant path to be absent, got err=%v", err)
	}
}

func setupRepoStoragePathTestDB(t *testing.T) (context.Context, *database.SQLiteDB, string) {
	t.Helper()

	ctx := context.Background()
	tmpDir := t.TempDir()
	db, err := database.OpenSQLite(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	return ctx, db, filepath.Join(tmpDir, "repos")
}

func createRepoStoragePathTestUser(t *testing.T, ctx context.Context, db *database.SQLiteDB, username string) *models.User {
	t.Helper()

	user := &models.User{
		Username:     username,
		Email:        username + "@example.com",
		PasswordHash: "x",
	}
	if err := db.CreateUser(ctx, user); err != nil {
		t.Fatalf("create user %q: %v", username, err)
	}
	return user
}
