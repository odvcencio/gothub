package service

import (
	"testing"

	"github.com/odvcencio/got/pkg/object"
)

func TestEnsureCommitIndexedPersistsCommitGenerations(t *testing.T) {
	ctx, prSvc, store, repo := setupPRMergeTestService(t)

	root := writeMainCommit(t, store, "package main\n\nfunc V() int { return 0 }\n", nil, "root", 1701000000)
	left := writeMainCommit(t, store, "package main\n\nfunc V() int { return 1 }\n", []object.Hash{root}, "left", 1701000010)
	right := writeMainCommit(t, store, "package main\n\nfunc V() int { return 2 }\n", []object.Hash{root}, "right", 1701000020)
	merge := writeMainCommit(t, store, "package main\n\nfunc V() int { return 3 }\n", []object.Hash{left, right}, "merge", 1701000030)

	browseSvc := NewBrowseService(prSvc.repoSvc)
	codeIntelSvc := NewCodeIntelService(prSvc.db, prSvc.repoSvc, browseSvc)
	if err := codeIntelSvc.EnsureCommitIndexed(ctx, repo.ID, store, "alice/repo", merge); err != nil {
		t.Fatalf("EnsureCommitIndexed: %v", err)
	}

	assertGeneration := func(hash object.Hash, wantGen int64, wantParentCount int) {
		meta, ok, err := prSvc.db.GetCommitMetadata(ctx, repo.ID, string(hash))
		if err != nil {
			t.Fatalf("GetCommitMetadata(%s): %v", hash, err)
		}
		if !ok {
			t.Fatalf("expected commit metadata for %s", hash)
		}
		if meta.Generation != wantGen {
			t.Fatalf("commit %s generation = %d, want %d", hash, meta.Generation, wantGen)
		}
		if meta.ParentCount != wantParentCount {
			t.Fatalf("commit %s parent_count = %d, want %d", hash, meta.ParentCount, wantParentCount)
		}
	}

	assertGeneration(root, 1, 0)
	assertGeneration(left, 2, 1)
	assertGeneration(right, 2, 1)
	assertGeneration(merge, 3, 2)
}
