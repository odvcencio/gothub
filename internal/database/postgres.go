package database

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/odvcencio/gothub/internal/models"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type PostgresDB struct {
	db *sql.DB
}

func OpenPostgres(dsn string) (*PostgresDB, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	return &PostgresDB{db: db}, nil
}

func (p *PostgresDB) Close() error { return p.db.Close() }

func (p *PostgresDB) Migrate(ctx context.Context) error {
	_, err := p.db.ExecContext(ctx, pgSchema)
	return err
}

const pgSchema = `
CREATE TABLE IF NOT EXISTS users (
	id BIGSERIAL PRIMARY KEY,
	username TEXT NOT NULL UNIQUE,
	email TEXT NOT NULL UNIQUE,
	password_hash TEXT NOT NULL,
	is_admin BOOLEAN NOT NULL DEFAULT FALSE,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS ssh_keys (
	id BIGSERIAL PRIMARY KEY,
	user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	name TEXT NOT NULL,
	fingerprint TEXT NOT NULL UNIQUE,
	public_key TEXT NOT NULL,
	key_type TEXT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS orgs (
	id BIGSERIAL PRIMARY KEY,
	name TEXT NOT NULL UNIQUE,
	display_name TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS org_members (
	org_id BIGINT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
	user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	role TEXT NOT NULL DEFAULT 'member',
	PRIMARY KEY (org_id, user_id)
);

CREATE TABLE IF NOT EXISTS repositories (
	id BIGSERIAL PRIMARY KEY,
	owner_user_id BIGINT REFERENCES users(id) ON DELETE CASCADE,
	owner_org_id BIGINT REFERENCES orgs(id) ON DELETE CASCADE,
	name TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	default_branch TEXT NOT NULL DEFAULT 'main',
	is_private BOOLEAN NOT NULL DEFAULT FALSE,
	storage_path TEXT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	UNIQUE(owner_user_id, name)
);

CREATE TABLE IF NOT EXISTS collaborators (
	repo_id BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
	user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	role TEXT NOT NULL DEFAULT 'read',
	PRIMARY KEY (repo_id, user_id)
);

CREATE TABLE IF NOT EXISTS pull_requests (
	id BIGSERIAL PRIMARY KEY,
	repo_id BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
	number INTEGER NOT NULL,
	title TEXT NOT NULL,
	body TEXT NOT NULL DEFAULT '',
	state TEXT NOT NULL DEFAULT 'open',
	author_id BIGINT NOT NULL REFERENCES users(id),
	source_branch TEXT NOT NULL,
	target_branch TEXT NOT NULL,
	source_commit TEXT NOT NULL DEFAULT '',
	target_commit TEXT NOT NULL DEFAULT '',
	merge_commit TEXT NOT NULL DEFAULT '',
	merge_method TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	merged_at TIMESTAMPTZ,
	UNIQUE(repo_id, number)
);

CREATE TABLE IF NOT EXISTS pr_comments (
	id BIGSERIAL PRIMARY KEY,
	pr_id BIGINT NOT NULL REFERENCES pull_requests(id) ON DELETE CASCADE,
	author_id BIGINT NOT NULL REFERENCES users(id),
	body TEXT NOT NULL,
	file_path TEXT NOT NULL DEFAULT '',
	entity_key TEXT NOT NULL DEFAULT '',
	line_number INTEGER,
	commit_hash TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS pr_reviews (
	id BIGSERIAL PRIMARY KEY,
	pr_id BIGINT NOT NULL REFERENCES pull_requests(id) ON DELETE CASCADE,
	author_id BIGINT NOT NULL REFERENCES users(id),
	state TEXT NOT NULL,
	body TEXT NOT NULL DEFAULT '',
	commit_hash TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS hash_mapping (
	repo_id BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
	got_hash TEXT NOT NULL,
	git_hash TEXT NOT NULL,
	object_type TEXT NOT NULL,
	PRIMARY KEY (repo_id, got_hash)
);

CREATE INDEX IF NOT EXISTS idx_hash_mapping_git ON hash_mapping(repo_id, git_hash);
`

// --- Users ---

func (p *PostgresDB) CreateUser(ctx context.Context, u *models.User) error {
	return p.db.QueryRowContext(ctx,
		`INSERT INTO users (username, email, password_hash, is_admin) VALUES ($1, $2, $3, $4) RETURNING id, created_at`,
		u.Username, u.Email, u.PasswordHash, u.IsAdmin).Scan(&u.ID, &u.CreatedAt)
}

func (p *PostgresDB) GetUserByID(ctx context.Context, id int64) (*models.User, error) {
	return p.scanUser(p.db.QueryRowContext(ctx,
		`SELECT id, username, email, password_hash, is_admin, created_at FROM users WHERE id = $1`, id))
}

func (p *PostgresDB) GetUserByUsername(ctx context.Context, username string) (*models.User, error) {
	return p.scanUser(p.db.QueryRowContext(ctx,
		`SELECT id, username, email, password_hash, is_admin, created_at FROM users WHERE username = $1`, username))
}

func (p *PostgresDB) GetUserByEmail(ctx context.Context, email string) (*models.User, error) {
	return p.scanUser(p.db.QueryRowContext(ctx,
		`SELECT id, username, email, password_hash, is_admin, created_at FROM users WHERE email = $1`, email))
}

func (p *PostgresDB) scanUser(row *sql.Row) (*models.User, error) {
	u := &models.User{}
	if err := row.Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.IsAdmin, &u.CreatedAt); err != nil {
		return nil, err
	}
	return u, nil
}

// --- SSH Keys ---

func (p *PostgresDB) CreateSSHKey(ctx context.Context, k *models.SSHKey) error {
	return p.db.QueryRowContext(ctx,
		`INSERT INTO ssh_keys (user_id, name, fingerprint, public_key, key_type) VALUES ($1, $2, $3, $4, $5) RETURNING id, created_at`,
		k.UserID, k.Name, k.Fingerprint, k.PublicKey, k.KeyType).Scan(&k.ID, &k.CreatedAt)
}

func (p *PostgresDB) ListSSHKeys(ctx context.Context, userID int64) ([]models.SSHKey, error) {
	rows, err := p.db.QueryContext(ctx,
		`SELECT id, user_id, name, fingerprint, public_key, key_type, created_at FROM ssh_keys WHERE user_id = $1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []models.SSHKey
	for rows.Next() {
		var k models.SSHKey
		if err := rows.Scan(&k.ID, &k.UserID, &k.Name, &k.Fingerprint, &k.PublicKey, &k.KeyType, &k.CreatedAt); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

func (p *PostgresDB) GetSSHKeyByFingerprint(ctx context.Context, fp string) (*models.SSHKey, error) {
	k := &models.SSHKey{}
	err := p.db.QueryRowContext(ctx,
		`SELECT id, user_id, name, fingerprint, public_key, key_type, created_at FROM ssh_keys WHERE fingerprint = $1`, fp).
		Scan(&k.ID, &k.UserID, &k.Name, &k.Fingerprint, &k.PublicKey, &k.KeyType, &k.CreatedAt)
	if err != nil {
		return nil, err
	}
	return k, nil
}

func (p *PostgresDB) DeleteSSHKey(ctx context.Context, id, userID int64) error {
	_, err := p.db.ExecContext(ctx, `DELETE FROM ssh_keys WHERE id = $1 AND user_id = $2`, id, userID)
	return err
}

// --- Repositories ---

func (p *PostgresDB) CreateRepository(ctx context.Context, r *models.Repository) error {
	return p.db.QueryRowContext(ctx,
		`INSERT INTO repositories (owner_user_id, owner_org_id, name, description, default_branch, is_private, storage_path)
		 VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id, created_at`,
		r.OwnerUserID, r.OwnerOrgID, r.Name, r.Description, r.DefaultBranch, r.IsPrivate, r.StoragePath).Scan(&r.ID, &r.CreatedAt)
}

func (p *PostgresDB) GetRepository(ctx context.Context, ownerName, repoName string) (*models.Repository, error) {
	r := &models.Repository{}
	// Try user-owned first, then org-owned
	err := p.db.QueryRowContext(ctx,
		`SELECT r.id, r.owner_user_id, r.owner_org_id, r.name, r.description, r.default_branch, r.is_private, r.storage_path, r.created_at, u.username
		 FROM repositories r
		 JOIN users u ON u.id = r.owner_user_id
		 WHERE u.username = $1 AND r.name = $2`, ownerName, repoName).
		Scan(&r.ID, &r.OwnerUserID, &r.OwnerOrgID, &r.Name, &r.Description, &r.DefaultBranch, &r.IsPrivate, &r.StoragePath, &r.CreatedAt, &r.OwnerName)
	if err == nil {
		return r, nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}
	// Try org-owned
	err = p.db.QueryRowContext(ctx,
		`SELECT r.id, r.owner_user_id, r.owner_org_id, r.name, r.description, r.default_branch, r.is_private, r.storage_path, r.created_at, o.name
		 FROM repositories r
		 JOIN orgs o ON o.id = r.owner_org_id
		 WHERE o.name = $1 AND r.name = $2`, ownerName, repoName).
		Scan(&r.ID, &r.OwnerUserID, &r.OwnerOrgID, &r.Name, &r.Description, &r.DefaultBranch, &r.IsPrivate, &r.StoragePath, &r.CreatedAt, &r.OwnerName)
	if err != nil {
		return nil, err
	}
	return r, nil
}

func (p *PostgresDB) GetRepositoryByID(ctx context.Context, id int64) (*models.Repository, error) {
	r := &models.Repository{}
	err := p.db.QueryRowContext(ctx,
		`SELECT r.id, r.owner_user_id, r.owner_org_id, r.name, r.description, r.default_branch, r.is_private, r.storage_path, r.created_at,
		 COALESCE(u.username, o.name, '')
		 FROM repositories r
		 LEFT JOIN users u ON u.id = r.owner_user_id
		 LEFT JOIN orgs o ON o.id = r.owner_org_id
		 WHERE r.id = $1`, id).
		Scan(&r.ID, &r.OwnerUserID, &r.OwnerOrgID, &r.Name, &r.Description, &r.DefaultBranch, &r.IsPrivate, &r.StoragePath, &r.CreatedAt, &r.OwnerName)
	if err != nil {
		return nil, err
	}
	return r, nil
}

func (p *PostgresDB) ListUserRepositories(ctx context.Context, userID int64) ([]models.Repository, error) {
	rows, err := p.db.QueryContext(ctx,
		`SELECT r.id, r.owner_user_id, r.owner_org_id, r.name, r.description, r.default_branch, r.is_private, r.storage_path, r.created_at,
		 COALESCE(u.username, o.name, '')
		 FROM repositories r
		 LEFT JOIN users u ON u.id = r.owner_user_id
		 LEFT JOIN orgs o ON o.id = r.owner_org_id
		 WHERE r.owner_user_id = $1
		 UNION
		 SELECT r.id, r.owner_user_id, r.owner_org_id, r.name, r.description, r.default_branch, r.is_private, r.storage_path, r.created_at,
		 o.name
		 FROM repositories r
		 JOIN orgs o ON o.id = r.owner_org_id
		 JOIN org_members om ON om.org_id = o.id AND om.user_id = $1
		 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var repos []models.Repository
	for rows.Next() {
		var r models.Repository
		if err := rows.Scan(&r.ID, &r.OwnerUserID, &r.OwnerOrgID, &r.Name, &r.Description, &r.DefaultBranch, &r.IsPrivate, &r.StoragePath, &r.CreatedAt, &r.OwnerName); err != nil {
			return nil, err
		}
		repos = append(repos, r)
	}
	return repos, rows.Err()
}

func (p *PostgresDB) DeleteRepository(ctx context.Context, id int64) error {
	_, err := p.db.ExecContext(ctx, `DELETE FROM repositories WHERE id = $1`, id)
	return err
}

// --- Collaborators ---

func (p *PostgresDB) AddCollaborator(ctx context.Context, c *models.Collaborator) error {
	_, err := p.db.ExecContext(ctx,
		`INSERT INTO collaborators (repo_id, user_id, role) VALUES ($1, $2, $3)
		 ON CONFLICT (repo_id, user_id) DO UPDATE SET role = EXCLUDED.role`,
		c.RepoID, c.UserID, c.Role)
	return err
}

func (p *PostgresDB) GetCollaborator(ctx context.Context, repoID, userID int64) (*models.Collaborator, error) {
	c := &models.Collaborator{}
	err := p.db.QueryRowContext(ctx,
		`SELECT repo_id, user_id, role FROM collaborators WHERE repo_id = $1 AND user_id = $2`, repoID, userID).
		Scan(&c.RepoID, &c.UserID, &c.Role)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (p *PostgresDB) ListCollaborators(ctx context.Context, repoID int64) ([]models.Collaborator, error) {
	rows, err := p.db.QueryContext(ctx,
		`SELECT repo_id, user_id, role FROM collaborators WHERE repo_id = $1`, repoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var collabs []models.Collaborator
	for rows.Next() {
		var c models.Collaborator
		if err := rows.Scan(&c.RepoID, &c.UserID, &c.Role); err != nil {
			return nil, err
		}
		collabs = append(collabs, c)
	}
	return collabs, rows.Err()
}

func (p *PostgresDB) RemoveCollaborator(ctx context.Context, repoID, userID int64) error {
	_, err := p.db.ExecContext(ctx, `DELETE FROM collaborators WHERE repo_id = $1 AND user_id = $2`, repoID, userID)
	return err
}

// --- Pull Requests ---

func (p *PostgresDB) CreatePullRequest(ctx context.Context, pr *models.PullRequest) error {
	// Auto-assign next PR number for this repo
	var maxNum int
	p.db.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(number), 0) FROM pull_requests WHERE repo_id = $1`, pr.RepoID).Scan(&maxNum)
	pr.Number = maxNum + 1

	return p.db.QueryRowContext(ctx,
		`INSERT INTO pull_requests (repo_id, number, title, body, state, author_id, source_branch, target_branch, source_commit, target_commit)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING id, created_at`,
		pr.RepoID, pr.Number, pr.Title, pr.Body, pr.State, pr.AuthorID, pr.SourceBranch, pr.TargetBranch, pr.SourceCommit, pr.TargetCommit).
		Scan(&pr.ID, &pr.CreatedAt)
}

func (p *PostgresDB) GetPullRequest(ctx context.Context, repoID int64, number int) (*models.PullRequest, error) {
	pr := &models.PullRequest{}
	err := p.db.QueryRowContext(ctx,
		`SELECT id, repo_id, number, title, body, state, author_id, source_branch, target_branch, source_commit, target_commit, merge_commit, merge_method, created_at, merged_at
		 FROM pull_requests WHERE repo_id = $1 AND number = $2`, repoID, number).
		Scan(&pr.ID, &pr.RepoID, &pr.Number, &pr.Title, &pr.Body, &pr.State, &pr.AuthorID,
			&pr.SourceBranch, &pr.TargetBranch, &pr.SourceCommit, &pr.TargetCommit,
			&pr.MergeCommit, &pr.MergeMethod, &pr.CreatedAt, &pr.MergedAt)
	if err != nil {
		return nil, err
	}
	return pr, nil
}

func (p *PostgresDB) ListPullRequests(ctx context.Context, repoID int64, state string) ([]models.PullRequest, error) {
	query := `SELECT id, repo_id, number, title, body, state, author_id, source_branch, target_branch, source_commit, target_commit, merge_commit, merge_method, created_at, merged_at
		 FROM pull_requests WHERE repo_id = $1`
	args := []any{repoID}
	if state != "" {
		query += ` AND state = $2`
		args = append(args, state)
	}
	query += ` ORDER BY number DESC`

	rows, err := p.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var prs []models.PullRequest
	for rows.Next() {
		var pr models.PullRequest
		if err := rows.Scan(&pr.ID, &pr.RepoID, &pr.Number, &pr.Title, &pr.Body, &pr.State, &pr.AuthorID,
			&pr.SourceBranch, &pr.TargetBranch, &pr.SourceCommit, &pr.TargetCommit,
			&pr.MergeCommit, &pr.MergeMethod, &pr.CreatedAt, &pr.MergedAt); err != nil {
			return nil, err
		}
		prs = append(prs, pr)
	}
	return prs, rows.Err()
}

func (p *PostgresDB) UpdatePullRequest(ctx context.Context, pr *models.PullRequest) error {
	_, err := p.db.ExecContext(ctx,
		`UPDATE pull_requests SET title=$1, body=$2, state=$3, source_commit=$4, target_commit=$5, merge_commit=$6, merge_method=$7, merged_at=$8
		 WHERE id = $9`,
		pr.Title, pr.Body, pr.State, pr.SourceCommit, pr.TargetCommit, pr.MergeCommit, pr.MergeMethod, pr.MergedAt, pr.ID)
	return err
}

// --- PR Comments ---

func (p *PostgresDB) CreatePRComment(ctx context.Context, c *models.PRComment) error {
	return p.db.QueryRowContext(ctx,
		`INSERT INTO pr_comments (pr_id, author_id, body, file_path, entity_key, line_number, commit_hash)
		 VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id, created_at`,
		c.PRID, c.AuthorID, c.Body, c.FilePath, c.EntityKey, c.LineNumber, c.CommitHash).Scan(&c.ID, &c.CreatedAt)
}

func (p *PostgresDB) ListPRComments(ctx context.Context, prID int64) ([]models.PRComment, error) {
	rows, err := p.db.QueryContext(ctx,
		`SELECT id, pr_id, author_id, body, file_path, entity_key, line_number, commit_hash, created_at
		 FROM pr_comments WHERE pr_id = $1 ORDER BY created_at`, prID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var comments []models.PRComment
	for rows.Next() {
		var c models.PRComment
		if err := rows.Scan(&c.ID, &c.PRID, &c.AuthorID, &c.Body, &c.FilePath, &c.EntityKey, &c.LineNumber, &c.CommitHash, &c.CreatedAt); err != nil {
			return nil, err
		}
		comments = append(comments, c)
	}
	return comments, rows.Err()
}

// --- PR Reviews ---

func (p *PostgresDB) CreatePRReview(ctx context.Context, r *models.PRReview) error {
	return p.db.QueryRowContext(ctx,
		`INSERT INTO pr_reviews (pr_id, author_id, state, body, commit_hash) VALUES ($1, $2, $3, $4, $5) RETURNING id, created_at`,
		r.PRID, r.AuthorID, r.State, r.Body, r.CommitHash).Scan(&r.ID, &r.CreatedAt)
}

func (p *PostgresDB) ListPRReviews(ctx context.Context, prID int64) ([]models.PRReview, error) {
	rows, err := p.db.QueryContext(ctx,
		`SELECT id, pr_id, author_id, state, body, commit_hash, created_at
		 FROM pr_reviews WHERE pr_id = $1 ORDER BY created_at`, prID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var reviews []models.PRReview
	for rows.Next() {
		var r models.PRReview
		if err := rows.Scan(&r.ID, &r.PRID, &r.AuthorID, &r.State, &r.Body, &r.CommitHash, &r.CreatedAt); err != nil {
			return nil, err
		}
		reviews = append(reviews, r)
	}
	return reviews, rows.Err()
}

// --- Hash Mapping ---

func (p *PostgresDB) SetHashMapping(ctx context.Context, m *models.HashMapping) error {
	_, err := p.db.ExecContext(ctx,
		`INSERT INTO hash_mapping (repo_id, got_hash, git_hash, object_type) VALUES ($1, $2, $3, $4)
		 ON CONFLICT (repo_id, got_hash) DO UPDATE SET git_hash = EXCLUDED.git_hash`,
		m.RepoID, m.GotHash, m.GitHash, m.ObjectType)
	return err
}

func (p *PostgresDB) GetGotHash(ctx context.Context, repoID int64, gitHash string) (string, error) {
	var h string
	err := p.db.QueryRowContext(ctx,
		`SELECT got_hash FROM hash_mapping WHERE repo_id = $1 AND git_hash = $2`, repoID, gitHash).Scan(&h)
	return h, err
}

func (p *PostgresDB) GetGitHash(ctx context.Context, repoID int64, gotHash string) (string, error) {
	var h string
	err := p.db.QueryRowContext(ctx,
		`SELECT git_hash FROM hash_mapping WHERE repo_id = $1 AND got_hash = $2`, repoID, gotHash).Scan(&h)
	return h, err
}

// --- Organizations ---

func (p *PostgresDB) CreateOrg(ctx context.Context, o *models.Org) error {
	return p.db.QueryRowContext(ctx,
		`INSERT INTO orgs (name, display_name) VALUES ($1, $2) RETURNING id`,
		o.Name, o.DisplayName).Scan(&o.ID)
}

func (p *PostgresDB) GetOrg(ctx context.Context, name string) (*models.Org, error) {
	o := &models.Org{}
	err := p.db.QueryRowContext(ctx,
		`SELECT id, name, display_name FROM orgs WHERE name = $1`, name).
		Scan(&o.ID, &o.Name, &o.DisplayName)
	if err != nil {
		return nil, err
	}
	return o, nil
}

func (p *PostgresDB) GetOrgByID(ctx context.Context, id int64) (*models.Org, error) {
	o := &models.Org{}
	err := p.db.QueryRowContext(ctx,
		`SELECT id, name, display_name FROM orgs WHERE id = $1`, id).
		Scan(&o.ID, &o.Name, &o.DisplayName)
	if err != nil {
		return nil, err
	}
	return o, nil
}

func (p *PostgresDB) ListUserOrgs(ctx context.Context, userID int64) ([]models.Org, error) {
	rows, err := p.db.QueryContext(ctx,
		`SELECT o.id, o.name, o.display_name FROM orgs o
		 JOIN org_members om ON om.org_id = o.id
		 WHERE om.user_id = $1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var orgs []models.Org
	for rows.Next() {
		var o models.Org
		if err := rows.Scan(&o.ID, &o.Name, &o.DisplayName); err != nil {
			return nil, err
		}
		orgs = append(orgs, o)
	}
	return orgs, rows.Err()
}

func (p *PostgresDB) DeleteOrg(ctx context.Context, id int64) error {
	_, err := p.db.ExecContext(ctx, `DELETE FROM orgs WHERE id = $1`, id)
	return err
}

func (p *PostgresDB) AddOrgMember(ctx context.Context, m *models.OrgMember) error {
	_, err := p.db.ExecContext(ctx,
		`INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, $3)
		 ON CONFLICT (org_id, user_id) DO UPDATE SET role = EXCLUDED.role`,
		m.OrgID, m.UserID, m.Role)
	return err
}

func (p *PostgresDB) GetOrgMember(ctx context.Context, orgID, userID int64) (*models.OrgMember, error) {
	m := &models.OrgMember{}
	err := p.db.QueryRowContext(ctx,
		`SELECT org_id, user_id, role FROM org_members WHERE org_id = $1 AND user_id = $2`, orgID, userID).
		Scan(&m.OrgID, &m.UserID, &m.Role)
	if err != nil {
		return nil, err
	}
	return m, nil
}

func (p *PostgresDB) ListOrgMembers(ctx context.Context, orgID int64) ([]models.OrgMember, error) {
	rows, err := p.db.QueryContext(ctx,
		`SELECT org_id, user_id, role FROM org_members WHERE org_id = $1`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var members []models.OrgMember
	for rows.Next() {
		var m models.OrgMember
		if err := rows.Scan(&m.OrgID, &m.UserID, &m.Role); err != nil {
			return nil, err
		}
		members = append(members, m)
	}
	return members, rows.Err()
}

func (p *PostgresDB) RemoveOrgMember(ctx context.Context, orgID, userID int64) error {
	_, err := p.db.ExecContext(ctx, `DELETE FROM org_members WHERE org_id = $1 AND user_id = $2`, orgID, userID)
	return err
}

func (p *PostgresDB) ListOrgRepositories(ctx context.Context, orgID int64) ([]models.Repository, error) {
	rows, err := p.db.QueryContext(ctx,
		`SELECT r.id, r.owner_user_id, r.owner_org_id, r.name, r.description, r.default_branch, r.is_private, r.storage_path, r.created_at, o.name
		 FROM repositories r
		 JOIN orgs o ON o.id = r.owner_org_id
		 WHERE r.owner_org_id = $1`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var repos []models.Repository
	for rows.Next() {
		var r models.Repository
		if err := rows.Scan(&r.ID, &r.OwnerUserID, &r.OwnerOrgID, &r.Name, &r.Description, &r.DefaultBranch, &r.IsPrivate, &r.StoragePath, &r.CreatedAt, &r.OwnerName); err != nil {
			return nil, err
		}
		repos = append(repos, r)
	}
	return repos, rows.Err()
}

// Compile-time interface check
var _ DB = (*PostgresDB)(nil)
