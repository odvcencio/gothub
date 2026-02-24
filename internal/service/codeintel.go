package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/odvcencio/got/pkg/object"
	"github.com/odvcencio/gothub/internal/database"
	"github.com/odvcencio/gothub/internal/gotstore"
	"github.com/odvcencio/gothub/internal/models"
	"github.com/odvcencio/gts-suite/pkg/index"
	"github.com/odvcencio/gts-suite/pkg/model"
	"github.com/odvcencio/gts-suite/pkg/query"
	"github.com/odvcencio/gts-suite/pkg/xref"
)

const (
	repoIndexObjectType           object.ObjectType = "repoindex"
	repoIndexSchemaVersion                          = "0.1.0"
	defaultCodeIntelCacheMaxItems                   = 128
	defaultCodeIntelCacheTTL                        = 15 * time.Minute
	defaultSymbolSearchLimit                        = 500
)

var ErrInvalidSymbolSelector = errors.New("invalid symbol selector")

type codeIntelCacheEntry struct {
	index      *model.Index
	lastAccess time.Time
}

// CodeIntelService provides code intelligence powered by gts-suite.
type CodeIntelService struct {
	db        database.DB
	repoSvc   *RepoService
	browseSvc *BrowseService

	mu      sync.RWMutex
	indexes map[string]codeIntelCacheEntry // cache: "owner/repo@commitHash" -> index

	cacheMaxItems int
	cacheTTL      time.Duration
}

func NewCodeIntelService(db database.DB, repoSvc *RepoService, browseSvc *BrowseService) *CodeIntelService {
	return &CodeIntelService{
		db:            db,
		repoSvc:       repoSvc,
		browseSvc:     browseSvc,
		indexes:       make(map[string]codeIntelCacheEntry),
		cacheMaxItems: defaultCodeIntelCacheMaxItems,
		cacheTTL:      defaultCodeIntelCacheTTL,
	}
}

// BuildIndex returns a semantic index for a repository at a ref.
// It checks in-memory cache and persisted index first, then builds directly
// from the object store if needed.
func (s *CodeIntelService) BuildIndex(ctx context.Context, owner, repo, ref string) (*model.Index, error) {
	commitHash, err := s.browseSvc.ResolveRef(ctx, owner, repo, ref)
	if err != nil {
		return nil, fmt.Errorf("resolve ref: %w", err)
	}
	key := fmt.Sprintf("%s/%s@%s", owner, repo, commitHash)
	if idx, ok := s.getCachedIndex(key); ok {
		return idx, nil
	}
	repoModel, err := s.repoSvc.Get(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	store, err := s.repoSvc.OpenStore(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	if idx, ok, err := s.loadPersistedIndex(ctx, store, repoModel.ID, string(commitHash)); err == nil && ok {
		s.setCachedIndex(key, idx)
		return idx, nil
	}

	idx, err := s.buildIndexFromStore(store, commitHash, fmt.Sprintf("%s/%s", owner, repo))
	if err != nil {
		return nil, err
	}
	_ = s.persistIndex(ctx, store, repoModel.ID, string(commitHash), idx)
	s.setCachedIndex(key, idx)
	return idx, nil
}

// EnsureCommitIndexed ensures a persisted semantic index exists for a commit.
// cacheKey should be "owner/repo" when available, or empty to skip cache updates.
func (s *CodeIntelService) EnsureCommitIndexed(ctx context.Context, repoID int64, store *gotstore.RepoStore, cacheKey string, commitHash object.Hash) error {
	if strings.TrimSpace(string(commitHash)) == "" || store == nil {
		return nil
	}

	key := ""
	if strings.TrimSpace(cacheKey) != "" {
		key = fmt.Sprintf("%s@%s", cacheKey, commitHash)
		if idx, ok := s.getCachedIndex(key); ok {
			return s.persistEntityIndexIfNeeded(ctx, repoID, string(commitHash), idx)
		}
	}

	if idx, ok, err := s.loadPersistedIndex(ctx, store, repoID, string(commitHash)); err == nil && ok {
		if err := s.persistEntityIndexIfNeeded(ctx, repoID, string(commitHash), idx); err != nil {
			return err
		}
		if key != "" {
			s.setCachedIndex(key, idx)
		}
		return nil
	}

	idx, err := s.buildIndexFromStore(store, commitHash, cacheKey)
	if err != nil {
		return err
	}
	if err := s.persistIndex(ctx, store, repoID, string(commitHash), idx); err != nil {
		return err
	}
	if err := s.persistEntityIndex(ctx, repoID, string(commitHash), idx); err != nil {
		return err
	}
	if key != "" {
		s.setCachedIndex(key, idx)
	}
	return nil
}

func (s *CodeIntelService) buildIndexFromStore(store *gotstore.RepoStore, commitHash object.Hash, indexRoot string) (*model.Index, error) {
	// Read file blobs directly from the object store to avoid temp-dir materialization.
	files, err := s.flattenTreeForCommit(store, commitHash)
	if err != nil {
		return nil, fmt.Errorf("flatten tree: %w", err)
	}
	if strings.TrimSpace(indexRoot) == "" {
		indexRoot = string(commitHash)
	}

	builder := index.NewBuilder()
	idx := &model.Index{
		Version:     repoIndexSchemaVersion,
		Root:        fmt.Sprintf("%s@%s", indexRoot, commitHash),
		GeneratedAt: time.Now().UTC(),
		Files:       make([]model.FileSummary, 0, len(files)),
	}

	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	for _, fe := range files {
		parser, ok := builder.ParserForPath(fe.Path)
		if !ok {
			continue
		}
		blob, err := store.Objects.ReadBlob(object.Hash(fe.BlobHash))
		if err != nil {
			idx.Errors = append(idx.Errors, model.ParseError{
				Path:  fe.Path,
				Error: fmt.Sprintf("read blob: %v", err),
			})
			continue
		}
		summary, err := parser.Parse(fe.Path, blob.Data)
		if err != nil {
			idx.Errors = append(idx.Errors, model.ParseError{
				Path:  fe.Path,
				Error: err.Error(),
			})
			continue
		}

		summary.Path = fe.Path
		summary.Language = parser.Language()
		summary.SizeBytes = int64(len(blob.Data))
		for i := range summary.Symbols {
			summary.Symbols[i].File = fe.Path
		}
		for i := range summary.References {
			summary.References[i].File = fe.Path
		}
		idx.Files = append(idx.Files, summary)
	}
	return idx, nil
}

func (s *CodeIntelService) flattenTreeForCommit(store *gotstore.RepoStore, commitHash object.Hash) ([]FileEntry, error) {
	commit, err := store.Objects.ReadCommit(commitHash)
	if err != nil {
		return nil, fmt.Errorf("read commit: %w", err)
	}
	return flattenTree(store.Objects, commit.TreeHash, "")
}

func (s *CodeIntelService) loadPersistedIndex(ctx context.Context, store *gotstore.RepoStore, repoID int64, commitHash string) (*model.Index, bool, error) {
	indexHash, err := s.db.GetCommitIndex(ctx, repoID, commitHash)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, false, nil
		}
		return nil, false, err
	}
	objType, raw, err := store.Objects.Read(object.Hash(indexHash))
	if err != nil {
		return nil, false, nil
	}
	if objType != repoIndexObjectType {
		return nil, false, nil
	}
	var idx model.Index
	if err := json.Unmarshal(raw, &idx); err != nil {
		return nil, false, nil
	}
	return &idx, true, nil
}

func (s *CodeIntelService) persistIndex(ctx context.Context, store *gotstore.RepoStore, repoID int64, commitHash string, idx *model.Index) error {
	if idx == nil {
		return nil
	}
	raw, err := json.Marshal(idx)
	if err != nil {
		return err
	}
	indexHash, err := store.Objects.Write(repoIndexObjectType, raw)
	if err != nil {
		return err
	}
	return s.db.SetCommitIndex(ctx, repoID, commitHash, string(indexHash))
}

func (s *CodeIntelService) persistEntityIndexIfNeeded(ctx context.Context, repoID int64, commitHash string, idx *model.Index) error {
	ok, err := s.db.HasEntityIndexForCommit(ctx, repoID, commitHash)
	if err == nil && ok {
		return nil
	}
	return s.persistEntityIndex(ctx, repoID, commitHash, idx)
}

func (s *CodeIntelService) persistEntityIndex(ctx context.Context, repoID int64, commitHash string, idx *model.Index) error {
	entries := make([]models.EntityIndexEntry, 0)
	if idx != nil {
		entries = buildEntityIndexEntries(repoID, commitHash, idx)
	}
	return s.db.SetEntityIndexEntries(ctx, repoID, commitHash, entries)
}

func buildEntityIndexEntries(repoID int64, commitHash string, idx *model.Index) []models.EntityIndexEntry {
	if idx == nil {
		return nil
	}
	entries := make([]models.EntityIndexEntry, 0, idx.SymbolCount())
	for _, file := range idx.Files {
		for _, sym := range file.Symbols {
			key := symbolIndexKey(file.Path, sym)
			if strings.TrimSpace(key) == "" {
				continue
			}
			entries = append(entries, models.EntityIndexEntry{
				RepoID:     repoID,
				CommitHash: commitHash,
				FilePath:   file.Path,
				SymbolKey:  key,
				Kind:       sym.Kind,
				Name:       sym.Name,
				Signature:  sym.Signature,
				Receiver:   sym.Receiver,
				Language:   file.Language,
				StartLine:  sym.StartLine,
				EndLine:    sym.EndLine,
			})
		}
	}
	return entries
}

func symbolIndexKey(filePath string, sym model.Symbol) string {
	seed := strings.Join([]string{
		filePath,
		sym.Kind,
		sym.Name,
		sym.Signature,
		sym.Receiver,
		fmt.Sprintf("%d", sym.StartLine),
		fmt.Sprintf("%d", sym.EndLine),
	}, "\x00")
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:])
}

func (s *CodeIntelService) getCachedIndex(key string) (*model.Index, bool) {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.indexes[key]
	if !ok {
		return nil, false
	}
	if s.cacheTTL > 0 && now.Sub(entry.lastAccess) > s.cacheTTL {
		delete(s.indexes, key)
		return nil, false
	}
	entry.lastAccess = now
	s.indexes[key] = entry
	return entry.index, true
}

func (s *CodeIntelService) setCachedIndex(key string, idx *model.Index) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.indexes[key] = codeIntelCacheEntry{
		index:      idx,
		lastAccess: time.Now(),
	}
	if s.cacheMaxItems <= 0 {
		return
	}
	for len(s.indexes) > s.cacheMaxItems {
		oldestKey := ""
		var oldest time.Time
		for k, entry := range s.indexes {
			if oldestKey == "" || entry.lastAccess.Before(oldest) {
				oldestKey = k
				oldest = entry.lastAccess
			}
		}
		if oldestKey == "" {
			break
		}
		delete(s.indexes, oldestKey)
	}
}

// SymbolResult is a symbol with its file path.
type SymbolResult struct {
	File      string `json:"file"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Signature string `json:"signature,omitempty"`
	Receiver  string `json:"receiver,omitempty"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
}

// SearchSymbols finds symbols matching a selector or name pattern.
func (s *CodeIntelService) SearchSymbols(ctx context.Context, owner, repo, ref, selectorStr string) ([]SymbolResult, error) {
	selectorText := strings.TrimSpace(selectorStr)
	if selectorText == "" {
		selectorText = "*"
	}

	commitHash, err := s.browseSvc.ResolveRef(ctx, owner, repo, ref)
	if err != nil {
		return nil, fmt.Errorf("resolve ref: %w", err)
	}
	repoModel, err := s.repoSvc.Get(ctx, owner, repo)
	if err != nil {
		return nil, err
	}

	sel, selErr := query.ParseSelector(selectorText)
	if selErr != nil && selectorContainsFilterSyntax(selectorText) {
		return nil, fmt.Errorf("%w: %v", ErrInvalidSymbolSelector, selErr)
	}

	if err := s.ensureEntityIndexForCommit(ctx, owner, repo, ref, repoModel.ID, commitHash); err == nil {
		if selErr == nil {
			results, err := s.searchSymbolsFromEntityIndexSelector(ctx, repoModel.ID, string(commitHash), sel)
			if err == nil {
				return results, nil
			}
		} else {
			entries, err := s.db.SearchEntityIndexEntries(ctx, repoModel.ID, string(commitHash), selectorText, "", defaultSymbolSearchLimit)
			if err == nil {
				return symbolResultsFromEntityIndexEntries(entries), nil
			}
		}
	}

	// Fallback: use semantic index in memory/object-store when DB-backed symbol rows are unavailable.
	idx, err := s.BuildIndex(ctx, owner, repo, ref)
	if err != nil {
		return nil, err
	}
	if selErr == nil {
		return searchSymbolsFromIndexWithSelector(idx, sel), nil
	}
	return searchSymbolsFromIndexWithContains(idx, selectorText), nil
}

func (s *CodeIntelService) ensureEntityIndexForCommit(ctx context.Context, owner, repo, ref string, repoID int64, commitHash object.Hash) error {
	ok, err := s.db.HasEntityIndexForCommit(ctx, repoID, string(commitHash))
	if err == nil && ok {
		return nil
	}

	idx, err := s.BuildIndex(ctx, owner, repo, ref)
	if err != nil {
		return err
	}
	return s.persistEntityIndex(ctx, repoID, string(commitHash), idx)
}

func (s *CodeIntelService) searchSymbolsFromEntityIndexSelector(ctx context.Context, repoID int64, commitHash string, sel query.Selector) ([]SymbolResult, error) {
	kind := ""
	if sel.Kind != "*" {
		kind = sel.Kind
	}

	entries, err := s.db.ListEntityIndexEntriesByCommit(ctx, repoID, commitHash, kind, 0)
	if err != nil {
		return nil, err
	}
	if !selectorNeedsSymbolMatch(sel) {
		return symbolResultsFromEntityIndexEntries(entries), nil
	}

	var results []SymbolResult
	for _, entry := range entries {
		sym := model.Symbol{
			File:      entry.FilePath,
			Kind:      entry.Kind,
			Name:      entry.Name,
			Signature: entry.Signature,
			Receiver:  entry.Receiver,
			StartLine: entry.StartLine,
			EndLine:   entry.EndLine,
		}
		if sel.Match(sym) {
			results = append(results, symbolResultFromEntityIndexEntry(entry))
		}
	}
	return results, nil
}

func selectorContainsFilterSyntax(raw string) bool {
	return strings.Contains(raw, "[") || strings.Contains(raw, "]")
}

func selectorNeedsSymbolMatch(sel query.Selector) bool {
	return sel.NameRE != nil ||
		sel.SignatureRE != nil ||
		sel.ReceiverRE != nil ||
		sel.FileRE != nil ||
		sel.StartMin != nil ||
		sel.StartMax != nil ||
		sel.EndMin != nil ||
		sel.EndMax != nil ||
		sel.Line != nil
}

func symbolResultsFromEntityIndexEntries(entries []models.EntityIndexEntry) []SymbolResult {
	results := make([]SymbolResult, 0, len(entries))
	for _, entry := range entries {
		results = append(results, symbolResultFromEntityIndexEntry(entry))
	}
	return results
}

func symbolResultFromEntityIndexEntry(entry models.EntityIndexEntry) SymbolResult {
	return SymbolResult{
		File:      entry.FilePath,
		Kind:      entry.Kind,
		Name:      entry.Name,
		Signature: entry.Signature,
		Receiver:  entry.Receiver,
		StartLine: entry.StartLine,
		EndLine:   entry.EndLine,
	}
}

func searchSymbolsFromIndexWithSelector(idx *model.Index, sel query.Selector) []SymbolResult {
	var results []SymbolResult
	for _, f := range idx.Files {
		for _, sym := range f.Symbols {
			if sel.Match(sym) {
				results = append(results, SymbolResult{
					File:      f.Path,
					Kind:      sym.Kind,
					Name:      sym.Name,
					Signature: sym.Signature,
					Receiver:  sym.Receiver,
					StartLine: sym.StartLine,
					EndLine:   sym.EndLine,
				})
			}
		}
	}
	return results
}

func searchSymbolsFromIndexWithContains(idx *model.Index, rawQuery string) []SymbolResult {
	needle := strings.ToLower(strings.TrimSpace(rawQuery))
	if needle == "" {
		return nil
	}
	var results []SymbolResult
	for _, f := range idx.Files {
		for _, sym := range f.Symbols {
			if strings.Contains(strings.ToLower(sym.Name), needle) ||
				strings.Contains(strings.ToLower(sym.Signature), needle) ||
				strings.Contains(strings.ToLower(sym.Receiver), needle) {
				results = append(results, SymbolResult{
					File:      f.Path,
					Kind:      sym.Kind,
					Name:      sym.Name,
					Signature: sym.Signature,
					Receiver:  sym.Receiver,
					StartLine: sym.StartLine,
					EndLine:   sym.EndLine,
				})
			}
		}
	}
	return results
}

// ReferenceResult is a reference to a symbol.
type ReferenceResult struct {
	File        string `json:"file"`
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	StartLine   int    `json:"start_line"`
	EndLine     int    `json:"end_line"`
	StartColumn int    `json:"start_column"`
	EndColumn   int    `json:"end_column"`
}

// FindReferences finds all references matching a name pattern.
func (s *CodeIntelService) FindReferences(ctx context.Context, owner, repo, ref, name string) ([]ReferenceResult, error) {
	idx, err := s.BuildIndex(ctx, owner, repo, ref)
	if err != nil {
		return nil, err
	}

	var results []ReferenceResult
	for _, f := range idx.Files {
		for _, r := range f.References {
			if r.Name == name {
				results = append(results, ReferenceResult{
					File:        f.Path,
					Kind:        r.Kind,
					Name:        r.Name,
					StartLine:   r.StartLine,
					EndLine:     r.EndLine,
					StartColumn: r.StartColumn,
					EndColumn:   r.EndColumn,
				})
			}
		}
	}
	return results, nil
}

// CallGraphResult represents a traversal of the call graph.
type CallGraphResult struct {
	Definitions []xref.Definition `json:"definitions"`
	Edges       []CallEdge        `json:"edges"`
}

// CallEdge is a simplified call graph edge for JSON serialization.
type CallEdge struct {
	CallerName string `json:"caller_name"`
	CallerFile string `json:"caller_file"`
	CalleeName string `json:"callee_name"`
	CalleeFile string `json:"callee_file"`
	Count      int    `json:"count"`
}

// GetCallGraph builds and traverses the call graph.
func (s *CodeIntelService) GetCallGraph(ctx context.Context, owner, repo, ref, symbol string, depth int, reverse bool) (*CallGraphResult, error) {
	idx, err := s.BuildIndex(ctx, owner, repo, ref)
	if err != nil {
		return nil, err
	}

	graph, err := xref.Build(idx)
	if err != nil {
		return nil, fmt.Errorf("build call graph: %w", err)
	}

	defs, err := graph.FindDefinitions(symbol, false)
	if err != nil || len(defs) == 0 {
		return &CallGraphResult{}, nil
	}

	ids := make([]string, len(defs))
	for i, d := range defs {
		ids[i] = d.ID
	}

	walk := graph.Walk(ids, depth, reverse)

	var edges []CallEdge
	for _, e := range walk.Edges {
		edges = append(edges, CallEdge{
			CallerName: e.Caller.Name,
			CallerFile: e.Caller.File,
			CalleeName: e.Callee.Name,
			CalleeFile: e.Callee.File,
			Count:      e.Count,
		})
	}

	return &CallGraphResult{
		Definitions: walk.Nodes,
		Edges:       edges,
	}, nil
}
