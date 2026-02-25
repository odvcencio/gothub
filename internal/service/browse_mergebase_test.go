package service

import (
	"strings"
	"testing"

	"github.com/odvcencio/got/pkg/object"
)

func TestFindMergeBaseReturnsNearestCommonAncestor(t *testing.T) {
	_, _, store, _ := setupPRMergeTestService(t)

	root := writeMainCommit(t, store, "package main\n\nfunc V() int { return 0 }\n", nil, "root", 1700000000)
	a1 := writeMainCommit(t, store, "package main\n\nfunc V() int { return 1 }\n", []object.Hash{root}, "a1", 1700000010)
	a2 := writeMainCommit(t, store, "package main\n\nfunc V() int { return 2 }\n", []object.Hash{a1}, "a2", 1700000020)
	b1 := writeMainCommit(t, store, "package main\n\nfunc V() int { return 3 }\n", []object.Hash{a1}, "b1", 1700000030)
	b2 := writeMainCommit(t, store, "package main\n\nfunc V() int { return 4 }\n", []object.Hash{b1}, "b2", 1700000040)

	base, err := FindMergeBase(store.Objects, a2, b2)
	if err != nil {
		t.Fatalf("FindMergeBase: %v", err)
	}
	if base != a1 {
		t.Fatalf("merge base = %s, want %s", base, a1)
	}
}

func TestFindMergeBaseHandlesAncestorFastPath(t *testing.T) {
	_, _, store, _ := setupPRMergeTestService(t)

	root := writeMainCommit(t, store, "package main\n\nfunc V() int { return 0 }\n", nil, "root", 1700000100)
	left := writeMainCommit(t, store, "package main\n\nfunc V() int { return 1 }\n", []object.Hash{root}, "left", 1700000110)
	right := writeMainCommit(t, store, "package main\n\nfunc V() int { return 2 }\n", []object.Hash{root}, "right", 1700000120)
	merge := writeMainCommit(t, store, "package main\n\nfunc V() int { return 3 }\n", []object.Hash{left, right}, "merge", 1700000130)
	tip := writeMainCommit(t, store, "package main\n\nfunc V() int { return 4 }\n", []object.Hash{merge}, "tip", 1700000140)

	base, err := FindMergeBase(store.Objects, tip, left)
	if err != nil {
		t.Fatalf("FindMergeBase: %v", err)
	}
	if base != left {
		t.Fatalf("merge base = %s, want %s", base, left)
	}
}

func TestFindMergeBaseNoCommonAncestor(t *testing.T) {
	_, _, store, _ := setupPRMergeTestService(t)

	rootA := writeMainCommit(t, store, "package main\n\nfunc A() int { return 1 }\n", nil, "rootA", 1700000200)
	rootB := writeMainCommit(t, store, "package main\n\nfunc B() int { return 1 }\n", nil, "rootB", 1700000210)

	_, err := FindMergeBase(store.Objects, rootA, rootB)
	if err == nil {
		t.Fatal("expected no common ancestor error")
	}
	if !strings.Contains(err.Error(), "no common ancestor") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFindMergeBaseUsesCacheWhenAvailable(t *testing.T) {
	_, _, store, _ := setupPRMergeTestService(t)

	root := writeMainCommit(t, store, "package main\n\nfunc C() int { return 0 }\n", nil, "root", 1700000300)
	ours := writeMainCommit(t, store, "package main\n\nfunc C() int { return 1 }\n", []object.Hash{root}, "ours", 1700000310)
	theirs := writeMainCommit(t, store, "package main\n\nfunc C() int { return 2 }\n", []object.Hash{root}, "theirs", 1700000320)

	base1, err := FindMergeBase(store.Objects, ours, theirs)
	if err != nil {
		t.Fatalf("FindMergeBase first call: %v", err)
	}
	base2, err := FindMergeBase(store.Objects, ours, theirs)
	if err != nil {
		t.Fatalf("FindMergeBase second call: %v", err)
	}
	if base1 != root || base2 != root {
		t.Fatalf("expected cached merge base %s, got %s and %s", root, base1, base2)
	}

	// Ensure cache key is order-invariant.
	base3, err := FindMergeBase(store.Objects, theirs, ours)
	if err != nil {
		t.Fatalf("FindMergeBase reversed call: %v", err)
	}
	if base3 != root {
		t.Fatalf("expected cached merge base %s for reversed order, got %s", root, base3)
	}
}
