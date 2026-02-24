package api

import (
	"context"
	"net/http"

	"github.com/odvcencio/gothub/internal/auth"
	"github.com/odvcencio/gothub/internal/database"
	"github.com/odvcencio/gothub/internal/gotprotocol"
	"github.com/odvcencio/gothub/internal/gotstore"
	"github.com/odvcencio/gothub/internal/service"
)

type Server struct {
	db      database.DB
	authSvc *auth.Service
	repoSvc *service.RepoService
	mux     *http.ServeMux
}

func NewServer(db database.DB, authSvc *auth.Service, repoSvc *service.RepoService) *Server {
	s := &Server{
		db:      db,
		authSvc: authSvc,
		repoSvc: repoSvc,
		mux:     http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	handler := auth.Middleware(s.authSvc)(s.mux)
	handler.ServeHTTP(w, r)
}

func (s *Server) routes() {
	// Auth
	s.mux.HandleFunc("POST /api/v1/auth/register", s.handleRegister)
	s.mux.HandleFunc("POST /api/v1/auth/login", s.handleLogin)

	// User
	s.mux.HandleFunc("GET /api/v1/user", s.requireAuth(s.handleGetCurrentUser))
	s.mux.HandleFunc("GET /api/v1/user/ssh-keys", s.requireAuth(s.handleListSSHKeys))
	s.mux.HandleFunc("POST /api/v1/user/ssh-keys", s.requireAuth(s.handleCreateSSHKey))
	s.mux.HandleFunc("DELETE /api/v1/user/ssh-keys/{id}", s.requireAuth(s.handleDeleteSSHKey))

	// Repositories
	s.mux.HandleFunc("POST /api/v1/repos", s.requireAuth(s.handleCreateRepo))
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}", s.handleGetRepo)
	s.mux.HandleFunc("GET /api/v1/user/repos", s.requireAuth(s.handleListUserRepos))
	s.mux.HandleFunc("DELETE /api/v1/repos/{owner}/{repo}", s.requireAuth(s.handleDeleteRepo))

	// Got protocol
	gotProto := gotprotocol.NewHandler(func(owner, repo string) (*gotstore.RepoStore, error) {
		return s.repoSvc.OpenStore(context.Background(), owner, repo)
	})
	gotProto.RegisterRoutes(s.mux)
}

func (s *Server) requireAuth(fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if auth.GetClaims(r.Context()) == nil {
			http.Error(w, `{"error":"authentication required"}`, http.StatusUnauthorized)
			return
		}
		fn(w, r)
	}
}
