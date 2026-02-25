package api

import (
	"sync"
	"time"
)

type repoEvent struct {
	Type       string         `json:"type"`
	RepoID     int64          `json:"repo_id"`
	OccurredAt time.Time      `json:"occurred_at"`
	Payload    map[string]any `json:"payload,omitempty"`
}

type repoEventBroker struct {
	mu   sync.RWMutex
	subs map[int64]map[chan repoEvent]struct{}
}

func newRepoEventBroker() *repoEventBroker {
	return &repoEventBroker{
		subs: make(map[int64]map[chan repoEvent]struct{}),
	}
}

func (b *repoEventBroker) Subscribe(repoID int64) (<-chan repoEvent, func()) {
	ch := make(chan repoEvent, 32)
	b.mu.Lock()
	if _, ok := b.subs[repoID]; !ok {
		b.subs[repoID] = make(map[chan repoEvent]struct{})
	}
	b.subs[repoID][ch] = struct{}{}
	b.mu.Unlock()

	unsubscribe := func() {
		b.mu.Lock()
		if subs, ok := b.subs[repoID]; ok {
			delete(subs, ch)
			if len(subs) == 0 {
				delete(b.subs, repoID)
			}
		}
		b.mu.Unlock()
	}
	return ch, unsubscribe
}

func (b *repoEventBroker) Publish(repoID int64, event repoEvent) {
	b.mu.RLock()
	subs, ok := b.subs[repoID]
	if !ok || len(subs) == 0 {
		b.mu.RUnlock()
		return
	}
	channels := make([]chan repoEvent, 0, len(subs))
	for ch := range subs {
		channels = append(channels, ch)
	}
	b.mu.RUnlock()

	for _, ch := range channels {
		select {
		case ch <- event:
		default:
			// Drop event for slow consumers to keep publisher non-blocking.
		}
	}
}

func (s *Server) publishRepoEvent(repoID int64, eventType string, payload map[string]any) {
	if s.realtime == nil || repoID <= 0 || eventType == "" {
		return
	}
	s.realtime.Publish(repoID, repoEvent{
		Type:       eventType,
		RepoID:     repoID,
		OccurredAt: time.Now().UTC(),
		Payload:    payload,
	})
}
