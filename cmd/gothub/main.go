package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/odvcencio/gothub/internal/api"
	"github.com/odvcencio/gothub/internal/auth"
	"github.com/odvcencio/gothub/internal/config"
	"github.com/odvcencio/gothub/internal/database"
	"github.com/odvcencio/gothub/internal/service"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: gothub <command>\n\nCommands:\n  serve    Start the server\n  migrate  Run database migrations\n")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "serve":
		cmdServe(os.Args[2:])
	case "migrate":
		cmdMigrate(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		os.Exit(1)
	}
}

func cmdServe(args []string) {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	configPath := fs.String("config", "", "path to config file")
	fs.Parse(args)

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}
	if err := validateServeConfig(cfg); err != nil {
		slog.Error("invalid config", "error", err)
		os.Exit(1)
	}

	traceShutdown, err := initTracing(context.Background())
	if err != nil {
		slog.Error("init tracing", "error", err)
		os.Exit(1)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := traceShutdown(ctx); err != nil {
			slog.Error("shutdown tracing", "error", err)
		}
	}()

	db, err := openDB(cfg)
	if err != nil {
		slog.Error("open database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	// Auto-migrate on startup
	if err := db.Migrate(context.Background()); err != nil {
		slog.Error("migrate", "error", err)
		os.Exit(1)
	}

	dur, err := time.ParseDuration(cfg.Auth.TokenDuration)
	if err != nil {
		dur = 24 * time.Hour
	}
	authSvc := auth.NewService(cfg.Auth.JWTSecret, dur)
	repoSvc := service.NewRepoService(db, cfg.Storage.Path)
	server := api.NewServerWithOptions(db, authSvc, repoSvc, api.ServerOptions{
		EnablePasswordAuth: cfg.Auth.EnablePasswordAuth,
	})

	httpServer := &http.Server{
		Addr:         cfg.Addr(),
		Handler:      server,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Graceful shutdown
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt)

	go func() {
		slog.Info("gothub listening", "addr", cfg.Addr())
		if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
			slog.Error("listen", "error", err)
			os.Exit(1)
		}
	}()

	<-done
	slog.Info("shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	httpServer.Shutdown(ctx)
}

func cmdMigrate(args []string) {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	fs := flag.NewFlagSet("migrate", flag.ExitOnError)
	configPath := fs.String("config", "", "path to config file")
	fs.Parse(args)

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}

	db, err := openDB(cfg)
	if err != nil {
		slog.Error("open database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	if err := db.Migrate(context.Background()); err != nil {
		slog.Error("migrate", "error", err)
		os.Exit(1)
	}
	slog.Info("migrations complete")
}

func openDB(cfg *config.Config) (database.DB, error) {
	switch cfg.Database.Driver {
	case "sqlite":
		return database.OpenSQLite(cfg.Database.DSN)
	case "postgres":
		return database.OpenPostgres(cfg.Database.DSN)
	default:
		return nil, fmt.Errorf("unsupported database driver: %s", cfg.Database.Driver)
	}
}

func validateServeConfig(cfg *config.Config) error {
	if cfg.Auth.JWTSecret == "" || cfg.Auth.JWTSecret == "change-me-in-production" {
		return fmt.Errorf("GOTHUB_JWT_SECRET must be set to a non-default value")
	}
	if len(cfg.Auth.JWTSecret) < 16 {
		return fmt.Errorf("GOTHUB_JWT_SECRET must be at least 16 characters")
	}
	if cfg.Storage.Path == "" {
		return fmt.Errorf("storage.path must be configured")
	}
	return nil
}
