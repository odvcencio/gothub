package service

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/odvcencio/got/pkg/object"
	"github.com/odvcencio/gothub/internal/database"
	"github.com/odvcencio/gothub/internal/gotstore"
	"github.com/odvcencio/gothub/internal/models"
	"github.com/odvcencio/gts-suite/pkg/model"
	"github.com/odvcencio/gts-suite/pkg/query"
	"github.com/odvcencio/gts-suite/pkg/xref"
)

var (
	benchmarkSymbolResultsSink []SymbolResult
	benchmarkImpactDefsSink    []xref.Definition
	benchmarkImpactCallersSink []ImpactDirectCaller
	benchmarkMergeBaseSink     object.Hash
)

func BenchmarkCodeIntelCacheSetAndGet(b *testing.B) {
	svc := &CodeIntelService{
		indexes:       make(map[string]codeIntelCacheEntry),
		cacheMaxItems: 4096,
		cacheTTL:      time.Hour,
	}
	idx := &model.Index{}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := "acme/repo@" + strconv.Itoa(i%2048)
		svc.setCachedIndex(key, idx)
		_, _ = svc.getCachedIndex(key)
	}
}

func BenchmarkCodeIntelCacheHitLookup(b *testing.B) {
	svc := &CodeIntelService{
		indexes:       make(map[string]codeIntelCacheEntry),
		cacheMaxItems: 8192,
		cacheTTL:      time.Hour,
	}
	idx := &model.Index{}
	for i := 0; i < 4096; i++ {
		svc.setCachedIndex("acme/repo@"+strconv.Itoa(i), idx)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = svc.getCachedIndex("acme/repo@" + strconv.Itoa(i%4096))
	}
}

func BenchmarkMergePreviewCacheSetAndGet(b *testing.B) {
	svc := &PRService{
		mergePreviewCache: make(map[string]mergePreviewCacheEntry),
	}
	resp := &MergePreviewResponse{
		Files: []FileMergeInfo{
			{Path: "main.go", Status: "clean"},
		},
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := "pr/" + strconv.Itoa(i%1024)
		svc.setCachedMergePreview(key, resp)
		_, _ = svc.getCachedMergePreview(key)
	}
}

func BenchmarkCodeIntelSymbolSearchContains(b *testing.B) {
	idx := benchmarkCodeIntelIndex(64, 24)
	queryText := "processorder"

	initial := searchSymbolsFromIndexWithContains(idx, queryText)
	if len(initial) == 0 {
		b.Fatal("expected symbol search fixture to produce matches")
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchmarkSymbolResultsSink = searchSymbolsFromIndexWithContains(idx, queryText)
	}
}

func BenchmarkCodeIntelSymbolSearchSelector(b *testing.B) {
	idx := benchmarkCodeIntelIndex(64, 24)
	sel, err := query.ParseSelector("*[name=/^ProcessOrder$/]")
	if err != nil {
		b.Fatalf("parse selector: %v", err)
	}

	initial := searchSymbolsFromIndexWithSelector(idx, sel)
	if len(initial) == 0 {
		b.Fatal("expected selector fixture to produce matches")
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchmarkSymbolResultsSink = searchSymbolsFromIndexWithSelector(idx, sel)
	}
}

func BenchmarkImpactAnalysisPreparationFromGraph(b *testing.B) {
	graph := benchmarkImpactGraph(512, 4)
	symbol := "ProcessOrder"

	defs := findImpactDefinitionsFromGraph(graph, symbol)
	if len(defs) == 0 {
		b.Fatal("expected impact fixture to produce definitions")
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchmarkImpactDefsSink = findImpactDefinitionsFromGraph(graph, symbol)
		benchmarkImpactCallersSink = collectDirectCallersFromGraph(graph, benchmarkImpactDefsSink)
	}
}

func BenchmarkPRMergeBaseLookupCacheHit(b *testing.B) {
	ctx, prSvc, store, repo := setupPRMergeBenchmarkService(b)

	root := writeBenchmarkMainCommit(b, store, "package main\n\nfunc V() int { return 0 }\n", nil, "root", 1700001000)
	left := writeBenchmarkMainCommit(b, store, "package main\n\nfunc V() int { return 1 }\n", []object.Hash{root}, "left", 1700001010)
	right := writeBenchmarkMainCommit(b, store, "package main\n\nfunc V() int { return 2 }\n", []object.Hash{root}, "right", 1700001020)
	base, err := FindMergeBase(store.Objects, left, right)
	if err != nil {
		b.Fatalf("find merge base: %v", err)
	}
	if err := prSvc.db.SetMergeBaseCache(ctx, repo.ID, string(left), string(right), string(base)); err != nil {
		b.Fatalf("seed merge-base cache: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resolved, err := prSvc.findMergeBaseCached(ctx, repo.ID, store.Objects, left, right)
		if err != nil {
			b.Fatalf("findMergeBaseCached: %v", err)
		}
		benchmarkMergeBaseSink = resolved
	}
	if benchmarkMergeBaseSink != base {
		b.Fatalf("cached merge base = %s, want %s", benchmarkMergeBaseSink, base)
	}
}

func benchmarkCodeIntelIndex(fileCount, symbolsPerFile int) *model.Index {
	idx := &model.Index{
		Version: "0.1.0",
		Files:   make([]model.FileSummary, 0, fileCount),
	}
	for fileNum := 0; fileNum < fileCount; fileNum++ {
		symbols := make([]model.Symbol, 0, symbolsPerFile)
		for symNum := 0; symNum < symbolsPerFile; symNum++ {
			name := fmt.Sprintf("Helper%d_%d", fileNum, symNum)
			if symNum%17 == 0 {
				name = "ProcessOrder"
			}
			symbols = append(symbols, model.Symbol{
				Name:      name,
				Kind:      "function",
				Signature: fmt.Sprintf("func %s()", name),
				StartLine: symNum*3 + 1,
				EndLine:   symNum*3 + 2,
			})
		}
		idx.Files = append(idx.Files, model.FileSummary{
			Path:     fmt.Sprintf("pkg%03d/main.go", fileNum),
			Language: "go",
			Symbols:  symbols,
		})
	}
	return idx
}

func benchmarkImpactGraph(definitionCount, fanIn int) xref.Graph {
	defs := make([]xref.Definition, 0, definitionCount)
	for i := 0; i < definitionCount; i++ {
		name := fmt.Sprintf("Helper%d", i)
		if i%64 == 0 {
			name = "ProcessOrder"
		}
		defs = append(defs, xref.Definition{
			ID:        fmt.Sprintf("def-%04d", i),
			File:      fmt.Sprintf("pkg%03d/main.go", i%32),
			Package:   fmt.Sprintf("pkg%03d", i%32),
			Kind:      "function",
			Name:      name,
			Signature: fmt.Sprintf("func %s()", name),
			StartLine: i*2 + 1,
			EndLine:   i*2 + 2,
			Callable:  true,
		})
	}

	edges := make([]xref.Edge, 0, definitionCount*fanIn)
	for i := 0; i < definitionCount; i++ {
		callee := defs[i]
		for j := 1; j <= fanIn; j++ {
			caller := defs[(i+j)%definitionCount]
			if caller.ID == callee.ID {
				continue
			}
			edges = append(edges, xref.Edge{
				Caller:     caller,
				Callee:     callee,
				Resolution: "static",
				Count:      1 + (j % 3),
			})
		}
	}

	return xref.Graph{
		Root:        "bench",
		Definitions: defs,
		Edges:       edges,
	}
}

func setupPRMergeBenchmarkService(b *testing.B) (context.Context, *PRService, *gotstore.RepoStore, *models.Repository) {
	b.Helper()

	ctx := context.Background()
	tmpDir := b.TempDir()

	db, err := database.OpenSQLite(filepath.Join(tmpDir, "bench.db"))
	if err != nil {
		b.Fatalf("open sqlite: %v", err)
	}
	b.Cleanup(func() {
		_ = db.Close()
	})

	if err := db.Migrate(ctx); err != nil {
		b.Fatalf("migrate sqlite: %v", err)
	}

	user := &models.User{
		Username:     "bench",
		Email:        "bench@example.com",
		PasswordHash: "x",
	}
	if err := db.CreateUser(ctx, user); err != nil {
		b.Fatalf("create benchmark user: %v", err)
	}

	repoSvc := NewRepoService(db, filepath.Join(tmpDir, "repos"))
	repo, err := repoSvc.Create(ctx, user.ID, "repo", "", false)
	if err != nil {
		b.Fatalf("create benchmark repository: %v", err)
	}
	store, err := repoSvc.OpenStore(ctx, "bench", "repo")
	if err != nil {
		b.Fatalf("open repository store: %v", err)
	}

	prSvc := NewPRService(db, repoSvc, nil)
	return ctx, prSvc, store, repo
}

func writeBenchmarkMainCommit(b *testing.B, store *gotstore.RepoStore, content string, parents []object.Hash, message string, ts int64) object.Hash {
	b.Helper()

	blobHash, err := store.Objects.WriteBlob(&object.Blob{Data: []byte(content)})
	if err != nil {
		b.Fatalf("write blob: %v", err)
	}
	treeHash, err := store.Objects.WriteTree(&object.TreeObj{
		Entries: []object.TreeEntry{
			{
				Name:     "main.go",
				BlobHash: blobHash,
			},
		},
	})
	if err != nil {
		b.Fatalf("write tree: %v", err)
	}
	commitHash, err := store.Objects.WriteCommit(&object.CommitObj{
		TreeHash:  treeHash,
		Parents:   parents,
		Author:    "bench",
		Timestamp: ts,
		Message:   message,
	})
	if err != nil {
		b.Fatalf("write commit: %v", err)
	}
	return commitHash
}
