package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/odvcencio/got/pkg/object"
	"github.com/odvcencio/gts-suite/pkg/index"
	"github.com/odvcencio/gts-suite/pkg/model"
	"github.com/odvcencio/gts-suite/pkg/query"
	"github.com/odvcencio/gts-suite/pkg/xref"
)

// CodeIntelService provides code intelligence powered by gts-suite.
type CodeIntelService struct {
	repoSvc  *RepoService
	browsSvc *BrowseService

	mu      sync.RWMutex
	indexes map[string]*model.Index // cache: "owner/repo@ref" -> index
}

func NewCodeIntelService(repoSvc *RepoService, browseSvc *BrowseService) *CodeIntelService {
	return &CodeIntelService{
		repoSvc:  repoSvc,
		browsSvc: browseSvc,
		indexes:  make(map[string]*model.Index),
	}
}

// BuildIndex builds or returns cached index for a repo at a given ref.
// For now, this materializes the tree to a temp dir and indexes it.
func (s *CodeIntelService) BuildIndex(ctx context.Context, owner, repo, ref string) (*model.Index, error) {
	key := fmt.Sprintf("%s/%s@%s", owner, repo, ref)

	s.mu.RLock()
	if idx, ok := s.indexes[key]; ok {
		s.mu.RUnlock()
		return idx, nil
	}
	s.mu.RUnlock()

	// Materialize tree to temp dir
	files, err := s.browsSvc.FlattenTree(ctx, owner, repo, ref)
	if err != nil {
		return nil, fmt.Errorf("flatten tree: %w", err)
	}

	store, err := s.repoSvc.OpenStore(ctx, owner, repo)
	if err != nil {
		return nil, err
	}

	tmpDir, err := os.MkdirTemp("", "gothub-index-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)

	// Write all files to temp dir by reading blobs
	for _, fe := range files {
		blob, bErr := store.Objects.ReadBlob(object.Hash(fe.BlobHash))
		if bErr != nil {
			continue
		}
		full := filepath.Join(tmpDir, fe.Path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(full, blob.Data, 0o644); err != nil {
			return nil, err
		}
	}

	// Build index
	builder := index.NewBuilder()
	idx, err := builder.BuildPath(tmpDir)
	if err != nil {
		return nil, fmt.Errorf("build index: %w", err)
	}

	s.mu.Lock()
	s.indexes[key] = idx
	s.mu.Unlock()

	return idx, nil
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
	idx, err := s.BuildIndex(ctx, owner, repo, ref)
	if err != nil {
		return nil, err
	}

	sel, err := query.ParseSelector(selectorStr)
	if err != nil {
		return nil, fmt.Errorf("parse selector: %w", err)
	}

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
	return results, nil
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
