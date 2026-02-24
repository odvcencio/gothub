package api

import (
	"net/http"

	"github.com/odvcencio/gothub/internal/auth"
)

type repoStarsResponse struct {
	Count   int  `json:"count"`
	Starred bool `json:"starred"`
}

type stargazerResponse struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

func (s *Server) handleGetRepoStars(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.authorizeRepoRequest(w, r, false)
	if !ok {
		return
	}

	count, err := s.db.CountRepoStars(r.Context(), repo.ID)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	starred := false
	if claims := auth.GetClaims(r.Context()); claims != nil {
		starred, err = s.db.IsRepoStarred(r.Context(), repo.ID, claims.UserID)
		if err != nil {
			jsonError(w, "internal error", http.StatusInternalServerError)
			return
		}
	}

	jsonResponse(w, http.StatusOK, repoStarsResponse{Count: count, Starred: starred})
}

func (s *Server) handleStarRepo(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		jsonError(w, "authentication required", http.StatusUnauthorized)
		return
	}
	repo, ok := s.authorizeRepoRequest(w, r, false)
	if !ok {
		return
	}

	if err := s.db.AddRepoStar(r.Context(), repo.ID, claims.UserID); err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	count, err := s.db.CountRepoStars(r.Context(), repo.ID)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	jsonResponse(w, http.StatusOK, repoStarsResponse{Count: count, Starred: true})
}

func (s *Server) handleUnstarRepo(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		jsonError(w, "authentication required", http.StatusUnauthorized)
		return
	}
	repo, ok := s.authorizeRepoRequest(w, r, false)
	if !ok {
		return
	}

	if err := s.db.RemoveRepoStar(r.Context(), repo.ID, claims.UserID); err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	count, err := s.db.CountRepoStars(r.Context(), repo.ID)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	jsonResponse(w, http.StatusOK, repoStarsResponse{Count: count, Starred: false})
}

func (s *Server) handleListRepoStargazers(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.authorizeRepoRequest(w, r, false)
	if !ok {
		return
	}

	page, perPage := parsePagination(r, 50, 200)
	limit := perPage
	offset := (page - 1) * perPage
	users, err := s.db.ListRepoStargazersPage(r.Context(), repo.ID, limit, offset)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	resp := make([]stargazerResponse, 0, len(users))
	for i := range users {
		resp = append(resp, stargazerResponse{ID: users[i].ID, Username: users[i].Username})
	}
	jsonResponse(w, http.StatusOK, resp)
}

func (s *Server) handleListUserStarredRepos(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		jsonError(w, "authentication required", http.StatusUnauthorized)
		return
	}

	page, perPage := parsePagination(r, 30, 200)
	limit := perPage
	offset := (page - 1) * perPage
	repos, err := s.db.ListUserStarredRepositoriesPage(r.Context(), claims.UserID, limit, offset)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	jsonResponse(w, http.StatusOK, repos)
}
