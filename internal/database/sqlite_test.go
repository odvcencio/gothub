package database

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"github.com/odvcencio/gothub/internal/models"
)

func TestSQLiteGetRepositoryFallsBackToOrgOwner(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := OpenSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	org := &models.Org{Name: "acme", DisplayName: "Acme Corp"}
	if err := db.CreateOrg(ctx, org); err != nil {
		t.Fatal(err)
	}

	repo := &models.Repository{
		OwnerOrgID:    &org.ID,
		Name:          "platform",
		Description:   "",
		DefaultBranch: "main",
		IsPrivate:     false,
		StoragePath:   "pending",
	}
	if err := db.CreateRepository(ctx, repo); err != nil {
		t.Fatal(err)
	}

	got, err := db.GetRepository(ctx, "acme", "platform")
	if err != nil {
		t.Fatalf("expected org-owned repository lookup to work: %v", err)
	}
	if got.OwnerOrgID == nil || *got.OwnerOrgID != org.ID {
		t.Fatalf("expected owner org id %d, got %#v", org.ID, got.OwnerOrgID)
	}
}

func TestSQLiteUpdateRepositoryStoragePath(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := OpenSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	user := &models.User{Username: "bob", Email: "bob@example.com", PasswordHash: "x"}
	if err := db.CreateUser(ctx, user); err != nil {
		t.Fatal(err)
	}

	repo := &models.Repository{
		OwnerUserID:   &user.ID,
		Name:          "repo",
		Description:   "",
		DefaultBranch: "main",
		IsPrivate:     false,
		StoragePath:   "pending",
	}
	if err := db.CreateRepository(ctx, repo); err != nil {
		t.Fatal(err)
	}

	const realPath = "/var/lib/gothub/repos/123"
	if err := db.UpdateRepositoryStoragePath(ctx, repo.ID, realPath); err != nil {
		t.Fatal(err)
	}

	got, err := db.GetRepository(ctx, "bob", "repo")
	if err != nil {
		t.Fatal(err)
	}
	if got.StoragePath != realPath {
		t.Fatalf("expected storage path %q, got %q", realPath, got.StoragePath)
	}
}

func TestSQLiteCreatePullRequestAssignsUniqueNumbersConcurrently(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := OpenSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	user := &models.User{Username: "alice", Email: "alice@example.com", PasswordHash: "x"}
	if err := db.CreateUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	repo := &models.Repository{
		OwnerUserID:   &user.ID,
		Name:          "repo",
		DefaultBranch: "main",
		StoragePath:   "pending",
	}
	if err := db.CreateRepository(ctx, repo); err != nil {
		t.Fatal(err)
	}

	const n = 12
	errCh := make(chan error, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			pr := &models.PullRequest{
				RepoID:       repo.ID,
				Title:        "PR",
				Body:         "",
				State:        "open",
				AuthorID:     user.ID,
				SourceBranch: "feature-" + string(rune('a'+i)),
				TargetBranch: "main",
			}
			errCh <- db.CreatePullRequest(ctx, pr)
		}()
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Fatalf("concurrent create PR failed: %v", err)
		}
	}

	prs, err := db.ListPullRequests(ctx, repo.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(prs) != n {
		t.Fatalf("expected %d pull requests, got %d", n, len(prs))
	}

	seen := make(map[int]bool, n)
	for _, pr := range prs {
		if pr.Number < 1 || pr.Number > n {
			t.Fatalf("unexpected PR number %d", pr.Number)
		}
		if seen[pr.Number] {
			t.Fatalf("duplicate PR number %d", pr.Number)
		}
		seen[pr.Number] = true
	}
	for i := 1; i <= n; i++ {
		if !seen[i] {
			t.Fatalf("missing PR number %d", i)
		}
	}
}
