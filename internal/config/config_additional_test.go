package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestAddr(t *testing.T) {
	cfg := Default()
	cfg.Server.Host = "127.0.0.1"
	cfg.Server.Port = 8088

	if got := cfg.Addr(); got != "127.0.0.1:8088" {
		t.Fatalf("Addr() = %q, want %q", got, "127.0.0.1:8088")
	}
}

func TestLoadReadError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.yaml")

	_, err := Load(missing)
	if err == nil {
		t.Fatal("Load(missing) error = nil, want error")
	}
	if !strings.Contains(err.Error(), "read config") {
		t.Fatalf("Load(missing) error = %v, want read config error", err)
	}
}

func TestLoadParseError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("server: [\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load(invalid yaml) error = nil, want error")
	}
	if !strings.Contains(err.Error(), "parse config") {
		t.Fatalf("Load(invalid yaml) error = %v, want parse config error", err)
	}
}

func TestLoadParsesTrustedProxiesAndCORSOriginsFromEnv(t *testing.T) {
	t.Setenv("GOTHUB_TRUSTED_PROXIES", " 10.0.0.0/8, , 192.168.1.10 ,, ")
	t.Setenv("GOTHUB_CORS_ALLOW_ORIGINS", " https://app.example.com, ,https://admin.example.com ")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	wantProxies := []string{"10.0.0.0/8", "192.168.1.10"}
	if !reflect.DeepEqual(cfg.Server.TrustedProxies, wantProxies) {
		t.Fatalf("Server.TrustedProxies = %#v, want %#v", cfg.Server.TrustedProxies, wantProxies)
	}

	wantOrigins := []string{"https://app.example.com", "https://admin.example.com"}
	if !reflect.DeepEqual(cfg.Server.CORSAllowedOrigins, wantOrigins) {
		t.Fatalf("Server.CORSAllowedOrigins = %#v, want %#v", cfg.Server.CORSAllowedOrigins, wantOrigins)
	}
}

func TestLoadInvalidEnvValuesDoNotOverrideDefaults(t *testing.T) {
	t.Setenv("GOTHUB_PORT", "not-an-int")
	t.Setenv("GOTHUB_ENABLE_PASSWORD_AUTH", "not-a-bool")
	t.Setenv("GOTHUB_TRUSTED_PROXIES", ",, ,")
	t.Setenv("GOTHUB_CORS_ALLOW_ORIGINS", ",  ,")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Server.Port != 3000 {
		t.Fatalf("Server.Port = %d, want default 3000", cfg.Server.Port)
	}
	if cfg.Auth.EnablePasswordAuth {
		t.Fatal("Auth.EnablePasswordAuth = true, want default false")
	}
	if cfg.Server.TrustedProxies != nil {
		t.Fatalf("Server.TrustedProxies = %#v, want nil", cfg.Server.TrustedProxies)
	}
	if cfg.Server.CORSAllowedOrigins != nil {
		t.Fatalf("Server.CORSAllowedOrigins = %#v, want nil", cfg.Server.CORSAllowedOrigins)
	}
}

func TestLoadFromYAMLParsesCORSOrigins(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	body := []byte(`
server:
  host: 0.0.0.0
  port: 3000
  cors_allowed_origins:
    - https://web.example.com
    - https://admin.example.com
`)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load(path): %v", err)
	}

	want := []string{"https://web.example.com", "https://admin.example.com"}
	if !reflect.DeepEqual(cfg.Server.CORSAllowedOrigins, want) {
		t.Fatalf("Server.CORSAllowedOrigins = %#v, want %#v", cfg.Server.CORSAllowedOrigins, want)
	}
}

func TestParseCSV(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []string
	}{
		{name: "empty string", raw: "", want: nil},
		{name: "whitespace only", raw: "   ", want: nil},
		{name: "commas only", raw: " , ,, ", want: nil},
		{name: "values with whitespace", raw: "  alpha, , beta ,gamma  ", want: []string{"alpha", "beta", "gamma"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseCSV(tc.raw); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("parseCSV(%q) = %#v, want %#v", tc.raw, got, tc.want)
			}
		})
	}
}

func TestValidateServe(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *Config
		wantErr string
	}{
		{
			name:    "nil config",
			cfg:     nil,
			wantErr: "config is required",
		},
		{
			name:    "default jwt secret is rejected",
			cfg:     Default(),
			wantErr: "GOTHUB_JWT_SECRET must be set to a non-default value",
		},
		{
			name: "short jwt secret is rejected",
			cfg: &Config{
				Auth:    AuthConfig{JWTSecret: "short-secret"},
				Storage: StorageConfig{Path: "data/repos"},
			},
			wantErr: "GOTHUB_JWT_SECRET must be at least 16 characters",
		},
		{
			name: "missing storage path is rejected",
			cfg: &Config{
				Auth:    AuthConfig{JWTSecret: "1234567890abcdef"},
				Storage: StorageConfig{Path: ""},
			},
			wantErr: "storage.path must be configured",
		},
		{
			name: "valid serve config passes",
			cfg: &Config{
				Auth:    AuthConfig{JWTSecret: "1234567890abcdef"},
				Storage: StorageConfig{Path: "data/repos"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.ValidateServe()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateServe() error = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidateServe() error = nil, want %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("ValidateServe() error = %v, want substring %q", err, tc.wantErr)
			}
		})
	}
}
