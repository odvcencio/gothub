package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

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
	if _, err := p.db.ExecContext(ctx, pgSchema); err != nil {
		return err
	}
	if _, err := p.db.ExecContext(ctx, `ALTER TABLE repositories ADD COLUMN IF NOT EXISTS parent_repo_id BIGINT REFERENCES repositories(id) ON DELETE SET NULL`); err != nil {
		return err
	}
	if _, err := p.db.ExecContext(ctx, `ALTER TABLE pr_comments ADD COLUMN IF NOT EXISTS entity_stable_id TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if _, err := p.db.ExecContext(ctx, `ALTER TABLE branch_protection_rules ADD COLUMN IF NOT EXISTS require_entity_owner_approval BOOLEAN NOT NULL DEFAULT FALSE`); err != nil {
		return err
	}
	if _, err := p.db.ExecContext(ctx, `ALTER TABLE branch_protection_rules ADD COLUMN IF NOT EXISTS require_lint_pass BOOLEAN NOT NULL DEFAULT FALSE`); err != nil {
		return err
	}
	if _, err := p.db.ExecContext(ctx, `ALTER TABLE branch_protection_rules ADD COLUMN IF NOT EXISTS require_no_new_dead_code BOOLEAN NOT NULL DEFAULT FALSE`); err != nil {
		return err
	}
	_, err := p.db.ExecContext(ctx, `ALTER TABLE branch_protection_rules ADD COLUMN IF NOT EXISTS require_signed_commits BOOLEAN NOT NULL DEFAULT FALSE`)
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

CREATE TABLE IF NOT EXISTS magic_link_tokens (
	id BIGSERIAL PRIMARY KEY,
	user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	token_hash TEXT NOT NULL UNIQUE,
	expires_at TIMESTAMPTZ NOT NULL,
	used_at TIMESTAMPTZ,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS ssh_auth_challenges (
	id TEXT PRIMARY KEY,
	user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	fingerprint TEXT NOT NULL,
	challenge TEXT NOT NULL,
	expires_at TIMESTAMPTZ NOT NULL,
	used_at TIMESTAMPTZ,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS webauthn_credentials (
	id BIGSERIAL PRIMARY KEY,
	user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	credential_id TEXT NOT NULL UNIQUE,
	data_json TEXT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	last_used_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS webauthn_sessions (
	id TEXT PRIMARY KEY,
	user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	flow TEXT NOT NULL,
	data_json TEXT NOT NULL,
	expires_at TIMESTAMPTZ NOT NULL,
	used_at TIMESTAMPTZ,
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
	parent_repo_id BIGINT REFERENCES repositories(id) ON DELETE SET NULL,
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

CREATE TABLE IF NOT EXISTS repo_stars (
	repo_id BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
	user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
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
	entity_stable_id TEXT NOT NULL DEFAULT '',
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

CREATE TABLE IF NOT EXISTS issues (
	id BIGSERIAL PRIMARY KEY,
	repo_id BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
	number INTEGER NOT NULL,
	title TEXT NOT NULL,
	body TEXT NOT NULL DEFAULT '',
	state TEXT NOT NULL DEFAULT 'open',
	author_id BIGINT NOT NULL REFERENCES users(id),
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	closed_at TIMESTAMPTZ,
	UNIQUE(repo_id, number)
);

CREATE TABLE IF NOT EXISTS issue_comments (
	id BIGSERIAL PRIMARY KEY,
	issue_id BIGINT NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
	author_id BIGINT NOT NULL REFERENCES users(id),
	body TEXT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS notifications (
	id BIGSERIAL PRIMARY KEY,
	user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	actor_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	type TEXT NOT NULL,
	title TEXT NOT NULL,
	body TEXT NOT NULL DEFAULT '',
	resource_path TEXT NOT NULL DEFAULT '',
	repo_id BIGINT REFERENCES repositories(id) ON DELETE CASCADE,
	pr_id BIGINT REFERENCES pull_requests(id) ON DELETE CASCADE,
	issue_id BIGINT REFERENCES issues(id) ON DELETE CASCADE,
	read_at TIMESTAMPTZ,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS branch_protection_rules (
	id BIGSERIAL PRIMARY KEY,
	repo_id BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
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
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	UNIQUE(repo_id, branch)
);

CREATE TABLE IF NOT EXISTS pr_check_runs (
	id BIGSERIAL PRIMARY KEY,
	pr_id BIGINT NOT NULL REFERENCES pull_requests(id) ON DELETE CASCADE,
	name TEXT NOT NULL,
	status TEXT NOT NULL DEFAULT 'queued',
	conclusion TEXT NOT NULL DEFAULT '',
	details_url TEXT NOT NULL DEFAULT '',
	external_id TEXT NOT NULL DEFAULT '',
	head_commit TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	UNIQUE(pr_id, name)
);

CREATE TABLE IF NOT EXISTS repo_webhooks (
	id BIGSERIAL PRIMARY KEY,
	repo_id BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
	url TEXT NOT NULL,
	secret TEXT NOT NULL DEFAULT '',
	events_csv TEXT NOT NULL DEFAULT '*',
	active BOOLEAN NOT NULL DEFAULT TRUE,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS webhook_deliveries (
	id BIGSERIAL PRIMARY KEY,
	repo_id BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
	webhook_id BIGINT NOT NULL REFERENCES repo_webhooks(id) ON DELETE CASCADE,
	event TEXT NOT NULL,
	delivery_uid TEXT NOT NULL,
	attempt INTEGER NOT NULL DEFAULT 1,
	status_code INTEGER NOT NULL DEFAULT 0,
	success BOOLEAN NOT NULL DEFAULT FALSE,
	error TEXT NOT NULL DEFAULT '',
	request_body TEXT NOT NULL DEFAULT '',
	response_body TEXT NOT NULL DEFAULT '',
	duration_ms BIGINT NOT NULL DEFAULT 0,
	redelivery_of_id BIGINT,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS hash_mapping (
	repo_id BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
	git_hash TEXT NOT NULL,
	got_hash TEXT NOT NULL,
	object_type TEXT NOT NULL,
	PRIMARY KEY (repo_id, git_hash),
	UNIQUE (repo_id, got_hash)
);

CREATE TABLE IF NOT EXISTS merge_base_cache (
	repo_id BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
	left_hash TEXT NOT NULL,
	right_hash TEXT NOT NULL,
	base_hash TEXT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	PRIMARY KEY (repo_id, left_hash, right_hash)
);

CREATE TABLE IF NOT EXISTS indexing_jobs (
	id BIGSERIAL PRIMARY KEY,
	repo_id BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
	commit_hash TEXT NOT NULL,
	job_type TEXT NOT NULL DEFAULT 'commit_index',
	status TEXT NOT NULL DEFAULT 'queued',
	attempt_count INTEGER NOT NULL DEFAULT 0,
	max_attempts INTEGER NOT NULL DEFAULT 3,
	last_error TEXT NOT NULL DEFAULT '',
	next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	started_at TIMESTAMPTZ,
	completed_at TIMESTAMPTZ,
	UNIQUE (repo_id, commit_hash, job_type)
);

CREATE TABLE IF NOT EXISTS commit_indexes (
	repo_id BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
	commit_hash TEXT NOT NULL,
	index_hash TEXT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	PRIMARY KEY (repo_id, commit_hash)
);

CREATE TABLE IF NOT EXISTS git_tree_entry_modes (
	repo_id BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
	got_tree_hash TEXT NOT NULL,
	entry_name TEXT NOT NULL,
	mode TEXT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	PRIMARY KEY (repo_id, got_tree_hash, entry_name)
);

CREATE TABLE IF NOT EXISTS entity_identities (
	repo_id BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
	stable_id TEXT NOT NULL,
	name TEXT NOT NULL,
	decl_kind TEXT NOT NULL DEFAULT '',
	receiver TEXT NOT NULL DEFAULT '',
	first_seen_commit TEXT NOT NULL,
	last_seen_commit TEXT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	PRIMARY KEY (repo_id, stable_id)
);

CREATE TABLE IF NOT EXISTS entity_versions (
	repo_id BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
	stable_id TEXT NOT NULL,
	commit_hash TEXT NOT NULL,
	path TEXT NOT NULL,
	entity_hash TEXT NOT NULL,
	body_hash TEXT NOT NULL DEFAULT '',
	name TEXT NOT NULL,
	decl_kind TEXT NOT NULL DEFAULT '',
	receiver TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	PRIMARY KEY (repo_id, commit_hash, path, entity_hash),
	FOREIGN KEY (repo_id, stable_id) REFERENCES entity_identities(repo_id, stable_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS entity_index_commits (
	repo_id BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
	commit_hash TEXT NOT NULL,
	symbol_count INTEGER NOT NULL DEFAULT 0,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	PRIMARY KEY (repo_id, commit_hash)
);

CREATE TABLE IF NOT EXISTS entity_index (
	id BIGSERIAL PRIMARY KEY,
	repo_id BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
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
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	UNIQUE (repo_id, commit_hash, symbol_key)
);

CREATE TABLE IF NOT EXISTS xref_definitions (
	repo_id BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
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
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	PRIMARY KEY (repo_id, commit_hash, entity_id)
);

CREATE TABLE IF NOT EXISTS xref_edges (
	repo_id BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
	commit_hash TEXT NOT NULL,
	source_entity_id TEXT NOT NULL,
	target_entity_id TEXT NOT NULL,
	kind TEXT NOT NULL DEFAULT 'call',
	source_file TEXT NOT NULL DEFAULT '',
	source_line INTEGER NOT NULL DEFAULT 0,
	resolution TEXT NOT NULL DEFAULT '',
	count INTEGER NOT NULL DEFAULT 1,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	PRIMARY KEY (repo_id, commit_hash, source_entity_id, target_entity_id, kind)
);

CREATE INDEX IF NOT EXISTS idx_hash_mapping_git ON hash_mapping(repo_id, git_hash);
CREATE INDEX IF NOT EXISTS idx_hash_mapping_got ON hash_mapping(repo_id, got_hash);
CREATE INDEX IF NOT EXISTS idx_merge_base_cache_repo ON merge_base_cache(repo_id, updated_at DESC);
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
CREATE INDEX IF NOT EXISTS idx_entity_index_search ON entity_index
	USING GIN (to_tsvector('simple', coalesce(name, '') || ' ' || coalesce(signature, '') || ' ' || coalesce(doc_comment, '')));
CREATE INDEX IF NOT EXISTS idx_xref_definitions_repo_commit_name ON xref_definitions(repo_id, commit_hash, name);
CREATE INDEX IF NOT EXISTS idx_xref_edges_repo_commit_source ON xref_edges(repo_id, commit_hash, source_entity_id, kind);
CREATE INDEX IF NOT EXISTS idx_xref_edges_repo_commit_target ON xref_edges(repo_id, commit_hash, target_entity_id, kind);
CREATE INDEX IF NOT EXISTS idx_branch_protection_repo_branch ON branch_protection_rules(repo_id, branch);
CREATE INDEX IF NOT EXISTS idx_pr_check_runs_pr ON pr_check_runs(pr_id, updated_at DESC);
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

func (p *PostgresDB) UpdateUserPassword(ctx context.Context, userID int64, passwordHash string) error {
	_, err := p.db.ExecContext(ctx, `UPDATE users SET password_hash = $1 WHERE id = $2`, passwordHash, userID)
	return err
}

func (p *PostgresDB) CreateMagicLinkToken(ctx context.Context, token *models.MagicLinkToken) error {
	return p.db.QueryRowContext(ctx,
		`INSERT INTO magic_link_tokens (user_id, token_hash, expires_at) VALUES ($1, $2, $3) RETURNING id, created_at`,
		token.UserID, token.TokenHash, token.ExpiresAt).Scan(&token.ID, &token.CreatedAt)
}

func (p *PostgresDB) ConsumeMagicLinkToken(ctx context.Context, tokenHash string, now time.Time) (*models.User, error) {
	tx, err := p.db.BeginTx(ctx, nil)
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
		 WHERE m.token_hash = $1 AND m.used_at IS NULL AND m.expires_at > $2
		 FOR UPDATE`,
		tokenHash, now).Scan(&tokenID, &u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.IsAdmin, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	res, err := tx.ExecContext(ctx, `UPDATE magic_link_tokens SET used_at = $1 WHERE id = $2 AND used_at IS NULL`, now, tokenID)
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

func (p *PostgresDB) CreateSSHAuthChallenge(ctx context.Context, challenge *models.SSHAuthChallenge) error {
	_, err := p.db.ExecContext(ctx,
		`INSERT INTO ssh_auth_challenges (id, user_id, fingerprint, challenge, expires_at) VALUES ($1, $2, $3, $4, $5)`,
		challenge.ID, challenge.UserID, challenge.Fingerprint, challenge.Challenge, challenge.ExpiresAt)
	return err
}

func (p *PostgresDB) ConsumeSSHAuthChallenge(ctx context.Context, id string, now time.Time) (*models.SSHAuthChallenge, error) {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	ch := &models.SSHAuthChallenge{}
	err = tx.QueryRowContext(ctx,
		`SELECT id, user_id, fingerprint, challenge, expires_at, used_at, created_at
		 FROM ssh_auth_challenges
		 WHERE id = $1 AND used_at IS NULL AND expires_at > $2
		 FOR UPDATE`,
		id, now).
		Scan(&ch.ID, &ch.UserID, &ch.Fingerprint, &ch.Challenge, &ch.ExpiresAt, &ch.UsedAt, &ch.CreatedAt)
	if err != nil {
		return nil, err
	}
	res, err := tx.ExecContext(ctx, `UPDATE ssh_auth_challenges SET used_at = $1 WHERE id = $2 AND used_at IS NULL`, now, id)
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

func (p *PostgresDB) CreateWebAuthnCredential(ctx context.Context, credential *models.WebAuthnCredential) error {
	return p.db.QueryRowContext(ctx,
		`INSERT INTO webauthn_credentials (user_id, credential_id, data_json) VALUES ($1, $2, $3) RETURNING id, created_at`,
		credential.UserID, credential.CredentialID, credential.DataJSON).Scan(&credential.ID, &credential.CreatedAt)
}

func (p *PostgresDB) ListWebAuthnCredentials(ctx context.Context, userID int64) ([]models.WebAuthnCredential, error) {
	rows, err := p.db.QueryContext(ctx,
		`SELECT id, user_id, credential_id, data_json, created_at, last_used_at
		 FROM webauthn_credentials
		 WHERE user_id = $1
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

func (p *PostgresDB) UpdateWebAuthnCredential(ctx context.Context, credential *models.WebAuthnCredential) error {
	_, err := p.db.ExecContext(ctx,
		`UPDATE webauthn_credentials
		 SET data_json = $1, last_used_at = $2
		 WHERE user_id = $3 AND credential_id = $4`,
		credential.DataJSON, credential.LastUsedAt, credential.UserID, credential.CredentialID)
	return err
}

func (p *PostgresDB) CreateWebAuthnSession(ctx context.Context, session *models.WebAuthnSession) error {
	_, err := p.db.ExecContext(ctx,
		`INSERT INTO webauthn_sessions (id, user_id, flow, data_json, expires_at) VALUES ($1, $2, $3, $4, $5)`,
		session.ID, session.UserID, session.Flow, session.DataJSON, session.ExpiresAt)
	return err
}

func (p *PostgresDB) ConsumeWebAuthnSession(ctx context.Context, id, flow string, now time.Time) (*models.WebAuthnSession, error) {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	session := &models.WebAuthnSession{}
	err = tx.QueryRowContext(ctx,
		`SELECT id, user_id, flow, data_json, expires_at, used_at, created_at
		 FROM webauthn_sessions
		 WHERE id = $1 AND flow = $2 AND used_at IS NULL AND expires_at > $3
		 FOR UPDATE`,
		id, flow, now).
		Scan(&session.ID, &session.UserID, &session.Flow, &session.DataJSON, &session.ExpiresAt, &session.UsedAt, &session.CreatedAt)
	if err != nil {
		return nil, err
	}
	res, err := tx.ExecContext(ctx, `UPDATE webauthn_sessions SET used_at = $1 WHERE id = $2 AND used_at IS NULL`, now, id)
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
		`INSERT INTO repositories (owner_user_id, owner_org_id, parent_repo_id, name, description, default_branch, is_private, storage_path)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id, created_at`,
		r.OwnerUserID, r.OwnerOrgID, r.ParentRepoID, r.Name, r.Description, r.DefaultBranch, r.IsPrivate, r.StoragePath).Scan(&r.ID, &r.CreatedAt)
}

func (p *PostgresDB) UpdateRepositoryStoragePath(ctx context.Context, id int64, storagePath string) error {
	_, err := p.db.ExecContext(ctx, `UPDATE repositories SET storage_path = $1 WHERE id = $2`, storagePath, id)
	return err
}

func (p *PostgresDB) CloneRepoMetadata(ctx context.Context, sourceRepoID, targetRepoID int64) error {
	tx, err := p.db.BeginTx(ctx, nil)
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
					SELECT $1, git_hash, got_hash, object_type
					FROM hash_mapping
					WHERE repo_id = $2`,
			args: []any{targetRepoID, sourceRepoID},
		},
		{
			query: `INSERT INTO merge_base_cache (repo_id, left_hash, right_hash, base_hash, created_at, updated_at)
					SELECT $1, left_hash, right_hash, base_hash, created_at, updated_at
					FROM merge_base_cache
					WHERE repo_id = $2`,
			args: []any{targetRepoID, sourceRepoID},
		},
		{
			query: `INSERT INTO commit_indexes (repo_id, commit_hash, index_hash, created_at)
					SELECT $1, commit_hash, index_hash, created_at
					FROM commit_indexes
					WHERE repo_id = $2`,
			args: []any{targetRepoID, sourceRepoID},
		},
		{
			query: `INSERT INTO git_tree_entry_modes (repo_id, got_tree_hash, entry_name, mode, created_at)
					SELECT $1, got_tree_hash, entry_name, mode, created_at
					FROM git_tree_entry_modes
					WHERE repo_id = $2`,
			args: []any{targetRepoID, sourceRepoID},
		},
		{
			query: `INSERT INTO entity_identities (repo_id, stable_id, name, decl_kind, receiver, first_seen_commit, last_seen_commit, created_at, updated_at)
					SELECT $1, stable_id, name, decl_kind, receiver, first_seen_commit, last_seen_commit, created_at, updated_at
					FROM entity_identities
					WHERE repo_id = $2`,
			args: []any{targetRepoID, sourceRepoID},
		},
		{
			query: `INSERT INTO entity_versions (repo_id, stable_id, commit_hash, path, entity_hash, body_hash, name, decl_kind, receiver, created_at)
					SELECT $1, stable_id, commit_hash, path, entity_hash, body_hash, name, decl_kind, receiver, created_at
					FROM entity_versions
					WHERE repo_id = $2`,
			args: []any{targetRepoID, sourceRepoID},
		},
		{
			query: `INSERT INTO entity_index_commits (repo_id, commit_hash, symbol_count, created_at, updated_at)
					SELECT $1, commit_hash, symbol_count, created_at, updated_at
					FROM entity_index_commits
					WHERE repo_id = $2`,
			args: []any{targetRepoID, sourceRepoID},
		},
		{
			query: `INSERT INTO entity_index
						(repo_id, commit_hash, file_path, symbol_key, stable_id, kind, name, signature, receiver, language, doc_comment, start_line, end_line, created_at)
					SELECT $1, commit_hash, file_path, symbol_key, stable_id, kind, name, signature, receiver, language, doc_comment, start_line, end_line, created_at
					FROM entity_index
					WHERE repo_id = $2`,
			args: []any{targetRepoID, sourceRepoID},
		},
		{
			query: `INSERT INTO xref_definitions (repo_id, commit_hash, entity_id, file, package_name, kind, name, signature, receiver, start_line, end_line, callable, created_at)
					SELECT $1, commit_hash, entity_id, file, package_name, kind, name, signature, receiver, start_line, end_line, callable, created_at
					FROM xref_definitions
					WHERE repo_id = $2`,
			args: []any{targetRepoID, sourceRepoID},
		},
		{
			query: `INSERT INTO xref_edges (repo_id, commit_hash, source_entity_id, target_entity_id, kind, source_file, source_line, resolution, count, created_at)
					SELECT $1, commit_hash, source_entity_id, target_entity_id, kind, source_file, source_line, resolution, count, created_at
					FROM xref_edges
					WHERE repo_id = $2`,
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

func (p *PostgresDB) GetRepository(ctx context.Context, ownerName, repoName string) (*models.Repository, error) {
	r := &models.Repository{}
	// Try user-owned first, then org-owned
	err := p.db.QueryRowContext(ctx,
		`SELECT r.id, r.owner_user_id, r.owner_org_id, r.parent_repo_id, r.name, r.description, r.default_branch, r.is_private, r.storage_path, r.created_at,
		 u.username, COALESCE(pu.username, po.name, ''), COALESCE(pr.name, '')
		 FROM repositories r
		 JOIN users u ON u.id = r.owner_user_id
		 LEFT JOIN repositories pr ON pr.id = r.parent_repo_id
		 LEFT JOIN users pu ON pu.id = pr.owner_user_id
		 LEFT JOIN orgs po ON po.id = pr.owner_org_id
		 WHERE u.username = $1 AND r.name = $2`, ownerName, repoName).
		Scan(
			&r.ID, &r.OwnerUserID, &r.OwnerOrgID, &r.ParentRepoID, &r.Name, &r.Description, &r.DefaultBranch, &r.IsPrivate, &r.StoragePath, &r.CreatedAt,
			&r.OwnerName, &r.ParentOwner, &r.ParentName,
		)
	if err == nil {
		return r, nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}
	// Try org-owned
	err = p.db.QueryRowContext(ctx,
		`SELECT r.id, r.owner_user_id, r.owner_org_id, r.parent_repo_id, r.name, r.description, r.default_branch, r.is_private, r.storage_path, r.created_at,
		 o.name, COALESCE(pu.username, po.name, ''), COALESCE(pr.name, '')
		 FROM repositories r
		 JOIN orgs o ON o.id = r.owner_org_id
		 LEFT JOIN repositories pr ON pr.id = r.parent_repo_id
		 LEFT JOIN users pu ON pu.id = pr.owner_user_id
		 LEFT JOIN orgs po ON po.id = pr.owner_org_id
		 WHERE o.name = $1 AND r.name = $2`, ownerName, repoName).
		Scan(
			&r.ID, &r.OwnerUserID, &r.OwnerOrgID, &r.ParentRepoID, &r.Name, &r.Description, &r.DefaultBranch, &r.IsPrivate, &r.StoragePath, &r.CreatedAt,
			&r.OwnerName, &r.ParentOwner, &r.ParentName,
		)
	if err != nil {
		return nil, err
	}
	return r, nil
}

func (p *PostgresDB) GetRepositoryByID(ctx context.Context, id int64) (*models.Repository, error) {
	r := &models.Repository{}
	err := p.db.QueryRowContext(ctx,
		`SELECT r.id, r.owner_user_id, r.owner_org_id, r.parent_repo_id, r.name, r.description, r.default_branch, r.is_private, r.storage_path, r.created_at,
		 COALESCE(u.username, o.name, ''), COALESCE(pu.username, po.name, ''), COALESCE(pr.name, '')
		 FROM repositories r
		 LEFT JOIN users u ON u.id = r.owner_user_id
		 LEFT JOIN orgs o ON o.id = r.owner_org_id
		 LEFT JOIN repositories pr ON pr.id = r.parent_repo_id
		 LEFT JOIN users pu ON pu.id = pr.owner_user_id
		 LEFT JOIN orgs po ON po.id = pr.owner_org_id
		 WHERE r.id = $1`, id).
		Scan(
			&r.ID, &r.OwnerUserID, &r.OwnerOrgID, &r.ParentRepoID, &r.Name, &r.Description, &r.DefaultBranch, &r.IsPrivate, &r.StoragePath, &r.CreatedAt,
			&r.OwnerName, &r.ParentOwner, &r.ParentName,
		)
	if err != nil {
		return nil, err
	}
	return r, nil
}

func (p *PostgresDB) ListUserRepositories(ctx context.Context, userID int64) ([]models.Repository, error) {
	return p.ListUserRepositoriesPage(ctx, userID, 1<<30, 0)
}

func (p *PostgresDB) ListUserRepositoriesPage(ctx context.Context, userID int64, limit, offset int) ([]models.Repository, error) {
	if limit <= 0 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := p.db.QueryContext(ctx,
		`SELECT r.id, r.owner_user_id, r.owner_org_id, r.parent_repo_id, r.name, r.description, r.default_branch, r.is_private, r.storage_path, r.created_at,
		 COALESCE(u.username, o.name, ''), COALESCE(pu.username, po.name, ''), COALESCE(pr.name, '')
		 FROM repositories r
		 LEFT JOIN users u ON u.id = r.owner_user_id
		 LEFT JOIN orgs o ON o.id = r.owner_org_id
		 LEFT JOIN repositories pr ON pr.id = r.parent_repo_id
		 LEFT JOIN users pu ON pu.id = pr.owner_user_id
		 LEFT JOIN orgs po ON po.id = pr.owner_org_id
		 WHERE r.owner_user_id = $1
		 UNION
		 SELECT r.id, r.owner_user_id, r.owner_org_id, r.parent_repo_id, r.name, r.description, r.default_branch, r.is_private, r.storage_path, r.created_at,
		 o.name, COALESCE(pu.username, po.name, ''), COALESCE(pr.name, '')
		 FROM repositories r
		 JOIN orgs o ON o.id = r.owner_org_id
		 JOIN org_members om ON om.org_id = o.id AND om.user_id = $1
		 LEFT JOIN repositories pr ON pr.id = r.parent_repo_id
		 LEFT JOIN users pu ON pu.id = pr.owner_user_id
		 LEFT JOIN orgs po ON po.id = pr.owner_org_id
		 ORDER BY created_at DESC
		 LIMIT $2 OFFSET $3`, userID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var repos []models.Repository
	for rows.Next() {
		var r models.Repository
		if err := rows.Scan(
			&r.ID, &r.OwnerUserID, &r.OwnerOrgID, &r.ParentRepoID, &r.Name, &r.Description, &r.DefaultBranch, &r.IsPrivate, &r.StoragePath, &r.CreatedAt,
			&r.OwnerName, &r.ParentOwner, &r.ParentName,
		); err != nil {
			return nil, err
		}
		repos = append(repos, r)
	}
	return repos, rows.Err()
}

func (p *PostgresDB) ListRepositoryForks(ctx context.Context, parentRepoID int64) ([]models.Repository, error) {
	return p.ListRepositoryForksPage(ctx, parentRepoID, 1<<30, 0)
}

func (p *PostgresDB) ListRepositoryForksPage(ctx context.Context, parentRepoID int64, limit, offset int) ([]models.Repository, error) {
	if limit <= 0 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := p.db.QueryContext(ctx,
		`SELECT r.id, r.owner_user_id, r.owner_org_id, r.parent_repo_id, r.name, r.description, r.default_branch, r.is_private, r.storage_path, r.created_at,
		 COALESCE(u.username, o.name, ''), COALESCE(pu.username, po.name, ''), COALESCE(pr.name, '')
		 FROM repositories r
		 LEFT JOIN users u ON u.id = r.owner_user_id
		 LEFT JOIN orgs o ON o.id = r.owner_org_id
		 LEFT JOIN repositories pr ON pr.id = r.parent_repo_id
		 LEFT JOIN users pu ON pu.id = pr.owner_user_id
		 LEFT JOIN orgs po ON po.id = pr.owner_org_id
		 WHERE r.parent_repo_id = $1
		 ORDER BY r.created_at DESC, r.id DESC
		 LIMIT $2 OFFSET $3`, parentRepoID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var repos []models.Repository
	for rows.Next() {
		var r models.Repository
		if err := rows.Scan(
			&r.ID, &r.OwnerUserID, &r.OwnerOrgID, &r.ParentRepoID, &r.Name, &r.Description, &r.DefaultBranch, &r.IsPrivate, &r.StoragePath, &r.CreatedAt,
			&r.OwnerName, &r.ParentOwner, &r.ParentName,
		); err != nil {
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

// --- Stars ---

func (p *PostgresDB) AddRepoStar(ctx context.Context, repoID, userID int64) error {
	_, err := p.db.ExecContext(ctx,
		`INSERT INTO repo_stars (repo_id, user_id) VALUES ($1, $2)
		 ON CONFLICT (repo_id, user_id) DO NOTHING`,
		repoID, userID)
	return err
}

func (p *PostgresDB) RemoveRepoStar(ctx context.Context, repoID, userID int64) error {
	_, err := p.db.ExecContext(ctx,
		`DELETE FROM repo_stars WHERE repo_id = $1 AND user_id = $2`,
		repoID, userID)
	return err
}

func (p *PostgresDB) IsRepoStarred(ctx context.Context, repoID, userID int64) (bool, error) {
	var exists bool
	if err := p.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM repo_stars WHERE repo_id = $1 AND user_id = $2)`,
		repoID, userID).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

func (p *PostgresDB) CountRepoStars(ctx context.Context, repoID int64) (int, error) {
	var count int
	if err := p.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM repo_stars WHERE repo_id = $1`,
		repoID).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func (p *PostgresDB) ListRepoStargazers(ctx context.Context, repoID int64) ([]models.User, error) {
	return p.ListRepoStargazersPage(ctx, repoID, 1<<30, 0)
}

func (p *PostgresDB) ListRepoStargazersPage(ctx context.Context, repoID int64, limit, offset int) ([]models.User, error) {
	if limit <= 0 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := p.db.QueryContext(ctx,
		`SELECT u.id, u.username, u.email, u.password_hash, u.is_admin, u.created_at
		 FROM repo_stars rs
		 JOIN users u ON u.id = rs.user_id
		 WHERE rs.repo_id = $1
		 ORDER BY rs.created_at DESC, u.id DESC
		 LIMIT $2 OFFSET $3`,
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

func (p *PostgresDB) ListUserStarredRepositories(ctx context.Context, userID int64) ([]models.Repository, error) {
	return p.ListUserStarredRepositoriesPage(ctx, userID, 1<<30, 0)
}

func (p *PostgresDB) ListUserStarredRepositoriesPage(ctx context.Context, userID int64, limit, offset int) ([]models.Repository, error) {
	if limit <= 0 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := p.db.QueryContext(ctx,
		`SELECT r.id, r.owner_user_id, r.owner_org_id, r.parent_repo_id, r.name, r.description, r.default_branch, r.is_private, r.storage_path, r.created_at,
		 COALESCE(u.username, o.name, '')
		 FROM repo_stars rs
		 JOIN repositories r ON r.id = rs.repo_id
		 LEFT JOIN users u ON u.id = r.owner_user_id
		 LEFT JOIN orgs o ON o.id = r.owner_org_id
		 WHERE rs.user_id = $1
		 ORDER BY rs.created_at DESC, r.id DESC
		 LIMIT $2 OFFSET $3`,
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
	return p.ListCollaboratorsPage(ctx, repoID, 1<<30, 0)
}

func (p *PostgresDB) ListCollaboratorsPage(ctx context.Context, repoID int64, limit, offset int) ([]models.Collaborator, error) {
	if limit <= 0 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := p.db.QueryContext(ctx,
		`SELECT repo_id, user_id, role FROM collaborators WHERE repo_id = $1 ORDER BY user_id LIMIT $2 OFFSET $3`, repoID, limit, offset)
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
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Lock repository row to serialize PR number assignment per repository.
	var repoID int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM repositories WHERE id = $1 FOR UPDATE`, pr.RepoID).Scan(&repoID); err != nil {
		return err
	}

	var maxNum int
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(number), 0) FROM pull_requests WHERE repo_id = $1`, pr.RepoID).Scan(&maxNum); err != nil {
		return err
	}
	pr.Number = maxNum + 1

	if err := tx.QueryRowContext(ctx,
		`INSERT INTO pull_requests (repo_id, number, title, body, state, author_id, source_branch, target_branch, source_commit, target_commit)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING id, created_at`,
		pr.RepoID, pr.Number, pr.Title, pr.Body, pr.State, pr.AuthorID, pr.SourceBranch, pr.TargetBranch, pr.SourceCommit, pr.TargetCommit).
		Scan(&pr.ID, &pr.CreatedAt); err != nil {
		return err
	}

	return tx.Commit()
}

func (p *PostgresDB) GetPullRequest(ctx context.Context, repoID int64, number int) (*models.PullRequest, error) {
	pr := &models.PullRequest{}
	err := p.db.QueryRowContext(ctx,
		`SELECT pr.id, pr.repo_id, pr.number, pr.title, pr.body, pr.state, pr.author_id, u.username,
		        pr.source_branch, pr.target_branch, pr.source_commit, pr.target_commit, pr.merge_commit, pr.merge_method, pr.created_at, pr.merged_at
		 FROM pull_requests pr
		 JOIN users u ON u.id = pr.author_id
		 WHERE pr.repo_id = $1 AND pr.number = $2`, repoID, number).
		Scan(&pr.ID, &pr.RepoID, &pr.Number, &pr.Title, &pr.Body, &pr.State, &pr.AuthorID, &pr.AuthorName,
			&pr.SourceBranch, &pr.TargetBranch, &pr.SourceCommit, &pr.TargetCommit,
			&pr.MergeCommit, &pr.MergeMethod, &pr.CreatedAt, &pr.MergedAt)
	if err != nil {
		return nil, err
	}
	return pr, nil
}

func (p *PostgresDB) ListPullRequests(ctx context.Context, repoID int64, state string) ([]models.PullRequest, error) {
	return p.ListPullRequestsPage(ctx, repoID, state, 1<<30, 0)
}

func (p *PostgresDB) ListPullRequestsPage(ctx context.Context, repoID int64, state string, limit, offset int) ([]models.PullRequest, error) {
	query := `SELECT pr.id, pr.repo_id, pr.number, pr.title, pr.body, pr.state, pr.author_id, u.username,
	         pr.source_branch, pr.target_branch, pr.source_commit, pr.target_commit, pr.merge_commit, pr.merge_method, pr.created_at, pr.merged_at
		 FROM pull_requests pr
		 JOIN users u ON u.id = pr.author_id
		 WHERE pr.repo_id = $1`
	args := []any{repoID}
	argPos := 2
	if state != "" {
		query += fmt.Sprintf(" AND pr.state = $%d", argPos)
		args = append(args, state)
		argPos++
	}
	if limit <= 0 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	query += fmt.Sprintf(" ORDER BY pr.number DESC LIMIT $%d OFFSET $%d", argPos, argPos+1)
	args = append(args, limit, offset)

	rows, err := p.db.QueryContext(ctx, query, args...)
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
		`INSERT INTO pr_comments (pr_id, author_id, body, file_path, entity_key, entity_stable_id, line_number, commit_hash)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id, created_at`,
		c.PRID, c.AuthorID, c.Body, c.FilePath, c.EntityKey, c.EntityStableID, c.LineNumber, c.CommitHash).Scan(&c.ID, &c.CreatedAt)
}

func (p *PostgresDB) ListPRComments(ctx context.Context, prID int64) ([]models.PRComment, error) {
	return p.ListPRCommentsPage(ctx, prID, 1<<30, 0)
}

func (p *PostgresDB) ListPRCommentsPage(ctx context.Context, prID int64, limit, offset int) ([]models.PRComment, error) {
	if limit <= 0 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := p.db.QueryContext(ctx,
		`SELECT c.id, c.pr_id, c.author_id, u.username, c.body, c.file_path, c.entity_key, c.entity_stable_id, c.line_number, c.commit_hash, c.created_at
		 FROM pr_comments c
		 JOIN users u ON u.id = c.author_id
		 WHERE c.pr_id = $1
		 ORDER BY c.created_at
		 LIMIT $2 OFFSET $3`, prID, limit, offset)
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

func (p *PostgresDB) DeletePRComment(ctx context.Context, commentID, authorID int64) error {
	res, err := p.db.ExecContext(ctx, `DELETE FROM pr_comments WHERE id = $1 AND author_id = $2`, commentID, authorID)
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

func (p *PostgresDB) CreatePRReview(ctx context.Context, r *models.PRReview) error {
	return p.db.QueryRowContext(ctx,
		`INSERT INTO pr_reviews (pr_id, author_id, state, body, commit_hash) VALUES ($1, $2, $3, $4, $5) RETURNING id, created_at`,
		r.PRID, r.AuthorID, r.State, r.Body, r.CommitHash).Scan(&r.ID, &r.CreatedAt)
}

func (p *PostgresDB) ListPRReviews(ctx context.Context, prID int64) ([]models.PRReview, error) {
	return p.ListPRReviewsPage(ctx, prID, 1<<30, 0)
}

func (p *PostgresDB) ListPRReviewsPage(ctx context.Context, prID int64, limit, offset int) ([]models.PRReview, error) {
	if limit <= 0 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := p.db.QueryContext(ctx,
		`SELECT r.id, r.pr_id, r.author_id, u.username, r.state, r.body, r.commit_hash, r.created_at
		 FROM pr_reviews r
		 JOIN users u ON u.id = r.author_id
		 WHERE r.pr_id = $1
		 ORDER BY r.created_at
		 LIMIT $2 OFFSET $3`, prID, limit, offset)
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

func (p *PostgresDB) CreateIssue(ctx context.Context, issue *models.Issue) error {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var repoID int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM repositories WHERE id = $1 FOR UPDATE`, issue.RepoID).Scan(&repoID); err != nil {
		return err
	}

	var maxNum int
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(number), 0) FROM issues WHERE repo_id = $1`, issue.RepoID).Scan(&maxNum); err != nil {
		return err
	}
	issue.Number = maxNum + 1

	if err := tx.QueryRowContext(ctx,
		`INSERT INTO issues (repo_id, number, title, body, state, author_id)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id, created_at`,
		issue.RepoID, issue.Number, issue.Title, issue.Body, issue.State, issue.AuthorID).
		Scan(&issue.ID, &issue.CreatedAt); err != nil {
		return err
	}

	return tx.Commit()
}

func (p *PostgresDB) GetIssue(ctx context.Context, repoID int64, number int) (*models.Issue, error) {
	issue := &models.Issue{}
	err := p.db.QueryRowContext(ctx,
		`SELECT i.id, i.repo_id, i.number, i.title, i.body, i.state, i.author_id, u.username, i.created_at, i.closed_at
		 FROM issues i
		 JOIN users u ON u.id = i.author_id
		 WHERE i.repo_id = $1 AND i.number = $2`, repoID, number).
		Scan(&issue.ID, &issue.RepoID, &issue.Number, &issue.Title, &issue.Body, &issue.State, &issue.AuthorID, &issue.AuthorName, &issue.CreatedAt, &issue.ClosedAt)
	if err != nil {
		return nil, err
	}
	return issue, nil
}

func (p *PostgresDB) ListIssues(ctx context.Context, repoID int64, state string) ([]models.Issue, error) {
	return p.ListIssuesPage(ctx, repoID, state, 1<<30, 0)
}

func (p *PostgresDB) ListIssuesPage(ctx context.Context, repoID int64, state string, limit, offset int) ([]models.Issue, error) {
	query := `SELECT i.id, i.repo_id, i.number, i.title, i.body, i.state, i.author_id, u.username, i.created_at, i.closed_at
		 FROM issues i
		 JOIN users u ON u.id = i.author_id
		 WHERE i.repo_id = $1`
	args := []any{repoID}
	argPos := 2
	if state != "" {
		query += fmt.Sprintf(" AND i.state = $%d", argPos)
		args = append(args, state)
		argPos++
	}
	if limit <= 0 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	query += fmt.Sprintf(" ORDER BY i.number DESC LIMIT $%d OFFSET $%d", argPos, argPos+1)
	args = append(args, limit, offset)

	rows, err := p.db.QueryContext(ctx, query, args...)
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

func (p *PostgresDB) UpdateIssue(ctx context.Context, issue *models.Issue) error {
	_, err := p.db.ExecContext(ctx,
		`UPDATE issues SET title = $1, body = $2, state = $3, closed_at = $4 WHERE id = $5`,
		issue.Title, issue.Body, issue.State, issue.ClosedAt, issue.ID)
	return err
}

func (p *PostgresDB) CreateIssueComment(ctx context.Context, c *models.IssueComment) error {
	return p.db.QueryRowContext(ctx,
		`INSERT INTO issue_comments (issue_id, author_id, body) VALUES ($1, $2, $3) RETURNING id, created_at`,
		c.IssueID, c.AuthorID, c.Body).Scan(&c.ID, &c.CreatedAt)
}

func (p *PostgresDB) ListIssueComments(ctx context.Context, issueID int64) ([]models.IssueComment, error) {
	return p.ListIssueCommentsPage(ctx, issueID, 1<<30, 0)
}

func (p *PostgresDB) ListIssueCommentsPage(ctx context.Context, issueID int64, limit, offset int) ([]models.IssueComment, error) {
	if limit <= 0 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := p.db.QueryContext(ctx,
		`SELECT c.id, c.issue_id, c.author_id, u.username, c.body, c.created_at
		 FROM issue_comments c
		 JOIN users u ON u.id = c.author_id
		 WHERE c.issue_id = $1
		 ORDER BY c.created_at
		 LIMIT $2 OFFSET $3`, issueID, limit, offset)
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

func (p *PostgresDB) DeleteIssueComment(ctx context.Context, commentID, authorID int64) error {
	res, err := p.db.ExecContext(ctx, `DELETE FROM issue_comments WHERE id = $1 AND author_id = $2`, commentID, authorID)
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

func (p *PostgresDB) CreateNotification(ctx context.Context, n *models.Notification) error {
	return p.db.QueryRowContext(ctx,
		`INSERT INTO notifications (
			 user_id, actor_id, type, title, body, resource_path, repo_id, pr_id, issue_id, read_at
		 ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		 RETURNING id, created_at`,
		n.UserID, n.ActorID, n.Type, n.Title, n.Body, n.ResourcePath, n.RepoID, n.PRID, n.IssueID, n.ReadAt).
		Scan(&n.ID, &n.CreatedAt)
}

func (p *PostgresDB) ListNotifications(ctx context.Context, userID int64, unreadOnly bool) ([]models.Notification, error) {
	return p.ListNotificationsPage(ctx, userID, unreadOnly, 1<<30, 0)
}

func (p *PostgresDB) ListNotificationsPage(ctx context.Context, userID int64, unreadOnly bool, limit, offset int) ([]models.Notification, error) {
	query := `SELECT n.id, n.user_id, n.actor_id, a.username, n.type, n.title, n.body, n.resource_path, n.repo_id, n.pr_id, n.issue_id, n.read_at, n.created_at
		 FROM notifications n
		 JOIN users a ON a.id = n.actor_id
		 WHERE n.user_id = $1`
	args := []any{userID}
	argPos := 2
	if unreadOnly {
		query += ` AND n.read_at IS NULL`
	}
	if limit <= 0 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	query += fmt.Sprintf(" ORDER BY n.created_at DESC, n.id DESC LIMIT $%d OFFSET $%d", argPos, argPos+1)
	args = append(args, limit, offset)

	rows, err := p.db.QueryContext(ctx, query, args...)
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

func (p *PostgresDB) CountUnreadNotifications(ctx context.Context, userID int64) (int, error) {
	var count int
	if err := p.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM notifications WHERE user_id = $1 AND read_at IS NULL`,
		userID).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func (p *PostgresDB) MarkNotificationRead(ctx context.Context, id, userID int64) error {
	_, err := p.db.ExecContext(ctx,
		`UPDATE notifications SET read_at = NOW() WHERE id = $1 AND user_id = $2 AND read_at IS NULL`,
		id, userID)
	return err
}

func (p *PostgresDB) MarkAllNotificationsRead(ctx context.Context, userID int64) error {
	_, err := p.db.ExecContext(ctx,
		`UPDATE notifications SET read_at = NOW() WHERE user_id = $1 AND read_at IS NULL`,
		userID)
	return err
}

// --- Branch Protection ---

func (p *PostgresDB) UpsertBranchProtectionRule(ctx context.Context, rule *models.BranchProtectionRule) error {
	if rule.RequiredApprovals <= 0 {
		rule.RequiredApprovals = 1
	}
	return p.db.QueryRowContext(ctx,
		`INSERT INTO branch_protection_rules (
			 repo_id, branch, enabled, require_approvals, required_approvals, require_status_checks, require_entity_owner_approval, require_lint_pass, require_no_new_dead_code, require_signed_commits, required_checks_csv
		 ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		 ON CONFLICT(repo_id, branch) DO UPDATE SET
			 enabled = EXCLUDED.enabled,
			 require_approvals = EXCLUDED.require_approvals,
			 required_approvals = EXCLUDED.required_approvals,
			 require_status_checks = EXCLUDED.require_status_checks,
			 require_entity_owner_approval = EXCLUDED.require_entity_owner_approval,
			 require_lint_pass = EXCLUDED.require_lint_pass,
			 require_no_new_dead_code = EXCLUDED.require_no_new_dead_code,
			 require_signed_commits = EXCLUDED.require_signed_commits,
			 required_checks_csv = EXCLUDED.required_checks_csv,
			 updated_at = NOW()
		 RETURNING id, repo_id, branch, enabled, require_approvals, required_approvals, require_status_checks, require_entity_owner_approval, require_lint_pass, require_no_new_dead_code, require_signed_commits, required_checks_csv, created_at, updated_at`,
		rule.RepoID, rule.Branch, rule.Enabled, rule.RequireApprovals, rule.RequiredApprovals, rule.RequireStatusChecks, rule.RequireEntityOwnerApproval, rule.RequireLintPass, rule.RequireNoNewDeadCode, rule.RequireSignedCommits, rule.RequiredChecksCSV).
		Scan(&rule.ID, &rule.RepoID, &rule.Branch, &rule.Enabled, &rule.RequireApprovals, &rule.RequiredApprovals,
			&rule.RequireStatusChecks, &rule.RequireEntityOwnerApproval, &rule.RequireLintPass, &rule.RequireNoNewDeadCode, &rule.RequireSignedCommits, &rule.RequiredChecksCSV, &rule.CreatedAt, &rule.UpdatedAt)
}

func (p *PostgresDB) GetBranchProtectionRule(ctx context.Context, repoID int64, branch string) (*models.BranchProtectionRule, error) {
	rule := &models.BranchProtectionRule{}
	err := p.db.QueryRowContext(ctx,
		`SELECT id, repo_id, branch, enabled, require_approvals, required_approvals, require_status_checks, require_entity_owner_approval, require_lint_pass, require_no_new_dead_code, require_signed_commits, required_checks_csv, created_at, updated_at
		 FROM branch_protection_rules
		 WHERE repo_id = $1 AND branch = $2`,
		repoID, branch).
		Scan(&rule.ID, &rule.RepoID, &rule.Branch, &rule.Enabled, &rule.RequireApprovals, &rule.RequiredApprovals,
			&rule.RequireStatusChecks, &rule.RequireEntityOwnerApproval, &rule.RequireLintPass, &rule.RequireNoNewDeadCode, &rule.RequireSignedCommits, &rule.RequiredChecksCSV, &rule.CreatedAt, &rule.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return rule, nil
}

func (p *PostgresDB) DeleteBranchProtectionRule(ctx context.Context, repoID int64, branch string) error {
	_, err := p.db.ExecContext(ctx,
		`DELETE FROM branch_protection_rules WHERE repo_id = $1 AND branch = $2`, repoID, branch)
	return err
}

// --- PR Check Runs ---

func (p *PostgresDB) UpsertPRCheckRun(ctx context.Context, run *models.PRCheckRun) error {
	return p.db.QueryRowContext(ctx,
		`INSERT INTO pr_check_runs (
			 pr_id, name, status, conclusion, details_url, external_id, head_commit
		 ) VALUES ($1, $2, $3, $4, $5, $6, $7)
		 ON CONFLICT(pr_id, name) DO UPDATE SET
			 status = EXCLUDED.status,
			 conclusion = EXCLUDED.conclusion,
			 details_url = EXCLUDED.details_url,
			 external_id = EXCLUDED.external_id,
			 head_commit = EXCLUDED.head_commit,
			 updated_at = NOW()
		 RETURNING id, pr_id, name, status, conclusion, details_url, external_id, head_commit, created_at, updated_at`,
		run.PRID, run.Name, run.Status, run.Conclusion, run.DetailsURL, run.ExternalID, run.HeadCommit).
		Scan(&run.ID, &run.PRID, &run.Name, &run.Status, &run.Conclusion, &run.DetailsURL, &run.ExternalID, &run.HeadCommit, &run.CreatedAt, &run.UpdatedAt)
}

func (p *PostgresDB) ListPRCheckRuns(ctx context.Context, prID int64) ([]models.PRCheckRun, error) {
	return p.ListPRCheckRunsPage(ctx, prID, 1<<30, 0)
}

func (p *PostgresDB) ListPRCheckRunsPage(ctx context.Context, prID int64, limit, offset int) ([]models.PRCheckRun, error) {
	if limit <= 0 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := p.db.QueryContext(ctx,
		`SELECT id, pr_id, name, status, conclusion, details_url, external_id, head_commit, created_at, updated_at
		 FROM pr_check_runs
		 WHERE pr_id = $1
		 ORDER BY updated_at DESC, id DESC
		 LIMIT $2 OFFSET $3`, prID, limit, offset)
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

func (p *PostgresDB) CreateWebhook(ctx context.Context, hook *models.Webhook) error {
	return p.db.QueryRowContext(ctx,
		`INSERT INTO repo_webhooks (repo_id, url, secret, events_csv, active)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id, created_at, updated_at`,
		hook.RepoID, hook.URL, hook.Secret, hook.EventsCSV, hook.Active).
		Scan(&hook.ID, &hook.CreatedAt, &hook.UpdatedAt)
}

func (p *PostgresDB) GetWebhook(ctx context.Context, repoID, webhookID int64) (*models.Webhook, error) {
	hook := &models.Webhook{}
	err := p.db.QueryRowContext(ctx,
		`SELECT id, repo_id, url, secret, events_csv, active, created_at, updated_at
		 FROM repo_webhooks
		 WHERE repo_id = $1 AND id = $2`, repoID, webhookID).
		Scan(&hook.ID, &hook.RepoID, &hook.URL, &hook.Secret, &hook.EventsCSV, &hook.Active, &hook.CreatedAt, &hook.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return hook, nil
}

func (p *PostgresDB) ListWebhooks(ctx context.Context, repoID int64) ([]models.Webhook, error) {
	return p.ListWebhooksPage(ctx, repoID, 1<<30, 0)
}

func (p *PostgresDB) ListWebhooksPage(ctx context.Context, repoID int64, limit, offset int) ([]models.Webhook, error) {
	if limit <= 0 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := p.db.QueryContext(ctx,
		`SELECT id, repo_id, url, secret, events_csv, active, created_at, updated_at
		 FROM repo_webhooks
		 WHERE repo_id = $1
		 ORDER BY id DESC
		 LIMIT $2 OFFSET $3`, repoID, limit, offset)
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

func (p *PostgresDB) DeleteWebhook(ctx context.Context, repoID, webhookID int64) error {
	_, err := p.db.ExecContext(ctx,
		`DELETE FROM repo_webhooks WHERE repo_id = $1 AND id = $2`, repoID, webhookID)
	return err
}

func (p *PostgresDB) CreateWebhookDelivery(ctx context.Context, delivery *models.WebhookDelivery) error {
	return p.db.QueryRowContext(ctx,
		`INSERT INTO webhook_deliveries (
			 repo_id, webhook_id, event, delivery_uid, attempt, status_code, success, error, request_body, response_body, duration_ms, redelivery_of_id
		 ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		 RETURNING id, created_at`,
		delivery.RepoID, delivery.WebhookID, delivery.Event, delivery.DeliveryUID, delivery.Attempt, delivery.StatusCode,
		delivery.Success, delivery.Error, delivery.RequestBody, delivery.ResponseBody, delivery.DurationMS, delivery.RedeliveryOfID).
		Scan(&delivery.ID, &delivery.CreatedAt)
}

func (p *PostgresDB) GetWebhookDelivery(ctx context.Context, repoID, webhookID, deliveryID int64) (*models.WebhookDelivery, error) {
	d := &models.WebhookDelivery{}
	err := p.db.QueryRowContext(ctx,
		`SELECT id, repo_id, webhook_id, event, delivery_uid, attempt, status_code, success, error, request_body, response_body, duration_ms, redelivery_of_id, created_at
		 FROM webhook_deliveries
		 WHERE repo_id = $1 AND webhook_id = $2 AND id = $3`,
		repoID, webhookID, deliveryID).
		Scan(&d.ID, &d.RepoID, &d.WebhookID, &d.Event, &d.DeliveryUID, &d.Attempt, &d.StatusCode, &d.Success, &d.Error,
			&d.RequestBody, &d.ResponseBody, &d.DurationMS, &d.RedeliveryOfID, &d.CreatedAt)
	if err != nil {
		return nil, err
	}
	return d, nil
}

func (p *PostgresDB) ListWebhookDeliveries(ctx context.Context, repoID, webhookID int64) ([]models.WebhookDelivery, error) {
	return p.ListWebhookDeliveriesPage(ctx, repoID, webhookID, 1<<30, 0)
}

func (p *PostgresDB) ListWebhookDeliveriesPage(ctx context.Context, repoID, webhookID int64, limit, offset int) ([]models.WebhookDelivery, error) {
	if limit <= 0 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := p.db.QueryContext(ctx,
		`SELECT id, repo_id, webhook_id, event, delivery_uid, attempt, status_code, success, error, request_body, response_body, duration_ms, redelivery_of_id, created_at
		 FROM webhook_deliveries
		 WHERE repo_id = $1 AND webhook_id = $2
		 ORDER BY id DESC
		 LIMIT $3 OFFSET $4`, repoID, webhookID, limit, offset)
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

func (p *PostgresDB) SetHashMapping(ctx context.Context, m *models.HashMapping) error {
	return p.SetHashMappings(ctx, []models.HashMapping{*m})
}

func (p *PostgresDB) SetHashMappings(ctx context.Context, mappings []models.HashMapping) error {
	if len(mappings) == 0 {
		return nil
	}
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO hash_mapping (repo_id, got_hash, git_hash, object_type) VALUES ($1, $2, $3, $4)
		 ON CONFLICT (repo_id, git_hash) DO UPDATE SET
			 got_hash = EXCLUDED.got_hash,
			 object_type = EXCLUDED.object_type`)
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

func (p *PostgresDB) SetMergeBaseCache(ctx context.Context, repoID int64, leftHash, rightHash, baseHash string) error {
	leftHash, rightHash = normalizeMergeBasePair(leftHash, rightHash)
	baseHash = strings.TrimSpace(baseHash)
	if leftHash == "" || rightHash == "" || baseHash == "" {
		return fmt.Errorf("merge-base cache requires non-empty hashes")
	}
	_, err := p.db.ExecContext(ctx,
		`INSERT INTO merge_base_cache (repo_id, left_hash, right_hash, base_hash)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT(repo_id, left_hash, right_hash) DO UPDATE SET
			 base_hash = EXCLUDED.base_hash,
			 updated_at = NOW()`,
		repoID, leftHash, rightHash, baseHash,
	)
	return err
}

func (p *PostgresDB) GetMergeBaseCache(ctx context.Context, repoID int64, leftHash, rightHash string) (string, bool, error) {
	leftHash, rightHash = normalizeMergeBasePair(leftHash, rightHash)
	if leftHash == "" || rightHash == "" {
		return "", false, nil
	}
	var baseHash string
	err := p.db.QueryRowContext(ctx,
		`SELECT base_hash FROM merge_base_cache WHERE repo_id = $1 AND left_hash = $2 AND right_hash = $3`,
		repoID, leftHash, rightHash,
	).Scan(&baseHash)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return baseHash, true, nil
}

func (p *PostgresDB) EnqueueIndexingJob(ctx context.Context, job *models.IndexingJob) error {
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

	row := p.db.QueryRowContext(ctx,
		`INSERT INTO indexing_jobs (
			 repo_id, commit_hash, job_type, status, attempt_count, max_attempts, last_error, next_attempt_at
		 ) VALUES ($1, $2, $3, $4, 0, $5, '', $6)
		 ON CONFLICT (repo_id, commit_hash, job_type) DO UPDATE SET
			 status = CASE
				WHEN indexing_jobs.status = $7 THEN indexing_jobs.status
				ELSE $8
			 END,
			 last_error = CASE
				WHEN indexing_jobs.status = $7 THEN indexing_jobs.last_error
				ELSE ''
			 END,
			 next_attempt_at = CASE
				WHEN indexing_jobs.status = $7 THEN indexing_jobs.next_attempt_at
				ELSE EXCLUDED.next_attempt_at
			 END,
			 completed_at = CASE
				WHEN indexing_jobs.status = $7 THEN indexing_jobs.completed_at
				ELSE NULL
			 END,
			 updated_at = CASE
				WHEN indexing_jobs.status = $7 THEN indexing_jobs.updated_at
				ELSE NOW()
			 END
		 RETURNING id, repo_id, commit_hash, job_type, status, attempt_count, max_attempts, last_error, next_attempt_at, created_at, updated_at, started_at, completed_at`,
		job.RepoID, job.CommitHash, jobType, status, maxAttempts, nextAttemptAt,
		models.IndexJobCompleted, models.IndexJobQueued,
	)

	loaded, err := scanPostgresIndexingJob(row)
	if err != nil {
		return err
	}
	*job = *loaded
	return nil
}

func (p *PostgresDB) ClaimIndexingJob(ctx context.Context) (*models.IndexingJob, error) {
	row := p.db.QueryRowContext(ctx,
		`WITH next_job AS (
			 SELECT id
			 FROM indexing_jobs
			 WHERE status = $1
			   AND next_attempt_at <= NOW()
			 ORDER BY next_attempt_at ASC, id ASC
			 LIMIT 1
			 FOR UPDATE SKIP LOCKED
		 )
		 UPDATE indexing_jobs j
		 SET status = $2,
			 attempt_count = j.attempt_count + 1,
			 started_at = NOW(),
			 completed_at = NULL,
			 updated_at = NOW()
		 FROM next_job
		 WHERE j.id = next_job.id
		 RETURNING j.id, j.repo_id, j.commit_hash, j.job_type, j.status, j.attempt_count, j.max_attempts, j.last_error, j.next_attempt_at, j.created_at, j.updated_at, j.started_at, j.completed_at`,
		models.IndexJobQueued, models.IndexJobInProgress,
	)
	job, err := scanPostgresIndexingJob(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return job, nil
}

func (p *PostgresDB) CompleteIndexingJob(ctx context.Context, jobID int64, status models.IndexJobStatus, errMsg string) error {
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
	res, err := p.db.ExecContext(ctx,
		`UPDATE indexing_jobs
		 SET status = $1,
			 last_error = $2,
			 completed_at = NOW(),
			 updated_at = NOW()
		 WHERE id = $3 AND status = $4`,
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

func (p *PostgresDB) RequeueIndexingJob(ctx context.Context, jobID int64, errMsg string, nextAttemptAt time.Time) error {
	trimmedErr := strings.TrimSpace(errMsg)
	if trimmedErr == "" {
		trimmedErr = "job failed"
	}
	if nextAttemptAt.IsZero() {
		nextAttemptAt = time.Now().UTC()
	}
	res, err := p.db.ExecContext(ctx,
		`UPDATE indexing_jobs
		 SET status = CASE
				 WHEN attempt_count >= max_attempts THEN $1
				 ELSE $2
			 END,
			 last_error = $3,
			 next_attempt_at = CASE
				 WHEN attempt_count >= max_attempts THEN next_attempt_at
				 ELSE $4
			 END,
			 started_at = NULL,
			 completed_at = CASE
				 WHEN attempt_count >= max_attempts THEN NOW()
				 ELSE NULL
			 END,
			 updated_at = NOW()
		 WHERE id = $5 AND status = $6`,
		models.IndexJobFailed, models.IndexJobQueued, trimmedErr, nextAttemptAt, jobID, models.IndexJobInProgress,
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

func (p *PostgresDB) GetIndexingJobStatus(ctx context.Context, repoID int64, commitHash string) (*models.IndexingJob, error) {
	row := p.db.QueryRowContext(ctx,
		`SELECT id, repo_id, commit_hash, job_type, status, attempt_count, max_attempts, last_error, next_attempt_at, created_at, updated_at, started_at, completed_at
		 FROM indexing_jobs
		 WHERE repo_id = $1 AND commit_hash = $2
		 ORDER BY created_at DESC, id DESC
		 LIMIT 1`,
		repoID, commitHash,
	)
	job, err := scanPostgresIndexingJob(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return job, nil
}

func scanPostgresIndexingJob(row *sql.Row) (*models.IndexingJob, error) {
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

func (p *PostgresDB) SetCommitIndex(ctx context.Context, repoID int64, commitHash, indexHash string) error {
	_, err := p.db.ExecContext(ctx,
		`INSERT INTO commit_indexes (repo_id, commit_hash, index_hash) VALUES ($1, $2, $3)
		 ON CONFLICT (repo_id, commit_hash) DO UPDATE SET
			 index_hash = EXCLUDED.index_hash`,
		repoID, commitHash, indexHash)
	return err
}

func (p *PostgresDB) GetCommitIndex(ctx context.Context, repoID int64, commitHash string) (string, error) {
	var h string
	err := p.db.QueryRowContext(ctx,
		`SELECT index_hash FROM commit_indexes WHERE repo_id = $1 AND commit_hash = $2`,
		repoID, commitHash).Scan(&h)
	return h, err
}

func (p *PostgresDB) SetGitTreeEntryModes(ctx context.Context, repoID int64, gotTreeHash string, modes map[string]string) error {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM git_tree_entry_modes WHERE repo_id = $1 AND got_tree_hash = $2`,
		repoID, gotTreeHash); err != nil {
		tx.Rollback()
		return err
	}
	if len(modes) == 0 {
		return tx.Commit()
	}
	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO git_tree_entry_modes (repo_id, got_tree_hash, entry_name, mode) VALUES ($1, $2, $3, $4)
		 ON CONFLICT (repo_id, got_tree_hash, entry_name) DO UPDATE SET mode = EXCLUDED.mode`)
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

func (p *PostgresDB) GetGitTreeEntryModes(ctx context.Context, repoID int64, gotTreeHash string) (map[string]string, error) {
	rows, err := p.db.QueryContext(ctx,
		`SELECT entry_name, mode FROM git_tree_entry_modes WHERE repo_id = $1 AND got_tree_hash = $2`,
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

func (p *PostgresDB) UpsertEntityIdentity(ctx context.Context, identity *models.EntityIdentity) error {
	_, err := p.db.ExecContext(ctx,
		`INSERT INTO entity_identities
			(repo_id, stable_id, name, decl_kind, receiver, first_seen_commit, last_seen_commit)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 ON CONFLICT (repo_id, stable_id) DO UPDATE SET
			 name = EXCLUDED.name,
			 decl_kind = EXCLUDED.decl_kind,
			 receiver = EXCLUDED.receiver,
			 last_seen_commit = EXCLUDED.last_seen_commit,
			 updated_at = NOW()`,
		identity.RepoID, identity.StableID, identity.Name, identity.DeclKind, identity.Receiver, identity.FirstSeenCommit, identity.LastSeenCommit)
	return err
}

func (p *PostgresDB) SetEntityVersion(ctx context.Context, version *models.EntityVersion) error {
	_, err := p.db.ExecContext(ctx,
		`INSERT INTO entity_versions
			(repo_id, stable_id, commit_hash, path, entity_hash, body_hash, name, decl_kind, receiver)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 ON CONFLICT (repo_id, commit_hash, path, entity_hash) DO UPDATE SET
			 stable_id = EXCLUDED.stable_id,
			 body_hash = EXCLUDED.body_hash,
			 name = EXCLUDED.name,
			 decl_kind = EXCLUDED.decl_kind,
			 receiver = EXCLUDED.receiver`,
		version.RepoID, version.StableID, version.CommitHash, version.Path, version.EntityHash, version.BodyHash, version.Name, version.DeclKind, version.Receiver)
	return err
}

func (p *PostgresDB) ListEntityVersionsByCommit(ctx context.Context, repoID int64, commitHash string) ([]models.EntityVersion, error) {
	rows, err := p.db.QueryContext(ctx,
		`SELECT repo_id, stable_id, commit_hash, path, entity_hash, body_hash, name, decl_kind, receiver, created_at
		 FROM entity_versions
		 WHERE repo_id = $1 AND commit_hash = $2
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

func (p *PostgresDB) CountEntityVersionsByCommitFiltered(ctx context.Context, repoID int64, commitHash, stableID, name, bodyHash string) (int, error) {
	query := `SELECT COUNT(*) FROM entity_versions WHERE repo_id = $1 AND commit_hash = $2`
	args := []any{repoID, commitHash}
	if strings.TrimSpace(stableID) != "" {
		args = append(args, stableID)
		query += fmt.Sprintf(` AND stable_id = $%d`, len(args))
	}
	if strings.TrimSpace(name) != "" {
		args = append(args, name)
		query += fmt.Sprintf(` AND name = $%d`, len(args))
	}
	if strings.TrimSpace(bodyHash) != "" {
		args = append(args, bodyHash)
		query += fmt.Sprintf(` AND lower(body_hash) = lower($%d)`, len(args))
	}

	var count int
	if err := p.db.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func (p *PostgresDB) ListEntityVersionsByCommitFilteredPage(ctx context.Context, repoID int64, commitHash, stableID, name, bodyHash string, limit, offset int) ([]models.EntityVersion, error) {
	if limit <= 0 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	query := `SELECT repo_id, stable_id, commit_hash, path, entity_hash, body_hash, name, decl_kind, receiver, created_at
		 FROM entity_versions
		 WHERE repo_id = $1 AND commit_hash = $2`
	args := []any{repoID, commitHash}
	if strings.TrimSpace(stableID) != "" {
		args = append(args, stableID)
		query += fmt.Sprintf(` AND stable_id = $%d`, len(args))
	}
	if strings.TrimSpace(name) != "" {
		args = append(args, name)
		query += fmt.Sprintf(` AND name = $%d`, len(args))
	}
	if strings.TrimSpace(bodyHash) != "" {
		args = append(args, bodyHash)
		query += fmt.Sprintf(` AND lower(body_hash) = lower($%d)`, len(args))
	}
	args = append(args, limit)
	query += fmt.Sprintf(` ORDER BY path, entity_hash LIMIT $%d`, len(args))
	args = append(args, offset)
	query += fmt.Sprintf(` OFFSET $%d`, len(args))

	rows, err := p.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	versions := make([]models.EntityVersion, 0, limit)
	for rows.Next() {
		var v models.EntityVersion
		if err := rows.Scan(&v.RepoID, &v.StableID, &v.CommitHash, &v.Path, &v.EntityHash, &v.BodyHash, &v.Name, &v.DeclKind, &v.Receiver, &v.CreatedAt); err != nil {
			return nil, err
		}
		versions = append(versions, v)
	}
	return versions, rows.Err()
}

func (p *PostgresDB) HasEntityVersionsForCommit(ctx context.Context, repoID int64, commitHash string) (bool, error) {
	var one int
	err := p.db.QueryRowContext(ctx,
		`SELECT 1 FROM entity_versions WHERE repo_id = $1 AND commit_hash = $2 LIMIT 1`,
		repoID, commitHash).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (p *PostgresDB) SetEntityIndexEntries(ctx context.Context, repoID int64, commitHash string, entries []models.EntityIndexEntry) error {
	if strings.TrimSpace(commitHash) == "" {
		return nil
	}

	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM entity_index WHERE repo_id = $1 AND commit_hash = $2`,
		repoID, commitHash); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO entity_index_commits (repo_id, commit_hash, symbol_count, updated_at)
		 VALUES ($1, $2, $3, NOW())
		 ON CONFLICT (repo_id, commit_hash) DO UPDATE SET
			 symbol_count = EXCLUDED.symbol_count,
			 updated_at = NOW()`,
		repoID, commitHash, len(entries)); err != nil {
		return err
	}

	if len(entries) == 0 {
		return tx.Commit()
	}

	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO entity_index
			(repo_id, commit_hash, file_path, symbol_key, stable_id, kind, name, signature, receiver, language, doc_comment, start_line, end_line)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		 ON CONFLICT (repo_id, commit_hash, symbol_key) DO UPDATE SET
			 file_path = EXCLUDED.file_path,
			 stable_id = EXCLUDED.stable_id,
			 kind = EXCLUDED.kind,
			 name = EXCLUDED.name,
			 signature = EXCLUDED.signature,
			 receiver = EXCLUDED.receiver,
			 language = EXCLUDED.language,
			 doc_comment = EXCLUDED.doc_comment,
			 start_line = EXCLUDED.start_line,
			 end_line = EXCLUDED.end_line`)
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

func (p *PostgresDB) ListEntityIndexEntriesByCommit(ctx context.Context, repoID int64, commitHash, kind string, limit int) ([]models.EntityIndexEntry, error) {
	query := `SELECT repo_id, commit_hash, file_path, symbol_key, stable_id, kind, name, signature, receiver, language, doc_comment, start_line, end_line, created_at
		FROM entity_index
		WHERE repo_id = $1 AND commit_hash = $2`
	args := []any{repoID, commitHash}
	if strings.TrimSpace(kind) != "" {
		args = append(args, kind)
		query += fmt.Sprintf(` AND kind = $%d`, len(args))
	}
	query += ` ORDER BY file_path, start_line, name`
	if limit > 0 {
		args = append(args, limit)
		query += fmt.Sprintf(` LIMIT $%d`, len(args))
	}

	rows, err := p.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPostgresEntityIndexEntries(rows)
}

func (p *PostgresDB) SearchEntityIndexEntries(ctx context.Context, repoID int64, commitHash, textQuery, kind string, limit int) ([]models.EntityIndexEntry, error) {
	queryText := strings.TrimSpace(textQuery)
	if queryText == "" {
		return p.ListEntityIndexEntriesByCommit(ctx, repoID, commitHash, kind, limit)
	}

	vectorExpr := `to_tsvector('simple', coalesce(name, '') || ' ' || coalesce(signature, '') || ' ' || coalesce(doc_comment, ''))`
	query := `SELECT repo_id, commit_hash, file_path, symbol_key, stable_id, kind, name, signature, receiver, language, doc_comment, start_line, end_line, created_at
		FROM entity_index
		WHERE repo_id = $1 AND commit_hash = $2`
	args := []any{repoID, commitHash}
	if strings.TrimSpace(kind) != "" {
		args = append(args, kind)
		query += fmt.Sprintf(` AND kind = $%d`, len(args))
	}

	args = append(args, queryText)
	searchArgPos := len(args)
	query += fmt.Sprintf(` AND (
		%s @@ plainto_tsquery('simple', $%d)
		OR name ILIKE '%%' || $%d || '%%'
		OR signature ILIKE '%%' || $%d || '%%'
		OR doc_comment ILIKE '%%' || $%d || '%%'
	)`, vectorExpr, searchArgPos, searchArgPos, searchArgPos, searchArgPos)
	query += fmt.Sprintf(` ORDER BY
		CASE WHEN %s @@ plainto_tsquery('simple', $%d) THEN 0 ELSE 1 END,
		ts_rank_cd(%s, plainto_tsquery('simple', $%d)) DESC,
		name, file_path, start_line`, vectorExpr, searchArgPos, vectorExpr, searchArgPos)
	if limit > 0 {
		args = append(args, limit)
		query += fmt.Sprintf(` LIMIT $%d`, len(args))
	}

	rows, err := p.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPostgresEntityIndexEntries(rows)
}

func (p *PostgresDB) HasEntityIndexForCommit(ctx context.Context, repoID int64, commitHash string) (bool, error) {
	var one int
	err := p.db.QueryRowContext(ctx,
		`SELECT 1 FROM entity_index_commits WHERE repo_id = $1 AND commit_hash = $2 LIMIT 1`,
		repoID, commitHash).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func scanPostgresEntityIndexEntries(rows *sql.Rows) ([]models.EntityIndexEntry, error) {
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

func (p *PostgresDB) SetCommitXRefGraph(ctx context.Context, repoID int64, commitHash string, defs []models.XRefDefinition, edges []models.XRefEdge) error {
	if strings.TrimSpace(commitHash) == "" {
		return nil
	}

	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM xref_edges WHERE repo_id = $1 AND commit_hash = $2`,
		repoID, commitHash); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM xref_definitions WHERE repo_id = $1 AND commit_hash = $2`,
		repoID, commitHash); err != nil {
		return err
	}

	if len(defs) > 0 {
		stmt, err := tx.PrepareContext(ctx,
			`INSERT INTO xref_definitions
				(repo_id, commit_hash, entity_id, file, package_name, kind, name, signature, receiver, start_line, end_line, callable)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
			 ON CONFLICT (repo_id, commit_hash, entity_id) DO UPDATE SET
				 file = EXCLUDED.file,
				 package_name = EXCLUDED.package_name,
				 kind = EXCLUDED.kind,
				 name = EXCLUDED.name,
				 signature = EXCLUDED.signature,
				 receiver = EXCLUDED.receiver,
				 start_line = EXCLUDED.start_line,
				 end_line = EXCLUDED.end_line,
				 callable = EXCLUDED.callable`)
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
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			 ON CONFLICT (repo_id, commit_hash, source_entity_id, target_entity_id, kind) DO UPDATE SET
				 source_file = EXCLUDED.source_file,
				 source_line = EXCLUDED.source_line,
				 resolution = EXCLUDED.resolution,
				 count = EXCLUDED.count`)
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

func (p *PostgresDB) HasXRefGraphForCommit(ctx context.Context, repoID int64, commitHash string) (bool, error) {
	var one int
	err := p.db.QueryRowContext(ctx,
		`SELECT 1 FROM xref_definitions WHERE repo_id = $1 AND commit_hash = $2 LIMIT 1`,
		repoID, commitHash).Scan(&one)
	if err == nil {
		return true, nil
	}
	if err != sql.ErrNoRows {
		return false, err
	}

	err = p.db.QueryRowContext(ctx,
		`SELECT 1 FROM xref_edges WHERE repo_id = $1 AND commit_hash = $2 LIMIT 1`,
		repoID, commitHash).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (p *PostgresDB) FindXRefDefinitionsByName(ctx context.Context, repoID int64, commitHash, name string) ([]models.XRefDefinition, error) {
	rows, err := p.db.QueryContext(ctx,
		`SELECT repo_id, commit_hash, entity_id, file, package_name, kind, name, signature, receiver, start_line, end_line, callable, created_at
		 FROM xref_definitions
		 WHERE repo_id = $1 AND commit_hash = $2 AND name = $3
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

func (p *PostgresDB) GetXRefDefinition(ctx context.Context, repoID int64, commitHash, entityID string) (*models.XRefDefinition, error) {
	var d models.XRefDefinition
	err := p.db.QueryRowContext(ctx,
		`SELECT repo_id, commit_hash, entity_id, file, package_name, kind, name, signature, receiver, start_line, end_line, callable, created_at
		 FROM xref_definitions
		 WHERE repo_id = $1 AND commit_hash = $2 AND entity_id = $3`,
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

func (p *PostgresDB) ListXRefEdgesFrom(ctx context.Context, repoID int64, commitHash, sourceEntityID, kind string) ([]models.XRefEdge, error) {
	query := `SELECT repo_id, commit_hash, source_entity_id, target_entity_id, kind, source_file, source_line, resolution, count, created_at
		FROM xref_edges
		WHERE repo_id = $1 AND commit_hash = $2 AND source_entity_id = $3`
	args := []any{repoID, commitHash, sourceEntityID}
	if strings.TrimSpace(kind) != "" {
		query += ` AND kind = $4`
		args = append(args, kind)
	}
	query += ` ORDER BY target_entity_id, kind`

	rows, err := p.db.QueryContext(ctx, query, args...)
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

func (p *PostgresDB) ListXRefEdgesTo(ctx context.Context, repoID int64, commitHash, targetEntityID, kind string) ([]models.XRefEdge, error) {
	query := `SELECT repo_id, commit_hash, source_entity_id, target_entity_id, kind, source_file, source_line, resolution, count, created_at
		FROM xref_edges
		WHERE repo_id = $1 AND commit_hash = $2 AND target_entity_id = $3`
	args := []any{repoID, commitHash, targetEntityID}
	if strings.TrimSpace(kind) != "" {
		query += ` AND kind = $4`
		args = append(args, kind)
	}
	query += ` ORDER BY source_entity_id, kind`

	rows, err := p.db.QueryContext(ctx, query, args...)
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
	return p.ListUserOrgsPage(ctx, userID, 1<<30, 0)
}

func (p *PostgresDB) ListUserOrgsPage(ctx context.Context, userID int64, limit, offset int) ([]models.Org, error) {
	if limit <= 0 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := p.db.QueryContext(ctx,
		`SELECT o.id, o.name, o.display_name FROM orgs o
		 JOIN org_members om ON om.org_id = o.id
		 WHERE om.user_id = $1
		 ORDER BY o.name ASC
		 LIMIT $2 OFFSET $3`, userID, limit, offset)
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
	return p.ListOrgMembersPage(ctx, orgID, 1<<30, 0)
}

func (p *PostgresDB) ListOrgMembersPage(ctx context.Context, orgID int64, limit, offset int) ([]models.OrgMember, error) {
	if limit <= 0 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := p.db.QueryContext(ctx,
		`SELECT org_id, user_id, role FROM org_members WHERE org_id = $1 ORDER BY user_id LIMIT $2 OFFSET $3`, orgID, limit, offset)
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
	return p.ListOrgRepositoriesPage(ctx, orgID, 1<<30, 0)
}

func (p *PostgresDB) ListOrgRepositoriesPage(ctx context.Context, orgID int64, limit, offset int) ([]models.Repository, error) {
	if limit <= 0 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := p.db.QueryContext(ctx,
		`SELECT r.id, r.owner_user_id, r.owner_org_id, r.parent_repo_id, r.name, r.description, r.default_branch, r.is_private, r.storage_path, r.created_at, o.name
		 FROM repositories r
		 JOIN orgs o ON o.id = r.owner_org_id
		 WHERE r.owner_org_id = $1
		 ORDER BY r.created_at DESC, r.id DESC
		 LIMIT $2 OFFSET $3`, orgID, limit, offset)
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
var _ DB = (*PostgresDB)(nil)
