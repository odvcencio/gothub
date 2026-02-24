package entityutil

import (
	"path/filepath"
	"testing"

	"github.com/odvcencio/got/pkg/entity"
	"github.com/odvcencio/got/pkg/object"
	"github.com/odvcencio/gothub/internal/gotstore"
)

func TestKindNameMapping(t *testing.T) {
	cases := []struct {
		kind entity.EntityKind
		want string
	}{
		{entity.KindPreamble, "preamble"},
		{entity.KindImportBlock, "import"},
		{entity.KindDeclaration, "declaration"},
		{entity.KindInterstitial, "interstitial"},
		{entity.EntityKind(999), "unknown"},
	}

	for _, tc := range cases {
		if got := KindName(tc.kind); got != tc.want {
			t.Fatalf("KindName(%v) = %q, want %q", tc.kind, got, tc.want)
		}
	}
}

func TestExtractAndWriteEntityListPersistsEntities(t *testing.T) {
	store, err := gotstore.Open(filepath.Join(t.TempDir(), "repo"))
	if err != nil {
		t.Fatal(err)
	}
	blobHash, err := store.Objects.WriteBlob(&object.Blob{
		Data: []byte("package main\n\nfunc ProcessOrder() int { return 1 }\n"),
	})
	if err != nil {
		t.Fatal(err)
	}

	listHash, ok, err := ExtractAndWriteEntityList(store.Objects, "main.go", blobHash)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected extraction to persist an entity list")
	}
	if listHash == "" {
		t.Fatal("expected non-empty entity list hash")
	}

	el, err := store.Objects.ReadEntityList(listHash)
	if err != nil {
		t.Fatal(err)
	}
	if len(el.EntityRefs) == 0 {
		t.Fatal("expected entity list refs to be populated")
	}
}

func TestExtractAndWriteEntityListSkipsUnsupportedFiles(t *testing.T) {
	store, err := gotstore.Open(filepath.Join(t.TempDir(), "repo"))
	if err != nil {
		t.Fatal(err)
	}
	blobHash, err := store.Objects.WriteBlob(&object.Blob{Data: []byte("hello world")})
	if err != nil {
		t.Fatal(err)
	}

	listHash, ok, err := ExtractAndWriteEntityList(store.Objects, "notes.txt", blobHash)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected unsupported extension to skip entity persistence")
	}
	if listHash != "" {
		t.Fatalf("expected empty hash when extraction skipped, got %s", listHash)
	}
}
