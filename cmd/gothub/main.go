package main

import (
	"context"
	"flag"
	"fmt"
	"log"
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
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	configPath := fs.String("config", "", "path to config file")
	fs.Parse(args)

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	db, err := openDB(cfg)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()

	// Auto-migrate on startup
	if err := db.Migrate(context.Background()); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	dur, err := time.ParseDuration(cfg.Auth.TokenDuration)
	if err != nil {
		dur = 24 * time.Hour
	}
	authSvc := auth.NewService(cfg.Auth.JWTSecret, dur)
	repoSvc := service.NewRepoService(db, cfg.Storage.Path)
	server := api.NewServer(db, authSvc, repoSvc)

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
		log.Printf("gothub listening on %s", cfg.Addr())
		if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	<-done
	log.Println("shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	httpServer.Shutdown(ctx)
}

func cmdMigrate(args []string) {
	fs := flag.NewFlagSet("migrate", flag.ExitOnError)
	configPath := fs.String("config", "", "path to config file")
	fs.Parse(args)

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	db, err := openDB(cfg)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()

	if err := db.Migrate(context.Background()); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	log.Println("migrations complete")
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
