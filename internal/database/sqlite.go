package database

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

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
	if _, err := s.db.ExecContext(ctx, schema); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO entity_index_fts(entity_index_fts) VALUES('rebuild')`); err != nil {
		return err
	}
	// Backfill schema for existing installations created before entity_stable_id.
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE pr_comments ADD COLUMN entity_stable_id TEXT NOT NULL DEFAULT ''`); err != nil {
		if !isSQLiteDuplicateColumnErr(err) {
			return err
		}
	}
	// Backfill schema for existing installations created before fork support.
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE repositories ADD COLUMN parent_repo_id INTEGER`); err != nil {
		if !isSQLiteDuplicateColumnErr(err) {
			return err
		}
	}
	// Backfill schema for existing installations created before entity owner merge-gate support.
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE branch_protection_rules ADD COLUMN require_entity_owner_approval BOOLEAN NOT NULL DEFAULT FALSE`); err != nil {
		if !isSQLiteDuplicateColumnErr(err) {
			return err
		}
	}
	// Backfill schema for existing installations created before lint gate support.
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE branch_protection_rules ADD COLUMN require_lint_pass BOOLEAN NOT NULL DEFAULT FALSE`); err != nil {
		if !isSQLiteDuplicateColumnErr(err) {
			return err
		}
	}
	// Backfill schema for existing installations created before dead-code gate support.
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE branch_protection_rules ADD COLUMN require_no_new_dead_code BOOLEAN NOT NULL DEFAULT FALSE`); err != nil {
		if !isSQLiteDuplicateColumnErr(err) {
			return err
		}
	}
	// Backfill schema for existing installations created before signed-commit gate support.
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE branch_protection_rules ADD COLUMN require_signed_commits BOOLEAN NOT NULL DEFAULT FALSE`); err != nil {
		if !isSQLiteDuplicateColumnErr(err) {
			return err
		}
	}
	return nil
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

CREATE TABLE IF NOT EXISTS magic_link_tokens (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	token_hash TEXT NOT NULL UNIQUE,
	expires_at DATETIME NOT NULL,
	used_at DATETIME,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS ssh_auth_challenges (
	id TEXT PRIMARY KEY,
	user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	fingerprint TEXT NOT NULL,
	challenge TEXT NOT NULL,
	expires_at DATETIME NOT NULL,
	used_at DATETIME,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS webauthn_credentials (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	credential_id TEXT NOT NULL UNIQUE,
	data_json TEXT NOT NULL,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	last_used_at DATETIME
);

CREATE TABLE IF NOT EXISTS webauthn_sessions (
	id TEXT PRIMARY KEY,
	user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	flow TEXT NOT NULL,
	data_json TEXT NOT NULL,
	expires_at DATETIME NOT NULL,
	used_at DATETIME,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS orgs (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL UNIQUE,
	display_name TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS org_members (
	org_id INTEGER NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
	user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	role TEXT NOT NULL DEFAULT 'member',
	PRIMARY KEY (org_id, user_id)
);

CREATE TABLE IF NOT EXISTS repositories (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	owner_user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
	owner_org_id INTEGER,
	parent_repo_id INTEGER REFERENCES repositories(id) ON DELETE SET NULL,
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

CREATE TABLE IF NOT EXISTS repo_stars (
	repo_id INTEGER NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
	user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
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
	entity_stable_id TEXT NOT NULL DEFAULT '',
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

CREATE TABLE IF NOT EXISTS issues (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	repo_id INTEGER NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
	number INTEGER NOT NULL,
	title TEXT NOT NULL,
	body TEXT NOT NULL DEFAULT '',
	state TEXT NOT NULL DEFAULT 'open',
	author_id INTEGER NOT NULL REFERENCES users(id),
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	closed_at DATETIME,
	UNIQUE(repo_id, number)
);

CREATE TABLE IF NOT EXISTS issue_comments (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	issue_id INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
	author_id INTEGER NOT NULL REFERENCES users(id),
	body TEXT NOT NULL,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS notifications (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	actor_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	type TEXT NOT NULL,
	title TEXT NOT NULL,
	body TEXT NOT NULL DEFAULT '',
	resource_path TEXT NOT NULL DEFAULT '',
	repo_id INTEGER REFERENCES repositories(id) ON DELETE CASCADE,
	pr_id INTEGER REFERENCES pull_requests(id) ON DELETE CASCADE,
	issue_id INTEGER REFERENCES issues(id) ON DELETE CASCADE,
	read_at DATETIME,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS branch_protection_rules (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	repo_id INTEGER NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
	branch TEXT NOT NULL,
	enabled BOOLEAN NOT NULL DEFAULT TRUE,
	require_approvals BOOLEAN NOT NULL DEFAULT FALSE,
	required_approvals INTEGER NOT NULL DEFAULT 1,
	require_status_checks BOOLEAN NOT NULL DEFAULT FALSE,
	require_entity_owner_approval BOOLEAN NOT NULL DEFAULT FALSE,
	require_lint_pass BOOLEAN NOT NULL DEFAULT FALSE,
	require_no_new_dead_code BOOLEAN NOT NULL DEFAULT FALSE,
	require_signed_commits BOOLEAN NOT NULL DEFAULT FALSE,
	required_checks_csv TEXT NOT NULL DEFAULT '',
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(repo_id, branch)
);

CREATE TABLE IF NOT EXISTS pr_check_runs (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	pr_id INTEGER NOT NULL REFERENCES pull_requests(id) ON DELETE CASCADE,
	name TEXT NOT NULL,
	status TEXT NOT NULL DEFAULT 'queued',
	conclusion TEXT NOT NULL DEFAULT '',
	details_url TEXT NOT NULL DEFAULT '',
	external_id TEXT NOT NULL DEFAULT '',
	head_commit TEXT NOT NULL DEFAULT '',
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(pr_id, name)
);

CREATE TABLE IF NOT EXISTS repo_webhooks (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	repo_id INTEGER NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
	url TEXT NOT NULL,
	secret TEXT NOT NULL DEFAULT '',
	events_csv TEXT NOT NULL DEFAULT '*',
	active BOOLEAN NOT NULL DEFAULT TRUE,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS webhook_deliveries (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	repo_id INTEGER NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
	webhook_id INTEGER NOT NULL REFERENCES repo_webhooks(id) ON DELETE CASCADE,
	event TEXT NOT NULL,
	delivery_uid TEXT NOT NULL,
	attempt INTEGER NOT NULL DEFAULT 1,
	status_code INTEGER NOT NULL DEFAULT 0,
	success BOOLEAN NOT NULL DEFAULT FALSE,
	error TEXT NOT NULL DEFAULT '',
	request_body TEXT NOT NULL DEFAULT '',
	response_body TEXT NOT NULL DEFAULT '',
	duration_ms INTEGER NOT NULL DEFAULT 0,
	redelivery_of_id INTEGER,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS hash_mapping (
	repo_id INTEGER NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
	git_hash TEXT NOT NULL,
	got_hash TEXT NOT NULL,
	object_type TEXT NOT NULL,
	PRIMARY KEY (repo_id, git_hash),
	UNIQUE (repo_id, got_hash)
);

CREATE TABLE IF NOT EXISTS indexing_jobs (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	repo_id INTEGER NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
	commit_hash TEXT NOT NULL,
	job_type TEXT NOT NULL DEFAULT 'commit_index',
	status TEXT NOT NULL DEFAULT 'queued',
	attempt_count INTEGER NOT NULL DEFAULT 0,
	max_attempts INTEGER NOT NULL DEFAULT 3,
	last_error TEXT NOT NULL DEFAULT '',
	next_attempt_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	started_at DATETIME,
	completed_at DATETIME,
	UNIQUE (repo_id, commit_hash, job_type)
);

CREATE TABLE IF NOT EXISTS commit_indexes (
	repo_id INTEGER NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
	commit_hash TEXT NOT NULL,
	index_hash TEXT NOT NULL,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	PRIMARY KEY (repo_id, commit_hash)
);

CREATE TABLE IF NOT EXISTS git_tree_entry_modes (
	repo_id INTEGER NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
	got_tree_hash TEXT NOT NULL,
	entry_name TEXT NOT NULL,
	mode TEXT NOT NULL,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	PRIMARY KEY (repo_id, got_tree_hash, entry_name)
);

CREATE TABLE IF NOT EXISTS entity_identities (
	repo_id INTEGER NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
	stable_id TEXT NOT NULL,
	name TEXT NOT NULL,
	decl_kind TEXT NOT NULL DEFAULT '',
	receiver TEXT NOT NULL DEFAULT '',
	first_seen_commit TEXT NOT NULL,
	last_seen_commit TEXT NOT NULL,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	PRIMARY KEY (repo_id, stable_id)
);

CREATE TABLE IF NOT EXISTS entity_versions (
	repo_id INTEGER NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
	stable_id TEXT NOT NULL,
	commit_hash TEXT NOT NULL,
	path TEXT NOT NULL,
	entity_hash TEXT NOT NULL,
	body_hash TEXT NOT NULL DEFAULT '',
	name TEXT NOT NULL,
	decl_kind TEXT NOT NULL DEFAULT '',
	receiver TEXT NOT NULL DEFAULT '',
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	PRIMARY KEY (repo_id, commit_hash, path, entity_hash),
	FOREIGN KEY (repo_id, stable_id) REFERENCES entity_identities(repo_id, stable_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS entity_index_commits (
	repo_id INTEGER NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
	commit_hash TEXT NOT NULL,
	symbol_count INTEGER NOT NULL DEFAULT 0,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	PRIMARY KEY (repo_id, commit_hash)
);

CREATE TABLE IF NOT EXISTS entity_index (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	repo_id INTEGER NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
	commit_hash TEXT NOT NULL,
	file_path TEXT NOT NULL,
	symbol_key TEXT NOT NULL,
	stable_id TEXT NOT NULL DEFAULT '',
	kind TEXT NOT NULL DEFAULT '',
	name TEXT NOT NULL,
	signature TEXT NOT NULL DEFAULT '',
	receiver TEXT NOT NULL DEFAULT '',
	language TEXT NOT NULL DEFAULT '',
	doc_comment TEXT NOT NULL DEFAULT '',
	start_line INTEGER NOT NULL DEFAULT 0,
	end_line INTEGER NOT NULL DEFAULT 0,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	UNIQUE (repo_id, commit_hash, symbol_key)
);

CREATE VIRTUAL TABLE IF NOT EXISTS entity_index_fts USING fts5(
	name,
	signature,
	doc_comment,
	content='entity_index',
	content_rowid='id',
	tokenize='unicode61'
);

CREATE TRIGGER IF NOT EXISTS entity_index_ai AFTER INSERT ON entity_index BEGIN
	INSERT INTO entity_index_fts(rowid, name, signature, doc_comment)
	VALUES (new.id, new.name, new.signature, new.doc_comment);
END;

CREATE TRIGGER IF NOT EXISTS entity_index_ad AFTER DELETE ON entity_index BEGIN
	INSERT INTO entity_index_fts(entity_index_fts, rowid, name, signature, doc_comment)
	VALUES ('delete', old.id, old.name, old.signature, old.doc_comment);
END;

CREATE TRIGGER IF NOT EXISTS entity_index_au AFTER UPDATE ON entity_index BEGIN
	INSERT INTO entity_index_fts(entity_index_fts, rowid, name, signature, doc_comment)
	VALUES ('delete', old.id, old.name, old.signature, old.doc_comment);
	INSERT INTO entity_index_fts(rowid, name, signature, doc_comment)
	VALUES (new.id, new.name, new.signature, new.doc_comment);
END;

CREATE TABLE IF NOT EXISTS xref_definitions (
	repo_id INTEGER NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
	commit_hash TEXT NOT NULL,
	entity_id TEXT NOT NULL,
	file TEXT NOT NULL,
	package_name TEXT NOT NULL DEFAULT '',
	kind TEXT NOT NULL,
	name TEXT NOT NULL,
	signature TEXT NOT NULL DEFAULT '',
	receiver TEXT NOT NULL DEFAULT '',
	start_line INTEGER NOT NULL DEFAULT 0,
	end_line INTEGER NOT NULL DEFAULT 0,
	callable BOOLEAN NOT NULL DEFAULT FALSE,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	PRIMARY KEY (repo_id, commit_hash, entity_id)
);

CREATE TABLE IF NOT EXISTS xref_edges (
	repo_id INTEGER NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
	commit_hash TEXT NOT NULL,
	source_entity_id TEXT NOT NULL,
	target_entity_id TEXT NOT NULL,
	kind TEXT NOT NULL DEFAULT 'call',
	source_file TEXT NOT NULL DEFAULT '',
	source_line INTEGER NOT NULL DEFAULT 0,
	resolution TEXT NOT NULL DEFAULT '',
	count INTEGER NOT NULL DEFAULT 1,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	PRIMARY KEY (repo_id, commit_hash, source_entity_id, target_entity_id, kind)
);

CREATE INDEX IF NOT EXISTS idx_hash_mapping_git ON hash_mapping(repo_id, git_hash);
CREATE INDEX IF NOT EXISTS idx_hash_mapping_got ON hash_mapping(repo_id, got_hash);
CREATE INDEX IF NOT EXISTS idx_magic_link_tokens_hash ON magic_link_tokens(token_hash);
CREATE INDEX IF NOT EXISTS idx_magic_link_tokens_user ON magic_link_tokens(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_ssh_auth_challenges_user ON ssh_auth_challenges(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_webauthn_credentials_user ON webauthn_credentials(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_webauthn_sessions_user_flow ON webauthn_sessions(user_id, flow, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_indexing_jobs_claim ON indexing_jobs(status, next_attempt_at, id);
CREATE INDEX IF NOT EXISTS idx_indexing_jobs_repo_commit ON indexing_jobs(repo_id, commit_hash, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_commit_indexes_repo ON commit_indexes(repo_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_tree_modes_repo_tree ON git_tree_entry_modes(repo_id, got_tree_hash);
CREATE INDEX IF NOT EXISTS idx_entity_versions_repo_commit ON entity_versions(repo_id, commit_hash);
CREATE INDEX IF NOT EXISTS idx_entity_versions_repo_stable ON entity_versions(repo_id, stable_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_entity_versions_repo_bodyhash ON entity_versions(repo_id, body_hash);
CREATE INDEX IF NOT EXISTS idx_entity_index_commits_repo ON entity_index_commits(repo_id, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_entity_index_repo_commit ON entity_index(repo_id, commit_hash, file_path, start_line);
CREATE INDEX IF NOT EXISTS idx_entity_index_repo_kind ON entity_index(repo_id, commit_hash, kind);
CREATE INDEX IF NOT EXISTS idx_entity_index_repo_name ON entity_index(repo_id, commit_hash, name);
CREATE INDEX IF NOT EXISTS idx_xref_definitions_repo_commit_name ON xref_definitions(repo_id, commit_hash, name);
CREATE INDEX IF NOT EXISTS idx_xref_edges_repo_commit_source ON xref_edges(repo_id, commit_hash, source_entity_id, kind);
CREATE INDEX IF NOT EXISTS idx_xref_edges_repo_commit_target ON xref_edges(repo_id, commit_hash, target_entity_id, kind);
CREATE INDEX IF NOT EXISTS idx_branch_protection_repo_branch ON branch_protection_rules(repo_id, branch);
CREATE INDEX IF NOT EXISTS idx_pr_check_runs_pr ON pr_check_runs(pr_id, updated_at);
CREATE INDEX IF NOT EXISTS idx_issues_repo_number ON issues(repo_id, number DESC);
CREATE INDEX IF NOT EXISTS idx_issue_comments_issue ON issue_comments(issue_id, created_at);
CREATE INDEX IF NOT EXISTS idx_notifications_user_created ON notifications(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_notifications_user_unread ON notifications(user_id, read_at, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_repo_webhooks_repo ON repo_webhooks(repo_id);
CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_webhook_time ON webhook_deliveries(webhook_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_repo_stars_repo ON repo_stars(repo_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_repo_stars_user ON repo_stars(user_id, created_at DESC);
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

func (s *SQLiteDB) UpdateUserPassword(ctx context.Context, userID int64, passwordHash string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET password_hash = ? WHERE id = ?`, passwordHash, userID)
	return err
}

func (s *SQLiteDB) CreateMagicLinkToken(ctx context.Context, token *models.MagicLinkToken) error {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO magic_link_tokens (user_id, token_hash, expires_at) VALUES (?, ?, ?)`,
		token.UserID, token.TokenHash, token.ExpiresAt)
	if err != nil {
		return err
	}
	token.ID, _ = res.LastInsertId()
	return nil
}

func (s *SQLiteDB) ConsumeMagicLinkToken(ctx context.Context, tokenHash string, now time.Time) (*models.User, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var tokenID int64
	u := &models.User{}
	err = tx.QueryRowContext(ctx,
		`SELECT m.id, u.id, u.username, u.email, u.password_hash, u.is_admin, u.created_at
		 FROM magic_link_tokens m
		 JOIN users u ON u.id = m.user_id
		 WHERE m.token_hash = ? AND m.used_at IS NULL AND m.expires_at > ?`,
		tokenHash, now).Scan(&tokenID, &u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.IsAdmin, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	res, err := tx.ExecContext(ctx, `UPDATE magic_link_tokens SET used_at = ? WHERE id = ? AND used_at IS NULL`, now, tokenID)
	if err != nil {
		return nil, err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return nil, sql.ErrNoRows
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return u, nil
}

func (s *SQLiteDB) CreateSSHAuthChallenge(ctx context.Context, challenge *models.SSHAuthChallenge) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO ssh_auth_challenges (id, user_id, fingerprint, challenge, expires_at) VALUES (?, ?, ?, ?, ?)`,
		challenge.ID, challenge.UserID, challenge.Fingerprint, challenge.Challenge, challenge.ExpiresAt)
	return err
}

func (s *SQLiteDB) ConsumeSSHAuthChallenge(ctx context.Context, id string, now time.Time) (*models.SSHAuthChallenge, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	ch := &models.SSHAuthChallenge{}
	err = tx.QueryRowContext(ctx,
		`SELECT id, user_id, fingerprint, challenge, expires_at, used_at, created_at
		 FROM ssh_auth_challenges
		 WHERE id = ? AND used_at IS NULL AND expires_at > ?`,
		id, now).
		Scan(&ch.ID, &ch.UserID, &ch.Fingerprint, &ch.Challenge, &ch.ExpiresAt, &ch.UsedAt, &ch.CreatedAt)
	if err != nil {
		return nil, err
	}
	res, err := tx.ExecContext(ctx, `UPDATE ssh_auth_challenges SET used_at = ? WHERE id = ? AND used_at IS NULL`, now, id)
	if err != nil {
		return nil, err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return nil, sql.ErrNoRows
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return ch, nil
}

func (s *SQLiteDB) CreateWebAuthnCredential(ctx context.Context, credential *models.WebAuthnCredential) error {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO webauthn_credentials (user_id, credential_id, data_json) VALUES (?, ?, ?)`,
		credential.UserID, credential.CredentialID, credential.DataJSON)
	if err != nil {
		return err
	}
	credential.ID, _ = res.LastInsertId()
	return nil
}

func (s *SQLiteDB) ListWebAuthnCredentials(ctx context.Context, userID int64) ([]models.WebAuthnCredential, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, user_id, credential_id, data_json, created_at, last_used_at
		 FROM webauthn_credentials
		 WHERE user_id = ?
		 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []models.WebAuthnCredential
	for rows.Next() {
		var c models.WebAuthnCredential
		if err := rows.Scan(&c.ID, &c.UserID, &c.CredentialID, &c.DataJSON, &c.CreatedAt, &c.LastUsedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *SQLiteDB) UpdateWebAuthnCredential(ctx context.Context, credential *models.WebAuthnCredential) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE webauthn_credentials SET data_json = ?, last_used_at = ? WHERE user_id = ? AND credential_id = ?`,
		credential.DataJSON, credential.LastUsedAt, credential.UserID, credential.CredentialID)
	return err
}

func (s *SQLiteDB) CreateWebAuthnSession(ctx context.Context, session *models.WebAuthnSession) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO webauthn_sessions (id, user_id, flow, data_json, expires_at) VALUES (?, ?, ?, ?, ?)`,
		session.ID, session.UserID, session.Flow, session.DataJSON, session.ExpiresAt)
	return err
}

func (s *SQLiteDB) ConsumeWebAuthnSession(ctx context.Context, id, flow string, now time.Time) (*models.WebAuthnSession, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	session := &models.WebAuthnSession{}
	err = tx.QueryRowContext(ctx,
		`SELECT id, user_id, flow, data_json, expires_at, used_at, created_at
		 FROM webauthn_sessions
		 WHERE id = ? AND flow = ? AND used_at IS NULL AND expires_at > ?`,
		id, flow, now).
		Scan(&session.ID, &session.UserID, &session.Flow, &session.DataJSON, &session.ExpiresAt, &session.UsedAt, &session.CreatedAt)
	if err != nil {
		return nil, err
	}
	res, err := tx.ExecContext(ctx, `UPDATE webauthn_sessions SET used_at = ? WHERE id = ? AND used_at IS NULL`, now, id)
	if err != nil {
		return nil, err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return nil, sql.ErrNoRows
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return session, nil
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
		`INSERT INTO repositories (owner_user_id, owner_org_id, parent_repo_id, name, description, default_branch, is_private, storage_path)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		r.OwnerUserID, r.OwnerOrgID, r.ParentRepoID, r.Name, r.Description, r.DefaultBranch, r.IsPrivate, r.StoragePath)
	if err != nil {
		return err
	}
	r.ID, _ = res.LastInsertId()
	return nil
}

func (s *SQLiteDB) UpdateRepositoryStoragePath(ctx context.Context, id int64, storagePath string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE repositories SET storage_path = ? WHERE id = ?`, storagePath, id)
	return err
}

func (s *SQLiteDB) CloneRepoMetadata(ctx context.Context, sourceRepoID, targetRepoID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	copyStatements := []struct {
		query string
		args  []any
	}{
		{
			query: `INSERT INTO hash_mapping (repo_id, git_hash, got_hash, object_type)
					SELECT ?, git_hash, got_hash, object_type
					FROM hash_mapping
					WHERE repo_id = ?`,
			args: []any{targetRepoID, sourceRepoID},
		},
		{
			query: `INSERT INTO commit_indexes (repo_id, commit_hash, index_hash, created_at)
					SELECT ?, commit_hash, index_hash, created_at
					FROM commit_indexes
					WHERE repo_id = ?`,
			args: []any{targetRepoID, sourceRepoID},
		},
		{
			query: `INSERT INTO git_tree_entry_modes (repo_id, got_tree_hash, entry_name, mode, created_at)
					SELECT ?, got_tree_hash, entry_name, mode, created_at
					FROM git_tree_entry_modes
					WHERE repo_id = ?`,
			args: []any{targetRepoID, sourceRepoID},
		},
		{
			query: `INSERT INTO entity_identities (repo_id, stable_id, name, decl_kind, receiver, first_seen_commit, last_seen_commit, created_at, updated_at)
					SELECT ?, stable_id, name, decl_kind, receiver, first_seen_commit, last_seen_commit, created_at, updated_at
					FROM entity_identities
					WHERE repo_id = ?`,
			args: []any{targetRepoID, sourceRepoID},
		},
		{
			query: `INSERT INTO entity_versions (repo_id, stable_id, commit_hash, path, entity_hash, body_hash, name, decl_kind, receiver, created_at)
					SELECT ?, stable_id, commit_hash, path, entity_hash, body_hash, name, decl_kind, receiver, created_at
					FROM entity_versions
					WHERE repo_id = ?`,
			args: []any{targetRepoID, sourceRepoID},
		},
		{
			query: `INSERT INTO entity_index_commits (repo_id, commit_hash, symbol_count, created_at, updated_at)
					SELECT ?, commit_hash, symbol_count, created_at, updated_at
					FROM entity_index_commits
					WHERE repo_id = ?`,
			args: []any{targetRepoID, sourceRepoID},
		},
		{
			query: `INSERT INTO entity_index
						(repo_id, commit_hash, file_path, symbol_key, stable_id, kind, name, signature, receiver, language, doc_comment, start_line, end_line, created_at)
					SELECT ?, commit_hash, file_path, symbol_key, stable_id, kind, name, signature, receiver, language, doc_comment, start_line, end_line, created_at
					FROM entity_index
					WHERE repo_id = ?`,
			args: []any{targetRepoID, sourceRepoID},
		},
		{
			query: `INSERT INTO xref_definitions (repo_id, commit_hash, entity_id, file, package_name, kind, name, signature, receiver, start_line, end_line, callable, created_at)
					SELECT ?, commit_hash, entity_id, file, package_name, kind, name, signature, receiver, start_line, end_line, callable, created_at
					FROM xref_definitions
					WHERE repo_id = ?`,
			args: []any{targetRepoID, sourceRepoID},
		},
		{
			query: `INSERT INTO xref_edges (repo_id, commit_hash, source_entity_id, target_entity_id, kind, source_file, source_line, resolution, count, created_at)
					SELECT ?, commit_hash, source_entity_id, target_entity_id, kind, source_file, source_line, resolution, count, created_at
					FROM xref_edges
					WHERE repo_id = ?`,
			args: []any{targetRepoID, sourceRepoID},
		},
	}

	for _, stmt := range copyStatements {
		if _, err := tx.ExecContext(ctx, stmt.query, stmt.args...); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *SQLiteDB) GetRepository(ctx context.Context, ownerName, repoName string) (*models.Repository, error) {
	r := &models.Repository{}
	err := s.db.QueryRowContext(ctx,
		`SELECT r.id, r.owner_user_id, r.owner_org_id, r.parent_repo_id, r.name, r.description, r.default_branch, r.is_private, r.storage_path, r.created_at, u.username
		 FROM repositories r
		 JOIN users u ON u.id = r.owner_user_id
		 WHERE u.username = ? AND r.name = ?`, ownerName, repoName).
		Scan(&r.ID, &r.OwnerUserID, &r.OwnerOrgID, &r.ParentRepoID, &r.Name, &r.Description, &r.DefaultBranch, &r.IsPrivate, &r.StoragePath, &r.CreatedAt, &r.OwnerName)
	if err == nil {
		return r, nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}
	// Fallback to org-owned repositories.
	err = s.db.QueryRowContext(ctx,
		`SELECT r.id, r.owner_user_id, r.owner_org_id, r.parent_repo_id, r.name, r.description, r.default_branch, r.is_private, r.storage_path, r.created_at, o.name
		 FROM repositories r
		 JOIN orgs o ON o.id = r.owner_org_id
		 WHERE o.name = ? AND r.name = ?`, ownerName, repoName).
		Scan(&r.ID, &r.OwnerUserID, &r.OwnerOrgID, &r.ParentRepoID, &r.Name, &r.Description, &r.DefaultBranch, &r.IsPrivate, &r.StoragePath, &r.CreatedAt, &r.OwnerName)
	if err != nil {
		return nil, err
	}
	return r, nil
}

func (s *SQLiteDB) GetRepositoryByID(ctx context.Context, id int64) (*models.Repository, error) {
	r := &models.Repository{}
	err := s.db.QueryRowContext(ctx,
		`SELECT r.id, r.owner_user_id, r.owner_org_id, r.parent_repo_id, r.name, r.description, r.default_branch, r.is_private, r.storage_path, r.created_at,
		 COALESCE(u.username, o.name, '')
		 FROM repositories r
		 LEFT JOIN users u ON u.id = r.owner_user_id
		 LEFT JOIN orgs o ON o.id = r.owner_org_id
		 WHERE r.id = ?`, id).
		Scan(&r.ID, &r.OwnerUserID, &r.OwnerOrgID, &r.ParentRepoID, &r.Name, &r.Description, &r.DefaultBranch, &r.IsPrivate, &r.StoragePath, &r.CreatedAt, &r.OwnerName)
	if err != nil {
		return nil, err
	}
	return r, nil
}

func (s *SQLiteDB) ListUserRepositories(ctx context.Context, userID int64) ([]models.Repository, error) {
	return s.ListUserRepositoriesPage(ctx, userID, 1<<30, 0)
}

func (s *SQLiteDB) ListUserRepositoriesPage(ctx context.Context, userID int64, limit, offset int) ([]models.Repository, error) {
	if limit <= 0 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT r.id, r.owner_user_id, r.owner_org_id, r.parent_repo_id, r.name, r.description, r.default_branch, r.is_private, r.storage_path, r.created_at, u.username
		 FROM repositories r
		 JOIN users u ON u.id = r.owner_user_id
		 WHERE r.owner_user_id = ?
		 ORDER BY r.created_at DESC, r.id DESC
		 LIMIT ? OFFSET ?`, userID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var repos []models.Repository
	for rows.Next() {
		var r models.Repository
		if err := rows.Scan(&r.ID, &r.OwnerUserID, &r.OwnerOrgID, &r.ParentRepoID, &r.Name, &r.Description, &r.DefaultBranch, &r.IsPrivate, &r.StoragePath, &r.CreatedAt, &r.OwnerName); err != nil {
			return nil, err
		}
		repos = append(repos, r)
	}
	return repos, rows.Err()
}

func (s *SQLiteDB) ListRepositoryForks(ctx context.Context, parentRepoID int64) ([]models.Repository, error) {
	return s.ListRepositoryForksPage(ctx, parentRepoID, 1<<30, 0)
}

func (s *SQLiteDB) ListRepositoryForksPage(ctx context.Context, parentRepoID int64, limit, offset int) ([]models.Repository, error) {
	if limit <= 0 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT r.id, r.owner_user_id, r.owner_org_id, r.parent_repo_id, r.name, r.description, r.default_branch, r.is_private, r.storage_path, r.created_at,
		 COALESCE(u.username, o.name, '')
		 FROM repositories r
		 LEFT JOIN users u ON u.id = r.owner_user_id
		 LEFT JOIN orgs o ON o.id = r.owner_org_id
		 WHERE r.parent_repo_id = ?
		 ORDER BY r.created_at DESC, r.id DESC
		 LIMIT ? OFFSET ?`, parentRepoID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var repos []models.Repository
	for rows.Next() {
		var r models.Repository
		if err := rows.Scan(&r.ID, &r.OwnerUserID, &r.OwnerOrgID, &r.ParentRepoID, &r.Name, &r.Description, &r.DefaultBranch, &r.IsPrivate, &r.StoragePath, &r.CreatedAt, &r.OwnerName); err != nil {
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

// --- Stars ---

func (s *SQLiteDB) AddRepoStar(ctx context.Context, repoID, userID int64) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO repo_stars (repo_id, user_id) VALUES (?, ?)`,
		repoID, userID)
	return err
}

func (s *SQLiteDB) RemoveRepoStar(ctx context.Context, repoID, userID int64) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM repo_stars WHERE repo_id = ? AND user_id = ?`,
		repoID, userID)
	return err
}

func (s *SQLiteDB) IsRepoStarred(ctx context.Context, repoID, userID int64) (bool, error) {
	var one int
	err := s.db.QueryRowContext(ctx,
		`SELECT 1 FROM repo_stars WHERE repo_id = ? AND user_id = ?`,
		repoID, userID).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s *SQLiteDB) CountRepoStars(ctx context.Context, repoID int64) (int, error) {
	var count int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM repo_stars WHERE repo_id = ?`,
		repoID).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func (s *SQLiteDB) ListRepoStargazers(ctx context.Context, repoID int64) ([]models.User, error) {
	return s.ListRepoStargazersPage(ctx, repoID, 1<<30, 0)
}

func (s *SQLiteDB) ListRepoStargazersPage(ctx context.Context, repoID int64, limit, offset int) ([]models.User, error) {
	if limit <= 0 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT u.id, u.username, u.email, u.password_hash, u.is_admin, u.created_at
		 FROM repo_stars rs
		 JOIN users u ON u.id = rs.user_id
		 WHERE rs.repo_id = ?
		 ORDER BY rs.created_at DESC, u.id DESC
		 LIMIT ? OFFSET ?`,
		repoID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []models.User
	for rows.Next() {
		var u models.User
		if err := rows.Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.IsAdmin, &u.CreatedAt); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func (s *SQLiteDB) ListUserStarredRepositories(ctx context.Context, userID int64) ([]models.Repository, error) {
	return s.ListUserStarredRepositoriesPage(ctx, userID, 1<<30, 0)
}

func (s *SQLiteDB) ListUserStarredRepositoriesPage(ctx context.Context, userID int64, limit, offset int) ([]models.Repository, error) {
	if limit <= 0 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT r.id, r.owner_user_id, r.owner_org_id, r.parent_repo_id, r.name, r.description, r.default_branch, r.is_private, r.storage_path, r.created_at,
		 COALESCE(u.username, o.name, '')
		 FROM repo_stars rs
		 JOIN repositories r ON r.id = rs.repo_id
		 LEFT JOIN users u ON u.id = r.owner_user_id
		 LEFT JOIN orgs o ON o.id = r.owner_org_id
		 WHERE rs.user_id = ?
		 ORDER BY rs.created_at DESC, r.id DESC
		 LIMIT ? OFFSET ?`,
		userID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var repos []models.Repository
	for rows.Next() {
		var r models.Repository
		if err := rows.Scan(&r.ID, &r.OwnerUserID, &r.OwnerOrgID, &r.ParentRepoID, &r.Name, &r.Description, &r.DefaultBranch, &r.IsPrivate, &r.StoragePath, &r.CreatedAt, &r.OwnerName); err != nil {
			return nil, err
		}
		repos = append(repos, r)
	}
	return repos, rows.Err()
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
	return s.ListCollaboratorsPage(ctx, repoID, 1<<30, 0)
}

func (s *SQLiteDB) ListCollaboratorsPage(ctx context.Context, repoID int64, limit, offset int) ([]models.Collaborator, error) {
	if limit <= 0 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT repo_id, user_id, role FROM collaborators WHERE repo_id = ? ORDER BY user_id LIMIT ? OFFSET ?`, repoID, limit, offset)
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
	const maxAttempts = 20
	for attempt := 0; attempt < maxAttempts; attempt++ {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			if isSQLiteBusyErr(err) && attempt < maxAttempts-1 {
				time.Sleep(time.Duration(attempt+1) * 10 * time.Millisecond)
				continue
			}
			return err
		}

		// Allocate number and insert atomically within one transaction.
		var maxNum int
		if err := tx.QueryRowContext(ctx,
			`SELECT COALESCE(MAX(number), 0) FROM pull_requests WHERE repo_id = ?`, pr.RepoID).Scan(&maxNum); err != nil {
			tx.Rollback()
			if isSQLiteBusyErr(err) && attempt < maxAttempts-1 {
				time.Sleep(time.Duration(attempt+1) * 10 * time.Millisecond)
				continue
			}
			return err
		}
		pr.Number = maxNum + 1

		res, err := tx.ExecContext(ctx,
			`INSERT INTO pull_requests (repo_id, number, title, body, state, author_id, source_branch, target_branch, source_commit, target_commit)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			pr.RepoID, pr.Number, pr.Title, pr.Body, pr.State, pr.AuthorID, pr.SourceBranch, pr.TargetBranch, pr.SourceCommit, pr.TargetCommit)
		if err != nil {
			tx.Rollback()
			if (isSQLiteBusyErr(err) || isPRNumberUniqueConstraintErr(err)) && attempt < maxAttempts-1 {
				time.Sleep(time.Duration(attempt+1) * 10 * time.Millisecond)
				continue
			}
			return err
		}
		pr.ID, _ = res.LastInsertId()

		if err := tx.Commit(); err != nil {
			if (isSQLiteBusyErr(err) || isPRNumberUniqueConstraintErr(err)) && attempt < maxAttempts-1 {
				time.Sleep(time.Duration(attempt+1) * 10 * time.Millisecond)
				continue
			}
			return err
		}
		return nil
	}
	return fmt.Errorf("create pull request: retries exhausted")
}

func isSQLiteBusyErr(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "SQLITE_BUSY") || strings.Contains(s, "database is locked")
}

func isSQLiteDuplicateColumnErr(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "duplicate column name")
}

func isPRNumberUniqueConstraintErr(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "UNIQUE constraint failed: pull_requests.repo_id, pull_requests.number")
}

func (s *SQLiteDB) GetPullRequest(ctx context.Context, repoID int64, number int) (*models.PullRequest, error) {
	pr := &models.PullRequest{}
	err := s.db.QueryRowContext(ctx,
		`SELECT pr.id, pr.repo_id, pr.number, pr.title, pr.body, pr.state, pr.author_id, u.username,
		        pr.source_branch, pr.target_branch, pr.source_commit, pr.target_commit, pr.merge_commit, pr.merge_method, pr.created_at, pr.merged_at
		 FROM pull_requests pr
		 JOIN users u ON u.id = pr.author_id
		 WHERE pr.repo_id = ? AND pr.number = ?`, repoID, number).
		Scan(&pr.ID, &pr.RepoID, &pr.Number, &pr.Title, &pr.Body, &pr.State, &pr.AuthorID, &pr.AuthorName,
			&pr.SourceBranch, &pr.TargetBranch, &pr.SourceCommit, &pr.TargetCommit,
			&pr.MergeCommit, &pr.MergeMethod, &pr.CreatedAt, &pr.MergedAt)
	if err != nil {
		return nil, err
	}
	return pr, nil
}

func (s *SQLiteDB) ListPullRequests(ctx context.Context, repoID int64, state string) ([]models.PullRequest, error) {
	return s.ListPullRequestsPage(ctx, repoID, state, 1<<30, 0)
}

func (s *SQLiteDB) ListPullRequestsPage(ctx context.Context, repoID int64, state string, limit, offset int) ([]models.PullRequest, error) {
	query := `SELECT pr.id, pr.repo_id, pr.number, pr.title, pr.body, pr.state, pr.author_id, u.username,
	         pr.source_branch, pr.target_branch, pr.source_commit, pr.target_commit, pr.merge_commit, pr.merge_method, pr.created_at, pr.merged_at
		 FROM pull_requests pr
		 JOIN users u ON u.id = pr.author_id
		 WHERE pr.repo_id = ?`
	args := []any{repoID}
	if state != "" {
		query += ` AND pr.state = ?`
		args = append(args, state)
	}
	if limit <= 0 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	query += ` ORDER BY pr.number DESC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var prs []models.PullRequest
	for rows.Next() {
		var pr models.PullRequest
		if err := rows.Scan(&pr.ID, &pr.RepoID, &pr.Number, &pr.Title, &pr.Body, &pr.State, &pr.AuthorID, &pr.AuthorName,
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
		`INSERT INTO pr_comments (pr_id, author_id, body, file_path, entity_key, entity_stable_id, line_number, commit_hash)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		c.PRID, c.AuthorID, c.Body, c.FilePath, c.EntityKey, c.EntityStableID, c.LineNumber, c.CommitHash)
	if err != nil {
		return err
	}
	c.ID, _ = res.LastInsertId()
	return nil
}

func (s *SQLiteDB) ListPRComments(ctx context.Context, prID int64) ([]models.PRComment, error) {
	return s.ListPRCommentsPage(ctx, prID, 1<<30, 0)
}

func (s *SQLiteDB) ListPRCommentsPage(ctx context.Context, prID int64, limit, offset int) ([]models.PRComment, error) {
	if limit <= 0 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT c.id, c.pr_id, c.author_id, u.username, c.body, c.file_path, c.entity_key, c.entity_stable_id, c.line_number, c.commit_hash, c.created_at
		 FROM pr_comments c
		 JOIN users u ON u.id = c.author_id
		 WHERE c.pr_id = ?
		 ORDER BY c.created_at
		 LIMIT ? OFFSET ?`, prID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var comments []models.PRComment
	for rows.Next() {
		var c models.PRComment
		if err := rows.Scan(&c.ID, &c.PRID, &c.AuthorID, &c.AuthorName, &c.Body, &c.FilePath, &c.EntityKey, &c.EntityStableID, &c.LineNumber, &c.CommitHash, &c.CreatedAt); err != nil {
			return nil, err
		}
		comments = append(comments, c)
	}
	return comments, rows.Err()
}

func (s *SQLiteDB) DeletePRComment(ctx context.Context, commentID, authorID int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM pr_comments WHERE id = ? AND author_id = ?`, commentID, authorID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("comment not found or not owned by user")
	}
	return nil
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
	return s.ListPRReviewsPage(ctx, prID, 1<<30, 0)
}

func (s *SQLiteDB) ListPRReviewsPage(ctx context.Context, prID int64, limit, offset int) ([]models.PRReview, error) {
	if limit <= 0 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT r.id, r.pr_id, r.author_id, u.username, r.state, r.body, r.commit_hash, r.created_at
		 FROM pr_reviews r
		 JOIN users u ON u.id = r.author_id
		 WHERE r.pr_id = ?
		 ORDER BY r.created_at
		 LIMIT ? OFFSET ?`, prID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var reviews []models.PRReview
	for rows.Next() {
		var r models.PRReview
		if err := rows.Scan(&r.ID, &r.PRID, &r.AuthorID, &r.AuthorName, &r.State, &r.Body, &r.CommitHash, &r.CreatedAt); err != nil {
			return nil, err
		}
		reviews = append(reviews, r)
	}
	return reviews, rows.Err()
}

// --- Issues ---

func (s *SQLiteDB) CreateIssue(ctx context.Context, issue *models.Issue) error {
	const maxAttempts = 8
	for attempt := 0; attempt < maxAttempts; attempt++ {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			if isSQLiteBusyErr(err) && attempt < maxAttempts-1 {
				time.Sleep(time.Duration(attempt+1) * 5 * time.Millisecond)
				continue
			}
			return err
		}

		var maxNum int
		if err := tx.QueryRowContext(ctx,
			`SELECT COALESCE(MAX(number), 0) FROM issues WHERE repo_id = ?`, issue.RepoID).Scan(&maxNum); err != nil {
			tx.Rollback()
			if isSQLiteBusyErr(err) && attempt < maxAttempts-1 {
				time.Sleep(time.Duration(attempt+1) * 5 * time.Millisecond)
				continue
			}
			return err
		}
		issue.Number = maxNum + 1

		res, err := tx.ExecContext(ctx,
			`INSERT INTO issues (repo_id, number, title, body, state, author_id)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			issue.RepoID, issue.Number, issue.Title, issue.Body, issue.State, issue.AuthorID)
		if err != nil {
			tx.Rollback()
			if (isSQLiteBusyErr(err) || strings.Contains(err.Error(), "UNIQUE constraint failed: issues.repo_id, issues.number")) && attempt < maxAttempts-1 {
				time.Sleep(time.Duration(attempt+1) * 5 * time.Millisecond)
				continue
			}
			return err
		}
		issue.ID, _ = res.LastInsertId()

		if err := tx.Commit(); err != nil {
			if (isSQLiteBusyErr(err) || strings.Contains(err.Error(), "UNIQUE constraint failed: issues.repo_id, issues.number")) && attempt < maxAttempts-1 {
				time.Sleep(time.Duration(attempt+1) * 5 * time.Millisecond)
				continue
			}
			return err
		}
		return nil
	}
	return fmt.Errorf("create issue: retries exhausted")
}

func (s *SQLiteDB) GetIssue(ctx context.Context, repoID int64, number int) (*models.Issue, error) {
	issue := &models.Issue{}
	err := s.db.QueryRowContext(ctx,
		`SELECT i.id, i.repo_id, i.number, i.title, i.body, i.state, i.author_id, u.username, i.created_at, i.closed_at
		 FROM issues i
		 JOIN users u ON u.id = i.author_id
		 WHERE i.repo_id = ? AND i.number = ?`, repoID, number).
		Scan(&issue.ID, &issue.RepoID, &issue.Number, &issue.Title, &issue.Body, &issue.State, &issue.AuthorID, &issue.AuthorName, &issue.CreatedAt, &issue.ClosedAt)
	if err != nil {
		return nil, err
	}
	return issue, nil
}

func (s *SQLiteDB) ListIssues(ctx context.Context, repoID int64, state string) ([]models.Issue, error) {
	return s.ListIssuesPage(ctx, repoID, state, 1<<30, 0)
}

func (s *SQLiteDB) ListIssuesPage(ctx context.Context, repoID int64, state string, limit, offset int) ([]models.Issue, error) {
	query := `SELECT i.id, i.repo_id, i.number, i.title, i.body, i.state, i.author_id, u.username, i.created_at, i.closed_at
		 FROM issues i
		 JOIN users u ON u.id = i.author_id
		 WHERE i.repo_id = ?`
	args := []any{repoID}
	if state != "" {
		query += ` AND i.state = ?`
		args = append(args, state)
	}
	if limit <= 0 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	query += ` ORDER BY i.number DESC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var issues []models.Issue
	for rows.Next() {
		var issue models.Issue
		if err := rows.Scan(&issue.ID, &issue.RepoID, &issue.Number, &issue.Title, &issue.Body, &issue.State, &issue.AuthorID, &issue.AuthorName, &issue.CreatedAt, &issue.ClosedAt); err != nil {
			return nil, err
		}
		issues = append(issues, issue)
	}
	return issues, rows.Err()
}

func (s *SQLiteDB) UpdateIssue(ctx context.Context, issue *models.Issue) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE issues SET title = ?, body = ?, state = ?, closed_at = ? WHERE id = ?`,
		issue.Title, issue.Body, issue.State, issue.ClosedAt, issue.ID)
	return err
}

func (s *SQLiteDB) CreateIssueComment(ctx context.Context, c *models.IssueComment) error {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO issue_comments (issue_id, author_id, body) VALUES (?, ?, ?)`,
		c.IssueID, c.AuthorID, c.Body)
	if err != nil {
		return err
	}
	c.ID, _ = res.LastInsertId()
	return nil
}

func (s *SQLiteDB) ListIssueComments(ctx context.Context, issueID int64) ([]models.IssueComment, error) {
	return s.ListIssueCommentsPage(ctx, issueID, 1<<30, 0)
}

func (s *SQLiteDB) ListIssueCommentsPage(ctx context.Context, issueID int64, limit, offset int) ([]models.IssueComment, error) {
	if limit <= 0 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT c.id, c.issue_id, c.author_id, u.username, c.body, c.created_at
		 FROM issue_comments c
		 JOIN users u ON u.id = c.author_id
		 WHERE c.issue_id = ?
		 ORDER BY c.created_at
		 LIMIT ? OFFSET ?`, issueID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var comments []models.IssueComment
	for rows.Next() {
		var c models.IssueComment
		if err := rows.Scan(&c.ID, &c.IssueID, &c.AuthorID, &c.AuthorName, &c.Body, &c.CreatedAt); err != nil {
			return nil, err
		}
		comments = append(comments, c)
	}
	return comments, rows.Err()
}

func (s *SQLiteDB) DeleteIssueComment(ctx context.Context, commentID, authorID int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM issue_comments WHERE id = ? AND author_id = ?`, commentID, authorID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("comment not found or not owned by user")
	}
	return nil
}

// --- Notifications ---

func (s *SQLiteDB) CreateNotification(ctx context.Context, n *models.Notification) error {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO notifications (
			 user_id, actor_id, type, title, body, resource_path, repo_id, pr_id, issue_id, read_at
		 ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		n.UserID, n.ActorID, n.Type, n.Title, n.Body, n.ResourcePath, n.RepoID, n.PRID, n.IssueID, n.ReadAt)
	if err != nil {
		return err
	}
	n.ID, _ = res.LastInsertId()
	return s.db.QueryRowContext(ctx, `SELECT created_at FROM notifications WHERE id = ?`, n.ID).Scan(&n.CreatedAt)
}

func (s *SQLiteDB) ListNotifications(ctx context.Context, userID int64, unreadOnly bool) ([]models.Notification, error) {
	return s.ListNotificationsPage(ctx, userID, unreadOnly, 1<<30, 0)
}

func (s *SQLiteDB) ListNotificationsPage(ctx context.Context, userID int64, unreadOnly bool, limit, offset int) ([]models.Notification, error) {
	query := `SELECT n.id, n.user_id, n.actor_id, a.username, n.type, n.title, n.body, n.resource_path, n.repo_id, n.pr_id, n.issue_id, n.read_at, n.created_at
		 FROM notifications n
		 JOIN users a ON a.id = n.actor_id
		 WHERE n.user_id = ?`
	args := []any{userID}
	if unreadOnly {
		query += ` AND n.read_at IS NULL`
	}
	if limit <= 0 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	query += ` ORDER BY n.created_at DESC, n.id DESC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var notifications []models.Notification
	for rows.Next() {
		var n models.Notification
		var repoID, prID, issueID sql.NullInt64
		var readAt sql.NullTime
		if err := rows.Scan(&n.ID, &n.UserID, &n.ActorID, &n.ActorName, &n.Type, &n.Title, &n.Body, &n.ResourcePath, &repoID, &prID, &issueID, &readAt, &n.CreatedAt); err != nil {
			return nil, err
		}
		if repoID.Valid {
			v := repoID.Int64
			n.RepoID = &v
		}
		if prID.Valid {
			v := prID.Int64
			n.PRID = &v
		}
		if issueID.Valid {
			v := issueID.Int64
			n.IssueID = &v
		}
		if readAt.Valid {
			t := readAt.Time
			n.ReadAt = &t
		}
		notifications = append(notifications, n)
	}
	return notifications, rows.Err()
}

func (s *SQLiteDB) CountUnreadNotifications(ctx context.Context, userID int64) (int, error) {
	var count int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM notifications WHERE user_id = ? AND read_at IS NULL`,
		userID).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func (s *SQLiteDB) MarkNotificationRead(ctx context.Context, id, userID int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE notifications SET read_at = CURRENT_TIMESTAMP WHERE id = ? AND user_id = ? AND read_at IS NULL`,
		id, userID)
	return err
}

func (s *SQLiteDB) MarkAllNotificationsRead(ctx context.Context, userID int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE notifications SET read_at = CURRENT_TIMESTAMP WHERE user_id = ? AND read_at IS NULL`,
		userID)
	return err
}

// --- Branch Protection ---

func (s *SQLiteDB) UpsertBranchProtectionRule(ctx context.Context, rule *models.BranchProtectionRule) error {
	if rule.RequiredApprovals <= 0 {
		rule.RequiredApprovals = 1
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO branch_protection_rules (
			 repo_id, branch, enabled, require_approvals, required_approvals, require_status_checks, require_entity_owner_approval, require_lint_pass, require_no_new_dead_code, require_signed_commits, required_checks_csv
		 ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(repo_id, branch) DO UPDATE SET
			 enabled = excluded.enabled,
			 require_approvals = excluded.require_approvals,
			 required_approvals = excluded.required_approvals,
			 require_status_checks = excluded.require_status_checks,
			 require_entity_owner_approval = excluded.require_entity_owner_approval,
			 require_lint_pass = excluded.require_lint_pass,
			 require_no_new_dead_code = excluded.require_no_new_dead_code,
			 require_signed_commits = excluded.require_signed_commits,
			 required_checks_csv = excluded.required_checks_csv,
			 updated_at = CURRENT_TIMESTAMP`,
		rule.RepoID, rule.Branch, rule.Enabled, rule.RequireApprovals, rule.RequiredApprovals, rule.RequireStatusChecks, rule.RequireEntityOwnerApproval, rule.RequireLintPass, rule.RequireNoNewDeadCode, rule.RequireSignedCommits, rule.RequiredChecksCSV)
	if err != nil {
		return err
	}
	stored, err := s.GetBranchProtectionRule(ctx, rule.RepoID, rule.Branch)
	if err != nil {
		return err
	}
	*rule = *stored
	return nil
}

func (s *SQLiteDB) GetBranchProtectionRule(ctx context.Context, repoID int64, branch string) (*models.BranchProtectionRule, error) {
	rule := &models.BranchProtectionRule{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, repo_id, branch, enabled, require_approvals, required_approvals, require_status_checks, require_entity_owner_approval, require_lint_pass, require_no_new_dead_code, require_signed_commits, required_checks_csv, created_at, updated_at
		 FROM branch_protection_rules
		 WHERE repo_id = ? AND branch = ?`,
		repoID, branch).
		Scan(&rule.ID, &rule.RepoID, &rule.Branch, &rule.Enabled, &rule.RequireApprovals, &rule.RequiredApprovals,
			&rule.RequireStatusChecks, &rule.RequireEntityOwnerApproval, &rule.RequireLintPass, &rule.RequireNoNewDeadCode, &rule.RequireSignedCommits, &rule.RequiredChecksCSV, &rule.CreatedAt, &rule.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return rule, nil
}

func (s *SQLiteDB) DeleteBranchProtectionRule(ctx context.Context, repoID int64, branch string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM branch_protection_rules WHERE repo_id = ? AND branch = ?`, repoID, branch)
	return err
}

// --- PR Check Runs ---

func (s *SQLiteDB) UpsertPRCheckRun(ctx context.Context, run *models.PRCheckRun) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO pr_check_runs (
			 pr_id, name, status, conclusion, details_url, external_id, head_commit
		 ) VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(pr_id, name) DO UPDATE SET
			 status = excluded.status,
			 conclusion = excluded.conclusion,
			 details_url = excluded.details_url,
			 external_id = excluded.external_id,
			 head_commit = excluded.head_commit,
			 updated_at = CURRENT_TIMESTAMP`,
		run.PRID, run.Name, run.Status, run.Conclusion, run.DetailsURL, run.ExternalID, run.HeadCommit)
	if err != nil {
		return err
	}
	return s.db.QueryRowContext(ctx,
		`SELECT id, pr_id, name, status, conclusion, details_url, external_id, head_commit, created_at, updated_at
		 FROM pr_check_runs
		 WHERE pr_id = ? AND name = ?`,
		run.PRID, run.Name).
		Scan(&run.ID, &run.PRID, &run.Name, &run.Status, &run.Conclusion, &run.DetailsURL, &run.ExternalID, &run.HeadCommit, &run.CreatedAt, &run.UpdatedAt)
}

func (s *SQLiteDB) ListPRCheckRuns(ctx context.Context, prID int64) ([]models.PRCheckRun, error) {
	return s.ListPRCheckRunsPage(ctx, prID, 1<<30, 0)
}

func (s *SQLiteDB) ListPRCheckRunsPage(ctx context.Context, prID int64, limit, offset int) ([]models.PRCheckRun, error) {
	if limit <= 0 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, pr_id, name, status, conclusion, details_url, external_id, head_commit, created_at, updated_at
		 FROM pr_check_runs
		 WHERE pr_id = ?
		 ORDER BY updated_at DESC, id DESC
		 LIMIT ? OFFSET ?`, prID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var runs []models.PRCheckRun
	for rows.Next() {
		var run models.PRCheckRun
		if err := rows.Scan(&run.ID, &run.PRID, &run.Name, &run.Status, &run.Conclusion, &run.DetailsURL, &run.ExternalID, &run.HeadCommit, &run.CreatedAt, &run.UpdatedAt); err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

// --- Webhooks ---

func (s *SQLiteDB) CreateWebhook(ctx context.Context, hook *models.Webhook) error {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO repo_webhooks (repo_id, url, secret, events_csv, active)
		 VALUES (?, ?, ?, ?, ?)`,
		hook.RepoID, hook.URL, hook.Secret, hook.EventsCSV, hook.Active)
	if err != nil {
		return err
	}
	hook.ID, _ = res.LastInsertId()
	return s.db.QueryRowContext(ctx,
		`SELECT created_at, updated_at FROM repo_webhooks WHERE id = ?`, hook.ID).
		Scan(&hook.CreatedAt, &hook.UpdatedAt)
}

func (s *SQLiteDB) GetWebhook(ctx context.Context, repoID, webhookID int64) (*models.Webhook, error) {
	hook := &models.Webhook{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, repo_id, url, secret, events_csv, active, created_at, updated_at
		 FROM repo_webhooks
		 WHERE repo_id = ? AND id = ?`, repoID, webhookID).
		Scan(&hook.ID, &hook.RepoID, &hook.URL, &hook.Secret, &hook.EventsCSV, &hook.Active, &hook.CreatedAt, &hook.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return hook, nil
}

func (s *SQLiteDB) ListWebhooks(ctx context.Context, repoID int64) ([]models.Webhook, error) {
	return s.ListWebhooksPage(ctx, repoID, 1<<30, 0)
}

func (s *SQLiteDB) ListWebhooksPage(ctx context.Context, repoID int64, limit, offset int) ([]models.Webhook, error) {
	if limit <= 0 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, repo_id, url, secret, events_csv, active, created_at, updated_at
		 FROM repo_webhooks
		 WHERE repo_id = ?
		 ORDER BY id DESC
		 LIMIT ? OFFSET ?`, repoID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var hooks []models.Webhook
	for rows.Next() {
		var hook models.Webhook
		if err := rows.Scan(&hook.ID, &hook.RepoID, &hook.URL, &hook.Secret, &hook.EventsCSV, &hook.Active, &hook.CreatedAt, &hook.UpdatedAt); err != nil {
			return nil, err
		}
		hooks = append(hooks, hook)
	}
	return hooks, rows.Err()
}

func (s *SQLiteDB) DeleteWebhook(ctx context.Context, repoID, webhookID int64) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM repo_webhooks WHERE repo_id = ? AND id = ?`, repoID, webhookID)
	return err
}

func (s *SQLiteDB) CreateWebhookDelivery(ctx context.Context, delivery *models.WebhookDelivery) error {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO webhook_deliveries (
			 repo_id, webhook_id, event, delivery_uid, attempt, status_code, success, error, request_body, response_body, duration_ms, redelivery_of_id
		 ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		delivery.RepoID, delivery.WebhookID, delivery.Event, delivery.DeliveryUID, delivery.Attempt, delivery.StatusCode,
		delivery.Success, delivery.Error, delivery.RequestBody, delivery.ResponseBody, delivery.DurationMS, delivery.RedeliveryOfID)
	if err != nil {
		return err
	}
	delivery.ID, _ = res.LastInsertId()
	return s.db.QueryRowContext(ctx,
		`SELECT created_at FROM webhook_deliveries WHERE id = ?`, delivery.ID).
		Scan(&delivery.CreatedAt)
}

func (s *SQLiteDB) GetWebhookDelivery(ctx context.Context, repoID, webhookID, deliveryID int64) (*models.WebhookDelivery, error) {
	d := &models.WebhookDelivery{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, repo_id, webhook_id, event, delivery_uid, attempt, status_code, success, error, request_body, response_body, duration_ms, redelivery_of_id, created_at
		 FROM webhook_deliveries
		 WHERE repo_id = ? AND webhook_id = ? AND id = ?`,
		repoID, webhookID, deliveryID).
		Scan(&d.ID, &d.RepoID, &d.WebhookID, &d.Event, &d.DeliveryUID, &d.Attempt, &d.StatusCode, &d.Success, &d.Error,
			&d.RequestBody, &d.ResponseBody, &d.DurationMS, &d.RedeliveryOfID, &d.CreatedAt)
	if err != nil {
		return nil, err
	}
	return d, nil
}

func (s *SQLiteDB) ListWebhookDeliveries(ctx context.Context, repoID, webhookID int64) ([]models.WebhookDelivery, error) {
	return s.ListWebhookDeliveriesPage(ctx, repoID, webhookID, 1<<30, 0)
}

func (s *SQLiteDB) ListWebhookDeliveriesPage(ctx context.Context, repoID, webhookID int64, limit, offset int) ([]models.WebhookDelivery, error) {
	if limit <= 0 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, repo_id, webhook_id, event, delivery_uid, attempt, status_code, success, error, request_body, response_body, duration_ms, redelivery_of_id, created_at
		 FROM webhook_deliveries
		 WHERE repo_id = ? AND webhook_id = ?
		 ORDER BY id DESC
		 LIMIT ? OFFSET ?`, repoID, webhookID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var deliveries []models.WebhookDelivery
	for rows.Next() {
		var d models.WebhookDelivery
		if err := rows.Scan(&d.ID, &d.RepoID, &d.WebhookID, &d.Event, &d.DeliveryUID, &d.Attempt, &d.StatusCode, &d.Success, &d.Error,
			&d.RequestBody, &d.ResponseBody, &d.DurationMS, &d.RedeliveryOfID, &d.CreatedAt); err != nil {
			return nil, err
		}
		deliveries = append(deliveries, d)
	}
	return deliveries, rows.Err()
}

// --- Hash Mapping ---

func (s *SQLiteDB) SetHashMapping(ctx context.Context, m *models.HashMapping) error {
	return s.SetHashMappings(ctx, []models.HashMapping{*m})
}

func (s *SQLiteDB) SetHashMappings(ctx context.Context, mappings []models.HashMapping) error {
	if len(mappings) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO hash_mapping (repo_id, got_hash, git_hash, object_type) VALUES (?, ?, ?, ?)
		 ON CONFLICT(repo_id, git_hash) DO UPDATE SET
			 got_hash = excluded.got_hash,
			 object_type = excluded.object_type`)
	if err != nil {
		tx.Rollback()
		return err
	}
	defer stmt.Close()

	for i := range mappings {
		m := mappings[i]
		if _, err := stmt.ExecContext(ctx, m.RepoID, m.GotHash, m.GitHash, m.ObjectType); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
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

func (s *SQLiteDB) EnqueueIndexingJob(ctx context.Context, job *models.IndexingJob) error {
	if job == nil {
		return fmt.Errorf("indexing job is nil")
	}
	status := job.Status
	if status == "" {
		status = models.IndexJobQueued
	}
	jobType := job.JobType
	if jobType == "" {
		jobType = models.IndexJobTypeCommitIndex
	}
	maxAttempts := job.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	nextAttemptAt := job.NextAttemptAt
	if nextAttemptAt.IsZero() {
		nextAttemptAt = time.Now().UTC()
	}
	nextAttempt := sqliteTimestamp(nextAttemptAt)

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO indexing_jobs (
			 repo_id, commit_hash, job_type, status, attempt_count, max_attempts, last_error, next_attempt_at
		 ) VALUES (?, ?, ?, ?, 0, ?, '', datetime(?))
		 ON CONFLICT(repo_id, commit_hash, job_type) DO UPDATE SET
			 status = CASE
				WHEN indexing_jobs.status = ? THEN indexing_jobs.status
				ELSE ?
			 END,
			 last_error = CASE
				WHEN indexing_jobs.status = ? THEN indexing_jobs.last_error
				ELSE ''
			 END,
			 next_attempt_at = CASE
				WHEN indexing_jobs.status = ? THEN indexing_jobs.next_attempt_at
				ELSE excluded.next_attempt_at
			 END,
			 completed_at = CASE
				WHEN indexing_jobs.status = ? THEN indexing_jobs.completed_at
				ELSE NULL
			 END,
			 updated_at = CASE
				WHEN indexing_jobs.status = ? THEN indexing_jobs.updated_at
				ELSE CURRENT_TIMESTAMP
			 END`,
		job.RepoID, job.CommitHash, jobType, status, maxAttempts, nextAttempt,
		models.IndexJobCompleted, models.IndexJobQueued,
		models.IndexJobCompleted,
		models.IndexJobCompleted,
		models.IndexJobCompleted,
		models.IndexJobCompleted,
	)
	if err != nil {
		return err
	}

	loaded, err := s.getIndexingJobStatusByType(ctx, job.RepoID, job.CommitHash, jobType)
	if err != nil {
		return err
	}
	if loaded == nil {
		return sql.ErrNoRows
	}
	*job = *loaded
	return nil
}

func (s *SQLiteDB) ClaimIndexingJob(ctx context.Context) (*models.IndexingJob, error) {
	row := s.db.QueryRowContext(ctx,
		`UPDATE indexing_jobs
		 SET status = ?,
			 attempt_count = attempt_count + 1,
			 started_at = CURRENT_TIMESTAMP,
			 completed_at = NULL,
			 updated_at = CURRENT_TIMESTAMP
		 WHERE id = (
			 SELECT id
			 FROM indexing_jobs
			 WHERE status = ?
			   AND datetime(next_attempt_at) <= CURRENT_TIMESTAMP
			 ORDER BY next_attempt_at ASC, id ASC
			 LIMIT 1
		 )
		 RETURNING id, repo_id, commit_hash, job_type, status, attempt_count, max_attempts, last_error, next_attempt_at, created_at, updated_at, started_at, completed_at`,
		models.IndexJobInProgress, models.IndexJobQueued,
	)
	job, err := scanSQLiteIndexingJob(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return job, nil
}

func (s *SQLiteDB) CompleteIndexingJob(ctx context.Context, jobID int64, status models.IndexJobStatus, errMsg string) error {
	trimmedErr := strings.TrimSpace(errMsg)
	switch status {
	case models.IndexJobCompleted:
		trimmedErr = ""
	case models.IndexJobFailed:
		if trimmedErr == "" {
			trimmedErr = "job failed"
		}
	default:
		return fmt.Errorf("unsupported terminal status %q", status)
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE indexing_jobs
		 SET status = ?,
			 last_error = ?,
			 completed_at = CURRENT_TIMESTAMP,
			 updated_at = CURRENT_TIMESTAMP
		 WHERE id = ? AND status = ?`,
		status, trimmedErr, jobID, models.IndexJobInProgress,
	)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *SQLiteDB) RequeueIndexingJob(ctx context.Context, jobID int64, errMsg string, nextAttemptAt time.Time) error {
	trimmedErr := strings.TrimSpace(errMsg)
	if trimmedErr == "" {
		trimmedErr = "job failed"
	}
	if nextAttemptAt.IsZero() {
		nextAttemptAt = time.Now().UTC()
	}
	nextAttempt := sqliteTimestamp(nextAttemptAt)
	res, err := s.db.ExecContext(ctx,
		`UPDATE indexing_jobs
		 SET status = CASE
				 WHEN attempt_count >= max_attempts THEN ?
				 ELSE ?
			 END,
			 last_error = ?,
			 next_attempt_at = CASE
				 WHEN attempt_count >= max_attempts THEN next_attempt_at
				 ELSE datetime(?)
			 END,
			 started_at = NULL,
			 completed_at = CASE
				 WHEN attempt_count >= max_attempts THEN CURRENT_TIMESTAMP
				 ELSE NULL
			 END,
			 updated_at = CURRENT_TIMESTAMP
		 WHERE id = ? AND status = ?`,
		models.IndexJobFailed, models.IndexJobQueued, trimmedErr, nextAttempt, jobID, models.IndexJobInProgress,
	)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *SQLiteDB) GetIndexingJobStatus(ctx context.Context, repoID int64, commitHash string) (*models.IndexingJob, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, repo_id, commit_hash, job_type, status, attempt_count, max_attempts, last_error, next_attempt_at, created_at, updated_at, started_at, completed_at
		 FROM indexing_jobs
		 WHERE repo_id = ? AND commit_hash = ?
		 ORDER BY created_at DESC, id DESC
		 LIMIT 1`,
		repoID, commitHash,
	)
	job, err := scanSQLiteIndexingJob(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return job, nil
}

func (s *SQLiteDB) getIndexingJobStatusByType(ctx context.Context, repoID int64, commitHash string, jobType models.IndexJobType) (*models.IndexingJob, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, repo_id, commit_hash, job_type, status, attempt_count, max_attempts, last_error, next_attempt_at, created_at, updated_at, started_at, completed_at
		 FROM indexing_jobs
		 WHERE repo_id = ? AND commit_hash = ? AND job_type = ?
		 LIMIT 1`,
		repoID, commitHash, jobType,
	)
	job, err := scanSQLiteIndexingJob(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return job, nil
}

func scanSQLiteIndexingJob(row *sql.Row) (*models.IndexingJob, error) {
	var job models.IndexingJob
	var jobType string
	var status string
	var startedAt sql.NullTime
	var completedAt sql.NullTime
	if err := row.Scan(
		&job.ID,
		&job.RepoID,
		&job.CommitHash,
		&jobType,
		&status,
		&job.AttemptCount,
		&job.MaxAttempts,
		&job.LastError,
		&job.NextAttemptAt,
		&job.CreatedAt,
		&job.UpdatedAt,
		&startedAt,
		&completedAt,
	); err != nil {
		return nil, err
	}
	job.JobType = models.IndexJobType(jobType)
	job.Status = models.IndexJobStatus(status)
	if startedAt.Valid {
		v := startedAt.Time
		job.StartedAt = &v
	}
	if completedAt.Valid {
		v := completedAt.Time
		job.CompletedAt = &v
	}
	return &job, nil
}

func sqliteTimestamp(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04:05")
}

func (s *SQLiteDB) SetCommitIndex(ctx context.Context, repoID int64, commitHash, indexHash string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO commit_indexes (repo_id, commit_hash, index_hash) VALUES (?, ?, ?)
		 ON CONFLICT(repo_id, commit_hash) DO UPDATE SET
			 index_hash = excluded.index_hash`,
		repoID, commitHash, indexHash)
	return err
}

func (s *SQLiteDB) GetCommitIndex(ctx context.Context, repoID int64, commitHash string) (string, error) {
	var h string
	err := s.db.QueryRowContext(ctx,
		`SELECT index_hash FROM commit_indexes WHERE repo_id = ? AND commit_hash = ?`,
		repoID, commitHash).Scan(&h)
	return h, err
}

func (s *SQLiteDB) SetGitTreeEntryModes(ctx context.Context, repoID int64, gotTreeHash string, modes map[string]string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM git_tree_entry_modes WHERE repo_id = ? AND got_tree_hash = ?`,
		repoID, gotTreeHash); err != nil {
		tx.Rollback()
		return err
	}
	if len(modes) == 0 {
		return tx.Commit()
	}
	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO git_tree_entry_modes (repo_id, got_tree_hash, entry_name, mode) VALUES (?, ?, ?, ?)
		 ON CONFLICT(repo_id, got_tree_hash, entry_name) DO UPDATE SET mode = excluded.mode`)
	if err != nil {
		tx.Rollback()
		return err
	}
	defer stmt.Close()
	for entryName, mode := range modes {
		if strings.TrimSpace(entryName) == "" || strings.TrimSpace(mode) == "" {
			continue
		}
		if _, err := stmt.ExecContext(ctx, repoID, gotTreeHash, entryName, mode); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLiteDB) GetGitTreeEntryModes(ctx context.Context, repoID int64, gotTreeHash string) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT entry_name, mode FROM git_tree_entry_modes WHERE repo_id = ? AND got_tree_hash = ?`,
		repoID, gotTreeHash)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	modes := make(map[string]string)
	for rows.Next() {
		var entryName, mode string
		if err := rows.Scan(&entryName, &mode); err != nil {
			return nil, err
		}
		modes[entryName] = mode
	}
	return modes, rows.Err()
}

func (s *SQLiteDB) UpsertEntityIdentity(ctx context.Context, identity *models.EntityIdentity) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO entity_identities
			(repo_id, stable_id, name, decl_kind, receiver, first_seen_commit, last_seen_commit)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(repo_id, stable_id) DO UPDATE SET
			 name = excluded.name,
			 decl_kind = excluded.decl_kind,
			 receiver = excluded.receiver,
			 last_seen_commit = excluded.last_seen_commit,
			 updated_at = CURRENT_TIMESTAMP`,
		identity.RepoID, identity.StableID, identity.Name, identity.DeclKind, identity.Receiver, identity.FirstSeenCommit, identity.LastSeenCommit)
	return err
}

func (s *SQLiteDB) SetEntityVersion(ctx context.Context, version *models.EntityVersion) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO entity_versions
			(repo_id, stable_id, commit_hash, path, entity_hash, body_hash, name, decl_kind, receiver)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(repo_id, commit_hash, path, entity_hash) DO UPDATE SET
			 stable_id = excluded.stable_id,
			 body_hash = excluded.body_hash,
			 name = excluded.name,
			 decl_kind = excluded.decl_kind,
			 receiver = excluded.receiver`,
		version.RepoID, version.StableID, version.CommitHash, version.Path, version.EntityHash, version.BodyHash, version.Name, version.DeclKind, version.Receiver)
	return err
}

func (s *SQLiteDB) ListEntityVersionsByCommit(ctx context.Context, repoID int64, commitHash string) ([]models.EntityVersion, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT repo_id, stable_id, commit_hash, path, entity_hash, body_hash, name, decl_kind, receiver, created_at
		 FROM entity_versions
		 WHERE repo_id = ? AND commit_hash = ?
		 ORDER BY path, entity_hash`,
		repoID, commitHash)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	versions := make([]models.EntityVersion, 0)
	for rows.Next() {
		var v models.EntityVersion
		if err := rows.Scan(&v.RepoID, &v.StableID, &v.CommitHash, &v.Path, &v.EntityHash, &v.BodyHash, &v.Name, &v.DeclKind, &v.Receiver, &v.CreatedAt); err != nil {
			return nil, err
		}
		versions = append(versions, v)
	}
	return versions, rows.Err()
}

func (s *SQLiteDB) HasEntityVersionsForCommit(ctx context.Context, repoID int64, commitHash string) (bool, error) {
	var one int
	err := s.db.QueryRowContext(ctx,
		`SELECT 1 FROM entity_versions WHERE repo_id = ? AND commit_hash = ? LIMIT 1`,
		repoID, commitHash).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s *SQLiteDB) SetEntityIndexEntries(ctx context.Context, repoID int64, commitHash string, entries []models.EntityIndexEntry) error {
	if strings.TrimSpace(commitHash) == "" {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM entity_index WHERE repo_id = ? AND commit_hash = ?`,
		repoID, commitHash); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO entity_index_commits (repo_id, commit_hash, symbol_count, updated_at)
		 VALUES (?, ?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(repo_id, commit_hash) DO UPDATE SET
			 symbol_count = excluded.symbol_count,
			 updated_at = CURRENT_TIMESTAMP`,
		repoID, commitHash, len(entries)); err != nil {
		return err
	}

	if len(entries) == 0 {
		return tx.Commit()
	}

	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO entity_index
			(repo_id, commit_hash, file_path, symbol_key, stable_id, kind, name, signature, receiver, language, doc_comment, start_line, end_line)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(repo_id, commit_hash, symbol_key) DO UPDATE SET
			 file_path = excluded.file_path,
			 stable_id = excluded.stable_id,
			 kind = excluded.kind,
			 name = excluded.name,
			 signature = excluded.signature,
			 receiver = excluded.receiver,
			 language = excluded.language,
			 doc_comment = excluded.doc_comment,
			 start_line = excluded.start_line,
			 end_line = excluded.end_line`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, entry := range entries {
		if strings.TrimSpace(entry.SymbolKey) == "" {
			continue
		}
		if _, err := stmt.ExecContext(ctx,
			repoID,
			commitHash,
			entry.FilePath,
			entry.SymbolKey,
			entry.StableID,
			entry.Kind,
			entry.Name,
			entry.Signature,
			entry.Receiver,
			entry.Language,
			entry.DocComment,
			entry.StartLine,
			entry.EndLine,
		); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *SQLiteDB) ListEntityIndexEntriesByCommit(ctx context.Context, repoID int64, commitHash, kind string, limit int) ([]models.EntityIndexEntry, error) {
	query := `SELECT repo_id, commit_hash, file_path, symbol_key, stable_id, kind, name, signature, receiver, language, doc_comment, start_line, end_line, created_at
		FROM entity_index
		WHERE repo_id = ? AND commit_hash = ?`
	args := []any{repoID, commitHash}
	if strings.TrimSpace(kind) != "" {
		query += ` AND kind = ?`
		args = append(args, kind)
	}
	query += ` ORDER BY file_path, start_line, name`
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSQLiteEntityIndexEntries(rows)
}

func (s *SQLiteDB) SearchEntityIndexEntries(ctx context.Context, repoID int64, commitHash, textQuery, kind string, limit int) ([]models.EntityIndexEntry, error) {
	queryText := strings.TrimSpace(textQuery)
	if queryText == "" {
		return s.ListEntityIndexEntriesByCommit(ctx, repoID, commitHash, kind, limit)
	}

	ftsQuery := sqliteEntityIndexFTSQuery(queryText)
	if strings.TrimSpace(ftsQuery) == "" {
		return s.searchEntityIndexEntriesLike(ctx, repoID, commitHash, queryText, kind, limit)
	}

	stmt := `SELECT e.repo_id, e.commit_hash, e.file_path, e.symbol_key, e.stable_id, e.kind, e.name, e.signature, e.receiver, e.language, e.doc_comment, e.start_line, e.end_line, e.created_at
		FROM entity_index_fts
		JOIN entity_index e ON e.id = entity_index_fts.rowid
		WHERE e.repo_id = ? AND e.commit_hash = ?`
	args := []any{repoID, commitHash}
	if strings.TrimSpace(kind) != "" {
		stmt += ` AND e.kind = ?`
		args = append(args, kind)
	}
	stmt += ` AND entity_index_fts MATCH ?`
	args = append(args, ftsQuery)
	stmt += ` ORDER BY bm25(entity_index_fts), e.name, e.file_path, e.start_line`
	if limit > 0 {
		stmt += ` LIMIT ?`
		args = append(args, limit)
	}

	rows, err := s.db.QueryContext(ctx, stmt, args...)
	if err != nil {
		return s.searchEntityIndexEntriesLike(ctx, repoID, commitHash, queryText, kind, limit)
	}
	defer rows.Close()
	ftsEntries, err := scanSQLiteEntityIndexEntries(rows)
	if err != nil {
		return nil, err
	}

	// FTS5 prefix search misses infix matches (for example "Order" in "ProcessOrder").
	// Merge LIKE-based substring hits so plain-text search behavior matches Postgres.
	likeEntries, err := s.searchEntityIndexEntriesLike(ctx, repoID, commitHash, queryText, kind, 0)
	if err != nil {
		if limit > 0 && len(ftsEntries) > limit {
			return ftsEntries[:limit], nil
		}
		return ftsEntries, nil
	}

	if len(ftsEntries) == 0 {
		if limit > 0 && len(likeEntries) > limit {
			return likeEntries[:limit], nil
		}
		return likeEntries, nil
	}

	seen := make(map[string]struct{}, len(ftsEntries))
	merged := make([]models.EntityIndexEntry, 0, len(ftsEntries)+len(likeEntries))
	for _, entry := range ftsEntries {
		key := entityIndexEntryKey(entry)
		seen[key] = struct{}{}
		merged = append(merged, entry)
	}
	for _, entry := range likeEntries {
		key := entityIndexEntryKey(entry)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		merged = append(merged, entry)
	}

	if limit > 0 && len(merged) > limit {
		return merged[:limit], nil
	}
	return merged, nil
}

func (s *SQLiteDB) searchEntityIndexEntriesLike(ctx context.Context, repoID int64, commitHash, textQuery, kind string, limit int) ([]models.EntityIndexEntry, error) {
	stmt := `SELECT repo_id, commit_hash, file_path, symbol_key, stable_id, kind, name, signature, receiver, language, doc_comment, start_line, end_line, created_at
		FROM entity_index
		WHERE repo_id = ? AND commit_hash = ?`
	args := []any{repoID, commitHash}
	if strings.TrimSpace(kind) != "" {
		stmt += ` AND kind = ?`
		args = append(args, kind)
	}
	stmt += ` AND (
		instr(lower(name), lower(?)) > 0 OR
		instr(lower(signature), lower(?)) > 0 OR
		instr(lower(doc_comment), lower(?)) > 0
	)`
	args = append(args, textQuery, textQuery, textQuery)
	stmt += ` ORDER BY name, file_path, start_line`
	if limit > 0 {
		stmt += ` LIMIT ?`
		args = append(args, limit)
	}

	rows, err := s.db.QueryContext(ctx, stmt, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSQLiteEntityIndexEntries(rows)
}

func (s *SQLiteDB) HasEntityIndexForCommit(ctx context.Context, repoID int64, commitHash string) (bool, error) {
	var one int
	err := s.db.QueryRowContext(ctx,
		`SELECT 1 FROM entity_index_commits WHERE repo_id = ? AND commit_hash = ? LIMIT 1`,
		repoID, commitHash).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func scanSQLiteEntityIndexEntries(rows *sql.Rows) ([]models.EntityIndexEntry, error) {
	entries := make([]models.EntityIndexEntry, 0)
	for rows.Next() {
		var entry models.EntityIndexEntry
		if err := rows.Scan(
			&entry.RepoID,
			&entry.CommitHash,
			&entry.FilePath,
			&entry.SymbolKey,
			&entry.StableID,
			&entry.Kind,
			&entry.Name,
			&entry.Signature,
			&entry.Receiver,
			&entry.Language,
			&entry.DocComment,
			&entry.StartLine,
			&entry.EndLine,
			&entry.CreatedAt,
		); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func entityIndexEntryKey(entry models.EntityIndexEntry) string {
	if key := strings.TrimSpace(entry.SymbolKey); key != "" {
		return key
	}
	return strings.Join([]string{
		entry.FilePath,
		entry.Kind,
		entry.Name,
		strconv.Itoa(entry.StartLine),
		strconv.Itoa(entry.EndLine),
	}, "\x00")
}

func sqliteEntityIndexFTSQuery(text string) string {
	parts := strings.Fields(text)
	tokens := make([]string, 0, len(parts))
	for _, part := range parts {
		token := strings.Map(func(r rune) rune {
			if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
				return unicode.ToLower(r)
			}
			return -1
		}, part)
		if token == "" {
			continue
		}
		token = strings.ReplaceAll(token, `"`, `""`)
		tokens = append(tokens, `"`+token+`"*`)
	}
	return strings.Join(tokens, " AND ")
}

func (s *SQLiteDB) SetCommitXRefGraph(ctx context.Context, repoID int64, commitHash string, defs []models.XRefDefinition, edges []models.XRefEdge) error {
	if strings.TrimSpace(commitHash) == "" {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM xref_edges WHERE repo_id = ? AND commit_hash = ?`,
		repoID, commitHash); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM xref_definitions WHERE repo_id = ? AND commit_hash = ?`,
		repoID, commitHash); err != nil {
		return err
	}

	if len(defs) > 0 {
		stmt, err := tx.PrepareContext(ctx,
			`INSERT INTO xref_definitions
				(repo_id, commit_hash, entity_id, file, package_name, kind, name, signature, receiver, start_line, end_line, callable)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			 ON CONFLICT(repo_id, commit_hash, entity_id) DO UPDATE SET
				 file = excluded.file,
				 package_name = excluded.package_name,
				 kind = excluded.kind,
				 name = excluded.name,
				 signature = excluded.signature,
				 receiver = excluded.receiver,
				 start_line = excluded.start_line,
				 end_line = excluded.end_line,
				 callable = excluded.callable`)
		if err != nil {
			return err
		}
		defer stmt.Close()

		for i := range defs {
			d := defs[i]
			entityID := strings.TrimSpace(d.EntityID)
			if entityID == "" {
				continue
			}
			if _, err := stmt.ExecContext(ctx,
				repoID,
				commitHash,
				entityID,
				d.File,
				d.PackageName,
				d.Kind,
				d.Name,
				d.Signature,
				d.Receiver,
				d.StartLine,
				d.EndLine,
				d.Callable,
			); err != nil {
				return err
			}
		}
	}

	if len(edges) > 0 {
		stmt, err := tx.PrepareContext(ctx,
			`INSERT INTO xref_edges
				(repo_id, commit_hash, source_entity_id, target_entity_id, kind, source_file, source_line, resolution, count)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			 ON CONFLICT(repo_id, commit_hash, source_entity_id, target_entity_id, kind) DO UPDATE SET
				 source_file = excluded.source_file,
				 source_line = excluded.source_line,
				 resolution = excluded.resolution,
				 count = excluded.count`)
		if err != nil {
			return err
		}
		defer stmt.Close()

		for i := range edges {
			e := edges[i]
			sourceID := strings.TrimSpace(e.SourceEntityID)
			targetID := strings.TrimSpace(e.TargetEntityID)
			if sourceID == "" || targetID == "" {
				continue
			}
			kind := strings.TrimSpace(e.Kind)
			if kind == "" {
				kind = "call"
			}
			count := e.Count
			if count <= 0 {
				count = 1
			}
			if _, err := stmt.ExecContext(ctx,
				repoID,
				commitHash,
				sourceID,
				targetID,
				kind,
				e.SourceFile,
				e.SourceLine,
				e.Resolution,
				count,
			); err != nil {
				return err
			}
		}
	}

	return tx.Commit()
}

func (s *SQLiteDB) HasXRefGraphForCommit(ctx context.Context, repoID int64, commitHash string) (bool, error) {
	var one int
	err := s.db.QueryRowContext(ctx,
		`SELECT 1 FROM xref_definitions WHERE repo_id = ? AND commit_hash = ? LIMIT 1`,
		repoID, commitHash).Scan(&one)
	if err == nil {
		return true, nil
	}
	if err != sql.ErrNoRows {
		return false, err
	}

	err = s.db.QueryRowContext(ctx,
		`SELECT 1 FROM xref_edges WHERE repo_id = ? AND commit_hash = ? LIMIT 1`,
		repoID, commitHash).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s *SQLiteDB) FindXRefDefinitionsByName(ctx context.Context, repoID int64, commitHash, name string) ([]models.XRefDefinition, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT repo_id, commit_hash, entity_id, file, package_name, kind, name, signature, receiver, start_line, end_line, callable, created_at
		 FROM xref_definitions
		 WHERE repo_id = ? AND commit_hash = ? AND name = ?
		 ORDER BY file, start_line, entity_id`,
		repoID, commitHash, name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	defs := make([]models.XRefDefinition, 0)
	for rows.Next() {
		var d models.XRefDefinition
		if err := rows.Scan(
			&d.RepoID,
			&d.CommitHash,
			&d.EntityID,
			&d.File,
			&d.PackageName,
			&d.Kind,
			&d.Name,
			&d.Signature,
			&d.Receiver,
			&d.StartLine,
			&d.EndLine,
			&d.Callable,
			&d.CreatedAt,
		); err != nil {
			return nil, err
		}
		defs = append(defs, d)
	}
	return defs, rows.Err()
}

func (s *SQLiteDB) GetXRefDefinition(ctx context.Context, repoID int64, commitHash, entityID string) (*models.XRefDefinition, error) {
	var d models.XRefDefinition
	err := s.db.QueryRowContext(ctx,
		`SELECT repo_id, commit_hash, entity_id, file, package_name, kind, name, signature, receiver, start_line, end_line, callable, created_at
		 FROM xref_definitions
		 WHERE repo_id = ? AND commit_hash = ? AND entity_id = ?`,
		repoID, commitHash, entityID).Scan(
		&d.RepoID,
		&d.CommitHash,
		&d.EntityID,
		&d.File,
		&d.PackageName,
		&d.Kind,
		&d.Name,
		&d.Signature,
		&d.Receiver,
		&d.StartLine,
		&d.EndLine,
		&d.Callable,
		&d.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (s *SQLiteDB) ListXRefEdgesFrom(ctx context.Context, repoID int64, commitHash, sourceEntityID, kind string) ([]models.XRefEdge, error) {
	query := `SELECT repo_id, commit_hash, source_entity_id, target_entity_id, kind, source_file, source_line, resolution, count, created_at
		FROM xref_edges
		WHERE repo_id = ? AND commit_hash = ? AND source_entity_id = ?`
	args := []any{repoID, commitHash, sourceEntityID}
	if strings.TrimSpace(kind) != "" {
		query += ` AND kind = ?`
		args = append(args, kind)
	}
	query += ` ORDER BY target_entity_id, kind`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	edges := make([]models.XRefEdge, 0)
	for rows.Next() {
		var e models.XRefEdge
		if err := rows.Scan(
			&e.RepoID,
			&e.CommitHash,
			&e.SourceEntityID,
			&e.TargetEntityID,
			&e.Kind,
			&e.SourceFile,
			&e.SourceLine,
			&e.Resolution,
			&e.Count,
			&e.CreatedAt,
		); err != nil {
			return nil, err
		}
		edges = append(edges, e)
	}
	return edges, rows.Err()
}

func (s *SQLiteDB) ListXRefEdgesTo(ctx context.Context, repoID int64, commitHash, targetEntityID, kind string) ([]models.XRefEdge, error) {
	query := `SELECT repo_id, commit_hash, source_entity_id, target_entity_id, kind, source_file, source_line, resolution, count, created_at
		FROM xref_edges
		WHERE repo_id = ? AND commit_hash = ? AND target_entity_id = ?`
	args := []any{repoID, commitHash, targetEntityID}
	if strings.TrimSpace(kind) != "" {
		query += ` AND kind = ?`
		args = append(args, kind)
	}
	query += ` ORDER BY source_entity_id, kind`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	edges := make([]models.XRefEdge, 0)
	for rows.Next() {
		var e models.XRefEdge
		if err := rows.Scan(
			&e.RepoID,
			&e.CommitHash,
			&e.SourceEntityID,
			&e.TargetEntityID,
			&e.Kind,
			&e.SourceFile,
			&e.SourceLine,
			&e.Resolution,
			&e.Count,
			&e.CreatedAt,
		); err != nil {
			return nil, err
		}
		edges = append(edges, e)
	}
	return edges, rows.Err()
}

// --- Organizations ---

func (s *SQLiteDB) CreateOrg(ctx context.Context, o *models.Org) error {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO orgs (name, display_name) VALUES (?, ?)`,
		o.Name, o.DisplayName)
	if err != nil {
		return err
	}
	o.ID, _ = res.LastInsertId()
	return nil
}

func (s *SQLiteDB) GetOrg(ctx context.Context, name string) (*models.Org, error) {
	o := &models.Org{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, display_name FROM orgs WHERE name = ?`, name).
		Scan(&o.ID, &o.Name, &o.DisplayName)
	if err != nil {
		return nil, err
	}
	return o, nil
}

func (s *SQLiteDB) GetOrgByID(ctx context.Context, id int64) (*models.Org, error) {
	o := &models.Org{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, display_name FROM orgs WHERE id = ?`, id).
		Scan(&o.ID, &o.Name, &o.DisplayName)
	if err != nil {
		return nil, err
	}
	return o, nil
}

func (s *SQLiteDB) ListUserOrgs(ctx context.Context, userID int64) ([]models.Org, error) {
	return s.ListUserOrgsPage(ctx, userID, 1<<30, 0)
}

func (s *SQLiteDB) ListUserOrgsPage(ctx context.Context, userID int64, limit, offset int) ([]models.Org, error) {
	if limit <= 0 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT o.id, o.name, o.display_name FROM orgs o
		 JOIN org_members om ON om.org_id = o.id
		 WHERE om.user_id = ?
		 ORDER BY o.name ASC
		 LIMIT ? OFFSET ?`, userID, limit, offset)
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

func (s *SQLiteDB) DeleteOrg(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM orgs WHERE id = ?`, id)
	return err
}

func (s *SQLiteDB) AddOrgMember(ctx context.Context, m *models.OrgMember) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO org_members (org_id, user_id, role) VALUES (?, ?, ?)`,
		m.OrgID, m.UserID, m.Role)
	return err
}

func (s *SQLiteDB) GetOrgMember(ctx context.Context, orgID, userID int64) (*models.OrgMember, error) {
	m := &models.OrgMember{}
	err := s.db.QueryRowContext(ctx,
		`SELECT org_id, user_id, role FROM org_members WHERE org_id = ? AND user_id = ?`, orgID, userID).
		Scan(&m.OrgID, &m.UserID, &m.Role)
	if err != nil {
		return nil, err
	}
	return m, nil
}

func (s *SQLiteDB) ListOrgMembers(ctx context.Context, orgID int64) ([]models.OrgMember, error) {
	return s.ListOrgMembersPage(ctx, orgID, 1<<30, 0)
}

func (s *SQLiteDB) ListOrgMembersPage(ctx context.Context, orgID int64, limit, offset int) ([]models.OrgMember, error) {
	if limit <= 0 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT org_id, user_id, role FROM org_members WHERE org_id = ? ORDER BY user_id LIMIT ? OFFSET ?`, orgID, limit, offset)
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

func (s *SQLiteDB) RemoveOrgMember(ctx context.Context, orgID, userID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM org_members WHERE org_id = ? AND user_id = ?`, orgID, userID)
	return err
}

func (s *SQLiteDB) ListOrgRepositories(ctx context.Context, orgID int64) ([]models.Repository, error) {
	return s.ListOrgRepositoriesPage(ctx, orgID, 1<<30, 0)
}

func (s *SQLiteDB) ListOrgRepositoriesPage(ctx context.Context, orgID int64, limit, offset int) ([]models.Repository, error) {
	if limit <= 0 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT r.id, r.owner_user_id, r.owner_org_id, r.parent_repo_id, r.name, r.description, r.default_branch, r.is_private, r.storage_path, r.created_at, o.name
		 FROM repositories r
		 JOIN orgs o ON o.id = r.owner_org_id
		 WHERE r.owner_org_id = ?
		 ORDER BY r.created_at DESC, r.id DESC
		 LIMIT ? OFFSET ?`, orgID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var repos []models.Repository
	for rows.Next() {
		var r models.Repository
		if err := rows.Scan(&r.ID, &r.OwnerUserID, &r.OwnerOrgID, &r.ParentRepoID, &r.Name, &r.Description, &r.DefaultBranch, &r.IsPrivate, &r.StoragePath, &r.CreatedAt, &r.OwnerName); err != nil {
			return nil, err
		}
		repos = append(repos, r)
	}
	return repos, rows.Err()
}

// Compile-time interface check
var _ DB = (*SQLiteDB)(nil)
