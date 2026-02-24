package service

import (
	"testing"
	"time"

	"github.com/odvcencio/gts-suite/pkg/model"
)

func TestCodeIntelCacheEvictsOldestEntry(t *testing.T) {
	svc := &CodeIntelService{
		indexes:       make(map[string]codeIntelCacheEntry),
		cacheMaxItems: 2,
		cacheTTL:      time.Hour,
	}

	svc.setCachedIndex("a", &model.Index{})
	time.Sleep(2 * time.Millisecond)
	svc.setCachedIndex("b", &model.Index{})
	time.Sleep(2 * time.Millisecond)
	svc.setCachedIndex("c", &model.Index{})

	if _, ok := svc.getCachedIndex("a"); ok {
		t.Fatal("expected oldest cache entry to be evicted")
	}
	if _, ok := svc.getCachedIndex("b"); !ok {
		t.Fatal("expected entry b to remain in cache")
	}
	if _, ok := svc.getCachedIndex("c"); !ok {
		t.Fatal("expected entry c to remain in cache")
	}
}

func TestCodeIntelCacheTTLExpiresEntries(t *testing.T) {
	svc := &CodeIntelService{
		indexes:       make(map[string]codeIntelCacheEntry),
		cacheMaxItems: 8,
		cacheTTL:      5 * time.Millisecond,
	}

	svc.setCachedIndex("k", &model.Index{})
	time.Sleep(10 * time.Millisecond)

	if _, ok := svc.getCachedIndex("k"); ok {
		t.Fatal("expected cached entry to expire by TTL")
	}
}
