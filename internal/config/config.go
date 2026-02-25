package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	Storage  StorageConfig  `yaml:"storage"`
	Auth     AuthConfig     `yaml:"auth"`
	Tenancy  TenancyConfig  `yaml:"tenancy"`
}

type ServerConfig struct {
	Host               string   `yaml:"host"`
	Port               int      `yaml:"port"`
	TrustedProxies     []string `yaml:"trusted_proxies"`
	CORSAllowedOrigins []string `yaml:"cors_allowed_origins"`
}

type DatabaseConfig struct {
	Driver string `yaml:"driver"` // "sqlite" or "postgres"
	DSN    string `yaml:"dsn"`    // file path for sqlite, connection string for postgres
}

type StorageConfig struct {
	Path string `yaml:"path"` // local filesystem path for repos
}

type AuthConfig struct {
	JWTSecret          string `yaml:"jwt_secret"`
	TokenDuration      string `yaml:"token_duration"` // e.g. "24h"
	EnablePasswordAuth bool   `yaml:"enable_password_auth"`
}

type TenancyConfig struct {
	Enabled         bool   `yaml:"enabled"`
	Header          string `yaml:"header"`
	DefaultTenantID string `yaml:"default_tenant_id"`
}

func (c *Config) Addr() string {
	return fmt.Sprintf("%s:%d", c.Server.Host, c.Server.Port)
}

func (c *Config) ValidateServe() error {
	if c == nil {
		return fmt.Errorf("config is required")
	}
	if c.Auth.JWTSecret == "" || c.Auth.JWTSecret == "change-me-in-production" {
		return fmt.Errorf("GOTHUB_JWT_SECRET must be set to a non-default value (example: GOTHUB_JWT_SECRET=dev-jwt-secret-change-this)")
	}
	if len(c.Auth.JWTSecret) < 16 {
		return fmt.Errorf("GOTHUB_JWT_SECRET must be at least 16 characters (current length: %d)", len(c.Auth.JWTSecret))
	}
	if c.Storage.Path == "" {
		return fmt.Errorf("storage.path must be configured")
	}
	return nil
}

func Default() *Config {
	return &Config{
		Server: ServerConfig{
			Host: "0.0.0.0",
			Port: 3000,
		},
		Database: DatabaseConfig{
			Driver: "sqlite",
			DSN:    "gothub.db",
		},
		Storage: StorageConfig{
			Path: "data/repos",
		},
		Auth: AuthConfig{
			JWTSecret:          "change-me-in-production",
			TokenDuration:      "24h",
			EnablePasswordAuth: false,
		},
		Tenancy: TenancyConfig{
			Enabled: false,
			Header:  "X-Gothub-Tenant-ID",
		},
	}
}

func Load(path string) (*Config, error) {
	cfg := Default()

	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read config: %w", err)
		}
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parse config: %w", err)
		}
	}

	applyEnv(cfg)
	return cfg, nil
}

func applyEnv(cfg *Config) {
	if v := os.Getenv("GOTHUB_HOST"); v != "" {
		cfg.Server.Host = v
	}
	if v := os.Getenv("GOTHUB_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			cfg.Server.Port = p
		}
	}
	if v := os.Getenv("GOTHUB_TRUSTED_PROXIES"); v != "" {
		cfg.Server.TrustedProxies = parseCSV(v)
	}
	if v := os.Getenv("GOTHUB_CORS_ALLOW_ORIGINS"); v != "" {
		cfg.Server.CORSAllowedOrigins = parseCSV(v)
	}
	if v := os.Getenv("GOTHUB_DB_DRIVER"); v != "" {
		cfg.Database.Driver = v
	}
	if v := os.Getenv("GOTHUB_DB_DSN"); v != "" {
		cfg.Database.DSN = v
	}
	if v := os.Getenv("GOTHUB_STORAGE_PATH"); v != "" {
		cfg.Storage.Path = v
	}
	if v := os.Getenv("GOTHUB_JWT_SECRET"); v != "" {
		cfg.Auth.JWTSecret = v
	}
	if v := os.Getenv("GOTHUB_ENABLE_PASSWORD_AUTH"); v != "" {
		if enabled, err := strconv.ParseBool(v); err == nil {
			cfg.Auth.EnablePasswordAuth = enabled
		}
	}
	if v := os.Getenv("GOTHUB_ENABLE_TENANCY"); v != "" {
		if enabled, err := strconv.ParseBool(v); err == nil {
			cfg.Tenancy.Enabled = enabled
		}
	}
	if v := os.Getenv("GOTHUB_TENANCY_HEADER"); v != "" {
		cfg.Tenancy.Header = v
	}
	if v := os.Getenv("GOTHUB_DEFAULT_TENANT_ID"); v != "" {
		cfg.Tenancy.DefaultTenantID = strings.TrimSpace(v)
	}
}

func parseCSV(v string) []string {
	raw := strings.TrimSpace(v)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		value := strings.TrimSpace(part)
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
