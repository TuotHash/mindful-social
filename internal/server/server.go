package server

import (
	"context"
	"database/sql"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"

	mindfulsocial "github.com/TuotHash/mindful-social"
	"github.com/TuotHash/mindful-social/internal/ai"
	"github.com/TuotHash/mindful-social/internal/audio"
	"github.com/TuotHash/mindful-social/internal/auth"
	"github.com/TuotHash/mindful-social/internal/config"
	"github.com/TuotHash/mindful-social/internal/db"
	"github.com/TuotHash/mindful-social/internal/migrate"
	"github.com/TuotHash/mindful-social/internal/views"
)

type Server struct {
	cfg         config.Config
	logger      *slog.Logger
	db          *pgxpool.Pool
	sqlDB       *sql.DB // bridge for scs/postgresstore
	queries     *db.Queries
	authSvc     *auth.Service
	sessions    *scs.SessionManager
	oauth       *auth.Registry
	csrf        func(http.Handler) http.Handler
	router      chi.Router
	audioWorker *audio.Worker
	// aiClient drafts nodes from a prompt. nil when AI_ENDPOINT_URL is
	// unset — the "Generate with AI" entry point is hidden and the
	// /nodes/generate routes 404.
	aiClient *ai.Client
	// genWorker drains the node_generation_jobs queue. nil when AI is
	// disabled.
	genWorker *ai.Worker
	// progressHub carries live generation updates from the worker to the SSE
	// handler that streams them to the browser. nil when AI is disabled.
	progressHub *ai.ProgressHub
}

func New(cfg config.Config, logger *slog.Logger) (*Server, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	// AfterConnect runs once per new physical connection. We lower the
	// pg_trgm word-similarity threshold so the picker and /search match
	// fuzzy/partial inputs ("nuc" → "nuclear", "nucear" → "nuclear"). The
	// default 0.6 is far too strict for live-search UX. Any other queries
	// that use the threshold operators (%>, <%) inherit this setting.
	poolCfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET pg_trgm.word_similarity_threshold = 0.25")
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}

	// scs/postgresstore is built against database/sql; bridge from pgxpool.
	sqlDB := stdlib.OpenDBFromPool(pool)

	// Apply pending migrations from the embedded MigrationsFS before
	// anything touches the schema. Idempotent — goose only runs what's
	// missing. Failing here aborts startup; a stale schema is worse than
	// a non-responsive server.
	if err := migrate.Up(sqlDB); err != nil {
		pool.Close()
		_ = sqlDB.Close()
		return nil, err
	}

	sm := auth.NewSessionManager(sqlDB, secureCookieForPublicBaseURL(cfg.PublicBaseURL))

	oauthCtx, oauthCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer oauthCancel()
	registry, err := auth.LoadProviders(oauthCtx, logger, cfg.PublicBaseURL)
	if err != nil {
		pool.Close()
		_ = sqlDB.Close()
		return nil, err
	}

	s := &Server{
		cfg:      cfg,
		logger:   logger,
		db:       pool,
		sqlDB:    sqlDB,
		queries:  db.New(pool),
		authSvc:  auth.NewService(pool),
		sessions: sm,
		oauth:    registry,
	}
	csrfMw, err := s.csrfMiddleware(cfg.PublicBaseURL)
	if err != nil {
		pool.Close()
		_ = sqlDB.Close()
		return nil, err
	}
	s.csrf = csrfMw
	views.SetSignupEnabled(cfg.SignupEnabled)
	// AI node drafting is opt-in via an OpenAI-compatible endpoint. When
	// unset the feature stays invisible; the client is otherwise stateless
	// so there's nothing to health-check at boot (per-request errors surface
	// to the user as a flash).
	if cfg.AIEndpointURL != "" {
		s.aiClient = ai.NewClient(cfg.AIEndpointURL, cfg.AIModel, cfg.AIAPIKey)
		aiLogger := logger.With("subsys", "ai")
		gatherer := ai.NewGatherer(cfg.SearxngURL, aiLogger)
		s.progressHub = ai.NewProgressHub()
		s.genWorker = ai.NewWorker(s.queries, s.aiClient, gatherer, s.progressHub, cfg.AIJobTimeout, cfg.AIStreamIdleTimeout, aiLogger)
		logger.Info("ai: node drafting enabled", "endpoint", cfg.AIEndpointURL, "model", cfg.AIModel, "web_search", cfg.SearxngURL != "")
	} else {
		logger.Info("ai: AI_ENDPOINT_URL unset, node drafting disabled")
	}
	views.SetAIEnabled(s.aiClient != nil)
	views.SetWebSearchEnabled(s.aiClient != nil && cfg.SearxngURL != "")
	if err := s.bootstrapAdmins(ctx); err != nil {
		// Don't fail boot if admin reconcile fails — just log it. A
		// transient DB error shouldn't keep the whole server down.
		logger.Warn("admin bootstrap", "err", err)
	}
	if err := s.startAudioWorker(); err != nil {
		// Audio is opt-in; treat startup failures as a warning so the
		// app comes up even when the sidecar isn't running yet.
		logger.Warn("audio worker disabled", "err", err)
	}
	s.routes()
	return s, nil
}

// startAudioWorker brings up the background TTS worker if the sidecar
// URL is configured. With TTS_SIDECAR_URL empty, audio is disabled —
// node uploads still succeed but no /audio routes will return content.
func (s *Server) startAudioWorker() error {
	if s.cfg.TTSSidecarURL == "" {
		s.logger.Info("audio: TTS_SIDECAR_URL unset, TTS disabled")
		w, err := audio.NewWorker(s.queries, nil, s.cfg.AudioDir, s.logger.With("subsys", "audio"))
		if err != nil {
			return err
		}
		s.audioWorker = w
		return nil
	}
	client := audio.NewSidecarClient(s.cfg.TTSSidecarURL)
	pingCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := client.Healthz(pingCtx); err != nil {
		// Sidecar is configured but not reachable. Spin up the worker
		// anyway — jobs will fail loudly until the operator brings it
		// up, but the rest of the app keeps working.
		s.logger.Warn("audio: sidecar /healthz failed; worker will retry per-job", "url", s.cfg.TTSSidecarURL, "err", err)
	}
	audioLogger := s.logger.With("subsys", "audio")
	w, err := audio.NewWorker(s.queries, client, s.cfg.AudioDir, audioLogger)
	if err != nil {
		return err
	}
	s.audioWorker = w
	// Catch up posts created before TTS was wired up (or while the
	// sidecar was down). Runs once per process, off the boot path so a
	// large backlog doesn't slow server startup.
	go audio.BackfillExistingNodes(context.Background(), s.queries, audioLogger)
	return nil
}

// bootstrapAdmins reconciles cfg.AdminUsers with the role column. Listed
// usernames that exist get promoted to admin if they aren't already;
// unknown usernames are logged and skipped. This runs at every startup,
// so demoting a username through the UI sticks (the env var only ever
// *grants* admin, it doesn't enforce the set).
func (s *Server) bootstrapAdmins(ctx context.Context) error {
	for _, username := range s.cfg.AdminUsers {
		u, err := s.queries.GetUserByUsername(ctx, username)
		if err != nil {
			s.logger.Warn("admin bootstrap: user not found", "username", username, "err", err)
			continue
		}
		if u.Role == db.UserRoleAdmin {
			continue
		}
		if err := s.queries.UpdateUserRole(ctx, db.UpdateUserRoleParams{ID: u.ID, Role: db.UserRoleAdmin}); err != nil {
			return err
		}
		s.logger.Info("admin bootstrap: promoted", "username", username)
	}
	return nil
}

func (s *Server) Handler() http.Handler { return s.router }

func (s *Server) Close() error {
	if s.audioWorker != nil {
		_ = s.audioWorker.Close()
	}
	if s.genWorker != nil {
		_ = s.genWorker.Close()
	}
	s.db.Close()
	return s.sqlDB.Close()
}

// timeoutExceptStreaming applies chi's request Timeout to normal requests but
// lets Server-Sent Events through untouched (identified by their Accept header).
// SSE handlers manage their own lifetime and end when the client disconnects.
func timeoutExceptStreaming(d time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		timed := middleware.Timeout(d)(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
				next.ServeHTTP(w, r)
				return
			}
			timed.ServeHTTP(w, r)
		})
	}
}

func (s *Server) routes() {
	r := chi.NewRouter()
	// Outer middleware that runs on every route, including the
	// IdP-to-server OIDC backchannel logout endpoint.
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(s.recoverer)
	// 30s request cap, but never on Server-Sent Events: a streaming response
	// (e.g. live AI generation) is long-lived by design, and the deadline this
	// sets on the request context would otherwise cut the stream at 30s (and
	// then write a superfluous 504 over the already-sent 200).
	r.Use(timeoutExceptStreaming(30 * time.Second))
	// Defense-in-depth headers. Sits at the outer layer so even early
	// error responses (CSRF rejects, backchannel JSON errors) carry them.
	r.Use(securityHeaders)

	// OIDC backchannel logout: server-to-server callback from the IdP. The
	// request carries no cookies and no CSRF token; trust comes from the
	// JWT signature, issuer, and audience checks inside the handler. Sits
	// outside the session/CSRF middleware on purpose.
	r.Post("/auth/backchannel-logout/{provider}", s.handleBackchannelLogout)

	// User-facing routes get session loading, user resolution, request
	// logging (with user_id), and CSRF on unsafe methods.
	r.Group(func(r chi.Router) {
		// scs LoadAndSave wraps the request in a session-aware response
		// writer and persists changes on the way out.
		r.Use(s.sessions.LoadAndSave)
		r.Use(s.loadUser)
		r.Use(s.requestLogger)
		// gorilla/csrf double-submit cookie + token check on unsafe
		// methods. The bridge inside csrfMiddleware copies the per-request
		// token onto our ctx so templates can render the hidden input via
		// views.CSRFField.
		r.Use(s.csrf)

		s.userFacingRoutes(r)
	})

	// Chi only invokes a single router-level NotFound / MethodNotAllowed
	// handler — middleware inside groups doesn't apply. Wrap ours in the
	// session + user chain so the styled error page can show the right
	// nav for signed-in visitors.
	errChain := func(h http.HandlerFunc) http.HandlerFunc {
		wrapped := s.sessions.LoadAndSave(s.loadUser(h))
		return wrapped.ServeHTTP
	}
	r.NotFound(errChain(s.notFound))
	r.MethodNotAllowed(errChain(s.methodNotAllowed))

	s.router = r
}

func (s *Server) userFacingRoutes(r chi.Router) {
	r.Get("/healthz", s.handleHealth)

	// Static assets are served from the embedded FS so the binary is
	// self-contained. Cache-Control lets the reverse proxy (and the
	// browser, and any CDN) hold them for a day; filenames will move to
	// content-hashed paths in a later release for true immutable
	// caching.
	staticSub, err := fs.Sub(mindfulsocial.StaticFS, "static")
	if err != nil {
		// Sub only fails on a malformed path. The embed directive is a
		// compile-time constant, so this panic flags a programmer bug
		// rather than a runtime configuration problem.
		panic(err)
	}
	staticFS := http.FileServer(http.FS(staticSub))
	r.Handle("/static/*", http.StripPrefix("/static/", cacheStatic(staticFS)))
	r.Handle("/uploads/*", http.HandlerFunc(s.handleUpload))

	r.Get("/", s.handleLanding)

	r.Get("/signup", s.handleSignupGet)
	r.Post("/signup", s.handleSignupPost)
	r.Get("/login", s.handleLoginGet)
	r.Post("/login", s.handleLoginPost)
	r.Post("/logout", s.handleLogout)
	r.Get("/auth/oauth/{provider}", s.handleOAuthStart)
	r.Get("/auth/callback/{provider}", s.handleOAuthCallback)

	r.Get("/nodes/{id}", s.handleNodeDetail)
	r.Get("/nodes/{id}/history", s.handleNodeHistory)
	r.Get("/nodes/{id}/history/{revision}", s.handleNodeRevisionView)
	r.Get("/nodes/{id}/audio/manifest", s.handleAudioManifest)
	r.Get("/nodes/{id}/audio/chunks/{n}", s.handleAudioChunk)
	r.Get("/graph", s.handleArgumentGraph)
	r.Get("/graph/data", s.handleArgumentGraphData)
	r.Get("/graph/3d", s.handleArgumentGraph3D)
	r.Get("/users/{username}", s.handleProfile)
	r.Get("/tags", s.handleTagsIndex)
	r.Get("/tags/{name}", s.handleTagDetail)
	r.Get("/groups", s.handleGroupsIndex)
	r.Get("/groups/{slug}", s.handleGroupDetail)
	r.Get("/search", s.handleSearch)
	r.Get("/search/suggest", s.handleSearchSuggest)
	r.Get("/graph/nodes/suggest", s.handleGraphNodesSuggest)
	r.Get("/users/suggest", s.handleUsersSuggest)
	r.Get("/tags/suggest", s.handleTagsSuggest)
	r.Get("/groups/suggest", s.handleGroupsSuggest)
	// Notification badge count — returns empty HTML when not logged in rather
	// than redirecting, so the htmx poll in the nav degrades gracefully on
	// session expiry.
	r.Get("/notifications/count", s.handleNotificationsCount)

	// Routes that require an authenticated user.
	r.Group(func(r chi.Router) {
		r.Use(s.requireUser)
		r.Get("/home", s.handleHome)
		r.Get("/notifications", s.handleNotificationsIndex)
		r.Get("/account", s.handleAccount)
		r.Post("/account/preferences", s.handleAccountPreferences)
		r.Post("/account/profile-image", s.handleAccountProfileImage)
		r.Post("/account/password", s.handleAccountPasswordSet)
		r.Post("/account/identities/{id}/disconnect", s.handleAccountIdentityDisconnect)
		r.Get("/nodes/new", s.handleNodeNew)
		r.Get("/nodes/generate", s.handleNodeGenerateForm)
		r.Post("/nodes/generate", s.handleNodeGenerate)
		r.Get("/nodes/generate/{id}", s.handleNodeGenerateStatus)
		r.Get("/nodes/generate/{id}/stream", s.handleNodeGenerateStream)
		r.Post("/nodes/new/images", s.handleNewNodeImageUpload)
		r.Post("/nodes/new/videos", s.handleNewNodeVideoUpload)
		r.Get("/nodes/parent-picker", s.handleParentPicker)
		r.Post("/nodes", s.handleNodeCreate)
		r.Get("/nodes/{id}/edit", s.handleNodeEdit)
		r.Post("/nodes/{id}", s.handleNodeUpdate)
		r.Get("/nodes/{id}/delete", s.handleNodeDeleteConfirm)
		r.Post("/nodes/{id}/delete", s.handleNodeDelete)
		r.Post("/nodes/{id}/history/{revision}/revert", s.handleNodeRevert)
		r.Get("/nodes/{id}/edges/new", s.handleEdgeNew)
		r.Get("/nodes/{id}/edges/picker", s.handleEdgePicker)
		r.Post("/nodes/{id}/edges", s.handleEdgeCreate)
		r.Post("/nodes/{id}/edges/{edgeID}/highlight", s.handleEdgeHighlight)
		r.Post("/nodes/{id}/edges/{edgeID}/unhighlight", s.handleEdgeUnhighlight)
		r.Post("/nodes/{id}/edges/{edgeID}/delete", s.handleEdgeDelete)
		r.Post("/nodes/{id}/images", s.handleNodeImageUpload)
		r.Post("/nodes/{id}/videos", s.handleNodeVideoUpload)
		r.Get("/nodes/{id}/pin", s.handlePinForm)
		r.Post("/nodes/{id}/pin", s.handlePinSet)
		r.Post("/nodes/{id}/unpin", s.handlePinDelete)
		r.Post("/nodes/{id}/stance", s.handleStanceSet)
		r.Post("/nodes/{id}/comments", s.handleCommentCreate)
		r.Get("/nodes/{id}/comments/{commentID}/edit", s.handleCommentEdit)
		r.Post("/nodes/{id}/comments/{commentID}", s.handleCommentUpdate)
		r.Post("/nodes/{id}/comments/{commentID}/delete", s.handleCommentDelete)
		r.Post("/users/{username}/follow", s.handleFollow)
		r.Post("/users/{username}/unfollow", s.handleUnfollow)
		r.Post("/groups", s.handleGroupCreate)
		r.Post("/groups/{slug}/join", s.handleGroupJoin)
		r.Post("/groups/{slug}/leave", s.handleGroupLeave)
		r.Post("/groups/{slug}/members", s.handleGroupAddMember)
		r.Post("/groups/{slug}/members/{userID}/delete", s.handleGroupRemoveMember)
		r.Post("/groups/{slug}/members/{userID}/role", s.handleGroupSetMemberRole)
		r.Post("/groups/{slug}/settings", s.handleGroupSettings)
	})

	// Admin-only routes. requireAdmin returns 404 for non-admins so the
	// route surface is invisible.
	r.Group(func(r chi.Router) {
		r.Use(s.requireUser)
		r.Use(s.requireAdmin)
		r.Get("/admin", s.handleAdminIndex)
		r.Post("/admin/users/{id}/role", s.handleAdminSetRole)
		r.Get("/admin/users/{id}/edit", s.handleAdminUserEdit)
		r.Post("/admin/users/{id}/username", s.handleAdminUpdateUsername)
		r.Post("/admin/users/{id}/email", s.handleAdminUpdateEmail)
		r.Post("/admin/users/{id}/password", s.handleAdminResetPassword)
		r.Get("/admin/users/{id}/delete", s.handleAdminUserDeleteConfirm)
		r.Post("/admin/users/{id}/delete", s.handleAdminDeleteUser)
	})
}
