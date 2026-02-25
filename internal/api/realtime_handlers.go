package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const repoEventKeepAliveInterval = 25 * time.Second

func (s *Server) handleRepoEvents(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.authorizeRepoRequest(w, r, false)
	if !ok {
		return
	}
	controller := http.NewResponseController(w)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	_, _ = fmt.Fprint(w, ": connected\n\n")
	if err := controller.Flush(); err != nil {
		jsonError(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	events, unsubscribe := s.realtime.Subscribe(repo.ID)
	defer unsubscribe()

	ticker := time.NewTicker(repoEventKeepAliveInterval)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case event := <-events:
			body, err := json.Marshal(event)
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, body); err != nil {
				return
			}
			if err := controller.Flush(); err != nil {
				return
			}
		case <-ticker.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			if err := controller.Flush(); err != nil {
				return
			}
		}
	}
}
