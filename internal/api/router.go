package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"

	"github.com/odvcencio/got/pkg/object"
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
	issueSvc     *service.IssueService
	webhookSvc   *service.WebhookService
	notifySvc    *service.NotificationService
	codeIntelSvc *service.CodeIntelService
	lineageSvc   *service.EntityLineageService
	rateLimiter  *requestRateLimiter
	mux          *http.ServeMux
	handler      http.Handler
}

func NewServer(db database.DB, authSvc *auth.Service, repoSvc *service.RepoService) *Server {
	browseSvc := service.NewBrowseService(repoSvc)
	lineageSvc := service.NewEntityLineageService(db)
	diffSvc := service.NewDiffService(repoSvc, browseSvc, db, lineageSvc)
	prSvc := service.NewPRService(db, repoSvc, browseSvc)
	issueSvc := service.NewIssueService(db)
	webhookSvc := service.NewWebhookService(db)
	notifySvc := service.NewNotificationService(db)
	codeIntelSvc := service.NewCodeIntelService(db, repoSvc, browseSvc)
	prSvc.SetCodeIntelService(codeIntelSvc)
	s := &Server{
		db:           db,
		authSvc:      authSvc,
		repoSvc:      repoSvc,
		browseSvc:    browseSvc,
		diffSvc:      diffSvc,
		prSvc:        prSvc,
		issueSvc:     issueSvc,
		webhookSvc:   webhookSvc,
		notifySvc:    notifySvc,
		codeIntelSvc: codeIntelSvc,
		lineageSvc:   lineageSvc,
		rateLimiter:  newRequestRateLimiter(),
		mux:          http.NewServeMux(),
	}
	s.routes()
	s.handler = requestLoggingMiddleware(
		corsMiddleware(
			requestRateLimitMiddleware(
				s.rateLimiter,
				requestBodyLimitMiddleware(auth.Middleware(s.authSvc)(s.mux)),
			),
		),
	)
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
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
	s.mux.HandleFunc("GET /api/v1/user/starred", s.requireAuth(s.handleListUserStarredRepos))
	s.mux.HandleFunc("GET /api/v1/notifications", s.requireAuth(s.handleListNotifications))
	s.mux.HandleFunc("GET /api/v1/notifications/unread-count", s.requireAuth(s.handleUnreadNotificationsCount))
	s.mux.HandleFunc("POST /api/v1/notifications/read-all", s.requireAuth(s.handleMarkAllNotificationsRead))
	s.mux.HandleFunc("POST /api/v1/notifications/{id}/read", s.requireAuth(s.handleMarkNotificationRead))

	// Repositories
	s.mux.HandleFunc("POST /api/v1/repos", s.requireAuth(s.handleCreateRepo))
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}", s.handleGetRepo)
	s.mux.HandleFunc("GET /api/v1/user/repos", s.requireAuth(s.handleListUserRepos))
	s.mux.HandleFunc("DELETE /api/v1/repos/{owner}/{repo}", s.requireAuth(s.handleDeleteRepo))
	s.mux.HandleFunc("POST /api/v1/repos/{owner}/{repo}/collaborators", s.requireAuth(s.handleAddCollaborator))
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/collaborators", s.handleListCollaborators)
	s.mux.HandleFunc("DELETE /api/v1/repos/{owner}/{repo}/collaborators/{username}", s.requireAuth(s.handleRemoveCollaborator))
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/stars", s.handleGetRepoStars)
	s.mux.HandleFunc("PUT /api/v1/repos/{owner}/{repo}/star", s.requireAuth(s.handleStarRepo))
	s.mux.HandleFunc("DELETE /api/v1/repos/{owner}/{repo}/star", s.requireAuth(s.handleUnstarRepo))
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/stargazers", s.handleListRepoStargazers)
	s.mux.HandleFunc("POST /api/v1/repos/{owner}/{repo}/webhooks", s.requireAuth(s.handleCreateWebhook))
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/webhooks", s.handleListWebhooks)
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/webhooks/{id}", s.handleGetWebhook)
	s.mux.HandleFunc("DELETE /api/v1/repos/{owner}/{repo}/webhooks/{id}", s.requireAuth(s.handleDeleteWebhook))
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/webhooks/{id}/deliveries", s.handleListWebhookDeliveries)
	s.mux.HandleFunc("POST /api/v1/repos/{owner}/{repo}/webhooks/{id}/deliveries/{delivery_id}/redeliver", s.requireAuth(s.handleRedeliverWebhookDelivery))
	s.mux.HandleFunc("POST /api/v1/repos/{owner}/{repo}/webhooks/{id}/ping", s.requireAuth(s.handlePingWebhook))

	// Code browsing
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/branches", s.handleListBranches)
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/tree/{ref}/{path...}", s.handleListTree)
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/tree/{ref}", s.handleListTree)
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/blob/{ref}/{path...}", s.handleGetBlob)
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/commits/{ref}", s.handleListCommits)
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/commit/{hash}", s.handleGetCommit)

	// Entities & diff
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/entities/{ref}/{path...}", s.handleListEntities)
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/entity-history/{ref}", s.handleEntityHistory)
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/diff/{spec}", s.handleDiff)
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/semver/{spec}", s.handleSemver)

	// Pull requests
	s.mux.HandleFunc("POST /api/v1/repos/{owner}/{repo}/pulls", s.requireAuth(s.handleCreatePR))
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/pulls", s.handleListPRs)
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/pulls/{number}", s.handleGetPR)
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/pulls/{number}/diff", s.handlePRDiff)
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/pulls/{number}/merge-preview", s.handleMergePreview)
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/pulls/{number}/merge-gate", s.handlePRMergeGate)
	s.mux.HandleFunc("POST /api/v1/repos/{owner}/{repo}/pulls/{number}/merge", s.requireAuth(s.handleMergePR))
	s.mux.HandleFunc("POST /api/v1/repos/{owner}/{repo}/pulls/{number}/comments", s.requireAuth(s.handleCreatePRComment))
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/pulls/{number}/comments", s.handleListPRComments)
	s.mux.HandleFunc("POST /api/v1/repos/{owner}/{repo}/pulls/{number}/reviews", s.requireAuth(s.handleCreatePRReview))
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/pulls/{number}/reviews", s.handleListPRReviews)
	s.mux.HandleFunc("POST /api/v1/repos/{owner}/{repo}/pulls/{number}/checks", s.requireAuth(s.handleUpsertPRCheckRun))
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/pulls/{number}/checks", s.handleListPRCheckRuns)

	// Issues
	s.mux.HandleFunc("POST /api/v1/repos/{owner}/{repo}/issues", s.requireAuth(s.handleCreateIssue))
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/issues", s.handleListIssues)
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/issues/{number}", s.handleGetIssue)
	s.mux.HandleFunc("PATCH /api/v1/repos/{owner}/{repo}/issues/{number}", s.requireAuth(s.handleUpdateIssue))
	s.mux.HandleFunc("POST /api/v1/repos/{owner}/{repo}/issues/{number}/comments", s.requireAuth(s.handleCreateIssueComment))
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/issues/{number}/comments", s.handleListIssueComments)

	// Branch protection
	s.mux.HandleFunc("PUT /api/v1/repos/{owner}/{repo}/branch-protection/{branch...}", s.requireAuth(s.handleUpsertBranchProtection))
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/branch-protection/{branch...}", s.handleGetBranchProtection)
	s.mux.HandleFunc("DELETE /api/v1/repos/{owner}/{repo}/branch-protection/{branch...}", s.requireAuth(s.handleDeleteBranchProtection))

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
	}, s.authorizeProtocolRepoAccess, func(ctx context.Context, owner, repo string, commitHash object.Hash) error {
		repoModel, err := s.repoSvc.Get(ctx, owner, repo)
		if err != nil {
			return err
		}
		store, err := s.repoSvc.OpenStore(ctx, owner, repo)
		if err != nil {
			return err
		}
		if err := s.lineageSvc.IndexCommit(ctx, repoModel.ID, store, commitHash); err != nil {
			return err
		}
		return s.codeIntelSvc.EnsureCommitIndexed(ctx, repoModel.ID, store, owner+"/"+repo, commitHash)
	})
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
		func(ctx context.Context, repoID int64, store *gotstore.RepoStore, commitHash object.Hash) error {
			if err := s.lineageSvc.IndexCommit(ctx, repoID, store, commitHash); err != nil {
				return err
			}
			return s.codeIntelSvc.EnsureCommitIndexed(ctx, repoID, store, "", commitHash)
		},
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
