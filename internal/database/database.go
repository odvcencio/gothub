package database

import (
	"context"

	"github.com/odvcencio/gothub/internal/models"
)

// DB defines the data access interface. Implemented by SQLite and PostgreSQL backends.
type DB interface {
	Close() error
	Migrate(ctx context.Context) error

	// Users
	CreateUser(ctx context.Context, user *models.User) error
	GetUserByID(ctx context.Context, id int64) (*models.User, error)
	GetUserByUsername(ctx context.Context, username string) (*models.User, error)
	GetUserByEmail(ctx context.Context, email string) (*models.User, error)

	// SSH Keys
	CreateSSHKey(ctx context.Context, key *models.SSHKey) error
	ListSSHKeys(ctx context.Context, userID int64) ([]models.SSHKey, error)
	GetSSHKeyByFingerprint(ctx context.Context, fingerprint string) (*models.SSHKey, error)
	DeleteSSHKey(ctx context.Context, id, userID int64) error

	// Repositories
	CreateRepository(ctx context.Context, repo *models.Repository) error
	UpdateRepositoryStoragePath(ctx context.Context, id int64, storagePath string) error
	GetRepository(ctx context.Context, ownerName, repoName string) (*models.Repository, error)
	GetRepositoryByID(ctx context.Context, id int64) (*models.Repository, error)
	ListUserRepositories(ctx context.Context, userID int64) ([]models.Repository, error)
	DeleteRepository(ctx context.Context, id int64) error

	// Collaborators
	AddCollaborator(ctx context.Context, c *models.Collaborator) error
	GetCollaborator(ctx context.Context, repoID, userID int64) (*models.Collaborator, error)
	ListCollaborators(ctx context.Context, repoID int64) ([]models.Collaborator, error)
	RemoveCollaborator(ctx context.Context, repoID, userID int64) error

	// Pull Requests
	CreatePullRequest(ctx context.Context, pr *models.PullRequest) error
	GetPullRequest(ctx context.Context, repoID int64, number int) (*models.PullRequest, error)
	ListPullRequests(ctx context.Context, repoID int64, state string) ([]models.PullRequest, error)
	UpdatePullRequest(ctx context.Context, pr *models.PullRequest) error

	// PR Comments
	CreatePRComment(ctx context.Context, comment *models.PRComment) error
	ListPRComments(ctx context.Context, prID int64) ([]models.PRComment, error)

	// PR Reviews
	CreatePRReview(ctx context.Context, review *models.PRReview) error
	ListPRReviews(ctx context.Context, prID int64) ([]models.PRReview, error)

	// Hash Mapping
	SetHashMapping(ctx context.Context, m *models.HashMapping) error
	GetGotHash(ctx context.Context, repoID int64, gitHash string) (string, error)
	GetGitHash(ctx context.Context, repoID int64, gotHash string) (string, error)

	// Organizations
	CreateOrg(ctx context.Context, o *models.Org) error
	GetOrg(ctx context.Context, name string) (*models.Org, error)
	GetOrgByID(ctx context.Context, id int64) (*models.Org, error)
	ListUserOrgs(ctx context.Context, userID int64) ([]models.Org, error)
	DeleteOrg(ctx context.Context, id int64) error
	AddOrgMember(ctx context.Context, m *models.OrgMember) error
	GetOrgMember(ctx context.Context, orgID, userID int64) (*models.OrgMember, error)
	ListOrgMembers(ctx context.Context, orgID int64) ([]models.OrgMember, error)
	RemoveOrgMember(ctx context.Context, orgID, userID int64) error
	ListOrgRepositories(ctx context.Context, orgID int64) ([]models.Repository, error)
}
