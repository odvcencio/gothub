package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := Default()

	if cfg.Server.Host != "0.0.0.0" {
		t.Fatalf("Server.Host = %q, want %q", cfg.Server.Host, "0.0.0.0")
	}
	if cfg.Server.Port != 3000 {
		t.Fatalf("Server.Port = %d, want 3000", cfg.Server.Port)
	}
	if cfg.Database.Driver != "sqlite" {
		t.Fatalf("Database.Driver = %q, want %q", cfg.Database.Driver, "sqlite")
	}
	if cfg.Auth.JWTSecret != "change-me-in-production" {
		t.Fatalf("Auth.JWTSecret = %q, want default", cfg.Auth.JWTSecret)
	}
	if cfg.Tenancy.Enabled {
		t.Fatal("Tenancy.Enabled = true, want default false")
	}
	if cfg.Tenancy.Header != "X-Gothub-Tenant-ID" {
		t.Fatalf("Tenancy.Header = %q, want %q", cfg.Tenancy.Header, "X-Gothub-Tenant-ID")
	}
}

func TestLoadAppliesEnvOverrides(t *testing.T) {
	t.Setenv("GOTHUB_HOST", "127.0.0.1")
	t.Setenv("GOTHUB_PORT", "4000")
	t.Setenv("GOTHUB_TRUSTED_PROXIES", "10.0.0.0/8, 192.168.1.10")
	t.Setenv("GOTHUB_DB_DRIVER", "postgres")
	t.Setenv("GOTHUB_DB_DSN", "postgres://example")
	t.Setenv("GOTHUB_STORAGE_PATH", "/tmp/repos")
	t.Setenv("GOTHUB_JWT_SECRET", "unit-test-secret-123")
	t.Setenv("GOTHUB_ENABLE_PASSWORD_AUTH", "true")
	t.Setenv("GOTHUB_ENABLE_TENANCY", "true")
	t.Setenv("GOTHUB_TENANCY_HEADER", "X-Tenant-ID")
	t.Setenv("GOTHUB_DEFAULT_TENANT_ID", "tenant-default")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Server.Host != "127.0.0.1" {
		t.Fatalf("Server.Host = %q, want %q", cfg.Server.Host, "127.0.0.1")
	}
	if cfg.Server.Port != 4000 {
		t.Fatalf("Server.Port = %d, want 4000", cfg.Server.Port)
	}
	if len(cfg.Server.TrustedProxies) != 2 {
		t.Fatalf("Server.TrustedProxies length = %d, want 2", len(cfg.Server.TrustedProxies))
	}
	if cfg.Server.TrustedProxies[0] != "10.0.0.0/8" {
		t.Fatalf("Server.TrustedProxies[0] = %q, want %q", cfg.Server.TrustedProxies[0], "10.0.0.0/8")
	}
	if cfg.Server.TrustedProxies[1] != "192.168.1.10" {
		t.Fatalf("Server.TrustedProxies[1] = %q, want %q", cfg.Server.TrustedProxies[1], "192.168.1.10")
	}
	if cfg.Database.Driver != "postgres" {
		t.Fatalf("Database.Driver = %q, want %q", cfg.Database.Driver, "postgres")
	}
	if cfg.Database.DSN != "postgres://example" {
		t.Fatalf("Database.DSN = %q, want %q", cfg.Database.DSN, "postgres://example")
	}
	if cfg.Storage.Path != "/tmp/repos" {
		t.Fatalf("Storage.Path = %q, want %q", cfg.Storage.Path, "/tmp/repos")
	}
	if cfg.Auth.JWTSecret != "unit-test-secret-123" {
		t.Fatalf("Auth.JWTSecret = %q, want override", cfg.Auth.JWTSecret)
	}
	if !cfg.Auth.EnablePasswordAuth {
		t.Fatal("Auth.EnablePasswordAuth = false, want true")
	}
	if !cfg.Tenancy.Enabled {
		t.Fatal("Tenancy.Enabled = false, want true")
	}
	if cfg.Tenancy.Header != "X-Tenant-ID" {
		t.Fatalf("Tenancy.Header = %q, want %q", cfg.Tenancy.Header, "X-Tenant-ID")
	}
	if cfg.Tenancy.DefaultTenantID != "tenant-default" {
		t.Fatalf("Tenancy.DefaultTenantID = %q, want %q", cfg.Tenancy.DefaultTenantID, "tenant-default")
	}
}

func TestLoadFromYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	body := []byte(`
server:
  host: 127.0.0.1
  port: 5555
  trusted_proxies:
    - 10.0.0.0/8
    - 192.168.1.10
database:
  driver: sqlite
  dsn: test.db
storage:
  path: data/repos
auth:
  jwt_secret: yaml-secret-123456
  token_duration: 12h
  enable_password_auth: true
tenancy:
  enabled: true
  header: X-Tenant-ID
  default_tenant_id: tenant-yaml
`)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load(path): %v", err)
	}

	if cfg.Server.Port != 5555 {
		t.Fatalf("Server.Port = %d, want 5555", cfg.Server.Port)
	}
	if len(cfg.Server.TrustedProxies) != 2 {
		t.Fatalf("Server.TrustedProxies length = %d, want 2", len(cfg.Server.TrustedProxies))
	}
	if cfg.Server.TrustedProxies[0] != "10.0.0.0/8" {
		t.Fatalf("Server.TrustedProxies[0] = %q, want %q", cfg.Server.TrustedProxies[0], "10.0.0.0/8")
	}
	if cfg.Server.TrustedProxies[1] != "192.168.1.10" {
		t.Fatalf("Server.TrustedProxies[1] = %q, want %q", cfg.Server.TrustedProxies[1], "192.168.1.10")
	}
	if cfg.Auth.TokenDuration != "12h" {
		t.Fatalf("Auth.TokenDuration = %q, want %q", cfg.Auth.TokenDuration, "12h")
	}
	if !cfg.Auth.EnablePasswordAuth {
		t.Fatal("Auth.EnablePasswordAuth = false, want true")
	}
	if !cfg.Tenancy.Enabled {
		t.Fatal("Tenancy.Enabled = false, want true")
	}
	if cfg.Tenancy.Header != "X-Tenant-ID" {
		t.Fatalf("Tenancy.Header = %q, want %q", cfg.Tenancy.Header, "X-Tenant-ID")
	}
	if cfg.Tenancy.DefaultTenantID != "tenant-yaml" {
		t.Fatalf("Tenancy.DefaultTenantID = %q, want %q", cfg.Tenancy.DefaultTenantID, "tenant-yaml")
	}
}
