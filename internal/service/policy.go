package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	gotdiff "github.com/odvcencio/got/pkg/diff"
	"github.com/odvcencio/got/pkg/object"
	"github.com/odvcencio/gothub/internal/gotstore"
	"github.com/odvcencio/gothub/internal/models"
	"github.com/odvcencio/gts-suite/pkg/index"
	"github.com/odvcencio/gts-suite/pkg/structdiff"
	"github.com/odvcencio/gts-suite/pkg/xref"
)

type MergeGateResult struct {
	Allowed bool     `json:"allowed"`
	Reasons []string `json:"reasons,omitempty"`
}

func (s *PRService) UpsertBranchProtectionRule(ctx context.Context, rule *models.BranchProtectionRule) error {
	rule.RequiredChecks = normalizeChecks(rule.RequiredChecks)
	rule.RequiredChecksCSV = strings.Join(rule.RequiredChecks, ",")
	if rule.RequiredApprovals <= 0 {
		rule.RequiredApprovals = 1
	}
	if err := s.db.UpsertBranchProtectionRule(ctx, rule); err != nil {
		return err
	}
	rule.RequiredChecks = parseChecksCSV(rule.RequiredChecksCSV)
	return nil
}

func (s *PRService) GetBranchProtectionRule(ctx context.Context, repoID int64, branch string) (*models.BranchProtectionRule, error) {
	rule, err := s.db.GetBranchProtectionRule(ctx, repoID, branch)
	if err != nil {
		return nil, err
	}
	rule.RequiredChecks = parseChecksCSV(rule.RequiredChecksCSV)
	return rule, nil
}

func (s *PRService) DeleteBranchProtectionRule(ctx context.Context, repoID int64, branch string) error {
	return s.db.DeleteBranchProtectionRule(ctx, repoID, branch)
}

func (s *PRService) UpsertPRCheckRun(ctx context.Context, run *models.PRCheckRun) error {
	run.Name = strings.TrimSpace(run.Name)
	run.Status = normalizeStatus(run.Status)
	run.Conclusion = strings.TrimSpace(strings.ToLower(run.Conclusion))
	return s.db.UpsertPRCheckRun(ctx, run)
}

func (s *PRService) ListPRCheckRuns(ctx context.Context, prID int64) ([]models.PRCheckRun, error) {
	return s.db.ListPRCheckRuns(ctx, prID)
}

// EvaluateBranchUpdateGate evaluates branch-protection checks that can be
// enforced for direct ref updates (pushes).
func (s *PRService) EvaluateBranchUpdateGate(ctx context.Context, repoID int64, branch string, oldHead, newHead object.Hash) ([]string, error) {
	if strings.TrimSpace(branch) == "" || newHead == "" {
		return nil, nil
	}

	rule, err := s.GetBranchProtectionRule(ctx, repoID, branch)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if !rule.Enabled {
		return nil, nil
	}

	store, err := s.repoSvc.OpenStoreByID(ctx, repoID)
	if err != nil {
		return nil, err
	}

	var reasons []string
	if rule.RequireSignedCommits {
		signedReasons, err := s.evaluateSignedCommitRange(ctx, store, newHead, oldHead)
		if err != nil {
			return nil, err
		}
		reasons = append(reasons, signedReasons...)
	}

	sort.Strings(reasons)
	return reasons, nil
}

func (s *PRService) EvaluateMergeGate(ctx context.Context, repoID int64, pr *models.PullRequest) (*MergeGateResult, error) {
	result := &MergeGateResult{Allowed: true}

	rule, err := s.GetBranchProtectionRule(ctx, repoID, pr.TargetBranch)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return result, nil
		}
		return nil, err
	}
	if !rule.Enabled {
		return result, nil
	}

	var reviews []models.PRReview
	if rule.RequireApprovals || rule.RequireEntityOwnerApproval {
		reviews, err = s.db.ListPRReviews(ctx, pr.ID)
		if err != nil {
			return nil, fmt.Errorf("list reviews: %w", err)
		}
	}

	if rule.RequireApprovals {
		approvals, hasChangesRequested := evaluateApprovals(reviews, pr.AuthorID)
		if hasChangesRequested {
			result.Reasons = append(result.Reasons, "changes requested review is blocking merge")
		}
		if approvals < rule.RequiredApprovals {
			result.Reasons = append(result.Reasons, fmt.Sprintf("requires %d approving review(s), currently %d", rule.RequiredApprovals, approvals))
		}
	}

	if rule.RequireEntityOwnerApproval {
		reasons, evalErr := s.evaluateEntityOwnerApprovals(ctx, repoID, pr, reviews)
		if evalErr != nil {
			result.Reasons = append(result.Reasons, fmt.Sprintf("unable to evaluate entity owner approvals: %v", evalErr))
		}
		result.Reasons = append(result.Reasons, reasons...)
	}

	if rule.RequireLintPass {
		reasons, evalErr := s.evaluateLintPass(ctx, repoID, pr)
		if evalErr != nil {
			result.Reasons = append(result.Reasons, fmt.Sprintf("unable to evaluate lint gate: %v", evalErr))
		}
		result.Reasons = append(result.Reasons, reasons...)
	}

	if rule.RequireNoNewDeadCode {
		reasons, evalErr := s.evaluateNoNewDeadCode(ctx, repoID, pr)
		if evalErr != nil {
			result.Reasons = append(result.Reasons, fmt.Sprintf("unable to evaluate dead-code gate: %v", evalErr))
		}
		result.Reasons = append(result.Reasons, reasons...)
	}

	if rule.RequireSignedCommits {
		reasons, evalErr := s.evaluateSignedCommits(ctx, repoID, pr)
		if evalErr != nil {
			result.Reasons = append(result.Reasons, fmt.Sprintf("unable to evaluate signed-commit gate: %v", evalErr))
		}
		result.Reasons = append(result.Reasons, reasons...)
	}

	if rule.RequireStatusChecks {
		runs, err := s.db.ListPRCheckRuns(ctx, pr.ID)
		if err != nil {
			return nil, fmt.Errorf("list check runs: %w", err)
		}
		missing := evaluateRequiredChecks(rule.RequiredChecks, runs)
		for _, reason := range missing {
			result.Reasons = append(result.Reasons, reason)
		}
	}

	result.Allowed = len(result.Reasons) == 0
	return result, nil
}

func (s *PRService) evaluateEntityOwnerApprovals(ctx context.Context, repoID int64, pr *models.PullRequest, reviews []models.PRReview) ([]string, error) {
	store, err := s.repoSvc.OpenStoreByID(ctx, repoID)
	if err != nil {
		return nil, err
	}

	cfg, err := loadGotOwnersForBranch(store, pr.TargetBranch)
	if err != nil {
		return nil, err
	}
	if len(cfg.rules) == 0 {
		return nil, nil
	}

	changes, err := listPREntityChanges(store, pr.SourceBranch, pr.TargetBranch)
	if err != nil {
		return nil, err
	}
	if len(changes) == 0 {
		return nil, nil
	}

	approvedUsers := make(map[string]bool, len(reviews))
	for authorID, review := range latestReviewsByAuthor(reviews) {
		if authorID == pr.AuthorID {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(review.State), "approved") {
			u := strings.ToLower(strings.TrimSpace(review.AuthorName))
			if u != "" {
				approvedUsers[u] = true
			}
		}
	}

	reasons := make([]string, 0)
	seenReasons := make(map[string]bool)
	for _, ch := range changes {
		owners := cfg.ownersForChange(ch)
		if len(owners) == 0 {
			continue
		}

		requiredUsers, unresolvedTeams := cfg.resolveOwners(owners)
		if len(unresolvedTeams) > 0 {
			reason := fmt.Sprintf("entity %q references undefined team(s): %s", ch.Key, formatTeamRefs(unresolvedTeams))
			if !seenReasons[reason] {
				seenReasons[reason] = true
				reasons = append(reasons, reason)
			}
		}
		if len(requiredUsers) == 0 {
			continue
		}

		hasOwnerApproval := false
		for _, user := range requiredUsers {
			if approvedUsers[user] {
				hasOwnerApproval = true
				break
			}
		}
		if hasOwnerApproval {
			continue
		}

		reason := fmt.Sprintf("entity %q requires approval from %s", ch.Key, formatUserMentions(requiredUsers))
		if !seenReasons[reason] {
			seenReasons[reason] = true
			reasons = append(reasons, reason)
		}
	}

	sort.Strings(reasons)
	return reasons, nil
}

func (s *PRService) evaluateLintPass(ctx context.Context, repoID int64, pr *models.PullRequest) ([]string, error) {
	store, err := s.repoSvc.OpenStoreByID(ctx, repoID)
	if err != nil {
		return nil, err
	}

	srcHash, err := store.Refs.Get("heads/" + pr.SourceBranch)
	if err != nil {
		return nil, fmt.Errorf("resolve source branch %q: %w", pr.SourceBranch, err)
	}
	tgtHash, err := store.Refs.Get("heads/" + pr.TargetBranch)
	if err != nil {
		return nil, fmt.Errorf("resolve target branch %q: %w", pr.TargetBranch, err)
	}
	srcCommit, err := store.Objects.ReadCommit(srcHash)
	if err != nil {
		return nil, fmt.Errorf("read source commit %q: %w", string(srcHash), err)
	}
	tgtCommit, err := store.Objects.ReadCommit(tgtHash)
	if err != nil {
		return nil, fmt.Errorf("read target commit %q: %w", string(tgtHash), err)
	}

	srcFiles, err := flattenTree(store.Objects, srcCommit.TreeHash, "")
	if err != nil {
		return nil, fmt.Errorf("flatten source tree: %w", err)
	}
	tgtFiles, err := flattenTree(store.Objects, tgtCommit.TreeHash, "")
	if err != nil {
		return nil, fmt.Errorf("flatten target tree: %w", err)
	}

	srcMap := make(map[string]FileEntry, len(srcFiles))
	for _, f := range srcFiles {
		srcMap[f.Path] = f
	}
	tgtMap := make(map[string]FileEntry, len(tgtFiles))
	for _, f := range tgtFiles {
		tgtMap[f.Path] = f
	}

	changedSet := make(map[string]struct{})
	for path, srcEntry := range srcMap {
		if tgtEntry, ok := tgtMap[path]; !ok || tgtEntry.BlobHash != srcEntry.BlobHash {
			changedSet[path] = struct{}{}
		}
	}
	for path := range tgtMap {
		if _, ok := srcMap[path]; !ok {
			changedSet[path] = struct{}{}
		}
	}
	if len(changedSet) == 0 {
		return nil, nil
	}

	tmpDir, err := os.MkdirTemp("", "gothub-pr-lint-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)

	sourceDataByPath := make(map[string][]byte, len(srcFiles))
	for _, fe := range srcFiles {
		if strings.TrimSpace(fe.BlobHash) == "" {
			continue
		}
		blob, readErr := store.Objects.ReadBlob(object.Hash(fe.BlobHash))
		if readErr != nil {
			continue
		}
		fullPath := filepath.Join(tmpDir, fe.Path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(fullPath, blob.Data, 0o644); err != nil {
			return nil, err
		}
		if _, tracked := changedSet[fe.Path]; tracked {
			sourceDataByPath[fe.Path] = blob.Data
		}
	}

	builder := index.NewBuilder()
	idx, err := builder.BuildPath(tmpDir)
	if err != nil {
		return nil, err
	}

	parseErrByPath := make(map[string]string, len(idx.Errors))
	for _, pe := range idx.Errors {
		p := strings.TrimPrefix(filepath.ToSlash(strings.TrimSpace(pe.Path)), "./")
		if p == "" {
			continue
		}
		parseErrByPath[p] = strings.TrimSpace(pe.Error)
	}
	symbolCountByPath := make(map[string]int, len(idx.Files))
	for _, f := range idx.Files {
		p := strings.TrimPrefix(filepath.ToSlash(strings.TrimSpace(f.Path)), "./")
		if p == "" {
			continue
		}
		symbolCountByPath[p] = len(f.Symbols)
	}

	reasons := make([]string, 0)
	seen := make(map[string]bool)
	for path := range changedSet {
		if parseErr, ok := parseErrByPath[path]; ok {
			reason := fmt.Sprintf("lint parse error in %s: %s", path, parseErr)
			if !seen[reason] {
				seen[reason] = true
				reasons = append(reasons, reason)
			}
			continue
		}
		if _, existsInSource := srcMap[path]; !existsInSource {
			continue
		}
		if symbolCountByPath[path] == 0 && hasLikelySymbolSyntax(sourceDataByPath[path]) {
			reason := fmt.Sprintf("lint symbol extraction failed in %s", path)
			if !seen[reason] {
				seen[reason] = true
				reasons = append(reasons, reason)
			}
		}
	}
	sort.Strings(reasons)
	return reasons, nil
}

func (s *PRService) evaluateNoNewDeadCode(ctx context.Context, repoID int64, pr *models.PullRequest) ([]string, error) {
	if s.codeIntelSvc == nil {
		return nil, fmt.Errorf("code intelligence service is not configured")
	}

	repo, err := s.repoSvc.GetByID(ctx, repoID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(repo.OwnerName) == "" {
		return nil, fmt.Errorf("repository owner is unavailable")
	}

	baseIdx, err := s.codeIntelSvc.BuildIndex(ctx, repo.OwnerName, repo.Name, pr.TargetBranch)
	if err != nil {
		return nil, fmt.Errorf("build base index: %w", err)
	}
	headIdx, err := s.codeIntelSvc.BuildIndex(ctx, repo.OwnerName, repo.Name, pr.SourceBranch)
	if err != nil {
		return nil, fmt.Errorf("build head index: %w", err)
	}

	report := structdiff.Compare(baseIdx, headIdx)
	if len(report.AddedSymbols) == 0 {
		return nil, nil
	}

	graph, err := xref.Build(headIdx)
	if err != nil {
		return nil, fmt.Errorf("build call graph: %w", err)
	}
	defByKey := make(map[string]xref.Definition, len(graph.Definitions))
	for _, def := range graph.Definitions {
		if !def.Callable {
			continue
		}
		defByKey[symbolLookupKey(def.File, def.Kind, def.Receiver, def.Name)] = def
	}

	reasons := make([]string, 0)
	seen := make(map[string]bool)
	for _, added := range report.AddedSymbols {
		def, ok := defByKey[symbolLookupKey(added.File, added.Kind, added.Receiver, added.Name)]
		if !ok || shouldIgnoreDeadCodeCandidate(def) {
			continue
		}
		if graph.IncomingCount(def.ID) > 0 || graph.OutgoingCount(def.ID) > 0 {
			continue
		}
		reason := fmt.Sprintf("new callable appears unused: %s (%s)", def.Name, def.File)
		if !seen[reason] {
			seen[reason] = true
			reasons = append(reasons, reason)
		}
	}
	sort.Strings(reasons)
	return reasons, nil
}

func (s *PRService) evaluateSignedCommits(ctx context.Context, repoID int64, pr *models.PullRequest) ([]string, error) {
	store, err := s.repoSvc.OpenStoreByID(ctx, repoID)
	if err != nil {
		return nil, err
	}

	sourceHead, err := store.Refs.Get("heads/" + pr.SourceBranch)
	if err != nil {
		return nil, fmt.Errorf("resolve source branch %q: %w", pr.SourceBranch, err)
	}
	targetHead, err := store.Refs.Get("heads/" + pr.TargetBranch)
	if err != nil {
		return nil, fmt.Errorf("resolve target branch %q: %w", pr.TargetBranch, err)
	}

	return s.evaluateSignedCommitRange(ctx, store, sourceHead, targetHead)
}

func (s *PRService) evaluateSignedCommitRange(ctx context.Context, store *gotstore.RepoStore, sourceHead, targetHead object.Hash) ([]string, error) {
	commitHashes, err := sourceOnlyCommits(store.Objects, sourceHead, targetHead)
	if err != nil {
		return nil, err
	}
	if len(commitHashes) == 0 {
		return nil, nil
	}

	reasons := make([]string, 0)
	for _, h := range commitHashes {
		commit, err := store.Objects.ReadCommit(h)
		if err != nil {
			return nil, fmt.Errorf("read commit %q: %w", string(h), err)
		}
		verified, _, err := verifyCommitSignature(ctx, s.db, commit)
		if err != nil {
			return nil, fmt.Errorf("verify commit %q signature: %w", string(h), err)
		}
		if verified {
			continue
		}
		reasons = append(reasons, fmt.Sprintf("commit %s is not signed or signature is unverified", shortHash(h)))
	}
	sort.Strings(reasons)
	return reasons, nil
}

func sourceOnlyCommits(store *object.Store, sourceHead, targetHead object.Hash) ([]object.Hash, error) {
	targetReachable, err := collectReachableCommits(store, targetHead)
	if err != nil {
		return nil, err
	}

	queue := []object.Hash{sourceHead}
	visited := make(map[object.Hash]bool)
	out := make([]object.Hash, 0)
	for len(queue) > 0 {
		h := queue[0]
		queue = queue[1:]
		if h == "" || visited[h] {
			continue
		}
		visited[h] = true
		if targetReachable[h] {
			continue
		}

		commit, err := store.ReadCommit(h)
		if err != nil {
			return nil, fmt.Errorf("read source commit %q: %w", string(h), err)
		}
		out = append(out, h)
		for _, p := range commit.Parents {
			if !visited[p] {
				queue = append(queue, p)
			}
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}

func collectReachableCommits(store *object.Store, head object.Hash) (map[object.Hash]bool, error) {
	reachable := make(map[object.Hash]bool)
	queue := []object.Hash{head}
	for len(queue) > 0 {
		h := queue[0]
		queue = queue[1:]
		if h == "" || reachable[h] {
			continue
		}
		reachable[h] = true
		commit, err := store.ReadCommit(h)
		if err != nil {
			return nil, fmt.Errorf("read target commit %q: %w", string(h), err)
		}
		for _, p := range commit.Parents {
			if !reachable[p] {
				queue = append(queue, p)
			}
		}
	}
	return reachable, nil
}

func shortHash(h object.Hash) string {
	s := string(h)
	if len(s) <= 12 {
		return s
	}
	return s[:12]
}

func loadGotOwnersForBranch(store *gotstore.RepoStore, branch string) (*gotOwnersConfig, error) {
	headHash, err := store.Refs.Get("heads/" + branch)
	if err != nil {
		return nil, fmt.Errorf("resolve target branch %q: %w", branch, err)
	}
	commit, err := store.Objects.ReadCommit(headHash)
	if err != nil {
		return nil, fmt.Errorf("read target commit %q: %w", string(headHash), err)
	}

	blobHash, err := findBlob(store.Objects, commit.TreeHash, ".gotowners")
	if err != nil {
		if isMissingFileErr(err) {
			return &gotOwnersConfig{teams: make(map[string][]string)}, nil
		}
		return nil, fmt.Errorf("read .gotowners: %w", err)
	}
	blob, err := store.Objects.ReadBlob(blobHash)
	if err != nil {
		return nil, fmt.Errorf("read .gotowners blob: %w", err)
	}
	return parseGotOwners(blob.Data)
}

func listPREntityChanges(store *gotstore.RepoStore, sourceBranch, targetBranch string) ([]ownerEntityChange, error) {
	srcHash, err := store.Refs.Get("heads/" + sourceBranch)
	if err != nil {
		return nil, fmt.Errorf("resolve source branch %q: %w", sourceBranch, err)
	}
	tgtHash, err := store.Refs.Get("heads/" + targetBranch)
	if err != nil {
		return nil, fmt.Errorf("resolve target branch %q: %w", targetBranch, err)
	}

	srcCommit, err := store.Objects.ReadCommit(srcHash)
	if err != nil {
		return nil, fmt.Errorf("read source commit %q: %w", string(srcHash), err)
	}
	tgtCommit, err := store.Objects.ReadCommit(tgtHash)
	if err != nil {
		return nil, fmt.Errorf("read target commit %q: %w", string(tgtHash), err)
	}

	srcFiles, err := flattenTree(store.Objects, srcCommit.TreeHash, "")
	if err != nil {
		return nil, fmt.Errorf("flatten source tree: %w", err)
	}
	tgtFiles, err := flattenTree(store.Objects, tgtCommit.TreeHash, "")
	if err != nil {
		return nil, fmt.Errorf("flatten target tree: %w", err)
	}

	srcMap := make(map[string]FileEntry, len(srcFiles))
	for _, f := range srcFiles {
		srcMap[f.Path] = f
	}
	tgtMap := make(map[string]FileEntry, len(tgtFiles))
	for _, f := range tgtFiles {
		tgtMap[f.Path] = f
	}

	pathSet := make(map[string]struct{}, len(srcMap)+len(tgtMap))
	for path, srcEntry := range srcMap {
		if tgtEntry, ok := tgtMap[path]; !ok || tgtEntry.BlobHash != srcEntry.BlobHash {
			pathSet[path] = struct{}{}
		}
	}
	for path := range tgtMap {
		if _, ok := srcMap[path]; !ok {
			pathSet[path] = struct{}{}
		}
	}

	paths := make([]string, 0, len(pathSet))
	for p := range pathSet {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	changes := make([]ownerEntityChange, 0)
	seen := make(map[string]bool)
	for _, path := range paths {
		srcEntry, hasSrc := srcMap[path]
		tgtEntry, hasTgt := tgtMap[path]

		var srcData, tgtData []byte
		if hasSrc && srcEntry.BlobHash != "" {
			srcData, err = readBlobData(store.Objects, object.Hash(srcEntry.BlobHash))
			if err != nil {
				continue
			}
		}
		if hasTgt && tgtEntry.BlobHash != "" {
			tgtData, err = readBlobData(store.Objects, object.Hash(tgtEntry.BlobHash))
			if err != nil {
				continue
			}
		}

		fd, err := gotdiff.DiffFiles(path, tgtData, srcData)
		if err != nil {
			continue
		}
		for _, c := range fd.Changes {
			key := path + "\x00" + c.Key
			if seen[key] {
				continue
			}
			seen[key] = true

			ch := ownerEntityChange{
				Path: path,
				Key:  c.Key,
			}
			if c.After != nil {
				ch.Name = c.After.Name
				ch.DeclKind = c.After.DeclKind
				ch.Receiver = c.After.Receiver
			} else if c.Before != nil {
				ch.Name = c.Before.Name
				ch.DeclKind = c.Before.DeclKind
				ch.Receiver = c.Before.Receiver
			}
			changes = append(changes, ch)
		}
	}
	sort.Slice(changes, func(i, j int) bool {
		if changes[i].Path == changes[j].Path {
			return changes[i].Key < changes[j].Key
		}
		return changes[i].Path < changes[j].Path
	})
	return changes, nil
}

func isMissingFileErr(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "file not found")
}

func formatUserMentions(users []string) string {
	if len(users) == 0 {
		return ""
	}
	mentions := make([]string, len(users))
	for i, u := range users {
		mentions[i] = "@" + u
	}
	return strings.Join(mentions, " or ")
}

func formatTeamRefs(teams []string) string {
	if len(teams) == 0 {
		return ""
	}
	refs := make([]string, len(teams))
	for i, t := range teams {
		refs[i] = "@team/" + t
	}
	return strings.Join(refs, ", ")
}

func hasLikelySymbolSyntax(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	src := strings.ToLower(string(data))
	keywords := [...]string{
		"func ", "function ", "class ", "interface ", "struct ", "enum ", "def ", "type ",
	}
	for _, kw := range keywords {
		if strings.Contains(src, kw) {
			return true
		}
	}
	return false
}

func symbolLookupKey(file, kind, receiver, name string) string {
	return file + "|" + kind + "|" + receiver + "|" + name
}

func shouldIgnoreDeadCodeCandidate(def xref.Definition) bool {
	name := strings.TrimSpace(def.Name)
	if name == "" {
		return true
	}
	if name == "init" {
		return true
	}
	if name == "main" && filepath.Base(def.File) == "main.go" {
		return true
	}
	if strings.HasSuffix(def.File, "_test.go") && strings.HasPrefix(name, "Test") {
		return true
	}
	return false
}

func parseChecksCSV(csv string) []string {
	if strings.TrimSpace(csv) == "" {
		return nil
	}
	return normalizeChecks(strings.Split(csv, ","))
}

func normalizeChecks(checks []string) []string {
	seen := make(map[string]bool, len(checks))
	out := make([]string, 0, len(checks))
	for _, c := range checks {
		c = strings.TrimSpace(c)
		if c == "" || seen[c] {
			continue
		}
		seen[c] = true
		out = append(out, c)
	}
	return out
}

func normalizeStatus(status string) string {
	switch strings.TrimSpace(strings.ToLower(status)) {
	case "queued", "in_progress", "completed":
		return strings.TrimSpace(strings.ToLower(status))
	default:
		return "queued"
	}
}

func latestReviewsByAuthor(reviews []models.PRReview) map[int64]models.PRReview {
	latestByAuthor := make(map[int64]models.PRReview, len(reviews))
	for _, r := range reviews {
		prev, exists := latestByAuthor[r.AuthorID]
		if !exists || r.CreatedAt.After(prev.CreatedAt) {
			latestByAuthor[r.AuthorID] = r
		}
	}
	return latestByAuthor
}

func evaluateApprovals(reviews []models.PRReview, prAuthorID int64) (approvals int, hasChangesRequested bool) {
	for authorID, review := range latestReviewsByAuthor(reviews) {
		if authorID == prAuthorID {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(review.State)) {
		case "approved":
			approvals++
		case "changes_requested":
			hasChangesRequested = true
		}
	}
	return approvals, hasChangesRequested
}

func evaluateRequiredChecks(required []string, runs []models.PRCheckRun) []string {
	if len(required) == 0 {
		return nil
	}

	latestByName := make(map[string]models.PRCheckRun, len(runs))
	for _, run := range runs {
		if _, exists := latestByName[run.Name]; !exists {
			latestByName[run.Name] = run
		}
	}

	var reasons []string
	for _, name := range required {
		run, exists := latestByName[name]
		if !exists {
			reasons = append(reasons, fmt.Sprintf("required check %q has not run", name))
			continue
		}
		if run.Status != "completed" || strings.ToLower(strings.TrimSpace(run.Conclusion)) != "success" {
			if run.Status != "completed" {
				reasons = append(reasons, fmt.Sprintf("required check %q is %s", name, run.Status))
			} else {
				reasons = append(reasons, fmt.Sprintf("required check %q concluded %s", name, run.Conclusion))
			}
		}
	}
	return reasons
}
