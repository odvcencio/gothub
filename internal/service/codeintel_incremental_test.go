package service

import (
	"fmt"
	"testing"
	"time"

	"github.com/odvcencio/got/pkg/object"
	"github.com/odvcencio/gothub/internal/gotstore"
	"github.com/odvcencio/gothub/internal/models"
	"github.com/odvcencio/gts-suite/pkg/model"
)

func TestEnsureCommitIndexedIncrementalParsesChangedFilesOnly(t *testing.T) {
	ctx, prSvc, store, repo := setupPRMergeTestService(t)

	browseSvc := NewBrowseService(prSvc.repoSvc)
	codeIntelSvc := NewCodeIntelService(prSvc.db, prSvc.repoSvc, browseSvc)

	parentFiles := map[string]string{
		"changed.go":   "package main\n\nfunc OldValue() int { return 1 }\n",
		"unchanged.go": "package main\n\nfunc SourceSymbol() int { return 10 }\n",
	}
	parent := writeCodeIntelIncrementalCommit(t, store, parentFiles, nil, "parent", 1702000000)

	parentIndex := &model.Index{
		Version:     repoIndexSchemaVersion,
		Root:        fmt.Sprintf("alice/repo@%s", parent),
		GeneratedAt: time.Unix(1702000000, 0).UTC(),
		Files: []model.FileSummary{
			{
				Path:      "changed.go",
				Language:  "go",
				SizeBytes: 32,
				Symbols: []model.Symbol{
					{File: "changed.go", Kind: "function", Name: "ParentChangedSentinel", Signature: "func ParentChangedSentinel()", StartLine: 3, EndLine: 3},
				},
			},
			{
				Path:      "unchanged.go",
				Language:  "go",
				SizeBytes: 32,
				Symbols: []model.Symbol{
					{File: "unchanged.go", Kind: "function", Name: "CarryForwardSentinel", Signature: "func CarryForwardSentinel()", StartLine: 3, EndLine: 3},
				},
			},
		},
	}
	if err := codeIntelSvc.persistIndex(ctx, store, repo.ID, string(parent), parentIndex); err != nil {
		t.Fatalf("persist parent index: %v", err)
	}

	childFiles := cloneCodeIntelIncrementalFiles(parentFiles)
	childFiles["changed.go"] = "package main\n\nfunc FreshValue() int { return 2 }\n"
	child := writeCodeIntelIncrementalCommit(t, store, childFiles, []object.Hash{parent}, "child", 1702000010)

	if err := codeIntelSvc.EnsureCommitIndexed(ctx, repo.ID, store, "alice/repo", child); err != nil {
		t.Fatalf("EnsureCommitIndexed: %v", err)
	}

	idx, ok, err := codeIntelSvc.loadPersistedIndex(ctx, store, repo.ID, string(child))
	if err != nil {
		t.Fatalf("loadPersistedIndex: %v", err)
	}
	if !ok {
		t.Fatalf("expected persisted index for %s", child)
	}

	changedSummary, ok := codeIntelFileSummaryByPath(idx, "changed.go")
	if !ok {
		t.Fatalf("expected changed.go summary in incremental index")
	}
	if !codeIntelSummaryHasSymbol(changedSummary, "FreshValue") {
		t.Fatalf("expected changed.go to be reparsed with FreshValue symbol, got %+v", changedSummary.Symbols)
	}
	if codeIntelSummaryHasSymbol(changedSummary, "ParentChangedSentinel") {
		t.Fatalf("changed.go reused parent summary unexpectedly: %+v", changedSummary.Symbols)
	}

	unchangedSummary, ok := codeIntelFileSummaryByPath(idx, "unchanged.go")
	if !ok {
		t.Fatalf("expected unchanged.go summary in incremental index")
	}
	if !codeIntelSummaryHasSymbol(unchangedSummary, "CarryForwardSentinel") {
		t.Fatalf("expected unchanged.go summary to be carried forward from parent index, got %+v", unchangedSummary.Symbols)
	}
	if codeIntelSummaryHasSymbol(unchangedSummary, "SourceSymbol") {
		t.Fatalf("unchanged.go appears reparsed instead of carried forward: %+v", unchangedSummary.Symbols)
	}

	hasEntityIndex, err := prSvc.db.HasEntityIndexForCommit(ctx, repo.ID, string(child))
	if err != nil {
		t.Fatalf("HasEntityIndexForCommit: %v", err)
	}
	if !hasEntityIndex {
		t.Fatalf("expected entity index entries persisted for child commit")
	}
	entityEntries, err := prSvc.db.ListEntityIndexEntriesByCommit(ctx, repo.ID, string(child), "", 0)
	if err != nil {
		t.Fatalf("ListEntityIndexEntriesByCommit: %v", err)
	}
	if !codeIntelEntityEntriesContainName(entityEntries, "FreshValue") {
		t.Fatalf("expected entity index to include changed symbol FreshValue")
	}
	if !codeIntelEntityEntriesContainName(entityEntries, "CarryForwardSentinel") {
		t.Fatalf("expected entity index to include carried-forward symbol CarryForwardSentinel")
	}
}

func TestEnsureCommitIndexedIncrementalCarriesUnchangedParseErrors(t *testing.T) {
	ctx, prSvc, store, repo := setupPRMergeTestService(t)

	browseSvc := NewBrowseService(prSvc.repoSvc)
	codeIntelSvc := NewCodeIntelService(prSvc.db, prSvc.repoSvc, browseSvc)

	parentFiles := map[string]string{
		"broken.go":  "package main\n\nfunc Broken( {\n",
		"changed.go": "package main\n\nfunc OldValue() int { return 1 }\n",
	}
	parent := writeCodeIntelIncrementalCommit(t, store, parentFiles, nil, "parent", 1703000000)

	parentIndex := &model.Index{
		Version:     repoIndexSchemaVersion,
		Root:        fmt.Sprintf("alice/repo@%s", parent),
		GeneratedAt: time.Unix(1703000000, 0).UTC(),
		Files: []model.FileSummary{
			{
				Path:      "changed.go",
				Language:  "go",
				SizeBytes: 32,
				Symbols: []model.Symbol{
					{File: "changed.go", Kind: "function", Name: "ParentChangedSentinel", Signature: "func ParentChangedSentinel()", StartLine: 3, EndLine: 3},
				},
			},
		},
		Errors: []model.ParseError{
			{Path: "broken.go", Error: "sentinel parent parse error"},
		},
	}
	if err := codeIntelSvc.persistIndex(ctx, store, repo.ID, string(parent), parentIndex); err != nil {
		t.Fatalf("persist parent index: %v", err)
	}

	childFiles := cloneCodeIntelIncrementalFiles(parentFiles)
	childFiles["changed.go"] = "package main\n\nfunc FreshValue() int { return 2 }\n"
	child := writeCodeIntelIncrementalCommit(t, store, childFiles, []object.Hash{parent}, "child", 1703000010)

	if err := codeIntelSvc.EnsureCommitIndexed(ctx, repo.ID, store, "alice/repo", child); err != nil {
		t.Fatalf("EnsureCommitIndexed: %v", err)
	}

	idx, ok, err := codeIntelSvc.loadPersistedIndex(ctx, store, repo.ID, string(child))
	if err != nil {
		t.Fatalf("loadPersistedIndex: %v", err)
	}
	if !ok {
		t.Fatalf("expected persisted index for %s", child)
	}

	parseErr, ok := codeIntelParseErrorByPath(idx, "broken.go")
	if !ok {
		t.Fatalf("expected unchanged parse error for broken.go to be carried forward")
	}
	if parseErr.Error != "sentinel parent parse error" {
		t.Fatalf("broken.go parse error = %q, want sentinel parent parse error", parseErr.Error)
	}

	changedSummary, ok := codeIntelFileSummaryByPath(idx, "changed.go")
	if !ok {
		t.Fatalf("expected changed.go summary in incremental index")
	}
	if !codeIntelSummaryHasSymbol(changedSummary, "FreshValue") {
		t.Fatalf("expected changed.go to be reparsed with FreshValue symbol, got %+v", changedSummary.Symbols)
	}
}

func writeCodeIntelIncrementalCommit(
	t *testing.T,
	store *gotstore.RepoStore,
	files map[string]string,
	parents []object.Hash,
	message string,
	ts int64,
) object.Hash {
	t.Helper()

	blobHashes := make(map[string]object.Hash, len(files))
	for path, content := range files {
		blobHash, err := store.Objects.WriteBlob(&object.Blob{Data: []byte(content)})
		if err != nil {
			t.Fatalf("write blob for %s: %v", path, err)
		}
		blobHashes[path] = blobHash
	}

	treeHash, err := buildTreeFromFiles(store.Objects, blobHashes)
	if err != nil {
		t.Fatalf("build tree: %v", err)
	}

	commitHash, err := store.Objects.WriteCommit(&object.CommitObj{
		TreeHash:  treeHash,
		Parents:   parents,
		Author:    "alice",
		Timestamp: ts,
		Message:   message,
	})
	if err != nil {
		t.Fatalf("write commit: %v", err)
	}
	return commitHash
}

func cloneCodeIntelIncrementalFiles(src map[string]string) map[string]string {
	cloned := make(map[string]string, len(src))
	for path, content := range src {
		cloned[path] = content
	}
	return cloned
}

func codeIntelFileSummaryByPath(idx *model.Index, path string) (model.FileSummary, bool) {
	if idx == nil {
		return model.FileSummary{}, false
	}
	for i := range idx.Files {
		if idx.Files[i].Path == path {
			return idx.Files[i], true
		}
	}
	return model.FileSummary{}, false
}

func codeIntelParseErrorByPath(idx *model.Index, path string) (model.ParseError, bool) {
	if idx == nil {
		return model.ParseError{}, false
	}
	for i := range idx.Errors {
		if idx.Errors[i].Path == path {
			return idx.Errors[i], true
		}
	}
	return model.ParseError{}, false
}

func codeIntelSummaryHasSymbol(summary model.FileSummary, symbolName string) bool {
	for i := range summary.Symbols {
		if summary.Symbols[i].Name == symbolName {
			return true
		}
	}
	return false
}

func codeIntelEntityEntriesContainName(entries []models.EntityIndexEntry, name string) bool {
	for i := range entries {
		if entries[i].Name == name {
			return true
		}
	}
	return false
}
