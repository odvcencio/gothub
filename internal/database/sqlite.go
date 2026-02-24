package database

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/odvcencio/gothub/internal/models"

	_ "modernc.org/sqlite"
)

type SQLiteDB struct {
	db *sql.DB
}

func OpenSQLite(dsn string) (*SQLiteDB, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// Enable WAL mode and foreign keys
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA foreign_keys=ON",
		"PRAGMA busy_timeout=5000",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("pragma %s: %w", pragma, err)
		}
	}
	return &SQLiteDB{db: db}, nil
}

func (s *SQLiteDB) Close() error { return s.db.Close() }

func (s *SQLiteDB) Migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, schema)
	return err
}

const schema = `
CREATE TABLE IF NOT EXISTS users (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	username TEXT NOT NULL UNIQUE,
	email TEXT NOT NULL UNIQUE,
	password_hash TEXT NOT NULL,
	is_admin BOOLEAN NOT NULL DEFAULT FALSE,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS ssh_keys (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	name TEXT NOT NULL,
	fingerprint TEXT NOT NULL UNIQUE,
	public_key TEXT NOT NULL,
	key_type TEXT NOT NULL,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS repositories (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	owner_user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
	owner_org_id INTEGER,
	name TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	default_branch TEXT NOT NULL DEFAULT 'main',
	is_private BOOLEAN NOT NULL DEFAULT FALSE,
	storage_path TEXT NOT NULL,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(owner_user_id, name)
);

CREATE TABLE IF NOT EXISTS collaborators (
	repo_id INTEGER NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
	user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	role TEXT NOT NULL DEFAULT 'read',
	PRIMARY KEY (repo_id, user_id)
);

CREATE TABLE IF NOT EXISTS pull_requests (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	repo_id INTEGER NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
	number INTEGER NOT NULL,
	title TEXT NOT NULL,
	body TEXT NOT NULL DEFAULT '',
	state TEXT NOT NULL DEFAULT 'open',
	author_id INTEGER NOT NULL REFERENCES users(id),
	source_branch TEXT NOT NULL,
	target_branch TEXT NOT NULL,
	source_commit TEXT NOT NULL DEFAULT '',
	target_commit TEXT NOT NULL DEFAULT '',
	merge_commit TEXT NOT NULL DEFAULT '',
	merge_method TEXT NOT NULL DEFAULT '',
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	merged_at DATETIME,
	UNIQUE(repo_id, number)
);

CREATE TABLE IF NOT EXISTS pr_comments (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	pr_id INTEGER NOT NULL REFERENCES pull_requests(id) ON DELETE CASCADE,
	author_id INTEGER NOT NULL REFERENCES users(id),
	body TEXT NOT NULL,
	file_path TEXT NOT NULL DEFAULT '',
	entity_key TEXT NOT NULL DEFAULT '',
	line_number INTEGER,
	commit_hash TEXT NOT NULL DEFAULT '',
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS pr_reviews (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	pr_id INTEGER NOT NULL REFERENCES pull_requests(id) ON DELETE CASCADE,
	author_id INTEGER NOT NULL REFERENCES users(id),
	state TEXT NOT NULL,
	body TEXT NOT NULL DEFAULT '',
	commit_hash TEXT NOT NULL DEFAULT '',
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS hash_mapping (
	repo_id INTEGER NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
	got_hash TEXT NOT NULL,
	git_hash TEXT NOT NULL,
	object_type TEXT NOT NULL,
	PRIMARY KEY (repo_id, got_hash)
);

CREATE INDEX IF NOT EXISTS idx_hash_mapping_git ON hash_mapping(repo_id, git_hash);
`

// --- Users ---

func (s *SQLiteDB) CreateUser(ctx context.Context, u *models.User) error {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO users (username, email, password_hash, is_admin) VALUES (?, ?, ?, ?)`,
		u.Username, u.Email, u.PasswordHash, u.IsAdmin)
	if err != nil {
		return err
	}
	u.ID, _ = res.LastInsertId()
	return nil
}

func (s *SQLiteDB) GetUserByID(ctx context.Context, id int64) (*models.User, error) {
	return s.scanUser(s.db.QueryRowContext(ctx,
		`SELECT id, username, email, password_hash, is_admin, created_at FROM users WHERE id = ?`, id))
}

func (s *SQLiteDB) GetUserByUsername(ctx context.Context, username string) (*models.User, error) {
	return s.scanUser(s.db.QueryRowContext(ctx,
		`SELECT id, username, email, password_hash, is_admin, created_at FROM users WHERE username = ?`, username))
}

func (s *SQLiteDB) GetUserByEmail(ctx context.Context, email string) (*models.User, error) {
	return s.scanUser(s.db.QueryRowContext(ctx,
		`SELECT id, username, email, password_hash, is_admin, created_at FROM users WHERE email = ?`, email))
}

func (s *SQLiteDB) scanUser(row *sql.Row) (*models.User, error) {
	u := &models.User{}
	if err := row.Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.IsAdmin, &u.CreatedAt); err != nil {
		return nil, err
	}
	return u, nil
}

// --- SSH Keys ---

func (s *SQLiteDB) CreateSSHKey(ctx context.Context, k *models.SSHKey) error {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO ssh_keys (user_id, name, fingerprint, public_key, key_type) VALUES (?, ?, ?, ?, ?)`,
		k.UserID, k.Name, k.Fingerprint, k.PublicKey, k.KeyType)
	if err != nil {
		return err
	}
	k.ID, _ = res.LastInsertId()
	return nil
}

func (s *SQLiteDB) ListSSHKeys(ctx context.Context, userID int64) ([]models.SSHKey, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, user_id, name, fingerprint, public_key, key_type, created_at FROM ssh_keys WHERE user_id = ?`, userID)
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

func (s *SQLiteDB) GetSSHKeyByFingerprint(ctx context.Context, fp string) (*models.SSHKey, error) {
	k := &models.SSHKey{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, name, fingerprint, public_key, key_type, created_at FROM ssh_keys WHERE fingerprint = ?`, fp).
		Scan(&k.ID, &k.UserID, &k.Name, &k.Fingerprint, &k.PublicKey, &k.KeyType, &k.CreatedAt)
	if err != nil {
		return nil, err
	}
	return k, nil
}

func (s *SQLiteDB) DeleteSSHKey(ctx context.Context, id, userID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM ssh_keys WHERE id = ? AND user_id = ?`, id, userID)
	return err
}

// --- Repositories ---

func (s *SQLiteDB) CreateRepository(ctx context.Context, r *models.Repository) error {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO repositories (owner_user_id, owner_org_id, name, description, default_branch, is_private, storage_path)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		r.OwnerUserID, r.OwnerOrgID, r.Name, r.Description, r.DefaultBranch, r.IsPrivate, r.StoragePath)
	if err != nil {
		return err
	}
	r.ID, _ = res.LastInsertId()
	return nil
}

func (s *SQLiteDB) GetRepository(ctx context.Context, ownerName, repoName string) (*models.Repository, error) {
	r := &models.Repository{}
	err := s.db.QueryRowContext(ctx,
		`SELECT r.id, r.owner_user_id, r.owner_org_id, r.name, r.description, r.default_branch, r.is_private, r.storage_path, r.created_at, u.username
		 FROM repositories r
		 JOIN users u ON u.id = r.owner_user_id
		 WHERE u.username = ? AND r.name = ?`, ownerName, repoName).
		Scan(&r.ID, &r.OwnerUserID, &r.OwnerOrgID, &r.Name, &r.Description, &r.DefaultBranch, &r.IsPrivate, &r.StoragePath, &r.CreatedAt, &r.OwnerName)
	if err != nil {
		return nil, err
	}
	return r, nil
}

func (s *SQLiteDB) GetRepositoryByID(ctx context.Context, id int64) (*models.Repository, error) {
	r := &models.Repository{}
	err := s.db.QueryRowContext(ctx,
		`SELECT r.id, r.owner_user_id, r.owner_org_id, r.name, r.description, r.default_branch, r.is_private, r.storage_path, r.created_at, u.username
		 FROM repositories r
		 JOIN users u ON u.id = r.owner_user_id
		 WHERE r.id = ?`, id).
		Scan(&r.ID, &r.OwnerUserID, &r.OwnerOrgID, &r.Name, &r.Description, &r.DefaultBranch, &r.IsPrivate, &r.StoragePath, &r.CreatedAt, &r.OwnerName)
	if err != nil {
		return nil, err
	}
	return r, nil
}

func (s *SQLiteDB) ListUserRepositories(ctx context.Context, userID int64) ([]models.Repository, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT r.id, r.owner_user_id, r.owner_org_id, r.name, r.description, r.default_branch, r.is_private, r.storage_path, r.created_at, u.username
		 FROM repositories r
		 JOIN users u ON u.id = r.owner_user_id
		 WHERE r.owner_user_id = ?`, userID)
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

func (s *SQLiteDB) DeleteRepository(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM repositories WHERE id = ?`, id)
	return err
}

// --- Collaborators ---

func (s *SQLiteDB) AddCollaborator(ctx context.Context, c *models.Collaborator) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO collaborators (repo_id, user_id, role) VALUES (?, ?, ?)`,
		c.RepoID, c.UserID, c.Role)
	return err
}

func (s *SQLiteDB) GetCollaborator(ctx context.Context, repoID, userID int64) (*models.Collaborator, error) {
	c := &models.Collaborator{}
	err := s.db.QueryRowContext(ctx,
		`SELECT repo_id, user_id, role FROM collaborators WHERE repo_id = ? AND user_id = ?`, repoID, userID).
		Scan(&c.RepoID, &c.UserID, &c.Role)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (s *SQLiteDB) ListCollaborators(ctx context.Context, repoID int64) ([]models.Collaborator, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT repo_id, user_id, role FROM collaborators WHERE repo_id = ?`, repoID)
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

func (s *SQLiteDB) RemoveCollaborator(ctx context.Context, repoID, userID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM collaborators WHERE repo_id = ? AND user_id = ?`, repoID, userID)
	return err
}

// --- Pull Requests ---

func (s *SQLiteDB) CreatePullRequest(ctx context.Context, pr *models.PullRequest) error {
	// Auto-assign next PR number for this repo
	var maxNum int
	s.db.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(number), 0) FROM pull_requests WHERE repo_id = ?`, pr.RepoID).Scan(&maxNum)
	pr.Number = maxNum + 1

	res, err := s.db.ExecContext(ctx,
		`INSERT INTO pull_requests (repo_id, number, title, body, state, author_id, source_branch, target_branch, source_commit, target_commit)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		pr.RepoID, pr.Number, pr.Title, pr.Body, pr.State, pr.AuthorID, pr.SourceBranch, pr.TargetBranch, pr.SourceCommit, pr.TargetCommit)
	if err != nil {
		return err
	}
	pr.ID, _ = res.LastInsertId()
	return nil
}

func (s *SQLiteDB) GetPullRequest(ctx context.Context, repoID int64, number int) (*models.PullRequest, error) {
	pr := &models.PullRequest{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, repo_id, number, title, body, state, author_id, source_branch, target_branch, source_commit, target_commit, merge_commit, merge_method, created_at, merged_at
		 FROM pull_requests WHERE repo_id = ? AND number = ?`, repoID, number).
		Scan(&pr.ID, &pr.RepoID, &pr.Number, &pr.Title, &pr.Body, &pr.State, &pr.AuthorID,
			&pr.SourceBranch, &pr.TargetBranch, &pr.SourceCommit, &pr.TargetCommit,
			&pr.MergeCommit, &pr.MergeMethod, &pr.CreatedAt, &pr.MergedAt)
	if err != nil {
		return nil, err
	}
	return pr, nil
}

func (s *SQLiteDB) ListPullRequests(ctx context.Context, repoID int64, state string) ([]models.PullRequest, error) {
	query := `SELECT id, repo_id, number, title, body, state, author_id, source_branch, target_branch, source_commit, target_commit, merge_commit, merge_method, created_at, merged_at
		 FROM pull_requests WHERE repo_id = ?`
	args := []any{repoID}
	if state != "" {
		query += ` AND state = ?`
		args = append(args, state)
	}
	query += ` ORDER BY number DESC`

	rows, err := s.db.QueryContext(ctx, query, args...)
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

func (s *SQLiteDB) UpdatePullRequest(ctx context.Context, pr *models.PullRequest) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE pull_requests SET title=?, body=?, state=?, source_commit=?, target_commit=?, merge_commit=?, merge_method=?, merged_at=?
		 WHERE id = ?`,
		pr.Title, pr.Body, pr.State, pr.SourceCommit, pr.TargetCommit, pr.MergeCommit, pr.MergeMethod, pr.MergedAt, pr.ID)
	return err
}

// --- PR Comments ---

func (s *SQLiteDB) CreatePRComment(ctx context.Context, c *models.PRComment) error {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO pr_comments (pr_id, author_id, body, file_path, entity_key, line_number, commit_hash)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		c.PRID, c.AuthorID, c.Body, c.FilePath, c.EntityKey, c.LineNumber, c.CommitHash)
	if err != nil {
		return err
	}
	c.ID, _ = res.LastInsertId()
	return nil
}

func (s *SQLiteDB) ListPRComments(ctx context.Context, prID int64) ([]models.PRComment, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, pr_id, author_id, body, file_path, entity_key, line_number, commit_hash, created_at
		 FROM pr_comments WHERE pr_id = ? ORDER BY created_at`, prID)
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

func (s *SQLiteDB) CreatePRReview(ctx context.Context, r *models.PRReview) error {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO pr_reviews (pr_id, author_id, state, body, commit_hash) VALUES (?, ?, ?, ?, ?)`,
		r.PRID, r.AuthorID, r.State, r.Body, r.CommitHash)
	if err != nil {
		return err
	}
	r.ID, _ = res.LastInsertId()
	return nil
}

func (s *SQLiteDB) ListPRReviews(ctx context.Context, prID int64) ([]models.PRReview, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, pr_id, author_id, state, body, commit_hash, created_at
		 FROM pr_reviews WHERE pr_id = ? ORDER BY created_at`, prID)
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

func (s *SQLiteDB) SetHashMapping(ctx context.Context, m *models.HashMapping) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO hash_mapping (repo_id, got_hash, git_hash, object_type) VALUES (?, ?, ?, ?)`,
		m.RepoID, m.GotHash, m.GitHash, m.ObjectType)
	return err
}

func (s *SQLiteDB) GetGotHash(ctx context.Context, repoID int64, gitHash string) (string, error) {
	var h string
	err := s.db.QueryRowContext(ctx,
		`SELECT got_hash FROM hash_mapping WHERE repo_id = ? AND git_hash = ?`, repoID, gitHash).Scan(&h)
	return h, err
}

func (s *SQLiteDB) GetGitHash(ctx context.Context, repoID int64, gotHash string) (string, error) {
	var h string
	err := s.db.QueryRowContext(ctx,
		`SELECT git_hash FROM hash_mapping WHERE repo_id = ? AND got_hash = ?`, repoID, gotHash).Scan(&h)
	return h, err
}

// Compile-time interface check
var _ DB = (*SQLiteDB)(nil)
