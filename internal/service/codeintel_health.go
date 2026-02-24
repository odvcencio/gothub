package service

import "time"

type CodeIntelCacheStats struct {
	Entries    int
	MaxItems   int
	TTLSeconds int64
}

func (s *CodeIntelService) CacheStats() CodeIntelCacheStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return CodeIntelCacheStats{
		Entries:    len(s.indexes),
		MaxItems:   s.cacheMaxItems,
		TTLSeconds: int64(s.cacheTTL / time.Second),
	}
}
