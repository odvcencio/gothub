package database

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/odvcencio/gothub/internal/models"
)

func setupPostgresPRTenantTestDB(t *testing.T) *PostgresDB {
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
			tenant_id TEXT NOT NULL
		)`,
		`CREATE TABLE pull_requests (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id TEXT NOT NULL
		)`,
		`CREATE TABLE pr_comments (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			pr_id INTEGER NOT NULL,
			author_id INTEGER NOT NULL,
			body TEXT NOT NULL,
			file_path TEXT NOT NULL DEFAULT '',
			entity_key TEXT NOT NULL DEFAULT '',
			entity_stable_id TEXT NOT NULL DEFAULT '',
			line_number INTEGER,
			commit_hash TEXT NOT NULL DEFAULT '',
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

func TestPostgresCreatePRCommentTenantIsolation(t *testing.T) {
	db := setupPostgresPRTenantTestDB(t)

	if _, err := db.db.Exec(`
		INSERT INTO users (id, username, tenant_id)
		VALUES (1, 'alice', 'tenant-a'),
		       (2, 'bob', 'tenant-b')
	`); err != nil {
		t.Fatalf("seed users: %v", err)
	}
	if _, err := db.db.Exec(`
		INSERT INTO pull_requests (id, tenant_id)
		VALUES (11, 'tenant-a')
	`); err != nil {
		t.Fatalf("seed pull requests: %v", err)
	}

	comment := &models.PRComment{
		PRID:     11,
		AuthorID: 1,
		Body:     "looks good",
	}
	if err := db.CreatePRComment(WithTenantID(context.Background(), "tenant-a"), comment); err != nil {
		t.Fatalf("CreatePRComment tenant match: %v", err)
	}

	var tenantID string
	if err := db.db.QueryRow(`SELECT tenant_id FROM pr_comments WHERE id = $1`, comment.ID).Scan(&tenantID); err != nil {
		t.Fatalf("query pr_comments tenant_id: %v", err)
	}
	if tenantID != "tenant-a" {
		t.Fatalf("tenant_id = %q, want %q", tenantID, "tenant-a")
	}

	crossTenantComment := &models.PRComment{
		PRID:     11,
		AuthorID: 2,
		Body:     "cross-tenant",
	}
	if err := db.CreatePRComment(WithTenantID(context.Background(), "tenant-b"), crossTenantComment); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("CreatePRComment cross-tenant repo error = %v, want %v", err, sql.ErrNoRows)
	}

	wrongAuthorTenantComment := &models.PRComment{
		PRID:     11,
		AuthorID: 2,
		Body:     "wrong author tenant",
	}
	if err := db.CreatePRComment(WithTenantID(context.Background(), "tenant-a"), wrongAuthorTenantComment); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("CreatePRComment cross-tenant author error = %v, want %v", err, sql.ErrNoRows)
	}
}
