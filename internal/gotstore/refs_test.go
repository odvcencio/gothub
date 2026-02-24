package gotstore

import (
	"os"
	"path/filepath"
	"testing"
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
