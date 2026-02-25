package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odvcencio/gothub/internal/database"
	"github.com/odvcencio/gothub/internal/models"
)

type forkTestDB struct {
	database.DB

	createRepositoryFn          func(ctx context.Context, repo *models.Repository) error
	cloneRepoMetadataFn         func(ctx context.Context, sourceRepoID, targetRepoID int64) error
	updateRepositoryStoragePath func(ctx context.Context, id int64, storagePath string) error
	deleteRepositoryFn          func(ctx context.Context, id int64) error
}

func (d *forkTestDB) CreateRepository(ctx context.Context, repo *models.Repository) error {
	if d.createRepositoryFn != nil {
		return d.createRepositoryFn(ctx, repo)
	}
	return d.DB.CreateRepository(ctx, repo)
}

func (d *forkTestDB) CloneRepoMetadata(ctx context.Context, sourceRepoID, targetRepoID int64) error {
	if d.cloneRepoMetadataFn != nil {
		return d.cloneRepoMetadataFn(ctx, sourceRepoID, targetRepoID)
	}
	return d.DB.CloneRepoMetadata(ctx, sourceRepoID, targetRepoID)
}

func (d *forkTestDB) UpdateRepositoryStoragePath(ctx context.Context, id int64, storagePath string) error {
	if d.updateRepositoryStoragePath != nil {
		return d.updateRepositoryStoragePath(ctx, id, storagePath)
	}
	return d.DB.UpdateRepositoryStoragePath(ctx, id, storagePath)
}

func (d *forkTestDB) DeleteRepository(ctx context.Context, id int64) error {
	if d.deleteRepositoryFn != nil {
		return d.deleteRepositoryFn(ctx, id)
	}
	return d.DB.DeleteRepository(ctx, id)
}

func TestForkCleansUpFailedCopyArtifacts(t *testing.T) {
	ctx, db, storageRoot, _, forkOwner, sourceRepo := setupForkTestFixture(t)

	var forkID int64
	hookDB := &forkTestDB{
		DB: db,
		createRepositoryFn: func(ctx context.Context, repo *models.Repository) error {
			if err := db.CreateRepository(ctx, repo); err != nil {
				return err
			}
			if repo.ParentRepoID != nil {
				forkID = repo.ID
			}
			return nil
		},
	}
	repoSvc := NewRepoService(hookDB, storageRoot)
	repoSvc.copyDirectoryFn = func(_, dst string) error {
		if err := os.MkdirAll(dst, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dst, "partial.tmp"), []byte("partial"), 0o644); err != nil {
			return err
		}
		return errors.New("simulated copy failure")
	}

	_, err := repoSvc.Fork(ctx, sourceRepo.ID, forkOwner.ID, "")
	if err == nil {
		t.Fatal("expected fork copy failure")
	}
	if !strings.Contains(err.Error(), "copy objects: simulated copy failure") {
		t.Fatalf("unexpected fork error: %v", err)
	}

	assertForkCleanedUp(t, db, storageRoot, forkID)
}

func TestForkRollbackStillCleansUpWhenRequestContextCanceled(t *testing.T) {
	ctx, db, storageRoot, _, forkOwner, sourceRepo := setupForkTestFixture(t)

	var forkID int64
	hookDB := &forkTestDB{
		DB: db,
		createRepositoryFn: func(ctx context.Context, repo *models.Repository) error {
			if err := db.CreateRepository(ctx, repo); err != nil {
				return err
			}
			if repo.ParentRepoID != nil {
				forkID = repo.ID
			}
			return nil
		},
	}
	repoSvc := NewRepoService(hookDB, storageRoot)

	reqCtx, cancel := context.WithCancel(ctx)
	repoSvc.copyDirectoryFn = func(_, _ string) error {
		cancel()
		return errors.New("simulated copy failure")
	}

	_, err := repoSvc.Fork(reqCtx, sourceRepo.ID, forkOwner.ID, "")
	if err == nil {
		t.Fatal("expected fork copy failure")
	}
	if !strings.Contains(err.Error(), "copy objects: simulated copy failure") {
		t.Fatalf("unexpected fork error: %v", err)
	}

	assertForkCleanedUp(t, db, storageRoot, forkID)
}

func TestForkCleansUpClonedMetadataWhenPersistStoragePathFails(t *testing.T) {
	ctx, db, storageRoot, _, forkOwner, sourceRepo := setupForkTestFixture(t)

	const sourceGitHash = "git-source-1"
	const sourceGotHash = "got-source-1"
	if err := db.SetHashMapping(ctx, &models.HashMapping{
		RepoID:     sourceRepo.ID,
		GitHash:    sourceGitHash,
		GotHash:    sourceGotHash,
		ObjectType: "commit",
	}); err != nil {
		t.Fatalf("seed source hash mapping: %v", err)
	}

	var forkID int64
	hookDB := &forkTestDB{
		DB: db,
		createRepositoryFn: func(ctx context.Context, repo *models.Repository) error {
			if err := db.CreateRepository(ctx, repo); err != nil {
				return err
			}
			if repo.ParentRepoID != nil {
				forkID = repo.ID
			}
			return nil
		},
		updateRepositoryStoragePath: func(ctx context.Context, id int64, storagePath string) error {
			if id == forkID {
				return errors.New("simulated persist failure")
			}
			return db.UpdateRepositoryStoragePath(ctx, id, storagePath)
		},
	}
	repoSvc := NewRepoService(hookDB, storageRoot)

	_, err := repoSvc.Fork(ctx, sourceRepo.ID, forkOwner.ID, "")
	if err == nil {
		t.Fatal("expected persist failure")
	}
	if !strings.Contains(err.Error(), "persist fork storage path: simulated persist failure") {
		t.Fatalf("unexpected fork error: %v", err)
	}

	assertForkCleanedUp(t, db, storageRoot, forkID)

	_, err = db.GetGotHash(ctx, forkID, sourceGitHash)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected no cloned hash mapping for cleaned-up fork, got err=%v", err)
	}

	gotHash, err := db.GetGotHash(ctx, sourceRepo.ID, sourceGitHash)
	if err != nil {
		t.Fatalf("source mapping should remain: %v", err)
	}
	if gotHash != sourceGotHash {
		t.Fatalf("source mapping got_hash=%q, want %q", gotHash, sourceGotHash)
	}
}

func TestForkSuccessPathUnchanged(t *testing.T) {
	ctx, db, storageRoot, _, forkOwner, sourceRepo := setupForkTestFixture(t)

	const sourceGitHash = "git-success-1"
	const sourceGotHash = "got-success-1"
	if err := db.SetHashMapping(ctx, &models.HashMapping{
		RepoID:     sourceRepo.ID,
		GitHash:    sourceGitHash,
		GotHash:    sourceGotHash,
		ObjectType: "commit",
	}); err != nil {
		t.Fatalf("seed source hash mapping: %v", err)
	}

	repoSvc := NewRepoService(db, storageRoot)
	fork, err := repoSvc.Fork(ctx, sourceRepo.ID, forkOwner.ID, "")
	if err != nil {
		t.Fatalf("fork should succeed: %v", err)
	}

	if fork.ParentRepoID == nil || *fork.ParentRepoID != sourceRepo.ID {
		t.Fatalf("fork parent repo id = %v, want %d", fork.ParentRepoID, sourceRepo.ID)
	}
	if fork.Name != sourceRepo.Name {
		t.Fatalf("fork name = %q, want %q", fork.Name, sourceRepo.Name)
	}

	expectedStoragePath := filepath.Join(storageRoot, fmt.Sprintf("%d", fork.ID))
	if fork.StoragePath != expectedStoragePath {
		t.Fatalf("fork storage path = %q, want %q", fork.StoragePath, expectedStoragePath)
	}
	if _, err := os.Stat(expectedStoragePath); err != nil {
		t.Fatalf("fork storage path should exist: %v", err)
	}

	gotHash, err := db.GetGotHash(ctx, fork.ID, sourceGitHash)
	if err != nil {
		t.Fatalf("fork hash mapping should be cloned: %v", err)
	}
	if gotHash != sourceGotHash {
		t.Fatalf("fork mapping got_hash=%q, want %q", gotHash, sourceGotHash)
	}
}

func setupForkTestFixture(t *testing.T) (context.Context, *database.SQLiteDB, string, *models.User, *models.User, *models.Repository) {
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

	sourceOwner := &models.User{
		Username:     "source-owner",
		Email:        "source-owner@example.com",
		PasswordHash: "x",
	}
	if err := db.CreateUser(ctx, sourceOwner); err != nil {
		t.Fatal(err)
	}
	forkOwner := &models.User{
		Username:     "fork-owner",
		Email:        "fork-owner@example.com",
		PasswordHash: "x",
	}
	if err := db.CreateUser(ctx, forkOwner); err != nil {
		t.Fatal(err)
	}

	storageRoot := filepath.Join(tmpDir, "repos")
	repoSvc := NewRepoService(db, storageRoot)
	sourceRepo, err := repoSvc.Create(ctx, sourceOwner.ID, "source", "source repo", false)
	if err != nil {
		t.Fatal(err)
	}
	return ctx, db, storageRoot, sourceOwner, forkOwner, sourceRepo
}

func assertForkCleanedUp(t *testing.T, db *database.SQLiteDB, storageRoot string, forkID int64) {
	t.Helper()

	if forkID == 0 {
		t.Fatal("expected fork repository ID to be assigned")
	}

	_, err := db.GetRepositoryByID(context.Background(), forkID)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected fork repository row to be deleted, got err=%v", err)
	}

	forkStoragePath := filepath.Join(storageRoot, fmt.Sprintf("%d", forkID))
	_, err = os.Stat(forkStoragePath)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected fork storage path to be removed, got err=%v", err)
	}
}
