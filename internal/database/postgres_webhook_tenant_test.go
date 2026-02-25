package database

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/odvcencio/gothub/internal/models"
)

func setupPostgresWebhookTenantTestDB(t *testing.T) *PostgresDB {
	t.Helper()

	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})

	schema := []string{
		`CREATE TABLE repositories (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id TEXT NOT NULL
		)`,
		`CREATE TABLE repo_webhooks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			repo_id INTEGER NOT NULL,
			url TEXT NOT NULL,
			secret TEXT NOT NULL DEFAULT '',
			events_csv TEXT NOT NULL DEFAULT '*',
			active BOOLEAN NOT NULL DEFAULT TRUE,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			tenant_id TEXT NOT NULL
		)`,
		`CREATE TABLE webhook_deliveries (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			repo_id INTEGER NOT NULL,
			webhook_id INTEGER NOT NULL,
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

func TestPostgresWebhookTenantIsolation(t *testing.T) {
	db := setupPostgresWebhookTenantTestDB(t)

	if _, err := db.db.Exec(`
		INSERT INTO repositories (id, tenant_id)
		VALUES (11, 'tenant-a'),
		       (22, 'tenant-b')
	`); err != nil {
		t.Fatalf("seed repositories: %v", err)
	}

	hook := &models.Webhook{
		RepoID:    11,
		URL:       "https://example.test/hook",
		Secret:    "shh",
		EventsCSV: "push",
		Active:    true,
	}
	if err := db.CreateWebhook(WithTenantID(context.Background(), "tenant-a"), hook); err != nil {
		t.Fatalf("CreateWebhook tenant match: %v", err)
	}

	var tenantID string
	if err := db.db.QueryRow(`SELECT tenant_id FROM repo_webhooks WHERE id = $1`, hook.ID).Scan(&tenantID); err != nil {
		t.Fatalf("query repo_webhooks tenant_id: %v", err)
	}
	if tenantID != "tenant-a" {
		t.Fatalf("tenant_id = %q, want %q", tenantID, "tenant-a")
	}

	if _, err := db.GetWebhook(WithTenantID(context.Background(), "tenant-b"), hook.RepoID, hook.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetWebhook cross-tenant error = %v, want %v", err, sql.ErrNoRows)
	}

	otherTenantHook := &models.Webhook{
		RepoID:    11,
		URL:       "https://example.test/hook-denied",
		Secret:    "nope",
		EventsCSV: "push",
		Active:    true,
	}
	if err := db.CreateWebhook(WithTenantID(context.Background(), "tenant-b"), otherTenantHook); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("CreateWebhook cross-tenant error = %v, want %v", err, sql.ErrNoRows)
	}
}

func TestPostgresWebhookDeliveryTenantIsolation(t *testing.T) {
	db := setupPostgresWebhookTenantTestDB(t)

	if _, err := db.db.Exec(`
		INSERT INTO repositories (id, tenant_id)
		VALUES (11, 'tenant-a')
	`); err != nil {
		t.Fatalf("seed repositories: %v", err)
	}

	hook := &models.Webhook{
		RepoID:    11,
		URL:       "https://example.test/hook",
		Secret:    "shh",
		EventsCSV: "push",
		Active:    true,
	}
	if err := db.CreateWebhook(WithTenantID(context.Background(), "tenant-a"), hook); err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}

	delivery := &models.WebhookDelivery{
		RepoID:       hook.RepoID,
		WebhookID:    hook.ID,
		Event:        "push",
		DeliveryUID:  "uid-1",
		Attempt:      1,
		StatusCode:   200,
		Success:      true,
		Error:        "",
		RequestBody:  "{}",
		ResponseBody: "{}",
		DurationMS:   10,
	}
	if err := db.CreateWebhookDelivery(WithTenantID(context.Background(), "tenant-a"), delivery); err != nil {
		t.Fatalf("CreateWebhookDelivery tenant match: %v", err)
	}

	var tenantID string
	if err := db.db.QueryRow(`SELECT tenant_id FROM webhook_deliveries WHERE id = $1`, delivery.ID).Scan(&tenantID); err != nil {
		t.Fatalf("query webhook_deliveries tenant_id: %v", err)
	}
	if tenantID != "tenant-a" {
		t.Fatalf("tenant_id = %q, want %q", tenantID, "tenant-a")
	}

	if _, err := db.GetWebhookDelivery(WithTenantID(context.Background(), "tenant-b"), hook.RepoID, hook.ID, delivery.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetWebhookDelivery cross-tenant error = %v, want %v", err, sql.ErrNoRows)
	}

	deniedDelivery := &models.WebhookDelivery{
		RepoID:      hook.RepoID,
		WebhookID:   hook.ID,
		Event:       "push",
		DeliveryUID: "uid-2",
		Attempt:     1,
		StatusCode:  200,
		Success:     true,
	}
	if err := db.CreateWebhookDelivery(WithTenantID(context.Background(), "tenant-b"), deniedDelivery); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("CreateWebhookDelivery cross-tenant error = %v, want %v", err, sql.ErrNoRows)
	}
}
