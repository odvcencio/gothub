package service

import (
	"container/heap"
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
	key        string
	index      *model.Index
	lastAccess time.Time
	heapIndex  int
}

type codeIntelIndexAccessHeap []*codeIntelCacheEntry

func (h codeIntelIndexAccessHeap) Len() int { return len(h) }

func (h codeIntelIndexAccessHeap) Less(i, j int) bool {
	if h[i].lastAccess.Equal(h[j].lastAccess) {
		return h[i].key < h[j].key
	}
	return h[i].lastAccess.Before(h[j].lastAccess)
}

func (h codeIntelIndexAccessHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].heapIndex = i
	h[j].heapIndex = j
}

func (h *codeIntelIndexAccessHeap) Push(x any) {
	entry := x.(*codeIntelCacheEntry)
	entry.heapIndex = len(*h)
	*h = append(*h, entry)
}

func (h *codeIntelIndexAccessHeap) Pop() any {
	old := *h
	n := len(old)
	entry := old[n-1]
	old[n-1] = nil
	entry.heapIndex = -1
	*h = old[:n-1]
	return entry
}

type codeIntelBloomCacheEntry struct {
	key        string
	filter     *symbolSearchBloomFilter
	lastAccess time.Time
	heapIndex  int
}

type codeIntelBloomAccessHeap []*codeIntelBloomCacheEntry

func (h codeIntelBloomAccessHeap) Len() int { return len(h) }

func (h codeIntelBloomAccessHeap) Less(i, j int) bool {
	if h[i].lastAccess.Equal(h[j].lastAccess) {
		return h[i].key < h[j].key
	}
	return h[i].lastAccess.Before(h[j].lastAccess)
}

func (h codeIntelBloomAccessHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].heapIndex = i
	h[j].heapIndex = j
}

func (h *codeIntelBloomAccessHeap) Push(x any) {
	entry := x.(*codeIntelBloomCacheEntry)
	entry.heapIndex = len(*h)
	*h = append(*h, entry)
}

func (h *codeIntelBloomAccessHeap) Pop() any {
	old := *h
	n := len(old)
	entry := old[n-1]
	old[n-1] = nil
	entry.heapIndex = -1
	*h = old[:n-1]
	return entry
}

// CodeIntelService provides code intelligence powered by gts-suite.
type CodeIntelService struct {
	db        database.DB
	repoSvc   *RepoService
	browseSvc *BrowseService

	mu           sync.RWMutex
	indexes      map[string]*codeIntelCacheEntry // cache: "owner/repo@commitHash" -> index
	indexAccess  codeIntelIndexAccessHeap
	symbolBlooms map[string]*codeIntelBloomCacheEntry // cache: "repoID@commitHash" -> bloom filter
	bloomAccess  codeIntelBloomAccessHeap

	cacheMaxItems int
	cacheTTL      time.Duration
}

func NewCodeIntelService(db database.DB, repoSvc *RepoService, browseSvc *BrowseService) *CodeIntelService {
	return &CodeIntelService{
		db:            db,
		repoSvc:       repoSvc,
		browseSvc:     browseSvc,
		indexes:       make(map[string]*codeIntelCacheEntry),
		symbolBlooms:  make(map[string]*codeIntelBloomCacheEntry),
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
	if err := s.persistCommitMetadata(ctx, repoID, store, commitHash); err != nil {
		return err
	}

	key := ""
	if strings.TrimSpace(cacheKey) != "" {
		key = fmt.Sprintf("%s@%s", cacheKey, commitHash)
		if idx, ok := s.getCachedIndex(key); ok {
			s.setCommitSymbolBloom(repoID, string(commitHash), buildSymbolSearchBloomFromIndex(idx))
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

func (s *CodeIntelService) persistCommitMetadata(ctx context.Context, repoID int64, store *gotstore.RepoStore, commitHash object.Hash) error {
	if s.db == nil || repoID <= 0 || store == nil || strings.TrimSpace(string(commitHash)) == "" {
		return nil
	}
	_, _, err := s.computeAndPersistCommitGeneration(ctx, repoID, store.Objects, commitHash, make(map[object.Hash]uint64), make(map[object.Hash]int), make(map[object.Hash]bool))
	return err
}

func (s *CodeIntelService) computeAndPersistCommitGeneration(
	ctx context.Context,
	repoID int64,
	objects *object.Store,
	commitHash object.Hash,
	generations map[object.Hash]uint64,
	parentCounts map[object.Hash]int,
	visiting map[object.Hash]bool,
) (uint64, int, error) {
	if g, ok := generations[commitHash]; ok {
		return g, parentCounts[commitHash], nil
	}
	if visiting[commitHash] {
		return 0, 0, fmt.Errorf("commit graph cycle detected at %s", commitHash)
	}
	visiting[commitHash] = true
	defer delete(visiting, commitHash)

	if meta, ok, err := s.db.GetCommitMetadata(ctx, repoID, string(commitHash)); err != nil {
		return 0, 0, err
	} else if ok && meta.Generation > 0 {
		gen := uint64(meta.Generation)
		generations[commitHash] = gen
		parentCounts[commitHash] = meta.ParentCount
		return gen, meta.ParentCount, nil
	}

	commit, err := objects.ReadCommit(commitHash)
	if err != nil {
		return 0, 0, fmt.Errorf("read commit %s: %w", commitHash, err)
	}

	maxParent := uint64(0)
	parentCount := 0
	for _, parent := range commit.Parents {
		if parent == "" {
			continue
		}
		parentCount++
		parentGen, _, err := s.computeAndPersistCommitGeneration(ctx, repoID, objects, parent, generations, parentCounts, visiting)
		if err != nil {
			return 0, 0, err
		}
		if parentGen > maxParent {
			maxParent = parentGen
		}
	}

	gen := maxParent + 1
	if err := s.db.UpsertCommitMetadata(ctx, &models.CommitMetadata{
		RepoID:      repoID,
		CommitHash:  string(commitHash),
		Generation:  int64(gen),
		ParentCount: parentCount,
	}); err != nil {
		return 0, 0, err
	}
	generations[commitHash] = gen
	parentCounts[commitHash] = parentCount
	return gen, parentCount, nil
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

	// New format: wrapped index payload with optional bloom filter metadata.
	var wrapped persistedRepoIndexObject
	if err := json.Unmarshal(raw, &wrapped); err == nil && wrapped.Index != nil {
		if wrapped.SymbolBloom != nil {
			s.setCommitSymbolBloom(repoID, commitHash, wrapped.SymbolBloom)
		}
		return wrapped.Index, true, nil
	}

	// Backward compatibility with older index objects.
	var idx model.Index
	if err := json.Unmarshal(raw, &idx); err != nil {
		return nil, false, nil
	}
	if bloom := buildSymbolSearchBloomFromIndex(&idx); bloom != nil {
		s.setCommitSymbolBloom(repoID, commitHash, bloom)
	}
	return &idx, true, nil
}

func (s *CodeIntelService) persistIndex(ctx context.Context, store *gotstore.RepoStore, repoID int64, commitHash string, idx *model.Index) error {
	if idx == nil {
		return nil
	}
	bloom := buildSymbolSearchBloomFromIndex(idx)
	payload := persistedRepoIndexObject{
		Index:       idx,
		SymbolBloom: bloom,
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	indexHash, err := store.Objects.Write(repoIndexObjectType, raw)
	if err != nil {
		return err
	}
	if bloom != nil {
		s.setCommitSymbolBloom(repoID, commitHash, bloom)
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
	if err := s.db.SetEntityIndexEntries(ctx, repoID, commitHash, entries); err != nil {
		return err
	}
	s.setCommitSymbolBloom(repoID, commitHash, buildSymbolSearchBloomFromEntries(entries))
	return nil
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
		s.removeIndexEntryLocked(entry)
		return nil, false
	}
	entry.lastAccess = now
	if !s.fixIndexEntryAccessLocked(entry) {
		s.rebuildIndexAccessHeapLocked()
		_ = s.fixIndexEntryAccessLocked(entry)
	}
	return entry.index, true
}

func (s *CodeIntelService) setCachedIndex(key string, idx *model.Index) {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.indexes == nil {
		s.indexes = make(map[string]*codeIntelCacheEntry)
	}
	if entry, ok := s.indexes[key]; ok {
		entry.index = idx
		entry.lastAccess = now
		if !s.fixIndexEntryAccessLocked(entry) {
			s.rebuildIndexAccessHeapLocked()
			_ = s.fixIndexEntryAccessLocked(entry)
		}
	} else {
		entry := &codeIntelCacheEntry{
			key:        key,
			index:      idx,
			lastAccess: now,
			heapIndex:  -1,
		}
		s.indexes[key] = entry
		heap.Push(&s.indexAccess, entry)
	}
	if s.cacheMaxItems <= 0 {
		return
	}
	for len(s.indexes) > s.cacheMaxItems {
		s.evictOldestIndexEntryLocked()
	}
}

func (s *CodeIntelService) fixIndexEntryAccessLocked(entry *codeIntelCacheEntry) bool {
	if entry == nil {
		return false
	}
	idx := entry.heapIndex
	if idx < 0 || idx >= len(s.indexAccess) || s.indexAccess[idx] != entry {
		return false
	}
	heap.Fix(&s.indexAccess, idx)
	return true
}

func (s *CodeIntelService) removeIndexEntryLocked(entry *codeIntelCacheEntry) {
	if entry == nil {
		return
	}

	if current, ok := s.indexes[entry.key]; ok && current == entry {
		delete(s.indexes, entry.key)
	}

	idx := entry.heapIndex
	if idx >= 0 && idx < len(s.indexAccess) && s.indexAccess[idx] == entry {
		heap.Remove(&s.indexAccess, idx)
		return
	}
	for i := range s.indexAccess {
		if s.indexAccess[i] == entry {
			heap.Remove(&s.indexAccess, i)
			return
		}
	}
	entry.heapIndex = -1
}

func (s *CodeIntelService) rebuildIndexAccessHeapLocked() {
	rebuilt := make(codeIntelIndexAccessHeap, 0, len(s.indexes))
	for key, entry := range s.indexes {
		if entry == nil {
			delete(s.indexes, key)
			continue
		}
		entry.key = key
		entry.heapIndex = len(rebuilt)
		rebuilt = append(rebuilt, entry)
	}
	heap.Init(&rebuilt)
	s.indexAccess = rebuilt
}

func (s *CodeIntelService) evictOldestIndexEntryLocked() {
	for len(s.indexAccess) > 0 {
		entry := heap.Pop(&s.indexAccess).(*codeIntelCacheEntry)
		current, ok := s.indexes[entry.key]
		if !ok || current != entry {
			continue
		}
		delete(s.indexes, entry.key)
		return
	}

	for _, entry := range s.indexes {
		s.removeIndexEntryLocked(entry)
		return
	}
}

func symbolBloomCacheKey(repoID int64, commitHash string) string {
	return fmt.Sprintf("%d@%s", repoID, strings.TrimSpace(commitHash))
}

func (s *CodeIntelService) getCommitSymbolBloom(repoID int64, commitHash string) (*symbolSearchBloomFilter, bool) {
	return s.getCachedSymbolBloom(symbolBloomCacheKey(repoID, commitHash))
}

func (s *CodeIntelService) getCachedSymbolBloom(key string) (*symbolSearchBloomFilter, bool) {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.symbolBlooms[key]
	if !ok {
		return nil, false
	}
	if s.cacheTTL > 0 && now.Sub(entry.lastAccess) > s.cacheTTL {
		s.removeSymbolBloomEntryLocked(entry)
		return nil, false
	}
	entry.lastAccess = now
	if !s.fixBloomEntryAccessLocked(entry) {
		s.rebuildBloomAccessHeapLocked()
		_ = s.fixBloomEntryAccessLocked(entry)
	}
	return entry.filter, true
}

func (s *CodeIntelService) setCommitSymbolBloom(repoID int64, commitHash string, bloom *symbolSearchBloomFilter) {
	if bloom == nil {
		return
	}
	s.setCachedSymbolBloom(symbolBloomCacheKey(repoID, commitHash), bloom)
}

func (s *CodeIntelService) setCachedSymbolBloom(key string, bloom *symbolSearchBloomFilter) {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.symbolBlooms == nil {
		s.symbolBlooms = make(map[string]*codeIntelBloomCacheEntry)
	}
	if entry, ok := s.symbolBlooms[key]; ok {
		entry.filter = bloom
		entry.lastAccess = now
		if !s.fixBloomEntryAccessLocked(entry) {
			s.rebuildBloomAccessHeapLocked()
			_ = s.fixBloomEntryAccessLocked(entry)
		}
	} else {
		entry := &codeIntelBloomCacheEntry{
			key:        key,
			filter:     bloom,
			lastAccess: now,
			heapIndex:  -1,
		}
		s.symbolBlooms[key] = entry
		heap.Push(&s.bloomAccess, entry)
	}
	if s.cacheMaxItems <= 0 {
		return
	}
	for len(s.symbolBlooms) > s.cacheMaxItems {
		s.evictOldestSymbolBloomEntryLocked()
	}
}

func (s *CodeIntelService) fixBloomEntryAccessLocked(entry *codeIntelBloomCacheEntry) bool {
	if entry == nil {
		return false
	}
	idx := entry.heapIndex
	if idx < 0 || idx >= len(s.bloomAccess) || s.bloomAccess[idx] != entry {
		return false
	}
	heap.Fix(&s.bloomAccess, idx)
	return true
}

func (s *CodeIntelService) removeSymbolBloomEntryLocked(entry *codeIntelBloomCacheEntry) {
	if entry == nil {
		return
	}

	if current, ok := s.symbolBlooms[entry.key]; ok && current == entry {
		delete(s.symbolBlooms, entry.key)
	}

	idx := entry.heapIndex
	if idx >= 0 && idx < len(s.bloomAccess) && s.bloomAccess[idx] == entry {
		heap.Remove(&s.bloomAccess, idx)
		return
	}
	for i := range s.bloomAccess {
		if s.bloomAccess[i] == entry {
			heap.Remove(&s.bloomAccess, i)
			return
		}
	}
	entry.heapIndex = -1
}

func (s *CodeIntelService) rebuildBloomAccessHeapLocked() {
	rebuilt := make(codeIntelBloomAccessHeap, 0, len(s.symbolBlooms))
	for key, entry := range s.symbolBlooms {
		if entry == nil {
			delete(s.symbolBlooms, key)
			continue
		}
		entry.key = key
		entry.heapIndex = len(rebuilt)
		rebuilt = append(rebuilt, entry)
	}
	heap.Init(&rebuilt)
	s.bloomAccess = rebuilt
}

func (s *CodeIntelService) evictOldestSymbolBloomEntryLocked() {
	for len(s.bloomAccess) > 0 {
		entry := heap.Pop(&s.bloomAccess).(*codeIntelBloomCacheEntry)
		current, ok := s.symbolBlooms[entry.key]
		if !ok || current != entry {
			continue
		}
		delete(s.symbolBlooms, entry.key)
		return
	}

	for _, entry := range s.symbolBlooms {
		s.removeSymbolBloomEntryLocked(entry)
		return
	}
}

func (s *CodeIntelService) loadCommitSymbolBloom(ctx context.Context, owner, repo string, repoID int64, commitHash object.Hash) (*symbolSearchBloomFilter, bool) {
	commit := string(commitHash)
	if bloom, ok := s.getCommitSymbolBloom(repoID, commit); ok {
		return bloom, true
	}

	cacheKey := fmt.Sprintf("%s/%s@%s", owner, repo, commitHash)
	if idx, ok := s.getCachedIndex(cacheKey); ok {
		bloom := buildSymbolSearchBloomFromIndex(idx)
		s.setCommitSymbolBloom(repoID, commit, bloom)
		return bloom, true
	}

	store, err := s.repoSvc.OpenStore(ctx, owner, repo)
	if err != nil {
		return nil, false
	}
	_, _, _ = s.loadPersistedIndex(ctx, store, repoID, commit)
	return s.getCommitSymbolBloom(repoID, commit)
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
			if bloom, ok := s.loadCommitSymbolBloom(ctx, owner, repo, repoModel.ID, commitHash); ok &&
				bloom.supportsEntityTextSearch() &&
				!bloomMightContainPlainTextQuery(bloom, selectorText) {
				return []SymbolResult{}, nil
			}
			entries, err := s.db.SearchEntityIndexEntries(ctx, repoModel.ID, string(commitHash), selectorText, "", defaultSymbolSearchLimit)
			if err == nil {
				return symbolResultsFromEntityIndexEntries(entries), nil
			}
		}
	}

	if selErr != nil {
		if bloom, ok := s.loadCommitSymbolBloom(ctx, owner, repo, repoModel.ID, commitHash); ok &&
			bloom.supportsIndexContainsSearch() &&
			!bloomMightContainPlainTextQuery(bloom, selectorText) {
			return []SymbolResult{}, nil
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

// ImpactSummary provides compact counts for impact analysis.
type ImpactSummary struct {
	MatchedDefinitions int `json:"matched_definitions"`
	DirectCallers      int `json:"direct_callers"`
	TotalIncomingCalls int `json:"total_incoming_calls"`
}

// ImpactDirectCaller is a direct caller of one or more matched definitions.
type ImpactDirectCaller struct {
	Definition xref.Definition `json:"definition"`
	CallCount  int             `json:"call_count"`
}

// ImpactAnalysisResult describes direct impact for a symbol.
type ImpactAnalysisResult struct {
	MatchedDefinitions []xref.Definition    `json:"matched_definitions"`
	DirectCallers      []ImpactDirectCaller `json:"direct_callers"`
	Summary            ImpactSummary        `json:"summary"`
}

// GetImpactAnalysis returns matched callable definitions and their direct callers.
// It prefers persisted xref rows when present and falls back to in-memory graph build.
func (s *CodeIntelService) GetImpactAnalysis(ctx context.Context, owner, repo, ref, symbol string) (*ImpactAnalysisResult, error) {
	symbol = strings.TrimSpace(symbol)
	if symbol == "" {
		return newImpactAnalysisResult(nil, nil), nil
	}

	commitHash, err := s.browseSvc.ResolveRef(ctx, owner, repo, ref)
	if err != nil {
		return nil, fmt.Errorf("resolve ref: %w", err)
	}
	repoModel, err := s.repoSvc.Get(ctx, owner, repo)
	if err != nil {
		return nil, err
	}

	defs, callers, ok, err := s.loadImpactFromPersistedXRef(ctx, repoModel.ID, string(commitHash), symbol)
	if err == nil && ok {
		return newImpactAnalysisResult(defs, callers), nil
	}

	defs, callers, err = s.loadImpactFromInMemoryXRef(ctx, owner, repo, ref, repoModel.ID, string(commitHash), symbol)
	if err != nil {
		return nil, err
	}
	return newImpactAnalysisResult(defs, callers), nil
}

func (s *CodeIntelService) loadImpactFromPersistedXRef(ctx context.Context, repoID int64, commitHash, symbol string) ([]xref.Definition, []ImpactDirectCaller, bool, error) {
	hasGraph, err := s.db.HasXRefGraphForCommit(ctx, repoID, commitHash)
	if err != nil {
		return nil, nil, false, err
	}
	if !hasGraph {
		return nil, nil, false, nil
	}

	defs, err := s.findImpactDefinitionsFromPersistedXRef(ctx, repoID, commitHash, symbol)
	if err != nil {
		return nil, nil, true, err
	}
	callers, err := s.collectDirectCallersFromPersistedXRef(ctx, repoID, commitHash, defs)
	if err != nil {
		return nil, nil, true, err
	}
	return defs, callers, true, nil
}

func (s *CodeIntelService) findImpactDefinitionsFromPersistedXRef(ctx context.Context, repoID int64, commitHash, symbol string) ([]xref.Definition, error) {
	seen := make(map[string]xref.Definition)

	defByID, err := s.db.GetXRefDefinition(ctx, repoID, commitHash, symbol)
	if err == nil {
		if defByID.Callable {
			seen[defByID.EntityID] = xRefDefinitionFromModel(*defByID)
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	defsByName, err := s.db.FindXRefDefinitionsByName(ctx, repoID, commitHash, symbol)
	if err != nil {
		return nil, err
	}
	for _, d := range defsByName {
		if !d.Callable {
			continue
		}
		seen[d.EntityID] = xRefDefinitionFromModel(d)
	}

	defs := make([]xref.Definition, 0, len(seen))
	for _, d := range seen {
		defs = append(defs, d)
	}
	sortImpactDefinitions(defs)
	return defs, nil
}

func (s *CodeIntelService) collectDirectCallersFromPersistedXRef(ctx context.Context, repoID int64, commitHash string, defs []xref.Definition) ([]ImpactDirectCaller, error) {
	if len(defs) == 0 {
		return nil, nil
	}

	defCache := make(map[string]xref.Definition, len(defs))
	for _, d := range defs {
		defCache[d.ID] = d
	}

	callersByID := make(map[string]ImpactDirectCaller)
	for _, d := range defs {
		edges, err := s.db.ListXRefEdgesTo(ctx, repoID, commitHash, d.ID, "call")
		if err != nil {
			return nil, err
		}
		for _, edge := range edges {
			callerID := strings.TrimSpace(edge.SourceEntityID)
			if callerID == "" {
				continue
			}
			callerDef, ok := defCache[callerID]
			if !ok {
				dbDef, err := s.db.GetXRefDefinition(ctx, repoID, commitHash, callerID)
				if err != nil {
					if errors.Is(err, sql.ErrNoRows) {
						continue
					}
					return nil, err
				}
				callerDef = xRefDefinitionFromModel(*dbDef)
				defCache[callerID] = callerDef
			}
			caller := callersByID[callerID]
			caller.Definition = callerDef
			if edge.Count > 0 {
				caller.CallCount += edge.Count
			} else {
				caller.CallCount++
			}
			callersByID[callerID] = caller
		}
	}

	callers := make([]ImpactDirectCaller, 0, len(callersByID))
	for _, caller := range callersByID {
		callers = append(callers, caller)
	}
	sortImpactCallers(callers)
	return callers, nil
}

func (s *CodeIntelService) loadImpactFromInMemoryXRef(ctx context.Context, owner, repo, ref string, repoID int64, commitHash, symbol string) ([]xref.Definition, []ImpactDirectCaller, error) {
	idx, err := s.BuildIndex(ctx, owner, repo, ref)
	if err != nil {
		return nil, nil, err
	}
	graph, err := xref.Build(idx)
	if err != nil {
		return nil, nil, fmt.Errorf("build call graph: %w", err)
	}

	defs := findImpactDefinitionsFromGraph(graph, symbol)
	callers := collectDirectCallersFromGraph(graph, defs)
	_ = s.persistXRefGraph(ctx, repoID, commitHash, graph)
	return defs, callers, nil
}

func findImpactDefinitionsFromGraph(graph xref.Graph, symbol string) []xref.Definition {
	symbol = strings.TrimSpace(symbol)
	if symbol == "" {
		return nil
	}

	seen := make(map[string]xref.Definition)
	for _, d := range graph.Definitions {
		if !d.Callable {
			continue
		}
		if d.ID == symbol {
			seen[d.ID] = d
		}
	}
	defsByName, err := graph.FindDefinitions(symbol, false)
	if err == nil {
		for _, d := range defsByName {
			seen[d.ID] = d
		}
	}

	defs := make([]xref.Definition, 0, len(seen))
	for _, d := range seen {
		defs = append(defs, d)
	}
	sortImpactDefinitions(defs)
	return defs
}

func collectDirectCallersFromGraph(graph xref.Graph, defs []xref.Definition) []ImpactDirectCaller {
	if len(defs) == 0 {
		return nil
	}

	callersByID := make(map[string]ImpactDirectCaller)
	for _, d := range defs {
		edges := graph.IncomingEdges(d.ID)
		for _, edge := range edges {
			callerID := strings.TrimSpace(edge.Caller.ID)
			if callerID == "" {
				continue
			}
			caller := callersByID[callerID]
			caller.Definition = edge.Caller
			if edge.Count > 0 {
				caller.CallCount += edge.Count
			} else {
				caller.CallCount++
			}
			callersByID[callerID] = caller
		}
	}

	callers := make([]ImpactDirectCaller, 0, len(callersByID))
	for _, caller := range callersByID {
		callers = append(callers, caller)
	}
	sortImpactCallers(callers)
	return callers
}

func (s *CodeIntelService) persistXRefGraph(ctx context.Context, repoID int64, commitHash string, graph xref.Graph) error {
	defs := make([]models.XRefDefinition, 0, len(graph.Definitions))
	for _, d := range graph.Definitions {
		defs = append(defs, models.XRefDefinition{
			RepoID:      repoID,
			CommitHash:  commitHash,
			EntityID:    d.ID,
			File:        d.File,
			PackageName: d.Package,
			Kind:        d.Kind,
			Name:        d.Name,
			Signature:   d.Signature,
			Receiver:    d.Receiver,
			StartLine:   d.StartLine,
			EndLine:     d.EndLine,
			Callable:    d.Callable,
		})
	}

	edges := make([]models.XRefEdge, 0, len(graph.Edges))
	for _, edge := range graph.Edges {
		sourceFile := edge.Caller.File
		sourceLine := 0
		if len(edge.Samples) > 0 {
			if strings.TrimSpace(edge.Samples[0].File) != "" {
				sourceFile = edge.Samples[0].File
			}
			sourceLine = edge.Samples[0].StartLine
		}
		count := edge.Count
		if count <= 0 {
			count = 1
		}
		edges = append(edges, models.XRefEdge{
			RepoID:         repoID,
			CommitHash:     commitHash,
			SourceEntityID: edge.Caller.ID,
			TargetEntityID: edge.Callee.ID,
			Kind:           "call",
			SourceFile:     sourceFile,
			SourceLine:     sourceLine,
			Resolution:     edge.Resolution,
			Count:          count,
		})
	}
	return s.db.SetCommitXRefGraph(ctx, repoID, commitHash, defs, edges)
}

func newImpactAnalysisResult(defs []xref.Definition, callers []ImpactDirectCaller) *ImpactAnalysisResult {
	if defs == nil {
		defs = []xref.Definition{}
	}
	if callers == nil {
		callers = []ImpactDirectCaller{}
	}
	totalIncomingCalls := 0
	for _, caller := range callers {
		totalIncomingCalls += caller.CallCount
	}
	return &ImpactAnalysisResult{
		MatchedDefinitions: defs,
		DirectCallers:      callers,
		Summary: ImpactSummary{
			MatchedDefinitions: len(defs),
			DirectCallers:      len(callers),
			TotalIncomingCalls: totalIncomingCalls,
		},
	}
}

func sortImpactDefinitions(defs []xref.Definition) {
	sort.Slice(defs, func(i, j int) bool {
		if defs[i].File != defs[j].File {
			return defs[i].File < defs[j].File
		}
		if defs[i].StartLine != defs[j].StartLine {
			return defs[i].StartLine < defs[j].StartLine
		}
		if defs[i].Name != defs[j].Name {
			return defs[i].Name < defs[j].Name
		}
		return defs[i].ID < defs[j].ID
	})
}

func sortImpactCallers(callers []ImpactDirectCaller) {
	sort.Slice(callers, func(i, j int) bool {
		if callers[i].CallCount != callers[j].CallCount {
			return callers[i].CallCount > callers[j].CallCount
		}
		if callers[i].Definition.File != callers[j].Definition.File {
			return callers[i].Definition.File < callers[j].Definition.File
		}
		if callers[i].Definition.StartLine != callers[j].Definition.StartLine {
			return callers[i].Definition.StartLine < callers[j].Definition.StartLine
		}
		if callers[i].Definition.Name != callers[j].Definition.Name {
			return callers[i].Definition.Name < callers[j].Definition.Name
		}
		return callers[i].Definition.ID < callers[j].Definition.ID
	})
}

func xRefDefinitionFromModel(d models.XRefDefinition) xref.Definition {
	return xref.Definition{
		ID:        d.EntityID,
		File:      d.File,
		Package:   d.PackageName,
		Kind:      d.Kind,
		Name:      d.Name,
		Signature: d.Signature,
		Receiver:  d.Receiver,
		StartLine: d.StartLine,
		EndLine:   d.EndLine,
		Callable:  d.Callable,
	}
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
