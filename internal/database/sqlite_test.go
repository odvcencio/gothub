package database

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

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

func TestSQLiteGetRepositoryByIDSupportsOrgOwner(t *testing.T) {
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

	got, err := db.GetRepositoryByID(ctx, repo.ID)
	if err != nil {
		t.Fatalf("expected org-owned repository by ID lookup to work: %v", err)
	}
	if got.OwnerName != "acme" {
		t.Fatalf("expected owner name acme, got %q", got.OwnerName)
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

func TestSQLiteRepositoryPersistsParentRepoID(t *testing.T) {
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

	parent := &models.Repository{
		OwnerUserID:   &user.ID,
		Name:          "upstream",
		DefaultBranch: "main",
		StoragePath:   "pending",
	}
	if err := db.CreateRepository(ctx, parent); err != nil {
		t.Fatal(err)
	}

	child := &models.Repository{
		OwnerUserID:   &user.ID,
		ParentRepoID:  &parent.ID,
		Name:          "upstream-fork",
		DefaultBranch: "main",
		StoragePath:   "pending",
	}
	if err := db.CreateRepository(ctx, child); err != nil {
		t.Fatal(err)
	}

	got, err := db.GetRepositoryByID(ctx, child.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ParentRepoID == nil || *got.ParentRepoID != parent.ID {
		t.Fatalf("expected parent repo id %d, got %+v", parent.ID, got.ParentRepoID)
	}
}

func TestSQLiteCloneRepoMetadataCopiesRecords(t *testing.T) {
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

	src := &models.Repository{
		OwnerUserID:   &user.ID,
		Name:          "src",
		DefaultBranch: "main",
		StoragePath:   "pending",
	}
	if err := db.CreateRepository(ctx, src); err != nil {
		t.Fatal(err)
	}
	dst := &models.Repository{
		OwnerUserID:   &user.ID,
		Name:          "dst",
		DefaultBranch: "main",
		StoragePath:   "pending",
	}
	if err := db.CreateRepository(ctx, dst); err != nil {
		t.Fatal(err)
	}

	const (
		gitHash    = "1111111111111111111111111111111111111111"
		gotHash    = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		commitHash = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		treeHash   = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
		indexHash  = "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	)
	if err := db.SetHashMappings(ctx, []models.HashMapping{
		{RepoID: src.ID, GitHash: gitHash, GotHash: gotHash, ObjectType: "commit"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetCommitIndex(ctx, src.ID, commitHash, indexHash); err != nil {
		t.Fatal(err)
	}
	if err := db.SetGitTreeEntryModes(ctx, src.ID, treeHash, map[string]string{"main.go": "100755"}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertEntityIdentity(ctx, &models.EntityIdentity{
		RepoID:          src.ID,
		StableID:        "ent-1",
		Name:            "Process",
		DeclKind:        "function",
		FirstSeenCommit: commitHash,
		LastSeenCommit:  commitHash,
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetEntityVersion(ctx, &models.EntityVersion{
		RepoID:     src.ID,
		StableID:   "ent-1",
		CommitHash: commitHash,
		Path:       "main.go",
		EntityHash: "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
		BodyHash:   "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
		Name:       "Process",
		DeclKind:   "function",
	}); err != nil {
		t.Fatal(err)
	}

	if err := db.CloneRepoMetadata(ctx, src.ID, dst.ID); err != nil {
		t.Fatal(err)
	}

	gotDstHash, err := db.GetGotHash(ctx, dst.ID, gitHash)
	if err != nil {
		t.Fatal(err)
	}
	if gotDstHash != gotHash {
		t.Fatalf("expected copied got hash %q, got %q", gotHash, gotDstHash)
	}

	gotDstIndex, err := db.GetCommitIndex(ctx, dst.ID, commitHash)
	if err != nil {
		t.Fatal(err)
	}
	if gotDstIndex != indexHash {
		t.Fatalf("expected copied index hash %q, got %q", indexHash, gotDstIndex)
	}

	modes, err := db.GetGitTreeEntryModes(ctx, dst.ID, treeHash)
	if err != nil {
		t.Fatal(err)
	}
	if modes["main.go"] != "100755" {
		t.Fatalf("expected copied mode 100755, got %#v", modes)
	}

	versions, err := db.ListEntityVersionsByCommit(ctx, dst.ID, commitHash)
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 1 || versions[0].StableID != "ent-1" {
		t.Fatalf("expected copied entity versions, got %+v", versions)
	}
}

func TestSQLiteListRepositoryForks(t *testing.T) {
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

	alice := &models.User{Username: "alice", Email: "alice@example.com", PasswordHash: "x"}
	bob := &models.User{Username: "bob", Email: "bob@example.com", PasswordHash: "x"}
	if err := db.CreateUser(ctx, alice); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateUser(ctx, bob); err != nil {
		t.Fatal(err)
	}

	parent := &models.Repository{
		OwnerUserID:   &alice.ID,
		Name:          "repo",
		DefaultBranch: "main",
		StoragePath:   "pending",
	}
	if err := db.CreateRepository(ctx, parent); err != nil {
		t.Fatal(err)
	}

	forkA := &models.Repository{
		OwnerUserID:   &bob.ID,
		ParentRepoID:  &parent.ID,
		Name:          "repo",
		DefaultBranch: "main",
		StoragePath:   "pending",
	}
	forkB := &models.Repository{
		OwnerUserID:   &bob.ID,
		ParentRepoID:  &parent.ID,
		Name:          "repo-fork-1",
		DefaultBranch: "main",
		StoragePath:   "pending",
	}
	if err := db.CreateRepository(ctx, forkA); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateRepository(ctx, forkB); err != nil {
		t.Fatal(err)
	}

	forks, err := db.ListRepositoryForks(ctx, parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(forks) != 2 {
		t.Fatalf("expected 2 forks, got %+v", forks)
	}
	for _, fork := range forks {
		if fork.ParentRepoID == nil || *fork.ParentRepoID != parent.ID {
			t.Fatalf("expected parent repo id %d, got %+v", parent.ID, fork.ParentRepoID)
		}
		if fork.OwnerName != "bob" {
			t.Fatalf("expected owner bob, got %+v", fork)
		}
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

func TestSQLiteSetHashMappingRemapsGitHash(t *testing.T) {
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

	gitHash := strings.Repeat("a", 40)
	first := &models.HashMapping{
		RepoID: repo.ID, GitHash: gitHash, GotHash: strings.Repeat("b", 64), ObjectType: "commit",
	}
	if err := db.SetHashMapping(ctx, first); err != nil {
		t.Fatal(err)
	}

	second := &models.HashMapping{
		RepoID: repo.ID, GitHash: gitHash, GotHash: strings.Repeat("c", 64), ObjectType: "commit",
	}
	if err := db.SetHashMapping(ctx, second); err != nil {
		t.Fatal(err)
	}

	gotHash, err := db.GetGotHash(ctx, repo.ID, gitHash)
	if err != nil {
		t.Fatal(err)
	}
	if gotHash != second.GotHash {
		t.Fatalf("expected git hash to map to latest got hash %q, got %q", second.GotHash, gotHash)
	}

	if _, err := db.GetGitHash(ctx, repo.ID, first.GotHash); err != sql.ErrNoRows {
		t.Fatalf("expected old got hash mapping to be removed, got err=%v", err)
	}
}

func TestSQLiteCommitIndexUpsertAndGet(t *testing.T) {
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

	commitHash := strings.Repeat("a", 64)
	firstIndex := strings.Repeat("b", 64)
	if err := db.SetCommitIndex(ctx, repo.ID, commitHash, firstIndex); err != nil {
		t.Fatal(err)
	}
	gotIndex, err := db.GetCommitIndex(ctx, repo.ID, commitHash)
	if err != nil {
		t.Fatal(err)
	}
	if gotIndex != firstIndex {
		t.Fatalf("expected index hash %q, got %q", firstIndex, gotIndex)
	}

	secondIndex := strings.Repeat("c", 64)
	if err := db.SetCommitIndex(ctx, repo.ID, commitHash, secondIndex); err != nil {
		t.Fatal(err)
	}
	gotIndex, err = db.GetCommitIndex(ctx, repo.ID, commitHash)
	if err != nil {
		t.Fatal(err)
	}
	if gotIndex != secondIndex {
		t.Fatalf("expected upserted index hash %q, got %q", secondIndex, gotIndex)
	}
}

func TestSQLiteGitTreeEntryModesReplaceAndGet(t *testing.T) {
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

	treeHash := strings.Repeat("a", 64)
	first := map[string]string{
		"script.sh": "100755",
		"main.go":   "100644",
	}
	if err := db.SetGitTreeEntryModes(ctx, repo.ID, treeHash, first); err != nil {
		t.Fatal(err)
	}

	got, err := db.GetGitTreeEntryModes(ctx, repo.ID, treeHash)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(first) {
		t.Fatalf("expected %d modes, got %d", len(first), len(got))
	}
	for k, want := range first {
		if got[k] != want {
			t.Fatalf("expected mode[%q]=%q, got %q", k, want, got[k])
		}
	}

	second := map[string]string{
		"script.sh": "100644",
	}
	if err := db.SetGitTreeEntryModes(ctx, repo.ID, treeHash, second); err != nil {
		t.Fatal(err)
	}

	got, err = db.GetGitTreeEntryModes(ctx, repo.ID, treeHash)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(second) {
		t.Fatalf("expected replacement to leave %d mode rows, got %d", len(second), len(got))
	}
	if got["script.sh"] != "100644" {
		t.Fatalf("expected updated script mode 100644, got %q", got["script.sh"])
	}
	if _, ok := got["main.go"]; ok {
		t.Fatalf("expected stale entry main.go to be removed after replacement")
	}
}

func TestSQLiteEntityLineageStorage(t *testing.T) {
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

	id := &models.EntityIdentity{
		RepoID:          repo.ID,
		StableID:        "stable-1",
		Name:            "ProcessOrder",
		DeclKind:        "function",
		Receiver:        "",
		FirstSeenCommit: strings.Repeat("a", 64),
		LastSeenCommit:  strings.Repeat("a", 64),
	}
	if err := db.UpsertEntityIdentity(ctx, id); err != nil {
		t.Fatal(err)
	}

	v1 := &models.EntityVersion{
		RepoID:     repo.ID,
		StableID:   id.StableID,
		CommitHash: strings.Repeat("a", 64),
		Path:       "main.go",
		EntityHash: strings.Repeat("b", 64),
		BodyHash:   strings.Repeat("c", 64),
		Name:       "ProcessOrder",
		DeclKind:   "function",
	}
	if err := db.SetEntityVersion(ctx, v1); err != nil {
		t.Fatal(err)
	}

	ok, err := db.HasEntityVersionsForCommit(ctx, repo.ID, v1.CommitHash)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatalf("expected commit %s to have entity versions", v1.CommitHash)
	}

	versions, err := db.ListEntityVersionsByCommit(ctx, repo.ID, v1.CommitHash)
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 1 {
		t.Fatalf("expected 1 entity version, got %d", len(versions))
	}
	if versions[0].StableID != id.StableID {
		t.Fatalf("expected stable id %q, got %q", id.StableID, versions[0].StableID)
	}
}

func TestSQLiteRepoStarsLifecycle(t *testing.T) {
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

	alice := &models.User{Username: "alice", Email: "alice@example.com", PasswordHash: "x"}
	if err := db.CreateUser(ctx, alice); err != nil {
		t.Fatal(err)
	}
	bob := &models.User{Username: "bob", Email: "bob@example.com", PasswordHash: "x"}
	if err := db.CreateUser(ctx, bob); err != nil {
		t.Fatal(err)
	}

	repo := &models.Repository{
		OwnerUserID:   &alice.ID,
		Name:          "repo",
		DefaultBranch: "main",
		StoragePath:   "pending",
	}
	if err := db.CreateRepository(ctx, repo); err != nil {
		t.Fatal(err)
	}

	if err := db.AddRepoStar(ctx, repo.ID, bob.ID); err != nil {
		t.Fatal(err)
	}
	starred, err := db.IsRepoStarred(ctx, repo.ID, bob.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !starred {
		t.Fatal("expected repo to be starred by bob")
	}

	count, err := db.CountRepoStars(ctx, repo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected 1 star, got %d", count)
	}

	stargazers, err := db.ListRepoStargazers(ctx, repo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stargazers) != 1 || stargazers[0].Username != "bob" {
		t.Fatalf("unexpected stargazers: %+v", stargazers)
	}

	starredRepos, err := db.ListUserStarredRepositories(ctx, bob.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(starredRepos) != 1 || starredRepos[0].Name != "repo" || starredRepos[0].OwnerName != "alice" {
		t.Fatalf("unexpected starred repositories: %+v", starredRepos)
	}

	if err := db.RemoveRepoStar(ctx, repo.ID, bob.ID); err != nil {
		t.Fatal(err)
	}
	starred, err = db.IsRepoStarred(ctx, repo.ID, bob.ID)
	if err != nil {
		t.Fatal(err)
	}
	if starred {
		t.Fatal("expected repo to be unstarred")
	}
}

func TestSQLiteBranchProtectionRuleCRUD(t *testing.T) {
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

	rule := &models.BranchProtectionRule{
		RepoID:                     repo.ID,
		Branch:                     "main",
		Enabled:                    true,
		RequireApprovals:           true,
		RequiredApprovals:          2,
		RequireStatusChecks:        true,
		RequireEntityOwnerApproval: true,
		RequireLintPass:            true,
		RequireNoNewDeadCode:       true,
		RequireSignedCommits:       true,
		RequiredChecksCSV:          "ci/test,lint",
	}
	if err := db.UpsertBranchProtectionRule(ctx, rule); err != nil {
		t.Fatal(err)
	}
	if rule.ID == 0 {
		t.Fatal("expected rule ID to be set")
	}

	got, err := db.GetBranchProtectionRule(ctx, repo.ID, "main")
	if err != nil {
		t.Fatal(err)
	}
	if !got.RequireApprovals || got.RequiredApprovals != 2 {
		t.Fatalf("unexpected approval settings: %+v", got)
	}
	if !got.RequireStatusChecks || got.RequiredChecksCSV != "ci/test,lint" {
		t.Fatalf("unexpected status check settings: %+v", got)
	}
	if !got.RequireEntityOwnerApproval {
		t.Fatalf("expected require_entity_owner_approval to persist, got %+v", got)
	}
	if !got.RequireLintPass {
		t.Fatalf("expected require_lint_pass to persist, got %+v", got)
	}
	if !got.RequireNoNewDeadCode {
		t.Fatalf("expected require_no_new_dead_code to persist, got %+v", got)
	}
	if !got.RequireSignedCommits {
		t.Fatalf("expected require_signed_commits to persist, got %+v", got)
	}

	if err := db.DeleteBranchProtectionRule(ctx, repo.ID, "main"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetBranchProtectionRule(ctx, repo.ID, "main"); err == nil {
		t.Fatal("expected deleted branch protection rule lookup to fail")
	}
}

func TestSQLitePRCheckRunUpsertAndList(t *testing.T) {
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
	pr := &models.PullRequest{
		RepoID:       repo.ID,
		Title:        "PR",
		Body:         "",
		State:        "open",
		AuthorID:     user.ID,
		SourceBranch: "feature",
		TargetBranch: "main",
	}
	if err := db.CreatePullRequest(ctx, pr); err != nil {
		t.Fatal(err)
	}

	run := &models.PRCheckRun{
		PRID:       pr.ID,
		Name:       "ci/test",
		Status:     "queued",
		Conclusion: "",
	}
	if err := db.UpsertPRCheckRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	if run.ID == 0 {
		t.Fatal("expected check run ID to be set")
	}

	run.Status = "completed"
	run.Conclusion = "success"
	if err := db.UpsertPRCheckRun(ctx, run); err != nil {
		t.Fatal(err)
	}

	runs, err := db.ListPRCheckRuns(ctx, pr.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected 1 check run, got %d", len(runs))
	}
	if runs[0].Status != "completed" || runs[0].Conclusion != "success" {
		t.Fatalf("unexpected check run state: %+v", runs[0])
	}
}

func TestSQLiteWebhookCRUDAndDeliveries(t *testing.T) {
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

	hook := &models.Webhook{
		RepoID:    repo.ID,
		URL:       "https://example.com/hook",
		Secret:    "secret",
		EventsCSV: "ping,pull_request",
		Active:    true,
	}
	if err := db.CreateWebhook(ctx, hook); err != nil {
		t.Fatal(err)
	}
	if hook.ID == 0 {
		t.Fatal("expected webhook id")
	}

	gotHook, err := db.GetWebhook(ctx, repo.ID, hook.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotHook.URL != hook.URL || gotHook.EventsCSV != hook.EventsCSV {
		t.Fatalf("unexpected webhook row: %+v", gotHook)
	}

	hooks, err := db.ListWebhooks(ctx, repo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(hooks) != 1 {
		t.Fatalf("expected 1 webhook, got %d", len(hooks))
	}

	delivery := &models.WebhookDelivery{
		RepoID:       repo.ID,
		WebhookID:    hook.ID,
		Event:        "ping",
		DeliveryUID:  "abc123",
		Attempt:      1,
		StatusCode:   204,
		Success:      true,
		RequestBody:  `{"ok":true}`,
		ResponseBody: "",
		DurationMS:   12,
	}
	if err := db.CreateWebhookDelivery(ctx, delivery); err != nil {
		t.Fatal(err)
	}
	if delivery.ID == 0 {
		t.Fatal("expected delivery id")
	}

	gotDelivery, err := db.GetWebhookDelivery(ctx, repo.ID, hook.ID, delivery.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotDelivery.DeliveryUID != "abc123" || !gotDelivery.Success {
		t.Fatalf("unexpected delivery row: %+v", gotDelivery)
	}

	deliveries, err := db.ListWebhookDeliveries(ctx, repo.ID, hook.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(deliveries) != 1 {
		t.Fatalf("expected 1 delivery, got %d", len(deliveries))
	}

	if err := db.DeleteWebhook(ctx, repo.ID, hook.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetWebhook(ctx, repo.ID, hook.ID); err == nil {
		t.Fatal("expected deleted webhook lookup to fail")
	}
}

func TestSQLiteIssueCRUDAndComments(t *testing.T) {
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

	issue := &models.Issue{
		RepoID:   repo.ID,
		Title:    "Issue 1",
		Body:     "desc",
		State:    "open",
		AuthorID: user.ID,
	}
	if err := db.CreateIssue(ctx, issue); err != nil {
		t.Fatal(err)
	}
	if issue.Number != 1 {
		t.Fatalf("expected issue number 1, got %d", issue.Number)
	}

	gotIssue, err := db.GetIssue(ctx, repo.ID, issue.Number)
	if err != nil {
		t.Fatal(err)
	}
	if gotIssue.Title != "Issue 1" || gotIssue.State != "open" {
		t.Fatalf("unexpected issue: %+v", gotIssue)
	}

	issues, err := db.ListIssues(ctx, repo.ID, "open")
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}

	now := time.Now()
	issue.Title = "Issue closed"
	issue.State = "closed"
	issue.ClosedAt = &now
	if err := db.UpdateIssue(ctx, issue); err != nil {
		t.Fatal(err)
	}

	gotIssue, err = db.GetIssue(ctx, repo.ID, issue.Number)
	if err != nil {
		t.Fatal(err)
	}
	if gotIssue.State != "closed" || gotIssue.ClosedAt == nil {
		t.Fatalf("expected closed issue with closed_at, got %+v", gotIssue)
	}

	comment := &models.IssueComment{
		IssueID:  issue.ID,
		AuthorID: user.ID,
		Body:     "first comment",
	}
	if err := db.CreateIssueComment(ctx, comment); err != nil {
		t.Fatal(err)
	}

	comments, err := db.ListIssueComments(ctx, issue.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) != 1 || comments[0].Body != "first comment" {
		t.Fatalf("unexpected comments: %+v", comments)
	}
}
