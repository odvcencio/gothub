package service

import (
	"testing"
	"time"

	"github.com/odvcencio/gts-suite/pkg/model"
)

func TestCodeIntelCacheEvictsOldestEntry(t *testing.T) {
	svc := &CodeIntelService{
		indexes:       make(map[string]*codeIntelCacheEntry),
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

func TestCodeIntelCacheAccessRefreshPreventsEviction(t *testing.T) {
	svc := &CodeIntelService{
		indexes:       make(map[string]*codeIntelCacheEntry),
		cacheMaxItems: 2,
		cacheTTL:      time.Hour,
	}

	svc.setCachedIndex("a", &model.Index{})
	time.Sleep(2 * time.Millisecond)
	svc.setCachedIndex("b", &model.Index{})
	time.Sleep(2 * time.Millisecond)
	if _, ok := svc.getCachedIndex("a"); !ok {
		t.Fatal("expected entry a to be cached before refresh")
	}
	time.Sleep(2 * time.Millisecond)
	svc.setCachedIndex("c", &model.Index{})

	if _, ok := svc.getCachedIndex("a"); !ok {
		t.Fatal("expected refreshed entry a to remain in cache")
	}
	if _, ok := svc.getCachedIndex("b"); ok {
		t.Fatal("expected stale entry b to be evicted")
	}
	if _, ok := svc.getCachedIndex("c"); !ok {
		t.Fatal("expected newest entry c to remain in cache")
	}
}

func TestCodeIntelCacheTTLExpiresEntries(t *testing.T) {
	svc := &CodeIntelService{
		indexes:       make(map[string]*codeIntelCacheEntry),
		cacheMaxItems: 8,
		cacheTTL:      5 * time.Millisecond,
	}

	svc.setCachedIndex("k", &model.Index{})
	time.Sleep(10 * time.Millisecond)

	if _, ok := svc.getCachedIndex("k"); ok {
		t.Fatal("expected cached entry to expire by TTL")
	}
}

func TestCodeIntelSymbolBloomCacheEvictsOldestEntry(t *testing.T) {
	svc := &CodeIntelService{
		symbolBlooms:  make(map[string]*codeIntelBloomCacheEntry),
		cacheMaxItems: 2,
		cacheTTL:      time.Hour,
	}
	filter := buildSymbolSearchBloomFromEntries(nil)

	svc.setCachedSymbolBloom("1@a", filter)
	time.Sleep(2 * time.Millisecond)
	svc.setCachedSymbolBloom("1@b", filter)
	time.Sleep(2 * time.Millisecond)
	svc.setCachedSymbolBloom("1@c", filter)

	if _, ok := svc.getCachedSymbolBloom("1@a"); ok {
		t.Fatal("expected oldest bloom cache entry to be evicted")
	}
	if _, ok := svc.getCachedSymbolBloom("1@b"); !ok {
		t.Fatal("expected bloom entry b to remain in cache")
	}
	if _, ok := svc.getCachedSymbolBloom("1@c"); !ok {
		t.Fatal("expected bloom entry c to remain in cache")
	}
}

func TestCodeIntelSymbolBloomCacheTTLExpiresEntries(t *testing.T) {
	svc := &CodeIntelService{
		symbolBlooms:  make(map[string]*codeIntelBloomCacheEntry),
		cacheMaxItems: 8,
		cacheTTL:      5 * time.Millisecond,
	}
	filter := buildSymbolSearchBloomFromEntries(nil)

	svc.setCachedSymbolBloom("1@k", filter)
	time.Sleep(10 * time.Millisecond)

	if _, ok := svc.getCachedSymbolBloom("1@k"); ok {
		t.Fatal("expected bloom cache entry to expire by TTL")
	}
}
