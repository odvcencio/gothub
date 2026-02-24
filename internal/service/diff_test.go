package service

import (
	"encoding/json"
	"testing"

	"github.com/odvcencio/got/pkg/diff"
	"github.com/odvcencio/got/pkg/entity"
)

func TestFileDiffToResponseClassifiesSemanticChanges(t *testing.T) {
	fd := &diff.FileDiff{
		Path: "main.go",
		Changes: []diff.EntityChange{
			{
				Type:  diff.Added,
				Key:   "decl:new",
				After: semanticTestEntity("NewFeature", "func NewFeature() int", "func NewFeature() int { return 1 }"),
			},
			{
				Type:   diff.Removed,
				Key:    "decl:old",
				Before: semanticTestEntity("Legacy", "func Legacy() int", "func Legacy() int { return 1 }"),
			},
			{
				Type:   diff.Modified,
				Key:    "decl:sig",
				Before: semanticTestEntity("ProcessOrder", "func ProcessOrder(input string) int", "func ProcessOrder(input string) int { return 1 }"),
				After:  semanticTestEntity("ProcessOrder", "func ProcessOrder(input string, retries int) int", "func ProcessOrder(input string, retries int) int { return retries }"),
			},
			{
				Type:   diff.Modified,
				Key:    "decl:body",
				Before: semanticTestEntity("applyDiscount", "func applyDiscount(v int) int", "func applyDiscount(v int) int { return v }"),
				After:  semanticTestEntity("applyDiscount", "func applyDiscount(v int) int", "func applyDiscount(v int) int { return v + 1 }"),
			},
		},
	}

	resp := fileDiffToResponse(fd)
	classCounts := map[string]int{}
	for _, change := range resp.Changes {
		classCounts[change.Classification]++
	}

	if classCounts[semanticChangeAddition] != 1 {
		t.Fatalf("expected one addition classification, got %v", classCounts)
	}
	if classCounts[semanticChangeRemoval] != 1 {
		t.Fatalf("expected one removal classification, got %v", classCounts)
	}
	if classCounts[semanticChangeSignature] != 1 {
		t.Fatalf("expected one signature-change classification, got %v", classCounts)
	}
	if classCounts[semanticChangeBodyOnly] != 1 {
		t.Fatalf("expected one body-only classification, got %v", classCounts)
	}
}

func TestDiffResponseMarshalJSONDerivesSummaryCounts(t *testing.T) {
	resp := DiffResponse{
		Base: "base",
		Head: "head",
		Files: []FileDiffResponse{
			{
				Path: "main.go",
				Changes: []EntityChangeInfo{
					{Type: "added", Classification: semanticChangeAddition, Key: "decl:add"},
					{Type: "removed", Classification: semanticChangeRemoval, Key: "decl:remove"},
					{
						Type:   "modified",
						Key:    "decl:sig",
						Before: &EntityInfo{Name: "ProcessOrder", DeclKind: "function", Signature: "func ProcessOrder(input string) int", BodyHash: "hash-a"},
						After:  &EntityInfo{Name: "ProcessOrder", DeclKind: "function", Signature: "func ProcessOrder(input string, retries int) int", BodyHash: "hash-b"},
					},
					{
						Type:   "modified",
						Key:    "decl:body",
						Before: &EntityInfo{Name: "applyDiscount", DeclKind: "function", Signature: "func applyDiscount(v int) int", BodyHash: "hash-c"},
						After:  &EntityInfo{Name: "applyDiscount", DeclKind: "function", Signature: "func applyDiscount(v int) int", BodyHash: "hash-d"},
					},
				},
			},
		},
	}

	encoded, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal diff response: %v", err)
	}

	var decoded struct {
		Summary DiffSummaryCounts `json:"summary"`
	}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal diff response: %v", err)
	}

	if decoded.Summary.TotalChanges != 4 {
		t.Fatalf("expected total changes=4, got %+v", decoded.Summary)
	}
	if decoded.Summary.Additions != 1 {
		t.Fatalf("expected additions=1, got %+v", decoded.Summary)
	}
	if decoded.Summary.Removals != 1 {
		t.Fatalf("expected removals=1, got %+v", decoded.Summary)
	}
	if decoded.Summary.SignatureChanges != 1 {
		t.Fatalf("expected signature_changes=1, got %+v", decoded.Summary)
	}
	if decoded.Summary.BodyOnlyChanges != 1 {
		t.Fatalf("expected body_only_changes=1, got %+v", decoded.Summary)
	}
}

func semanticTestEntity(name, signature, body string) *entity.Entity {
	e := &entity.Entity{
		Kind:      entity.KindDeclaration,
		Name:      name,
		DeclKind:  "function",
		Signature: signature,
		Body:      []byte(body),
	}
	e.ComputeHash()
	return e
}
