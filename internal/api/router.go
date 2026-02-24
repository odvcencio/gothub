package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"

	"github.com/odvcencio/gothub/internal/auth"
	"github.com/odvcencio/gothub/internal/database"
	"github.com/odvcencio/gothub/internal/gitinterop"
	"github.com/odvcencio/gothub/internal/gotprotocol"
	"github.com/odvcencio/gothub/internal/gotstore"
	"github.com/odvcencio/gothub/internal/service"
	"github.com/odvcencio/gothub/internal/web"
)

type Server struct {
	db           database.DB
	authSvc      *auth.Service
	repoSvc      *service.RepoService
	browseSvc    *service.BrowseService
	diffSvc      *service.DiffService
	prSvc        *service.PRService
	codeIntelSvc *service.CodeIntelService
	mux          *http.ServeMux
}

func NewServer(db database.DB, authSvc *auth.Service, repoSvc *service.RepoService) *Server {
	browseSvc := service.NewBrowseService(repoSvc)
	diffSvc := service.NewDiffService(repoSvc, browseSvc)
	prSvc := service.NewPRService(db, repoSvc, browseSvc)
	codeIntelSvc := service.NewCodeIntelService(repoSvc, browseSvc)
	s := &Server{
		db:           db,
		authSvc:      authSvc,
		repoSvc:      repoSvc,
		browseSvc:    browseSvc,
		diffSvc:      diffSvc,
		prSvc:        prSvc,
		codeIntelSvc: codeIntelSvc,
		mux:          http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	handler := requestLoggingMiddleware(auth.Middleware(s.authSvc)(s.mux))
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

	// Code browsing
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/branches", s.handleListBranches)
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/tree/{ref}/{path...}", s.handleListTree)
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/tree/{ref}", s.handleListTree)
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/blob/{ref}/{path...}", s.handleGetBlob)
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/commits/{ref}", s.handleListCommits)
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/commit/{hash}", s.handleGetCommit)

	// Entities & diff
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/entities/{ref}/{path...}", s.handleListEntities)
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/diff/{spec}", s.handleDiff)

	// Pull requests
	s.mux.HandleFunc("POST /api/v1/repos/{owner}/{repo}/pulls", s.requireAuth(s.handleCreatePR))
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/pulls", s.handleListPRs)
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/pulls/{number}", s.handleGetPR)
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/pulls/{number}/diff", s.handlePRDiff)
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/pulls/{number}/merge-preview", s.handleMergePreview)
	s.mux.HandleFunc("POST /api/v1/repos/{owner}/{repo}/pulls/{number}/merge", s.requireAuth(s.handleMergePR))
	s.mux.HandleFunc("POST /api/v1/repos/{owner}/{repo}/pulls/{number}/comments", s.requireAuth(s.handleCreatePRComment))
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/pulls/{number}/comments", s.handleListPRComments)
	s.mux.HandleFunc("POST /api/v1/repos/{owner}/{repo}/pulls/{number}/reviews", s.requireAuth(s.handleCreatePRReview))
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/pulls/{number}/reviews", s.handleListPRReviews)

	// Code intelligence
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/symbols/{ref}", s.handleSearchSymbols)
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/references/{ref}", s.handleFindReferences)
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/callgraph/{ref}", s.handleCallGraph)

	// Organizations
	s.mux.HandleFunc("POST /api/v1/orgs", s.requireAuth(s.handleCreateOrg))
	s.mux.HandleFunc("GET /api/v1/orgs/{org}", s.handleGetOrg)
	s.mux.HandleFunc("DELETE /api/v1/orgs/{org}", s.requireAuth(s.handleDeleteOrg))
	s.mux.HandleFunc("GET /api/v1/orgs/{org}/members", s.handleListOrgMembers)
	s.mux.HandleFunc("POST /api/v1/orgs/{org}/members", s.requireAuth(s.handleAddOrgMember))
	s.mux.HandleFunc("DELETE /api/v1/orgs/{org}/members/{username}", s.requireAuth(s.handleRemoveOrgMember))
	s.mux.HandleFunc("GET /api/v1/orgs/{org}/repos", s.handleListOrgRepos)
	s.mux.HandleFunc("GET /api/v1/user/orgs", s.requireAuth(s.handleListUserOrgs))

	// Got protocol
	gotProto := gotprotocol.NewHandler(func(owner, repo string) (*gotstore.RepoStore, error) {
		return s.repoSvc.OpenStore(context.Background(), owner, repo)
	}, s.authorizeProtocolRepoAccess)
	gotProto.RegisterRoutes(s.mux)

	// Git smart HTTP protocol
	gitHandler := gitinterop.NewSmartHTTPHandler(
		func(owner, repo string) (*gotstore.RepoStore, error) {
			return s.repoSvc.OpenStore(context.Background(), owner, repo)
		},
		s.db,
		func(ctx context.Context, owner, repo string) (int64, error) {
			r, err := s.repoSvc.Get(ctx, owner, repo)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return 0, err
				}
				return 0, err
			}
			return r.ID, nil
		},
		s.authorizeProtocolRepoAccess,
	)
	gitHandler.RegisterRoutes(s.mux)

	// Frontend SPA — fallback for all non-API/protocol routes
	s.mux.Handle("/", web.Handler())
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
