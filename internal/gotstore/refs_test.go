package gotstore

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/odvcencio/got/pkg/object"
)

func TestRefsSetUsesAtomicLockRename(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "refs")
	r := NewRefs(dir)

	if err := r.Set("heads/main", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"); err != nil {
		t.Fatalf("set ref: %v", err)
	}
	got, err := r.Get("heads/main")
	if err != nil {
		t.Fatalf("get ref: %v", err)
	}
	if string(got) != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("unexpected ref value: %q", got)
	}
}

func TestRefsSetFailsWhenLockExists(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "refs")
	r := NewRefs(dir)

	lockPath := filepath.Join(dir, "heads", "main.lock")
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, []byte("locked"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := r.Set("heads/main", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	if err == nil {
		t.Fatal("expected lock acquisition error")
	}
}

func TestRefsUpdateCASMismatch(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "refs")
	r := NewRefs(dir)

	oldHash := object.Hash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	newHash := object.Hash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	if err := r.Set("heads/main", oldHash); err != nil {
		t.Fatalf("set ref: %v", err)
	}

	expected := object.Hash("cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc")
	err := r.Update("heads/main", &expected, &newHash)
	if err == nil {
		t.Fatal("expected CAS mismatch error")
	}
	var mismatch *RefCASMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("expected RefCASMismatchError, got %T (%v)", err, err)
	}
	if mismatch.Expected != expected || mismatch.Actual != oldHash {
		t.Fatalf("unexpected mismatch error payload: %+v", mismatch)
	}

	got, err := r.Get("heads/main")
	if err != nil {
		t.Fatalf("get ref: %v", err)
	}
	if got != oldHash {
		t.Fatalf("expected ref to remain %s, got %s", oldHash, got)
	}
}
