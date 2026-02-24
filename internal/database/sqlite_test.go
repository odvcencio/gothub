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

func TestSQLiteForkRepositoryIncludesParentMetadata(t *testing.T) {
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

	fork := &models.Repository{
		OwnerUserID:   &bob.ID,
		ParentRepoID:  &parent.ID,
		Name:          "repo",
		DefaultBranch: "main",
		StoragePath:   "pending",
	}
	if err := db.CreateRepository(ctx, fork); err != nil {
		t.Fatal(err)
	}

	got, err := db.GetRepository(ctx, "bob", "repo")
	if err != nil {
		t.Fatal(err)
	}
	if got.ParentRepoID == nil || *got.ParentRepoID != parent.ID {
		t.Fatalf("expected parent repo id %d, got %+v", parent.ID, got.ParentRepoID)
	}
	if got.ParentOwner != "alice" {
		t.Fatalf("expected parent owner alice, got %q", got.ParentOwner)
	}
	if got.ParentName != "repo" {
		t.Fatalf("expected parent name repo, got %q", got.ParentName)
	}

	repos, err := db.ListUserRepositories(ctx, bob.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 1 {
		t.Fatalf("expected 1 repo, got %+v", repos)
	}
	if repos[0].ParentOwner != "alice" {
		t.Fatalf("expected list parent owner alice, got %q", repos[0].ParentOwner)
	}
	if repos[0].ParentName != "repo" {
		t.Fatalf("expected list parent name repo, got %q", repos[0].ParentName)
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
	if err := db.SetEntityIndexEntries(ctx, src.ID, commitHash, []models.EntityIndexEntry{
		{
			RepoID:     src.ID,
			CommitHash: commitHash,
			FilePath:   "main.go",
			SymbolKey:  "sym-1",
			StableID:   "ent-1",
			Kind:       "function",
			Name:       "Process",
			Signature:  "func Process()",
			StartLine:  20,
			EndLine:    23,
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetCommitXRefGraph(ctx, src.ID, commitHash,
		[]models.XRefDefinition{
			{
				EntityID:    "main.go\x00function\x00Caller\x0010",
				File:        "main.go",
				PackageName: "main",
				Kind:        "function",
				Name:        "Caller",
				StartLine:   10,
				EndLine:     12,
				Callable:    true,
			},
			{
				EntityID:    "main.go\x00function\x00Process\x0020",
				File:        "main.go",
				PackageName: "main",
				Kind:        "function",
				Name:        "Process",
				StartLine:   20,
				EndLine:     23,
				Callable:    true,
			},
		},
		[]models.XRefEdge{
			{
				SourceEntityID: "main.go\x00function\x00Caller\x0010",
				TargetEntityID: "main.go\x00function\x00Process\x0020",
				Kind:           "call",
				SourceFile:     "main.go",
				SourceLine:     11,
				Resolution:     "file",
				Count:          2,
			},
		},
	); err != nil {
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
	hasEntityIndex, err := db.HasEntityIndexForCommit(ctx, dst.ID, commitHash)
	if err != nil {
		t.Fatal(err)
	}
	if !hasEntityIndex {
		t.Fatalf("expected copied entity index commit marker for commit %s", commitHash)
	}
	entries, err := db.ListEntityIndexEntriesByCommit(ctx, dst.ID, commitHash, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name != "Process" {
		t.Fatalf("expected copied entity index entries, got %+v", entries)
	}

	hasXRef, err := db.HasXRefGraphForCommit(ctx, dst.ID, commitHash)
	if err != nil {
		t.Fatal(err)
	}
	if !hasXRef {
		t.Fatalf("expected copied xref graph for commit %s", commitHash)
	}

	callers, err := db.ListXRefEdgesTo(ctx, dst.ID, commitHash, "main.go\x00function\x00Process\x0020", "call")
	if err != nil {
		t.Fatal(err)
	}
	if len(callers) != 1 || callers[0].SourceEntityID != "main.go\x00function\x00Caller\x0010" {
		t.Fatalf("expected copied xref edge, got %+v", callers)
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
		if fork.ParentOwner != "alice" {
			t.Fatalf("expected parent owner alice, got %+v", fork)
		}
		if fork.ParentName != "repo" {
			t.Fatalf("expected parent name repo, got %+v", fork)
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

func TestSQLiteEntityVersionFilteredPagination(t *testing.T) {
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
	for _, stable := range []string{"stable-1", "stable-2"} {
		if err := db.UpsertEntityIdentity(ctx, &models.EntityIdentity{
			RepoID:          repo.ID,
			StableID:        stable,
			Name:            "ProcessOrder",
			DeclKind:        "function",
			FirstSeenCommit: commitHash,
			LastSeenCommit:  commitHash,
		}); err != nil {
			t.Fatal(err)
		}
	}

	versions := []models.EntityVersion{
		{
			RepoID: repo.ID, StableID: "stable-1", CommitHash: commitHash, Path: "a.go",
			EntityHash: strings.Repeat("1", 64), BodyHash: "deadbeef", Name: "ProcessOrder", DeclKind: "function",
		},
		{
			RepoID: repo.ID, StableID: "stable-1", CommitHash: commitHash, Path: "b.go",
			EntityHash: strings.Repeat("2", 64), BodyHash: "DEADBEEF", Name: "ProcessOrder", DeclKind: "function",
		},
		{
			RepoID: repo.ID, StableID: "stable-1", CommitHash: commitHash, Path: "c.go",
			EntityHash: strings.Repeat("3", 64), BodyHash: "cafebabe", Name: "ProcessOrder", DeclKind: "function",
		},
		{
			RepoID: repo.ID, StableID: "stable-2", CommitHash: commitHash, Path: "d.go",
			EntityHash: strings.Repeat("4", 64), BodyHash: "feedface", Name: "ValidateOrder", DeclKind: "function",
		},
	}
	for i := range versions {
		v := versions[i]
		if err := db.SetEntityVersion(ctx, &v); err != nil {
			t.Fatal(err)
		}
	}

	count, err := db.CountEntityVersionsByCommitFiltered(ctx, repo.ID, commitHash, "", "ProcessOrder", "")
	if err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("expected 3 ProcessOrder versions, got %d", count)
	}

	count, err = db.CountEntityVersionsByCommitFiltered(ctx, repo.ID, commitHash, "", "ProcessOrder", "DeAdBeEf")
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("expected body_hash filter to match 2 rows case-insensitively, got %d", count)
	}

	paged, err := db.ListEntityVersionsByCommitFilteredPage(ctx, repo.ID, commitHash, "", "ProcessOrder", "", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(paged) != 1 {
		t.Fatalf("expected one paged row, got %+v", paged)
	}
	if paged[0].Path != "b.go" {
		t.Fatalf("expected second ProcessOrder row by path order to be b.go, got %+v", paged[0])
	}

	unfiltered, err := db.ListEntityVersionsByCommitFilteredPage(ctx, repo.ID, commitHash, "", "", "", 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(unfiltered) != 2 {
		t.Fatalf("expected two unfiltered paged rows, got %+v", unfiltered)
	}
	if unfiltered[0].Path != "b.go" || unfiltered[1].Path != "c.go" {
		t.Fatalf("expected unfiltered page to return b.go,c.go, got %+v", unfiltered)
	}
}

func TestSQLiteXRefGraphStorageAndQueries(t *testing.T) {
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
	callerID := "main.go\x00function\x00Caller\x0010"
	calleeID := "main.go\x00function\x00ProcessOrder\x0020"

	err = db.SetCommitXRefGraph(ctx, repo.ID, commitHash,
		[]models.XRefDefinition{
			{
				EntityID:    callerID,
				File:        "main.go",
				PackageName: "main",
				Kind:        "function",
				Name:        "Caller",
				StartLine:   10,
				EndLine:     13,
				Callable:    true,
			},
			{
				EntityID:    calleeID,
				File:        "main.go",
				PackageName: "main",
				Kind:        "function",
				Name:        "ProcessOrder",
				StartLine:   20,
				EndLine:     24,
				Callable:    true,
			},
		},
		[]models.XRefEdge{
			{
				SourceEntityID: callerID,
				TargetEntityID: calleeID,
				Kind:           "call",
				SourceFile:     "main.go",
				SourceLine:     12,
				Resolution:     "file",
				Count:          3,
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	has, err := db.HasXRefGraphForCommit(ctx, repo.ID, commitHash)
	if err != nil {
		t.Fatal(err)
	}
	if !has {
		t.Fatalf("expected xref graph for commit %s", commitHash)
	}

	defs, err := db.FindXRefDefinitionsByName(ctx, repo.ID, commitHash, "ProcessOrder")
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 1 || defs[0].EntityID != calleeID {
		t.Fatalf("expected ProcessOrder definition, got %+v", defs)
	}

	def, err := db.GetXRefDefinition(ctx, repo.ID, commitHash, callerID)
	if err != nil {
		t.Fatal(err)
	}
	if def.Name != "Caller" {
		t.Fatalf("expected caller definition, got %+v", def)
	}

	outgoing, err := db.ListXRefEdgesFrom(ctx, repo.ID, commitHash, callerID, "call")
	if err != nil {
		t.Fatal(err)
	}
	if len(outgoing) != 1 || outgoing[0].TargetEntityID != calleeID {
		t.Fatalf("expected outgoing call edge, got %+v", outgoing)
	}

	incoming, err := db.ListXRefEdgesTo(ctx, repo.ID, commitHash, calleeID, "call")
	if err != nil {
		t.Fatal(err)
	}
	if len(incoming) != 1 || incoming[0].SourceEntityID != callerID {
		t.Fatalf("expected incoming call edge, got %+v", incoming)
	}

	err = db.SetCommitXRefGraph(ctx, repo.ID, commitHash,
		[]models.XRefDefinition{
			{
				EntityID:    calleeID,
				File:        "main.go",
				PackageName: "main",
				Kind:        "function",
				Name:        "ProcessOrder",
				StartLine:   20,
				EndLine:     24,
				Callable:    true,
			},
		},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}

	outgoing, err = db.ListXRefEdgesFrom(ctx, repo.ID, commitHash, callerID, "call")
	if err != nil {
		t.Fatal(err)
	}
	if len(outgoing) != 0 {
		t.Fatalf("expected replaced graph to clear outgoing edges, got %+v", outgoing)
	}
}

func TestSQLiteEntityIndexStorageAndSearch(t *testing.T) {
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
	entries := []models.EntityIndexEntry{
		{
			RepoID:     repo.ID,
			CommitHash: commitHash,
			FilePath:   "main.go",
			SymbolKey:  "sym-1",
			Kind:       "function",
			Name:       "ProcessOrder",
			Signature:  "func ProcessOrder()",
			StartLine:  10,
			EndLine:    14,
		},
		{
			RepoID:     repo.ID,
			CommitHash: commitHash,
			FilePath:   "main.go",
			SymbolKey:  "sym-2",
			Kind:       "function",
			Name:       "ValidateOrder",
			Signature:  "func ValidateOrder()",
			StartLine:  16,
			EndLine:    18,
		},
		{
			RepoID:     repo.ID,
			CommitHash: commitHash,
			FilePath:   "service.go",
			SymbolKey:  "sym-3",
			Kind:       "type",
			Name:       "OrderService",
			Signature:  "type OrderService struct{}",
			StartLine:  2,
			EndLine:    5,
		},
		{
			RepoID:     repo.ID,
			CommitHash: commitHash,
			FilePath:   "http.go",
			SymbolKey:  "sym-4",
			Kind:       "type",
			Name:       "HttpClient",
			Signature:  "type HttpClient struct{}",
			StartLine:  3,
			EndLine:    6,
		},
	}
	if err := db.SetEntityIndexEntries(ctx, repo.ID, commitHash, entries); err != nil {
		t.Fatal(err)
	}

	has, err := db.HasEntityIndexForCommit(ctx, repo.ID, commitHash)
	if err != nil {
		t.Fatal(err)
	}
	if !has {
		t.Fatalf("expected commit %s to have entity index marker", commitHash)
	}

	listed, err := db.ListEntityIndexEntriesByCommit(ctx, repo.ID, commitHash, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != len(entries) {
		t.Fatalf("expected %d listed entries, got %d", len(entries), len(listed))
	}

	results, err := db.SearchEntityIndexEntries(ctx, repo.ID, commitHash, "Order", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results for Order, got %+v", results)
	}

	functions, err := db.SearchEntityIndexEntries(ctx, repo.ID, commitHash, "Order", "function", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(functions) != 2 {
		t.Fatalf("expected 2 function results for Order, got %+v", functions)
	}

	if err := db.SetEntityIndexEntries(ctx, repo.ID, commitHash, nil); err != nil {
		t.Fatal(err)
	}
	afterReplace, err := db.ListEntityIndexEntriesByCommit(ctx, repo.ID, commitHash, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(afterReplace) != 0 {
		t.Fatalf("expected replacement with nil entries to clear rows, got %+v", afterReplace)
	}

	has, err = db.HasEntityIndexForCommit(ctx, repo.ID, commitHash)
	if err != nil {
		t.Fatal(err)
	}
	if !has {
		t.Fatalf("expected commit marker to remain after empty replacement for %s", commitHash)
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

func TestSQLiteIndexingJobLifecycle(t *testing.T) {
	db, ctx, repoID := setupSQLiteIndexingRepo(t)
	commitHash := strings.Repeat("a", 64)

	job := &models.IndexingJob{
		RepoID:      repoID,
		CommitHash:  commitHash,
		JobType:     models.IndexJobTypeCommitIndex,
		Status:      models.IndexJobQueued,
		MaxAttempts: 2,
	}
	if err := db.EnqueueIndexingJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	if job.ID == 0 {
		t.Fatal("expected indexing job id to be set")
	}
	if job.Status != models.IndexJobQueued {
		t.Fatalf("expected queued status after enqueue, got %q", job.Status)
	}

	claimed, err := db.ClaimIndexingJob(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if claimed == nil {
		t.Fatal("expected claimed indexing job")
	}
	if claimed.ID != job.ID {
		t.Fatalf("expected claimed id %d, got %d", job.ID, claimed.ID)
	}
	if claimed.Status != models.IndexJobInProgress {
		t.Fatalf("expected in_progress status after claim, got %q", claimed.Status)
	}
	if claimed.AttemptCount != 1 {
		t.Fatalf("expected attempt_count 1 after first claim, got %d", claimed.AttemptCount)
	}
	if claimed.StartedAt == nil {
		t.Fatal("expected started_at to be set after claim")
	}

	emptyClaim, err := db.ClaimIndexingJob(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if emptyClaim != nil {
		t.Fatal("expected queue to be empty after single claim")
	}

	if err := db.CompleteIndexingJob(ctx, claimed.ID, models.IndexJobCompleted, ""); err != nil {
		t.Fatal(err)
	}
	status, err := db.GetIndexingJobStatus(ctx, repoID, commitHash)
	if err != nil {
		t.Fatal(err)
	}
	if status == nil {
		t.Fatal("expected persisted indexing job status")
	}
	if status.Status != models.IndexJobCompleted {
		t.Fatalf("expected completed status, got %q", status.Status)
	}
	if status.CompletedAt == nil {
		t.Fatal("expected completed_at to be set")
	}
}

func TestSQLiteIndexingJobRetryAndFailureTransitions(t *testing.T) {
	db, ctx, repoID := setupSQLiteIndexingRepo(t)
	commitHash := strings.Repeat("b", 64)

	job := &models.IndexingJob{
		RepoID:      repoID,
		CommitHash:  commitHash,
		JobType:     models.IndexJobTypeCommitIndex,
		Status:      models.IndexJobQueued,
		MaxAttempts: 2,
	}
	if err := db.EnqueueIndexingJob(ctx, job); err != nil {
		t.Fatal(err)
	}

	first, err := db.ClaimIndexingJob(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if first == nil {
		t.Fatal("expected first claim to return job")
	}

	retryAt := time.Now().UTC().Add(-time.Second)
	if err := db.RequeueIndexingJob(ctx, first.ID, "temporary failure", retryAt); err != nil {
		t.Fatal(err)
	}

	queued, err := db.GetIndexingJobStatus(ctx, repoID, commitHash)
	if err != nil {
		t.Fatal(err)
	}
	if queued == nil {
		t.Fatal("expected queued indexing job after retry")
	}
	if queued.Status != models.IndexJobQueued {
		t.Fatalf("expected queued status after requeue, got %q", queued.Status)
	}
	if queued.AttemptCount != 1 {
		t.Fatalf("expected attempt_count to remain 1 after requeue, got %d", queued.AttemptCount)
	}
	if queued.LastError != "temporary failure" {
		t.Fatalf("expected last_error to be persisted, got %q", queued.LastError)
	}

	second, err := db.ClaimIndexingJob(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if second == nil {
		t.Fatal("expected second claim to return job")
	}
	if second.AttemptCount != 2 {
		t.Fatalf("expected attempt_count 2 on second claim, got %d", second.AttemptCount)
	}

	if err := db.RequeueIndexingJob(ctx, second.ID, "terminal failure", retryAt); err != nil {
		t.Fatal(err)
	}
	final, err := db.GetIndexingJobStatus(ctx, repoID, commitHash)
	if err != nil {
		t.Fatal(err)
	}
	if final == nil {
		t.Fatal("expected final indexing job status")
	}
	if final.Status != models.IndexJobFailed {
		t.Fatalf("expected failed status after max attempts, got %q", final.Status)
	}
	if final.CompletedAt == nil {
		t.Fatal("expected completed_at to be set for terminal failure")
	}
	if final.LastError != "terminal failure" {
		t.Fatalf("expected terminal last_error, got %q", final.LastError)
	}

	none, err := db.ClaimIndexingJob(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if none != nil {
		t.Fatal("expected no claimable job after terminal failure")
	}
}

func setupSQLiteIndexingRepo(t *testing.T) (*SQLiteDB, context.Context, int64) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := OpenSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	user := &models.User{Username: "queue-user", Email: "queue@example.com", PasswordHash: "x"}
	if err := db.CreateUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	repo := &models.Repository{
		OwnerUserID:   &user.ID,
		Name:          "queue-repo",
		DefaultBranch: "main",
		StoragePath:   "pending",
	}
	if err := db.CreateRepository(ctx, repo); err != nil {
		t.Fatal(err)
	}
	return db, ctx, repo.ID
}
