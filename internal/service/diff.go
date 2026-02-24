package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/odvcencio/got/pkg/diff"
	"github.com/odvcencio/got/pkg/entity"
	"github.com/odvcencio/got/pkg/object"
	gotrepo "github.com/odvcencio/got/pkg/repo"
	"github.com/odvcencio/gothub/internal/database"
	"github.com/odvcencio/gothub/internal/entityutil"
)

// EntityInfo represents a single entity for API responses.
type EntityInfo struct {
	Key       string `json:"key"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	DeclKind  string `json:"decl_kind"`
	Receiver  string `json:"receiver,omitempty"`
	Signature string `json:"signature,omitempty"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	BodyHash  string `json:"body_hash"`
}

// EntityListResponse holds the entities for a file.
type EntityListResponse struct {
	Language string       `json:"language"`
	Path     string       `json:"path"`
	Entities []EntityInfo `json:"entities"`
}

// EntityChangeInfo represents a single entity-level change for API responses.
type EntityChangeInfo struct {
	Type           string      `json:"type"` // "added", "removed", "modified"
	Classification string      `json:"classification"`
	Key            string      `json:"key"`
	Before         *EntityInfo `json:"before,omitempty"`
	After          *EntityInfo `json:"after,omitempty"`
}

// FileDiffResponse holds the entity-level diff for a file.
type FileDiffResponse struct {
	Path    string             `json:"path"`
	Changes []EntityChangeInfo `json:"changes"`
}

// DiffSummaryCounts aggregates semantic diff classifications.
type DiffSummaryCounts struct {
	TotalChanges     int `json:"total_changes"`
	Additions        int `json:"additions"`
	Removals         int `json:"removals"`
	SignatureChanges int `json:"signature_changes"`
	BodyOnlyChanges  int `json:"body_only_changes"`
	OtherChanges     int `json:"other_changes"`
}

// DiffResponse holds diffs across multiple files.
type DiffResponse struct {
	Base    string                `json:"base"`
	Head    string                `json:"head"`
	Summary DiffSummaryCounts     `json:"summary"`
	Files   []FileDiffResponse    `json:"files"`
	Semver  *SemverRecommendation `json:"semver,omitempty"`
}

func (r DiffResponse) MarshalJSON() ([]byte, error) {
	type diffResponseAlias DiffResponse
	encoded := diffResponseAlias(r)
	encoded.Summary = summarizeSemanticChanges(encoded.Files)
	return json.Marshal(encoded)
}

// SemverRecommendation suggests the semantic-version bump between two refs.
type SemverRecommendation struct {
	Base            string   `json:"base"`
	Head            string   `json:"head"`
	Bump            string   `json:"bump"` // "none", "patch", "minor", "major"
	BreakingChanges []string `json:"breaking_changes,omitempty"`
	Features        []string `json:"features,omitempty"`
	Fixes           []string `json:"fixes,omitempty"`
}

// EntityHistoryHit is one entity occurrence in commit history.
type EntityHistoryHit struct {
	CommitHash string `json:"commit_hash"`
	StableID   string `json:"stable_id,omitempty"`
	Author     string `json:"author"`
	Timestamp  int64  `json:"timestamp"`
	Message    string `json:"message"`

	Path       string `json:"path"`
	EntityHash string `json:"entity_hash"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	DeclKind   string `json:"decl_kind"`
	Receiver   string `json:"receiver,omitempty"`
	BodyHash   string `json:"body_hash"`
}

// EntityLogHit is one commit that changed a selected entity key.
type EntityLogHit struct {
	CommitHash string `json:"commit_hash"`
	Author     string `json:"author"`
	Timestamp  int64  `json:"timestamp"`
	Message    string `json:"message"`
	Path       string `json:"path,omitempty"`
	Key        string `json:"key"`
}

// EntityBlameInfo is the newest attribution for a selected entity key.
type EntityBlameInfo struct {
	CommitHash string `json:"commit_hash"`
	Author     string `json:"author"`
	Timestamp  int64  `json:"timestamp"`
	Message    string `json:"message"`
	Path       string `json:"path,omitempty"`
	Key        string `json:"key"`
}

var ErrEntityNotFound = errors.New("entity not found")

type DiffService struct {
	repoSvc    *RepoService
	browseSvc  *BrowseService
	db         database.DB
	lineageSvc *EntityLineageService
}

func NewDiffService(repoSvc *RepoService, browseSvc *BrowseService, db database.DB, lineageSvc *EntityLineageService) *DiffService {
	return &DiffService{repoSvc: repoSvc, browseSvc: browseSvc, db: db, lineageSvc: lineageSvc}
}

// ExtractEntities extracts entities from a file at the given ref/path.
func (s *DiffService) ExtractEntities(ctx context.Context, owner, repo, ref, filePath string) (*EntityListResponse, error) {
	blob, err := s.browseSvc.GetBlob(ctx, owner, repo, ref, filePath)
	if err != nil {
		return nil, err
	}
	el, err := entity.Extract(filePath, blob.Data)
	if err != nil {
		return nil, fmt.Errorf("extract entities: %w", err)
	}
	return entityListToResponse(el), nil
}

// DiffRefs computes entity-level diffs between two refs (branches/commits).
func (s *DiffService) DiffRefs(ctx context.Context, owner, repo, baseRef, headRef string) (*DiffResponse, error) {
	store, err := s.repoSvc.OpenStore(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	baseHash, err := s.browseSvc.ResolveRef(ctx, owner, repo, baseRef)
	if err != nil {
		return nil, fmt.Errorf("resolve base ref: %w", err)
	}
	headHash, err := s.browseSvc.ResolveRef(ctx, owner, repo, headRef)
	if err != nil {
		return nil, fmt.Errorf("resolve head ref: %w", err)
	}

	baseCommit, err := store.Objects.ReadCommit(baseHash)
	if err != nil {
		return nil, fmt.Errorf("read base commit: %w", err)
	}
	headCommit, err := store.Objects.ReadCommit(headHash)
	if err != nil {
		return nil, fmt.Errorf("read head commit: %w", err)
	}

	// Flatten both trees to get all files
	baseFiles, err := flattenTree(store.Objects, baseCommit.TreeHash, "")
	if err != nil {
		return nil, fmt.Errorf("flatten base tree: %w", err)
	}
	headFiles, err := flattenTree(store.Objects, headCommit.TreeHash, "")
	if err != nil {
		return nil, fmt.Errorf("flatten head tree: %w", err)
	}

	// Index files by path
	baseMap := make(map[string]FileEntry, len(baseFiles))
	for _, f := range baseFiles {
		baseMap[f.Path] = f
	}
	headMap := make(map[string]FileEntry, len(headFiles))
	for _, f := range headFiles {
		headMap[f.Path] = f
	}

	// Find changed files
	var fileDiffs []FileDiffResponse

	// Files in head (modified or added)
	for path, headEntry := range headMap {
		baseEntry, exists := baseMap[path]
		if exists && baseEntry.BlobHash == headEntry.BlobHash {
			continue // unchanged
		}
		var baseData, headData []byte
		if exists {
			baseData, err = readBlobData(store.Objects, object.Hash(baseEntry.BlobHash))
			if err != nil {
				continue
			}
		}
		headData, err = readBlobData(store.Objects, object.Hash(headEntry.BlobHash))
		if err != nil {
			continue
		}
		fd, err := diff.DiffFiles(path, baseData, headData)
		if err != nil {
			continue
		}
		if len(fd.Changes) > 0 {
			fileDiffs = append(fileDiffs, fileDiffToResponse(fd))
		}
	}

	// Files only in base (deleted)
	for path, baseEntry := range baseMap {
		if _, exists := headMap[path]; !exists {
			baseData, err := readBlobData(store.Objects, object.Hash(baseEntry.BlobHash))
			if err != nil {
				continue
			}
			fd, err := diff.DiffFiles(path, baseData, nil)
			if err != nil {
				continue
			}
			if len(fd.Changes) > 0 {
				fileDiffs = append(fileDiffs, fileDiffToResponse(fd))
			}
		}
	}

	resp := &DiffResponse{
		Base:    string(baseHash),
		Head:    string(headHash),
		Summary: summarizeSemanticChanges(fileDiffs),
		Files:   fileDiffs,
	}
	if semverRec, semverErr := s.RecommendSemver(ctx, owner, repo, baseRef, headRef); semverErr == nil {
		resp.Semver = semverRec
	}
	return resp, nil
}

// RecommendSemver analyzes structural changes and suggests a semver bump.
func (s *DiffService) RecommendSemver(ctx context.Context, owner, repo, baseRef, headRef string) (*SemverRecommendation, error) {
	store, err := s.repoSvc.OpenStore(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	baseHash, err := s.browseSvc.ResolveRef(ctx, owner, repo, baseRef)
	if err != nil {
		return nil, fmt.Errorf("resolve base ref: %w", err)
	}
	headHash, err := s.browseSvc.ResolveRef(ctx, owner, repo, headRef)
	if err != nil {
		return nil, fmt.Errorf("resolve head ref: %w", err)
	}

	baseCommit, err := store.Objects.ReadCommit(baseHash)
	if err != nil {
		return nil, fmt.Errorf("read base commit: %w", err)
	}
	headCommit, err := store.Objects.ReadCommit(headHash)
	if err != nil {
		return nil, fmt.Errorf("read head commit: %w", err)
	}

	baseFiles, err := flattenTree(store.Objects, baseCommit.TreeHash, "")
	if err != nil {
		return nil, fmt.Errorf("flatten base tree: %w", err)
	}
	headFiles, err := flattenTree(store.Objects, headCommit.TreeHash, "")
	if err != nil {
		return nil, fmt.Errorf("flatten head tree: %w", err)
	}

	baseMap := make(map[string]FileEntry, len(baseFiles))
	for _, f := range baseFiles {
		baseMap[f.Path] = f
	}
	headMap := make(map[string]FileEntry, len(headFiles))
	for _, f := range headFiles {
		headMap[f.Path] = f
	}

	rec := &SemverRecommendation{
		Base: baseRef,
		Head: headRef,
		Bump: "none",
	}
	impact := semverNone
	seenBreaking := make(map[string]bool)
	seenFeatures := make(map[string]bool)
	seenFixes := make(map[string]bool)

	classify := func(path string, fd *diff.FileDiff) {
		for _, c := range fd.Changes {
			label := formatEntityChangeLabel(path, c)
			exported := isExportedEntity(c.Before) || isExportedEntity(c.After)

			switch c.Type {
			case diff.Removed:
				if exported {
					impact = maxSemverImpact(impact, semverMajor)
					addUniqueStringSlice(&rec.BreakingChanges, seenBreaking, "removed "+label)
				} else {
					impact = maxSemverImpact(impact, semverPatch)
					addUniqueStringSlice(&rec.Fixes, seenFixes, "removed internal "+label)
				}
			case diff.Added:
				if exported {
					impact = maxSemverImpact(impact, semverMinor)
					addUniqueStringSlice(&rec.Features, seenFeatures, "added "+label)
				} else {
					impact = maxSemverImpact(impact, semverPatch)
					addUniqueStringSlice(&rec.Fixes, seenFixes, "added internal "+label)
				}
			case diff.Modified:
				if exported && isBreakingSignatureChange(c.Before, c.After) {
					impact = maxSemverImpact(impact, semverMajor)
					addUniqueStringSlice(&rec.BreakingChanges, seenBreaking, "changed signature "+label)
				} else {
					impact = maxSemverImpact(impact, semverPatch)
					addUniqueStringSlice(&rec.Fixes, seenFixes, "modified "+label)
				}
			}
		}
	}

	for path, headEntry := range headMap {
		baseEntry, exists := baseMap[path]
		if exists && baseEntry.BlobHash == headEntry.BlobHash {
			continue
		}
		var baseData, headData []byte
		if exists {
			baseData, err = readBlobData(store.Objects, object.Hash(baseEntry.BlobHash))
			if err != nil {
				continue
			}
		}
		headData, err = readBlobData(store.Objects, object.Hash(headEntry.BlobHash))
		if err != nil {
			continue
		}
		fd, err := diff.DiffFiles(path, baseData, headData)
		if err != nil {
			continue
		}
		classify(path, fd)
	}

	for path, baseEntry := range baseMap {
		if _, exists := headMap[path]; exists {
			continue
		}
		baseData, err := readBlobData(store.Objects, object.Hash(baseEntry.BlobHash))
		if err != nil {
			continue
		}
		fd, err := diff.DiffFiles(path, baseData, nil)
		if err != nil {
			continue
		}
		classify(path, fd)
	}

	rec.Bump = impact.String()
	sort.Strings(rec.BreakingChanges)
	sort.Strings(rec.Features)
	sort.Strings(rec.Fixes)
	return rec, nil
}

// EntityHistory returns commits (newest-first graph walk) where matching entities occur.
// At least one of stableID, name, or bodyHash must be provided.
func (s *DiffService) EntityHistory(ctx context.Context, owner, repo, ref, stableID, name, bodyHash string, limit, offset int) ([]EntityHistoryHit, error) {
	stableID = strings.TrimSpace(stableID)
	name = strings.TrimSpace(name)
	bodyHash = strings.TrimSpace(bodyHash)
	if stableID == "" && name == "" && bodyHash == "" {
		return nil, fmt.Errorf("stable_id, name, or body_hash query is required")
	}

	store, err := s.repoSvc.OpenStore(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	repoModel, err := s.repoSvc.Get(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	head, err := s.browseSvc.ResolveRef(ctx, owner, repo, ref)
	if err != nil {
		return nil, fmt.Errorf("resolve ref: %w", err)
	}
	if s.lineageSvc != nil {
		has, err := s.db.HasEntityVersionsForCommit(ctx, repoModel.ID, string(head))
		if err != nil {
			return nil, err
		}
		if !has {
			if err := s.lineageSvc.IndexCommit(ctx, repoModel.ID, store, head); err != nil {
				return nil, fmt.Errorf("lineage index: %w", err)
			}
		}
	}
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	hits := make([]EntityHistoryHit, 0, limit)
	queue := []object.Hash{head}
	seen := map[object.Hash]bool{}
	remainingTake := limit
	remainingSkip := offset

	for len(queue) > 0 && remainingTake > 0 {
		h := queue[0]
		queue = queue[1:]
		if seen[h] {
			continue
		}
		seen[h] = true

		commit, err := store.Objects.ReadCommit(h)
		if err != nil {
			continue
		}

		matchedCount, err := s.db.CountEntityVersionsByCommitFiltered(ctx, repoModel.ID, string(h), stableID, name, bodyHash)
		if err != nil {
			return nil, err
		}
		if matchedCount > remainingSkip {
			versions, err := s.db.ListEntityVersionsByCommitFilteredPage(ctx, repoModel.ID, string(h), stableID, name, bodyHash, remainingTake, remainingSkip)
			if err != nil {
				return nil, err
			}
			remainingSkip = 0
			for _, v := range versions {
				hits = append(hits, EntityHistoryHit{
					CommitHash: string(h),
					StableID:   v.StableID,
					Author:     commit.Author,
					Timestamp:  commit.Timestamp,
					Message:    commit.Message,
					Path:       v.Path,
					EntityHash: v.EntityHash,
					Name:       v.Name,
					DeclKind:   v.DeclKind,
					Receiver:   v.Receiver,
					BodyHash:   v.BodyHash,
				})
			}
			remainingTake -= len(versions)
		} else {
			remainingSkip -= matchedCount
		}

		for _, p := range commit.Parents {
			if !seen[p] {
				queue = append(queue, p)
			}
		}
	}

	return hits, nil
}

// EntityLog returns newest-first commits that changed the selected entity key.
// key is required. path is optional and narrows matching to a single file.
func (s *DiffService) EntityLog(ctx context.Context, owner, repo, ref, path, key string, limit int) ([]EntityLogHit, error) {
	key = strings.TrimSpace(key)
	path = strings.TrimSpace(path)
	if key == "" {
		return nil, fmt.Errorf("key query is required")
	}
	if limit <= 0 {
		limit = 50
	}

	store, err := s.repoSvc.OpenStore(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	head, err := s.browseSvc.ResolveRef(ctx, owner, repo, ref)
	if err != nil {
		return nil, fmt.Errorf("resolve ref: %w", err)
	}

	gr := &gotrepo.Repo{Store: store.Objects}
	entries, err := gr.LogByEntity(head, limit, path, key)
	if err != nil {
		return nil, err
	}
	hits := make([]EntityLogHit, 0, len(entries))
	for _, e := range entries {
		if e.Commit == nil {
			continue
		}
		hits = append(hits, EntityLogHit{
			CommitHash: string(e.Hash),
			Author:     e.Commit.Author,
			Timestamp:  e.Commit.Timestamp,
			Message:    e.Commit.Message,
			Path:       path,
			Key:        key,
		})
	}
	return hits, nil
}

// EntityBlame returns the newest commit that changed the selected entity key.
func (s *DiffService) EntityBlame(ctx context.Context, owner, repo, ref, path, key string, limit int) (*EntityBlameInfo, error) {
	hits, err := s.EntityLog(ctx, owner, repo, ref, path, key, limit)
	if err != nil {
		return nil, err
	}
	if len(hits) == 0 {
		return nil, ErrEntityNotFound
	}
	top := hits[0]
	return &EntityBlameInfo{
		CommitHash: top.CommitHash,
		Author:     top.Author,
		Timestamp:  top.Timestamp,
		Message:    top.Message,
		Path:       top.Path,
		Key:        top.Key,
	}, nil
}

// --- helpers ---

func readBlobData(store *object.Store, h object.Hash) ([]byte, error) {
	blob, err := store.ReadBlob(h)
	if err != nil {
		return nil, err
	}
	return blob.Data, nil
}

func entityListToResponse(el *entity.EntityList) *EntityListResponse {
	entities := make([]EntityInfo, len(el.Entities))
	for i, e := range el.Entities {
		entities[i] = entityToInfo(&e)
	}
	return &EntityListResponse{
		Language: el.Language,
		Path:     el.Path,
		Entities: entities,
	}
}

func entityToInfo(e *entity.Entity) EntityInfo {
	return EntityInfo{
		Key:       e.IdentityKey(),
		Kind:      entityutil.KindName(e.Kind),
		Name:      e.Name,
		DeclKind:  e.DeclKind,
		Receiver:  e.Receiver,
		Signature: e.Signature,
		StartLine: e.StartLine,
		EndLine:   e.EndLine,
		BodyHash:  e.BodyHash,
	}
}

var changeTypeNames = map[diff.ChangeType]string{
	diff.Added:    "added",
	diff.Removed:  "removed",
	diff.Modified: "modified",
}

const (
	semanticChangeAddition  = "addition"
	semanticChangeRemoval   = "removal"
	semanticChangeSignature = "signature_change"
	semanticChangeBodyOnly  = "body_only_change"
	semanticChangeOther     = "other_change"
)

func fileDiffToResponse(fd *diff.FileDiff) FileDiffResponse {
	normalized := normalizeEntityChanges(fd.Changes)
	changes := make([]EntityChangeInfo, len(normalized))
	for i, c := range normalized {
		change := EntityChangeInfo{
			Type:           changeTypeNames[c.Type],
			Classification: classifySemanticChange(c),
			Key:            c.Key,
		}
		if c.Before != nil {
			info := entityToInfo(c.Before)
			change.Before = &info
		}
		if c.After != nil {
			info := entityToInfo(c.After)
			change.After = &info
		}
		changes[i] = change
	}
	return FileDiffResponse{
		Path:    fd.Path,
		Changes: changes,
	}
}

func normalizeEntityChanges(changes []diff.EntityChange) []diff.EntityChange {
	normalized := make([]diff.EntityChange, 0, len(changes))
	consumed := make([]bool, len(changes))
	for i, change := range changes {
		if consumed[i] {
			continue
		}
		if change.Type != diff.Removed {
			normalized = append(normalized, change)
			consumed[i] = true
			continue
		}

		matchIndex := -1
		for j, candidate := range changes {
			if consumed[j] || j == i {
				continue
			}
			if candidate.Type != diff.Added {
				continue
			}
			if isSignatureReplacement(change.Before, candidate.After) {
				matchIndex = j
				break
			}
		}
		if matchIndex == -1 {
			normalized = append(normalized, change)
			consumed[i] = true
			continue
		}

		consumed[i] = true
		consumed[matchIndex] = true
		matched := changes[matchIndex]
		key := matched.Key
		if strings.TrimSpace(key) == "" {
			key = change.Key
		}
		normalized = append(normalized, diff.EntityChange{
			Type:   diff.Modified,
			Key:    key,
			Before: change.Before,
			After:  matched.After,
		})
	}
	return normalized
}

func isSignatureReplacement(before, after *entity.Entity) bool {
	if before == nil || after == nil {
		return false
	}
	if before.Kind != entity.KindDeclaration || after.Kind != entity.KindDeclaration {
		return false
	}
	if before.Ordinal != after.Ordinal {
		return false
	}
	if before.DeclKind != after.DeclKind || before.Receiver != after.Receiver || before.Name != after.Name {
		return false
	}
	return hasSignatureChange(before, after)
}

func classifySemanticChange(c diff.EntityChange) string {
	switch c.Type {
	case diff.Added:
		return semanticChangeAddition
	case diff.Removed:
		return semanticChangeRemoval
	case diff.Modified:
		if hasSignatureChange(c.Before, c.After) {
			return semanticChangeSignature
		}
		if isBodyOnlyChange(c.Before, c.After) {
			return semanticChangeBodyOnly
		}
		return semanticChangeOther
	default:
		return semanticChangeOther
	}
}

func summarizeSemanticChanges(files []FileDiffResponse) DiffSummaryCounts {
	summary := DiffSummaryCounts{}
	for _, file := range files {
		for _, change := range file.Changes {
			classification := classifySemanticChangeInfo(change)
			switch classification {
			case semanticChangeAddition:
				summary.Additions++
			case semanticChangeRemoval:
				summary.Removals++
			case semanticChangeSignature:
				summary.SignatureChanges++
			case semanticChangeBodyOnly:
				summary.BodyOnlyChanges++
			default:
				summary.OtherChanges++
			}
			summary.TotalChanges++
		}
	}
	return summary
}

func classifySemanticChangeInfo(change EntityChangeInfo) string {
	switch change.Type {
	case "added":
		return semanticChangeAddition
	case "removed":
		return semanticChangeRemoval
	case "modified":
		if hasEntityInfoSignatureChange(change.Before, change.After) {
			return semanticChangeSignature
		}
		if isEntityInfoBodyOnlyChange(change.Before, change.After) {
			return semanticChangeBodyOnly
		}
		if strings.TrimSpace(change.Classification) != "" {
			return change.Classification
		}
		return semanticChangeOther
	default:
		if strings.TrimSpace(change.Classification) != "" {
			return change.Classification
		}
		return semanticChangeOther
	}
}

type semverImpact uint8

const (
	semverNone semverImpact = iota
	semverPatch
	semverMinor
	semverMajor
)

func (i semverImpact) String() string {
	switch i {
	case semverPatch:
		return "patch"
	case semverMinor:
		return "minor"
	case semverMajor:
		return "major"
	default:
		return "none"
	}
}

func maxSemverImpact(a, b semverImpact) semverImpact {
	if a > b {
		return a
	}
	return b
}

func addUniqueStringSlice(out *[]string, seen map[string]bool, value string) {
	if value == "" || seen[value] {
		return
	}
	seen[value] = true
	*out = append(*out, value)
}

func formatEntityChangeLabel(path string, c diff.EntityChange) string {
	name := ""
	switch {
	case c.After != nil && strings.TrimSpace(c.After.Name) != "":
		name = c.After.Name
	case c.Before != nil && strings.TrimSpace(c.Before.Name) != "":
		name = c.Before.Name
	default:
		name = c.Key
	}
	return fmt.Sprintf("%s (%s)", name, path)
}

func isExportedEntity(e *entity.Entity) bool {
	if e == nil {
		return false
	}
	if isExportedName(e.Name) {
		return true
	}
	sig := strings.ToLower(firstEntitySignatureLine(e))
	return strings.HasPrefix(sig, "export ") || strings.Contains(sig, " public ")
}

func isExportedName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	for _, r := range name {
		return unicode.IsUpper(r)
	}
	return false
}

func firstEntitySignatureLine(e *entity.Entity) string {
	if e == nil || len(e.Body) == 0 {
		return ""
	}
	for _, line := range strings.Split(string(e.Body), "\n") {
		l := strings.TrimSpace(line)
		if l == "" || strings.HasPrefix(l, "//") || strings.HasPrefix(l, "/*") || strings.HasPrefix(l, "*") {
			continue
		}
		return l
	}
	return ""
}

func normalizeSignature(signature string) string {
	signature = strings.TrimSpace(signature)
	if signature == "" {
		return ""
	}
	return strings.Join(strings.Fields(signature), " ")
}

func normalizedEntitySignature(e *entity.Entity) string {
	if e == nil {
		return ""
	}
	if sig := normalizeSignature(e.Signature); sig != "" {
		return sig
	}
	return normalizeSignature(firstEntitySignatureLine(e))
}

func hasSignatureChange(before, after *entity.Entity) bool {
	if before == nil || after == nil {
		return false
	}
	if before.DeclKind != after.DeclKind || before.Receiver != after.Receiver || before.Name != after.Name {
		return true
	}
	return normalizedEntitySignature(before) != normalizedEntitySignature(after)
}

func isBodyOnlyChange(before, after *entity.Entity) bool {
	if before == nil || after == nil {
		return false
	}
	if hasSignatureChange(before, after) {
		return false
	}
	if strings.TrimSpace(before.BodyHash) != strings.TrimSpace(after.BodyHash) {
		return true
	}
	return string(before.Body) != string(after.Body)
}

func hasEntityInfoSignatureChange(before, after *EntityInfo) bool {
	if before == nil || after == nil {
		return false
	}
	if before.DeclKind != after.DeclKind || before.Receiver != after.Receiver || before.Name != after.Name {
		return true
	}
	return normalizeSignature(before.Signature) != normalizeSignature(after.Signature)
}

func isEntityInfoBodyOnlyChange(before, after *EntityInfo) bool {
	if before == nil || after == nil {
		return false
	}
	if hasEntityInfoSignatureChange(before, after) {
		return false
	}
	return strings.TrimSpace(before.BodyHash) != strings.TrimSpace(after.BodyHash)
}

func isBreakingSignatureChange(before, after *entity.Entity) bool {
	return hasSignatureChange(before, after)
}
