package service

import (
	"strconv"
	"testing"
	"time"

	"github.com/odvcencio/gts-suite/pkg/model"
)

func BenchmarkCodeIntelCacheSetAndGet(b *testing.B) {
	svc := &CodeIntelService{
		indexes:       make(map[string]codeIntelCacheEntry),
		cacheMaxItems: 4096,
		cacheTTL:      time.Hour,
	}
	idx := &model.Index{}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := "acme/repo@" + strconv.Itoa(i%2048)
		svc.setCachedIndex(key, idx)
		_, _ = svc.getCachedIndex(key)
	}
}

func BenchmarkCodeIntelCacheHitLookup(b *testing.B) {
	svc := &CodeIntelService{
		indexes:       make(map[string]codeIntelCacheEntry),
		cacheMaxItems: 8192,
		cacheTTL:      time.Hour,
	}
	idx := &model.Index{}
	for i := 0; i < 4096; i++ {
		svc.setCachedIndex("acme/repo@"+strconv.Itoa(i), idx)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = svc.getCachedIndex("acme/repo@" + strconv.Itoa(i%4096))
	}
}

func BenchmarkMergePreviewCacheSetAndGet(b *testing.B) {
	svc := &PRService{
		mergePreviewCache: make(map[string]mergePreviewCacheEntry),
	}
	resp := &MergePreviewResponse{
		Files: []FileMergeInfo{
			{Path: "main.go", Status: "clean"},
		},
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := "pr/" + strconv.Itoa(i%1024)
		svc.setCachedMergePreview(key, resp)
		_, _ = svc.getCachedMergePreview(key)
	}
}
