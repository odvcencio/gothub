package config

import (
	"fmt"
	"os"
	"strconv"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	Storage  StorageConfig  `yaml:"storage"`
	Auth     AuthConfig     `yaml:"auth"`
}

type ServerConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

type DatabaseConfig struct {
	Driver string `yaml:"driver"` // "sqlite" or "postgres"
	DSN    string `yaml:"dsn"`    // file path for sqlite, connection string for postgres
}

type StorageConfig struct {
	Backend   string `yaml:"backend"`    // "local" or "s3"
	Path      string `yaml:"path"`       // local filesystem path for repos
	Endpoint  string `yaml:"endpoint"`   // S3 endpoint (e.g. "s3.amazonaws.com" or "minio:9000")
	Bucket    string `yaml:"bucket"`     // S3 bucket name
	Region    string `yaml:"region"`     // S3 region
	AccessKey string `yaml:"access_key"` // S3 access key
	SecretKey string `yaml:"secret_key"` // S3 secret key
	UseSSL    bool   `yaml:"use_ssl"`    // S3 use SSL
}

type AuthConfig struct {
	JWTSecret     string `yaml:"jwt_secret"`
	TokenDuration string `yaml:"token_duration"` // e.g. "24h"
}

func (c *Config) Addr() string {
	return fmt.Sprintf("%s:%d", c.Server.Host, c.Server.Port)
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
			Backend: "local",
			Path:    "data/repos",
		},
		Auth: AuthConfig{
			JWTSecret:     "change-me-in-production",
			TokenDuration: "24h",
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
	if v := os.Getenv("GOTHUB_DB_DRIVER"); v != "" {
		cfg.Database.Driver = v
	}
	if v := os.Getenv("GOTHUB_DB_DSN"); v != "" {
		cfg.Database.DSN = v
	}
	if v := os.Getenv("GOTHUB_STORAGE_BACKEND"); v != "" {
		cfg.Storage.Backend = v
	}
	if v := os.Getenv("GOTHUB_STORAGE_PATH"); v != "" {
		cfg.Storage.Path = v
	}
	if v := os.Getenv("GOTHUB_JWT_SECRET"); v != "" {
		cfg.Auth.JWTSecret = v
	}
	if v := os.Getenv("GOTHUB_S3_ENDPOINT"); v != "" {
		cfg.Storage.Endpoint = v
	}
	if v := os.Getenv("GOTHUB_S3_BUCKET"); v != "" {
		cfg.Storage.Bucket = v
	}
	if v := os.Getenv("GOTHUB_S3_REGION"); v != "" {
		cfg.Storage.Region = v
	}
	if v := os.Getenv("GOTHUB_S3_ACCESS_KEY"); v != "" {
		cfg.Storage.AccessKey = v
	}
	if v := os.Getenv("GOTHUB_S3_SECRET_KEY"); v != "" {
		cfg.Storage.SecretKey = v
	}
	if v := os.Getenv("GOTHUB_S3_USE_SSL"); v == "true" {
		cfg.Storage.UseSSL = true
	}
}
