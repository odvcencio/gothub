package api

import (
	"net/http"

	"github.com/odvcencio/gothub/internal/models"
)

func (s *Server) handleExploreRepos(w http.ResponseWriter, r *http.Request) {
	page, perPage := parsePagination(r, 10, 50)
	sort := r.URL.Query().Get("sort")
	if sort == "" {
		sort = "created"
	}
	offset := (page - 1) * perPage
	repos, err := s.db.ListPublicRepositoriesPage(r.Context(), sort, perPage, offset)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	if repos == nil {
		repos = []models.Repository{}
	}
	jsonResponse(w, http.StatusOK, repos)
}
