package service

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"

	"github.com/odvcencio/gothub/internal/database"
	"github.com/odvcencio/gothub/internal/gotstore"
	"github.com/odvcencio/gothub/internal/models"
)

var validRepoName = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

type RepoService struct {
	db          database.DB
	storagePath string // root path for all repo storage
}

func NewRepoService(db database.DB, storagePath string) *RepoService {
	return &RepoService{db: db, storagePath: storagePath}
}

func (s *RepoService) Create(ctx context.Context, ownerID int64, name, description string, isPrivate bool) (*models.Repository, error) {
	if !validRepoName.MatchString(name) {
		return nil, fmt.Errorf("invalid repository name: %q", name)
	}

	repo := &models.Repository{
		OwnerUserID:   &ownerID,
		Name:          name,
		Description:   description,
		DefaultBranch: "main",
		IsPrivate:     isPrivate,
	}

	// Storage path will be set after we have the ID, but we need a temp path for creation
	repo.StoragePath = "pending"
	if err := s.db.CreateRepository(ctx, repo); err != nil {
		return nil, fmt.Errorf("create repo: %w", err)
	}

	// Set the real storage path using the repo ID
	repo.StoragePath = filepath.Join(s.storagePath, fmt.Sprintf("%d", repo.ID))

	// Initialize the bare repository store
	if _, err := gotstore.Open(repo.StoragePath); err != nil {
		return nil, fmt.Errorf("init repo store: %w", err)
	}

	if err := s.db.UpdateRepositoryStoragePath(ctx, repo.ID, repo.StoragePath); err != nil {
		return nil, fmt.Errorf("persist storage path: %w", err)
	}

	return repo, nil
}

func (s *RepoService) Get(ctx context.Context, owner, name string) (*models.Repository, error) {
	return s.db.GetRepository(ctx, owner, name)
}

func (s *RepoService) GetByID(ctx context.Context, id int64) (*models.Repository, error) {
	return s.db.GetRepositoryByID(ctx, id)
}

func (s *RepoService) List(ctx context.Context, userID int64) ([]models.Repository, error) {
	return s.db.ListUserRepositories(ctx, userID)
}

func (s *RepoService) Delete(ctx context.Context, id int64) error {
	return s.db.DeleteRepository(ctx, id)
}

// OpenStore opens the object store for a repository.
func (s *RepoService) OpenStore(ctx context.Context, owner, name string) (*gotstore.RepoStore, error) {
	repo, err := s.db.GetRepository(ctx, owner, name)
	if err != nil {
		return nil, fmt.Errorf("repo %s/%s: %w", owner, name, err)
	}
	// Backward compatibility: older rows may still have "pending".
	if repo.StoragePath == "" || repo.StoragePath == "pending" {
		repo.StoragePath = filepath.Join(s.storagePath, fmt.Sprintf("%d", repo.ID))
	}
	return gotstore.Open(repo.StoragePath)
}

// OpenStoreByID opens the object store for a repository by numeric ID.
func (s *RepoService) OpenStoreByID(ctx context.Context, repoID int64) (*gotstore.RepoStore, error) {
	repo, err := s.db.GetRepositoryByID(ctx, repoID)
	if err != nil {
		return nil, fmt.Errorf("repo %d: %w", repoID, err)
	}
	if repo.StoragePath == "" || repo.StoragePath == "pending" {
		repo.StoragePath = filepath.Join(s.storagePath, fmt.Sprintf("%d", repo.ID))
	}
	return gotstore.Open(repo.StoragePath)
}
