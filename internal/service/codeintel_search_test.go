package service

import (
	"testing"

	"github.com/odvcencio/gts-suite/pkg/model"
)

func TestSelectorContainsFilterSyntax(t *testing.T) {
	if selectorContainsFilterSyntax("ProcessOrder") {
		t.Fatal("plain text should not be treated as selector filter syntax")
	}
	if !selectorContainsFilterSyntax("*[name=/^ProcessOrder$/]") {
		t.Fatal("selector with filter clauses should be detected")
	}
}

func TestSearchSymbolsFromIndexWithContains(t *testing.T) {
	idx := &model.Index{
		Files: []model.FileSummary{
			{
				Path: "main.go",
				Symbols: []model.Symbol{
					{Name: "ProcessOrder", Kind: "function", Signature: "func ProcessOrder()", StartLine: 10, EndLine: 12},
					{Name: "HttpClient", Kind: "type", Signature: "type HttpClient struct{}", StartLine: 20, EndLine: 24},
				},
			},
		},
	}

	results := searchSymbolsFromIndexWithContains(idx, "order")
	if len(results) != 1 {
		t.Fatalf("expected 1 contains result, got %+v", results)
	}
	if results[0].Name != "ProcessOrder" {
		t.Fatalf("unexpected symbol in contains results: %+v", results[0])
	}
}

func TestBuildEntityIndexEntries(t *testing.T) {
	idx := &model.Index{
		Files: []model.FileSummary{
			{
				Path:     "main.go",
				Language: "go",
				Symbols: []model.Symbol{
					{Name: "ProcessOrder", Kind: "function", Signature: "func ProcessOrder()", StartLine: 10, EndLine: 12},
					{Name: "ValidateOrder", Kind: "function", Signature: "func ValidateOrder()", StartLine: 14, EndLine: 16},
				},
			},
		},
	}

	entries := buildEntityIndexEntries(42, "commit", idx)
	if len(entries) != 2 {
		t.Fatalf("expected 2 entity index entries, got %+v", entries)
	}
	if entries[0].RepoID != 42 || entries[0].CommitHash != "commit" {
		t.Fatalf("unexpected repo/commit values: %+v", entries[0])
	}
	if entries[0].SymbolKey == "" || entries[1].SymbolKey == "" {
		t.Fatalf("expected non-empty symbol keys: %+v", entries)
	}
	if entries[0].SymbolKey == entries[1].SymbolKey {
		t.Fatalf("expected unique symbol keys, got %+v", entries)
	}
}
