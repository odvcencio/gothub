package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/odvcencio/got/pkg/object"
	"github.com/odvcencio/gothub/internal/auth"
	"github.com/odvcencio/gothub/internal/database"
	"github.com/odvcencio/gothub/internal/gitinterop"
	"github.com/odvcencio/gothub/internal/gotprotocol"
	"github.com/odvcencio/gothub/internal/gotstore"
	"github.com/odvcencio/gothub/internal/jobs"
	"github.com/odvcencio/gothub/internal/service"
	"github.com/odvcencio/gothub/internal/web"
)

type Server struct {
	db                       database.DB
	authSvc                  *auth.Service
	repoSvc                  *service.RepoService
	browseSvc                *service.BrowseService
	diffSvc                  *service.DiffService
	prSvc                    *service.PRService
	issueSvc                 *service.IssueService
	webhookSvc               *service.WebhookService
	notifySvc                *service.NotificationService
	codeIntelSvc             *service.CodeIntelService
	lineageSvc               *service.EntityLineageService
	indexQueue               *jobs.Queue
	indexWorker              *jobs.WorkerPool
	asyncIndex               bool
	rateLimiter              *requestRateLimiter
	httpMetrics              *httpMetrics
	passkey                  *webauthn.WebAuthn
	enableAdminHealth        bool
	enablePprof              bool
	corsAllowedOrigins       []string
	restrictPublicOnly       bool
	maxPublicRepos           int
	requirePrivatePlan       bool
	maxPrivateRepos          int
	privateRepoAllowed       map[string]struct{}
	requireVerifiedEmail     bool
	requirePasskeyEnrollment bool
	enableOrganizations      bool
	polarWebhookSecret       string
	polarProductIDs          map[string]struct{}
	clientIPResolver         clientIPResolver
	tenantContext            tenantContextOptions
	adminRouteAccess         adminRouteAccess
	realtime                 *repoEventBroker
	mux                      *http.ServeMux
	handler                  http.Handler
}

type ServerOptions struct {
	EnableAsyncIndexing      bool
	IndexWorkerCount         int
	IndexWorkerPoll          time.Duration
	EnableAdminHealth        bool
	EnablePprof              bool
	AdminAllowedCIDRs        []string
	CORSAllowedOrigins       []string
	TrustedProxyCIDRs        []string
	EnableTenantContext      bool
	TenantHeader             string
	DefaultTenantID          string
	RestrictToPublic         bool
	MaxPublicRepos           int
	RequirePrivatePlan       bool
	MaxPrivateRepos          int
	PrivateRepoAllowed       []string
	RequireVerifiedEmail     bool
	RequirePasskeyEnrollment bool
	EnableOrganizations      bool
	PolarWebhookSecret       string
	PolarProductIDs          []string
}

type middlewareFunc func(http.Handler) http.Handler

func NewServer(db database.DB, authSvc *auth.Service, repoSvc *service.RepoService) *Server {
	return NewServerWithOptions(db, authSvc, repoSvc, ServerOptions{})
}

func NewServerWithOptions(db database.DB, authSvc *auth.Service, repoSvc *service.RepoService, opts ServerOptions) *Server {
	browseSvc := service.NewBrowseService(repoSvc)
	lineageSvc := service.NewEntityLineageService(db)
	diffSvc := service.NewDiffService(repoSvc, browseSvc, db, lineageSvc)
	prSvc := service.NewPRService(db, repoSvc, browseSvc)
	issueSvc := service.NewIssueService(db)
	webhookSvc := service.NewWebhookService(db)
	notifySvc := service.NewNotificationService(db)
	codeIntelSvc := service.NewCodeIntelService(db, repoSvc, browseSvc)
	indexQueue := jobs.NewQueue(db, jobs.QueueOptions{})
	httpMetrics := getDefaultHTTPMetrics()
	prSvc.SetCodeIntelService(codeIntelSvc)
	prSvc.SetLineageService(lineageSvc)
	adminCIDRs := opts.AdminAllowedCIDRs
	if (opts.EnableAdminHealth || opts.EnablePprof) && len(adminCIDRs) == 0 {
		adminCIDRs = defaultAdminRouteCIDRs
	}
	clientIPResolver := newClientIPResolver(opts.TrustedProxyCIDRs)
	privateRepoAllowed := make(map[string]struct{}, len(opts.PrivateRepoAllowed))
	for _, username := range opts.PrivateRepoAllowed {
		if username == "" {
			continue
		}
		privateRepoAllowed[username] = struct{}{}
	}
	polarProductIDs := make(map[string]struct{}, len(opts.PolarProductIDs))
	for _, id := range opts.PolarProductIDs {
		id = strings.TrimSpace(strings.ToLower(id))
		if id == "" {
			continue
		}
		polarProductIDs[id] = struct{}{}
	}
	s := &Server{
		db:                       db,
		authSvc:                  authSvc,
		repoSvc:                  repoSvc,
		browseSvc:                browseSvc,
		diffSvc:                  diffSvc,
		prSvc:                    prSvc,
		issueSvc:                 issueSvc,
		webhookSvc:               webhookSvc,
		notifySvc:                notifySvc,
		codeIntelSvc:             codeIntelSvc,
		lineageSvc:               lineageSvc,
		indexQueue:               indexQueue,
		asyncIndex:               opts.EnableAsyncIndexing,
		rateLimiter:              newRequestRateLimiter(),
		httpMetrics:              httpMetrics,
		passkey:                  initWebAuthn(),
		enableAdminHealth:        opts.EnableAdminHealth,
		enablePprof:              opts.EnablePprof,
		corsAllowedOrigins:       append([]string(nil), opts.CORSAllowedOrigins...),
		restrictPublicOnly:       opts.RestrictToPublic,
		maxPublicRepos:           opts.MaxPublicRepos,
		requirePrivatePlan:       opts.RequirePrivatePlan,
		maxPrivateRepos:          opts.MaxPrivateRepos,
		privateRepoAllowed:       privateRepoAllowed,
		requireVerifiedEmail:     opts.RequireVerifiedEmail,
		requirePasskeyEnrollment: opts.RequirePasskeyEnrollment,
		enableOrganizations:      opts.EnableOrganizations,
		polarWebhookSecret:       strings.TrimSpace(opts.PolarWebhookSecret),
		polarProductIDs:          polarProductIDs,
		clientIPResolver:         clientIPResolver,
		tenantContext:            newTenantContextOptions(opts.EnableTenantContext, opts.TenantHeader, opts.DefaultTenantID),
		adminRouteAccess:         newAdminRouteAccess(adminCIDRs, clientIPResolver.clientIPFromRequest),
		realtime:                 newRepoEventBroker(),
		mux:                      http.NewServeMux(),
	}
	if s.asyncIndex {
		s.indexWorker = s.newIndexWorker(opts.IndexWorkerCount, opts.IndexWorkerPoll)
	}
	s.routes()
	s.handler = s.buildHandler()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
}

func (s *Server) handleOrganizationsDisabled(w http.ResponseWriter, r *http.Request) {
	jsonError(w, "organizations feature is disabled", http.StatusNotFound)
}

func (s *Server) buildHandler() http.Handler {
	// Build the full middleware chain once during server initialization.
	return chainMiddleware(
		s.mux,
		requestTracingMiddleware,
		func(next http.Handler) http.Handler {
			return requestMetricsMiddleware(s.httpMetrics, next)
		},
		func(next http.Handler) http.Handler {
			return requestLoggingMiddleware(s.clientIPResolver, next)
		},
		func(next http.Handler) http.Handler {
			return corsMiddleware(s.corsAllowedOrigins, next)
		},
		func(next http.Handler) http.Handler {
			return requestRateLimitMiddleware(s.clientIPResolver, s.rateLimiter, next)
		},
		requestBodyLimitMiddleware,
		func(next http.Handler) http.Handler {
			return tenantContextMiddleware(s.tenantContext, s.clientIPResolver, next)
		},
		auth.Middleware(s.authSvc),
	)
}

func chainMiddleware(base http.Handler, stack ...middlewareFunc) http.Handler {
	wrapped := base
	for i := len(stack) - 1; i >= 0; i-- {
		middleware := stack[i]
		if middleware == nil {
			continue
		}
		wrapped = middleware(wrapped)
	}
	return wrapped
}

func (s *Server) routes() {
	// Health check
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})
	s.mux.Handle("GET /metrics", metricsHandler(nil))
	if s.enableAdminHealth {
		s.mux.Handle("GET /admin/health", s.adminRouteAccess.wrap(http.HandlerFunc(s.handleAdminHealth)))
	} else {
		s.mux.HandleFunc("GET /admin/health", http.NotFound)
	}
	if s.enablePprof {
		s.registerPprofRoutes()
	} else {
		s.mux.HandleFunc("/debug/pprof/", http.NotFound)
	}

	// Auth
	s.mux.HandleFunc("POST /api/v1/auth/register", s.handleRegister)
	s.mux.HandleFunc("POST /api/v1/auth/magic/request", s.handleRequestMagicLink)
	s.mux.HandleFunc("POST /api/v1/auth/magic/verify", s.handleVerifyMagicLink)
	s.mux.HandleFunc("POST /api/v1/auth/ssh/challenge", s.handleSSHChallenge)
	s.mux.HandleFunc("POST /api/v1/auth/ssh/verify", s.handleSSHVerify)
	s.mux.HandleFunc("POST /api/v1/auth/webauthn/register/begin", s.requireAuth(s.handleBeginWebAuthnRegistration))
	s.mux.HandleFunc("POST /api/v1/auth/webauthn/register/finish", s.requireAuth(s.handleFinishWebAuthnRegistration))
	s.mux.HandleFunc("POST /api/v1/auth/webauthn/login/begin", s.handleBeginWebAuthnLogin)
	s.mux.HandleFunc("POST /api/v1/auth/webauthn/login/finish", s.handleFinishWebAuthnLogin)
	s.mux.HandleFunc("GET /api/v1/auth/capabilities", s.handleAuthCapabilities)
	s.mux.HandleFunc("POST /api/v1/auth/refresh", s.requireAuth(s.handleRefreshToken))
	s.mux.HandleFunc("POST /api/v1/billing/polar/webhook", s.handlePolarWebhook)
	s.mux.HandleFunc("POST /api/v1/interest-signups", s.handleCreateInterestSignup)
	s.mux.HandleFunc("GET /api/v1/admin/interest-signups", s.requireAuth(s.handleListInterestSignups))

	// Explore
	s.mux.HandleFunc("GET /api/v1/explore/repos", s.handleExploreRepos)

	// User
	s.mux.HandleFunc("GET /api/v1/user", s.requireAuth(s.handleGetCurrentUser))
	s.mux.HandleFunc("GET /api/v1/user/ssh-keys", s.requireAuth(s.handleListSSHKeys))
	s.mux.HandleFunc("POST /api/v1/user/ssh-keys", s.requireAuth(s.handleCreateSSHKey))
	s.mux.HandleFunc("DELETE /api/v1/user/ssh-keys/{id}", s.requireAuth(s.handleDeleteSSHKey))
	s.mux.HandleFunc("GET /api/v1/user/passkeys", s.requireAuth(s.handleListPasskeys))
	s.mux.HandleFunc("GET /api/v1/user/repo-policy", s.requireAuth(s.handleGetRepoCreationPolicy))
	s.mux.HandleFunc("GET /api/v1/user/starred", s.requireAuth(s.handleListUserStarredRepos))
	s.mux.HandleFunc("GET /api/v1/user/subscription", s.requireAuth(s.handleGetSubscription))
	s.mux.HandleFunc("GET /api/v1/notifications", s.requireAuth(s.handleListNotifications))
	s.mux.HandleFunc("GET /api/v1/notifications/unread-count", s.requireAuth(s.handleUnreadNotificationsCount))
	s.mux.HandleFunc("POST /api/v1/notifications/read-all", s.requireAuth(s.handleMarkAllNotificationsRead))
	s.mux.HandleFunc("POST /api/v1/notifications/{id}/read", s.requireAuth(s.handleMarkNotificationRead))

	// Repositories
	s.mux.HandleFunc("POST /api/v1/repos", s.requireAuth(s.handleCreateRepo))
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}", s.handleGetRepo)
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/events", s.handleRepoEvents)
	s.mux.HandleFunc("POST /api/v1/repos/{owner}/{repo}/forks", s.requireAuth(s.handleForkRepo))
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/forks", s.handleListRepoForks)
	s.mux.HandleFunc("POST /api/v1/repos/{owner}/{repo}/runners/tokens", s.requireAuth(s.handleCreateRepoRunnerToken))
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/runners/tokens", s.requireAuth(s.handleListRepoRunnerTokens))
	s.mux.HandleFunc("DELETE /api/v1/repos/{owner}/{repo}/runners/tokens/{id}", s.requireAuth(s.handleDeleteRepoRunnerToken))
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
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/entity-log/{ref}", s.handleEntityLog)
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/entity-blame/{ref}", s.handleEntityBlame)
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/diff/{spec}", s.handleDiff)
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/semver/{spec}", s.handleSemver)

	// Pull requests
	s.mux.HandleFunc("POST /api/v1/repos/{owner}/{repo}/pulls", s.requireAuth(s.handleCreatePR))
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/pulls", s.handleListPRs)
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/pulls/{number}", s.handleGetPR)
	s.mux.HandleFunc("PATCH /api/v1/repos/{owner}/{repo}/pulls/{number}", s.requireAuth(s.handleUpdatePR))
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/pulls/{number}/diff", s.handlePRDiff)
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/pulls/{number}/merge-preview", s.handleMergePreview)
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/pulls/{number}/merge-gate", s.handlePRMergeGate)
	s.mux.HandleFunc("POST /api/v1/repos/{owner}/{repo}/pulls/{number}/merge", s.requireAuth(s.handleMergePR))
	s.mux.HandleFunc("POST /api/v1/repos/{owner}/{repo}/pulls/{number}/comments", s.requireAuth(s.handleCreatePRComment))
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/pulls/{number}/comments", s.handleListPRComments)
	s.mux.HandleFunc("DELETE /api/v1/repos/{owner}/{repo}/pulls/{number}/comments/{comment_id}", s.requireAuth(s.handleDeletePRComment))
	s.mux.HandleFunc("POST /api/v1/repos/{owner}/{repo}/pulls/{number}/reviews", s.requireAuth(s.handleCreatePRReview))
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/pulls/{number}/reviews", s.handleListPRReviews)
	s.mux.HandleFunc("POST /api/v1/repos/{owner}/{repo}/pulls/{number}/checks", s.requireAuth(s.handleUpsertPRCheckRun))
	s.mux.HandleFunc("POST /api/v1/repos/{owner}/{repo}/pulls/{number}/checks/runner", s.handleUpsertPRCheckRunByRunnerToken)
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/pulls/{number}/checks", s.handleListPRCheckRuns)

	// Issues
	s.mux.HandleFunc("POST /api/v1/repos/{owner}/{repo}/issues", s.requireAuth(s.handleCreateIssue))
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/issues", s.handleListIssues)
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/issues/{number}", s.handleGetIssue)
	s.mux.HandleFunc("PATCH /api/v1/repos/{owner}/{repo}/issues/{number}", s.requireAuth(s.handleUpdateIssue))
	s.mux.HandleFunc("POST /api/v1/repos/{owner}/{repo}/issues/{number}/comments", s.requireAuth(s.handleCreateIssueComment))
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/issues/{number}/comments", s.handleListIssueComments)
	s.mux.HandleFunc("DELETE /api/v1/repos/{owner}/{repo}/issues/{number}/comments/{comment_id}", s.requireAuth(s.handleDeleteIssueComment))

	// Branch protection
	s.mux.HandleFunc("PUT /api/v1/repos/{owner}/{repo}/branch-protection/{branch...}", s.requireAuth(s.handleUpsertBranchProtection))
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/branch-protection/{branch...}", s.handleGetBranchProtection)
	s.mux.HandleFunc("DELETE /api/v1/repos/{owner}/{repo}/branch-protection/{branch...}", s.requireAuth(s.handleDeleteBranchProtection))

	// Code intelligence
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/index/status", s.handleGetIndexStatus)
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/symbols/{ref}", s.handleSearchSymbols)
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/references/{ref}", s.handleFindReferences)
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/callgraph/{ref}", s.handleCallGraph)
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}/impact/{ref}", s.handleImpactAnalysis)

	// Organizations (optional)
	if s.enableOrganizations {
		s.mux.HandleFunc("POST /api/v1/orgs", s.requireAuth(s.handleCreateOrg))
		s.mux.HandleFunc("GET /api/v1/orgs/{org}", s.handleGetOrg)
		s.mux.HandleFunc("DELETE /api/v1/orgs/{org}", s.requireAuth(s.handleDeleteOrg))
		s.mux.HandleFunc("GET /api/v1/orgs/{org}/members", s.handleListOrgMembers)
		s.mux.HandleFunc("POST /api/v1/orgs/{org}/members", s.requireAuth(s.handleAddOrgMember))
		s.mux.HandleFunc("DELETE /api/v1/orgs/{org}/members/{username}", s.requireAuth(s.handleRemoveOrgMember))
		s.mux.HandleFunc("GET /api/v1/orgs/{org}/repos", s.handleListOrgRepos)
		s.mux.HandleFunc("GET /api/v1/user/orgs", s.requireAuth(s.handleListUserOrgs))
	} else {
		s.mux.HandleFunc("POST /api/v1/orgs", s.handleOrganizationsDisabled)
		s.mux.HandleFunc("GET /api/v1/orgs/{org}", s.handleOrganizationsDisabled)
		s.mux.HandleFunc("DELETE /api/v1/orgs/{org}", s.handleOrganizationsDisabled)
		s.mux.HandleFunc("GET /api/v1/orgs/{org}/members", s.handleOrganizationsDisabled)
		s.mux.HandleFunc("POST /api/v1/orgs/{org}/members", s.handleOrganizationsDisabled)
		s.mux.HandleFunc("DELETE /api/v1/orgs/{org}/members/{username}", s.handleOrganizationsDisabled)
		s.mux.HandleFunc("GET /api/v1/orgs/{org}/repos", s.handleOrganizationsDisabled)
		s.mux.HandleFunc("GET /api/v1/user/orgs", s.handleOrganizationsDisabled)
	}

	validateProtectedRefUpdate := func(ctx context.Context, repoID int64, refName string, oldHash, newHash object.Hash) error {
		if !strings.HasPrefix(refName, "heads/") || newHash == "" {
			return nil
		}
		branch := strings.TrimPrefix(refName, "heads/")
		reasons, err := s.prSvc.EvaluateBranchUpdateGate(ctx, repoID, branch, oldHash, newHash)
		if err != nil {
			return err
		}
		if len(reasons) == 0 {
			return nil
		}
		return errors.New(strings.Join(reasons, "; "))
	}

	indexByRepoName := func(ctx context.Context, owner, repo string, commitHash object.Hash) error {
		repoModel, err := s.repoSvc.Get(ctx, owner, repo)
		if err != nil {
			return err
		}
		if s.asyncIndex {
			_, err := s.indexQueue.EnqueueCommitIndex(ctx, repoModel.ID, string(commitHash))
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
	}

	indexByRepoID := func(ctx context.Context, repoID int64, store *gotstore.RepoStore, commitHash object.Hash) error {
		if s.asyncIndex {
			_, err := s.indexQueue.EnqueueCommitIndex(ctx, repoID, string(commitHash))
			return err
		}
		if err := s.lineageSvc.IndexCommit(ctx, repoID, store, commitHash); err != nil {
			return err
		}
		return s.codeIntelSvc.EnsureCommitIndexed(ctx, repoID, store, "", commitHash)
	}

	// Got protocol
	gotProto := gotprotocol.NewHandler(func(owner, repo string) (*gotstore.RepoStore, error) {
		return s.repoSvc.OpenStore(context.Background(), owner, repo)
	}, s.authorizeProtocolRepoAccess, indexByRepoName)
	gotProto.SetRefUpdateValidator(func(ctx context.Context, owner, repo, refName string, oldHash, newHash object.Hash) error {
		repoModel, err := s.repoSvc.Get(ctx, owner, repo)
		if err != nil {
			return err
		}
		return validateProtectedRefUpdate(ctx, repoModel.ID, refName, oldHash, newHash)
	})
	gotProtoMux := http.NewServeMux()
	gotProto.RegisterRoutes(gotProtoMux)
	s.mux.Handle("/got/", s.wrapGotBatchGraphErrors(gotProtoMux))

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
		indexByRepoID,
	)
	gitHandler.SetRefUpdateValidator(func(ctx context.Context, owner, repo string, repoID int64, refName string, oldHash, newHash object.Hash) error {
		return validateProtectedRefUpdate(ctx, repoID, refName, oldHash, newHash)
	})
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
