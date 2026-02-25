package database

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/odvcencio/gothub/internal/models"
)

func setupPostgresRepositoryTenantTestDB(t *testing.T) *PostgresDB {
	t.Helper()

	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})

	schema := []string{
		`CREATE TABLE users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT NOT NULL,
			email TEXT NOT NULL,
			password_hash TEXT NOT NULL,
			is_admin BOOLEAN NOT NULL DEFAULT FALSE,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			tenant_id TEXT NOT NULL
		)`,
		`CREATE TABLE orgs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			display_name TEXT NOT NULL DEFAULT '',
			tenant_id TEXT NOT NULL
		)`,
		`CREATE TABLE org_members (
			org_id INTEGER NOT NULL,
			user_id INTEGER NOT NULL,
			role TEXT NOT NULL DEFAULT 'member',
			PRIMARY KEY (org_id, user_id)
		)`,
		`CREATE TABLE repositories (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			owner_user_id INTEGER,
			owner_org_id INTEGER,
			parent_repo_id INTEGER,
			name TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			default_branch TEXT NOT NULL DEFAULT 'main',
			is_private BOOLEAN NOT NULL DEFAULT FALSE,
			storage_path TEXT NOT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			tenant_id TEXT NOT NULL
		)`,
	}
	for _, stmt := range schema {
		if _, err := sqlDB.Exec(stmt); err != nil {
			t.Fatalf("exec schema statement: %v", err)
		}
	}

	return &PostgresDB{db: sqlDB}
}

func TestPostgresRepositoryTenantIsolationByID(t *testing.T) {
	db := setupPostgresRepositoryTenantTestDB(t)

	if _, err := db.db.Exec(`
		INSERT INTO users (id, username, email, password_hash, tenant_id)
		VALUES (1, 'alice', 'alice@example.com', 'hash-a', 'tenant-a'),
		       (2, 'bob', 'bob@example.com', 'hash-b', 'tenant-b')
	`); err != nil {
		t.Fatalf("seed users: %v", err)
	}
	if _, err := db.db.Exec(`
		INSERT INTO repositories (id, owner_user_id, name, description, default_branch, is_private, storage_path, tenant_id)
		VALUES (11, 1, 'alpha', 'a', 'main', FALSE, '/tmp/a', 'tenant-a'),
		       (22, 2, 'beta',  'b', 'main', FALSE, '/tmp/b', 'tenant-b')
	`); err != nil {
		t.Fatalf("seed repositories: %v", err)
	}

	repo, err := db.GetRepositoryByID(WithTenantID(context.Background(), "tenant-a"), 11)
	if err != nil {
		t.Fatalf("GetRepositoryByID tenant match: %v", err)
	}
	if repo.ID != 11 {
		t.Fatalf("repo.ID = %d, want 11", repo.ID)
	}

	_, err = db.GetRepositoryByID(WithTenantID(context.Background(), "tenant-a"), 22)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetRepositoryByID cross-tenant error = %v, want %v", err, sql.ErrNoRows)
	}
}

func TestPostgresCreateRepositorySetsTenantIDFromContext(t *testing.T) {
	db := setupPostgresRepositoryTenantTestDB(t)

	if _, err := db.db.Exec(`
		INSERT INTO users (id, username, email, password_hash, tenant_id)
		VALUES (1, 'alice', 'alice@example.com', 'hash-a', 'tenant-a')
	`); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	ownerID := int64(1)
	repo := &models.Repository{
		OwnerUserID:   &ownerID,
		Name:          "tenant-aware-repo",
		Description:   "repo",
		DefaultBranch: "main",
		StoragePath:   "/tmp/repo",
	}
	if err := db.CreateRepository(WithTenantID(context.Background(), "tenant-a"), repo); err != nil {
		t.Fatalf("CreateRepository: %v", err)
	}

	var tenantID string
	if err := db.db.QueryRow(`SELECT tenant_id FROM repositories WHERE id = $1`, repo.ID).Scan(&tenantID); err != nil {
		t.Fatalf("query tenant_id: %v", err)
	}
	if tenantID != "tenant-a" {
		t.Fatalf("tenant_id = %q, want %q", tenantID, "tenant-a")
	}
}
