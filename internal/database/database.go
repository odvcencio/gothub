package database

import (
	"context"
	"time"

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
	UpdateUserPassword(ctx context.Context, userID int64, passwordHash string) error
	UpsertUserEntitlement(ctx context.Context, entitlement *models.UserEntitlement) error
	HasUserEntitlement(ctx context.Context, userID int64, feature string, at time.Time) (bool, error)
	CreateMagicLinkToken(ctx context.Context, token *models.MagicLinkToken) error
	ConsumeMagicLinkToken(ctx context.Context, tokenHash string, now time.Time) (*models.User, error)
	CreateSSHAuthChallenge(ctx context.Context, challenge *models.SSHAuthChallenge) error
	ConsumeSSHAuthChallenge(ctx context.Context, id string, now time.Time) (*models.SSHAuthChallenge, error)
	CreateWebAuthnCredential(ctx context.Context, credential *models.WebAuthnCredential) error
	ListWebAuthnCredentials(ctx context.Context, userID int64) ([]models.WebAuthnCredential, error)
	UpdateWebAuthnCredential(ctx context.Context, credential *models.WebAuthnCredential) error
	CreateWebAuthnSession(ctx context.Context, session *models.WebAuthnSession) error
	ConsumeWebAuthnSession(ctx context.Context, id, flow string, now time.Time) (*models.WebAuthnSession, error)

	// SSH Keys
	CreateSSHKey(ctx context.Context, key *models.SSHKey) error
	ListSSHKeys(ctx context.Context, userID int64) ([]models.SSHKey, error)
	GetSSHKeyByFingerprint(ctx context.Context, fingerprint string) (*models.SSHKey, error)
	DeleteSSHKey(ctx context.Context, id, userID int64) error

	// Repositories
	CreateRepository(ctx context.Context, repo *models.Repository) error
	UpdateRepositoryStoragePath(ctx context.Context, id int64, storagePath string) error
	CloneRepoMetadata(ctx context.Context, sourceRepoID, targetRepoID int64) error
	GetRepository(ctx context.Context, ownerName, repoName string) (*models.Repository, error)
	GetRepositoryByID(ctx context.Context, id int64) (*models.Repository, error)
	ListUserRepositories(ctx context.Context, userID int64) ([]models.Repository, error)
	ListUserRepositoriesPage(ctx context.Context, userID int64, limit, offset int) ([]models.Repository, error)
	CountUserOwnedRepositoriesByVisibility(ctx context.Context, userID int64, isPrivate bool) (int, error)
	ListRepositoryForks(ctx context.Context, parentRepoID int64) ([]models.Repository, error)
	ListRepositoryForksPage(ctx context.Context, parentRepoID int64, limit, offset int) ([]models.Repository, error)
	ListPublicRepositoriesPage(ctx context.Context, sort string, limit, offset int) ([]models.Repository, error)
	DeleteRepository(ctx context.Context, id int64) error

	// Stars
	AddRepoStar(ctx context.Context, repoID, userID int64) error
	RemoveRepoStar(ctx context.Context, repoID, userID int64) error
	IsRepoStarred(ctx context.Context, repoID, userID int64) (bool, error)
	CountRepoStars(ctx context.Context, repoID int64) (int, error)
	ListRepoStargazers(ctx context.Context, repoID int64) ([]models.User, error)
	ListRepoStargazersPage(ctx context.Context, repoID int64, limit, offset int) ([]models.User, error)
	ListUserStarredRepositories(ctx context.Context, userID int64) ([]models.Repository, error)
	ListUserStarredRepositoriesPage(ctx context.Context, userID int64, limit, offset int) ([]models.Repository, error)

	// Collaborators
	AddCollaborator(ctx context.Context, c *models.Collaborator) error
	GetCollaborator(ctx context.Context, repoID, userID int64) (*models.Collaborator, error)
	ListCollaborators(ctx context.Context, repoID int64) ([]models.Collaborator, error)
	ListCollaboratorsPage(ctx context.Context, repoID int64, limit, offset int) ([]models.Collaborator, error)
	RemoveCollaborator(ctx context.Context, repoID, userID int64) error

	// Pull Requests
	CreatePullRequest(ctx context.Context, pr *models.PullRequest) error
	GetPullRequest(ctx context.Context, repoID int64, number int) (*models.PullRequest, error)
	ListPullRequests(ctx context.Context, repoID int64, state string) ([]models.PullRequest, error)
	ListPullRequestsPage(ctx context.Context, repoID int64, state string, limit, offset int) ([]models.PullRequest, error)
	UpdatePullRequest(ctx context.Context, pr *models.PullRequest) error

	// PR Comments
	CreatePRComment(ctx context.Context, comment *models.PRComment) error
	ListPRComments(ctx context.Context, prID int64) ([]models.PRComment, error)
	ListPRCommentsPage(ctx context.Context, prID int64, limit, offset int) ([]models.PRComment, error)
	DeletePRComment(ctx context.Context, commentID, authorID int64) error

	// PR Reviews
	CreatePRReview(ctx context.Context, review *models.PRReview) error
	ListPRReviews(ctx context.Context, prID int64) ([]models.PRReview, error)
	ListPRReviewsPage(ctx context.Context, prID int64, limit, offset int) ([]models.PRReview, error)

	// Branch Protection
	UpsertBranchProtectionRule(ctx context.Context, rule *models.BranchProtectionRule) error
	GetBranchProtectionRule(ctx context.Context, repoID int64, branch string) (*models.BranchProtectionRule, error)
	DeleteBranchProtectionRule(ctx context.Context, repoID int64, branch string) error

	// PR Check Runs
	UpsertPRCheckRun(ctx context.Context, run *models.PRCheckRun) error
	ListPRCheckRuns(ctx context.Context, prID int64) ([]models.PRCheckRun, error)
	ListPRCheckRunsPage(ctx context.Context, prID int64, limit, offset int) ([]models.PRCheckRun, error)
	CreateRepoRunnerToken(ctx context.Context, token *models.RepoRunnerToken) error
	ListRepoRunnerTokens(ctx context.Context, repoID int64) ([]models.RepoRunnerToken, error)
	DeleteRepoRunnerToken(ctx context.Context, repoID, tokenID int64) error
	GetRepoRunnerTokenByHash(ctx context.Context, tokenHash string) (*models.RepoRunnerToken, error)
	TouchRepoRunnerTokenUsed(ctx context.Context, tokenID int64, usedAt time.Time) error

	// Issues
	CreateIssue(ctx context.Context, issue *models.Issue) error
	GetIssue(ctx context.Context, repoID int64, number int) (*models.Issue, error)
	ListIssues(ctx context.Context, repoID int64, state string) ([]models.Issue, error)
	ListIssuesPage(ctx context.Context, repoID int64, state string, limit, offset int) ([]models.Issue, error)
	UpdateIssue(ctx context.Context, issue *models.Issue) error
	CreateIssueComment(ctx context.Context, comment *models.IssueComment) error
	ListIssueComments(ctx context.Context, issueID int64) ([]models.IssueComment, error)
	ListIssueCommentsPage(ctx context.Context, issueID int64, limit, offset int) ([]models.IssueComment, error)
	DeleteIssueComment(ctx context.Context, commentID, authorID int64) error

	// Notifications
	CreateNotification(ctx context.Context, n *models.Notification) error
	ListNotifications(ctx context.Context, userID int64, unreadOnly bool) ([]models.Notification, error)
	ListNotificationsPage(ctx context.Context, userID int64, unreadOnly bool, limit, offset int) ([]models.Notification, error)
	CountUnreadNotifications(ctx context.Context, userID int64) (int, error)
	MarkNotificationRead(ctx context.Context, id, userID int64) error
	MarkAllNotificationsRead(ctx context.Context, userID int64) error

	// Webhooks
	CreateWebhook(ctx context.Context, hook *models.Webhook) error
	GetWebhook(ctx context.Context, repoID, webhookID int64) (*models.Webhook, error)
	ListWebhooks(ctx context.Context, repoID int64) ([]models.Webhook, error)
	ListWebhooksPage(ctx context.Context, repoID int64, limit, offset int) ([]models.Webhook, error)
	DeleteWebhook(ctx context.Context, repoID, webhookID int64) error
	CreateWebhookDelivery(ctx context.Context, delivery *models.WebhookDelivery) error
	GetWebhookDelivery(ctx context.Context, repoID, webhookID, deliveryID int64) (*models.WebhookDelivery, error)
	ListWebhookDeliveries(ctx context.Context, repoID, webhookID int64) ([]models.WebhookDelivery, error)
	ListWebhookDeliveriesPage(ctx context.Context, repoID, webhookID int64, limit, offset int) ([]models.WebhookDelivery, error)
	CreateInterestSignup(ctx context.Context, signup *models.InterestSignup) error
	ListInterestSignupsPage(ctx context.Context, limit, offset int) ([]models.InterestSignup, error)

	// Hash Mapping
	SetHashMapping(ctx context.Context, m *models.HashMapping) error
	SetHashMappings(ctx context.Context, mappings []models.HashMapping) error
	GetGotHash(ctx context.Context, repoID int64, gitHash string) (string, error)
	GetGitHash(ctx context.Context, repoID int64, gotHash string) (string, error)
	UpsertCommitMetadata(ctx context.Context, metadata *models.CommitMetadata) error
	GetCommitMetadata(ctx context.Context, repoID int64, commitHash string) (*models.CommitMetadata, bool, error)
	SetMergeBaseCache(ctx context.Context, repoID int64, leftHash, rightHash, baseHash string) error
	GetMergeBaseCache(ctx context.Context, repoID int64, leftHash, rightHash string) (string, bool, error)
	EnqueueIndexingJob(ctx context.Context, job *models.IndexingJob) error
	ClaimIndexingJob(ctx context.Context) (*models.IndexingJob, error)
	CompleteIndexingJob(ctx context.Context, jobID int64, status models.IndexJobStatus, errMsg string) error
	RequeueIndexingJob(ctx context.Context, jobID int64, errMsg string, nextAttemptAt time.Time) error
	GetIndexingJobStatus(ctx context.Context, repoID int64, commitHash string) (*models.IndexingJob, error)
	SetCommitIndex(ctx context.Context, repoID int64, commitHash, indexHash string) error
	GetCommitIndex(ctx context.Context, repoID int64, commitHash string) (string, error)
	SetGitTreeEntryModes(ctx context.Context, repoID int64, gotTreeHash string, modes map[string]string) error
	GetGitTreeEntryModes(ctx context.Context, repoID int64, gotTreeHash string) (map[string]string, error)
	UpsertEntityIdentity(ctx context.Context, identity *models.EntityIdentity) error
	SetEntityVersion(ctx context.Context, version *models.EntityVersion) error
	ListEntityVersionsByCommit(ctx context.Context, repoID int64, commitHash string) ([]models.EntityVersion, error)
	CountEntityVersionsByCommitFiltered(ctx context.Context, repoID int64, commitHash, stableID, name, bodyHash string) (int, error)
	ListEntityVersionsByCommitFilteredPage(ctx context.Context, repoID int64, commitHash, stableID, name, bodyHash string, limit, offset int) ([]models.EntityVersion, error)
	HasEntityVersionsForCommit(ctx context.Context, repoID int64, commitHash string) (bool, error)
	SetEntityIndexEntries(ctx context.Context, repoID int64, commitHash string, entries []models.EntityIndexEntry) error
	ListEntityIndexEntriesByCommit(ctx context.Context, repoID int64, commitHash, kind string, limit int) ([]models.EntityIndexEntry, error)
	SearchEntityIndexEntries(ctx context.Context, repoID int64, commitHash, textQuery, kind string, limit int) ([]models.EntityIndexEntry, error)
	HasEntityIndexForCommit(ctx context.Context, repoID int64, commitHash string) (bool, error)
	SetCommitXRefGraph(ctx context.Context, repoID int64, commitHash string, defs []models.XRefDefinition, edges []models.XRefEdge) error
	HasXRefGraphForCommit(ctx context.Context, repoID int64, commitHash string) (bool, error)
	FindXRefDefinitionsByName(ctx context.Context, repoID int64, commitHash, name string) ([]models.XRefDefinition, error)
	GetXRefDefinition(ctx context.Context, repoID int64, commitHash, entityID string) (*models.XRefDefinition, error)
	ListXRefEdgesFrom(ctx context.Context, repoID int64, commitHash, sourceEntityID, kind string) ([]models.XRefEdge, error)
	ListXRefEdgesTo(ctx context.Context, repoID int64, commitHash, targetEntityID, kind string) ([]models.XRefEdge, error)

	// Organizations
	CreateOrg(ctx context.Context, o *models.Org) error
	GetOrg(ctx context.Context, name string) (*models.Org, error)
	GetOrgByID(ctx context.Context, id int64) (*models.Org, error)
	ListUserOrgs(ctx context.Context, userID int64) ([]models.Org, error)
	ListUserOrgsPage(ctx context.Context, userID int64, limit, offset int) ([]models.Org, error)
	DeleteOrg(ctx context.Context, id int64) error
	AddOrgMember(ctx context.Context, m *models.OrgMember) error
	GetOrgMember(ctx context.Context, orgID, userID int64) (*models.OrgMember, error)
	ListOrgMembers(ctx context.Context, orgID int64) ([]models.OrgMember, error)
	ListOrgMembersPage(ctx context.Context, orgID int64, limit, offset int) ([]models.OrgMember, error)
	RemoveOrgMember(ctx context.Context, orgID, userID int64) error
	ListOrgRepositories(ctx context.Context, orgID int64) ([]models.Repository, error)
	ListOrgRepositoriesPage(ctx context.Context, orgID int64, limit, offset int) ([]models.Repository, error)
}
