package server

import (
	"context"
	"database/sql"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"

	mindfulsocial "github.com/TuotHash/mindful-social"
	"github.com/TuotHash/mindful-social/internal/auth"
	"github.com/TuotHash/mindful-social/internal/config"
	"github.com/TuotHash/mindful-social/internal/db"
	"github.com/TuotHash/mindful-social/internal/migrate"
	"github.com/TuotHash/mindful-social/internal/views"
)

type Server struct {
	cfg      config.Config
	logger   *slog.Logger
	db       *pgxpool.Pool
	sqlDB    *sql.DB // bridge for scs/postgresstore
	queries  *db.Queries
	authSvc  *auth.Service
	sessions *scs.SessionManager
	oauth    *auth.Registry
	csrf     func(http.Handler) http.Handler
	router   chi.Router
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

	sm := auth.NewSessionManager(sqlDB)

	oauthCtx, oauthCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer oauthCancel()
	registry, err := auth.LoadProviders(oauthCtx, logger, cfg.PublicBaseURL)
	if err != nil {
		pool.Close()
		_ = sqlDB.Close()
		return nil, err
	}

	csrfMw, err := csrfMiddleware(cfg.PublicBaseURL)
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
		csrf:     csrfMw,
	}
	views.SetSignupEnabled(cfg.SignupEnabled)
	if err := s.bootstrapAdmins(ctx); err != nil {
		// Don't fail boot if admin reconcile fails — just log it. A
		// transient DB error shouldn't keep the whole server down.
		logger.Warn("admin bootstrap", "err", err)
	}
	s.routes()
	return s, nil
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
	s.db.Close()
	return s.sqlDB.Close()
}

func (s *Server) routes() {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))
	// scs LoadAndSave wraps the request in a session-aware response writer
	// and persists changes on the way out.
	r.Use(s.sessions.LoadAndSave)
	r.Use(s.loadUser)
	// gorilla/csrf double-submit cookie + token check on unsafe methods.
	// The bridge inside csrfMiddleware copies the per-request token onto
	// our ctx so templates can render the hidden input via views.CSRFField.
	r.Use(s.csrf)

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
	uploadFS := http.FileServer(http.Dir(s.cfg.UploadDir))
	r.Handle("/uploads/*", http.StripPrefix("/uploads/", cacheStatic(uploadFS)))

	r.Get("/", s.handleLanding)

	r.Get("/signup", s.handleSignupGet)
	r.Post("/signup", s.handleSignupPost)
	r.Get("/login", s.handleLoginGet)
	r.Post("/login", s.handleLoginPost)
	r.Post("/logout", s.handleLogout)
	r.Get("/auth/oauth/{provider}", s.handleOAuthStart)
	r.Get("/auth/callback/{provider}", s.handleOAuthCallback)

	r.Get("/nodes/{id}", s.handleNodeDetail)
	r.Get("/users/{username}", s.handleProfile)
	r.Get("/tags", s.handleTagsIndex)
	r.Get("/tags/{name}", s.handleTagDetail)
	r.Get("/groups", s.handleGroupsIndex)
	r.Get("/groups/{slug}", s.handleGroupDetail)
	r.Get("/search", s.handleSearch)

	// Routes that require an authenticated user.
	r.Group(func(r chi.Router) {
		r.Use(s.requireUser)
		r.Get("/home", s.handleHome)
		r.Get("/account", s.handleAccount)
		r.Post("/account/preferences", s.handleAccountPreferences)
		r.Post("/account/profile-image", s.handleAccountProfileImage)
		r.Post("/account/password", s.handleAccountPasswordSet)
		r.Post("/account/identities/{id}/disconnect", s.handleAccountIdentityDisconnect)
		r.Get("/nodes/new", s.handleNodeNew)
		r.Get("/nodes/topic-picker", s.handleTopicPicker)
		r.Post("/nodes", s.handleNodeCreate)
		r.Get("/nodes/{id}/edit", s.handleNodeEdit)
		r.Post("/nodes/{id}", s.handleNodeUpdate)
		r.Get("/nodes/{id}/delete", s.handleNodeDeleteConfirm)
		r.Post("/nodes/{id}/delete", s.handleNodeDelete)
		r.Get("/nodes/{id}/edges/new", s.handleEdgeNew)
		r.Get("/nodes/{id}/edges/picker", s.handleEdgePicker)
		r.Post("/nodes/{id}/edges", s.handleEdgeCreate)
		r.Post("/nodes/{id}/edges/{edgeID}/highlight", s.handleEdgeHighlight)
		r.Post("/nodes/{id}/edges/{edgeID}/unhighlight", s.handleEdgeUnhighlight)
		r.Post("/nodes/{id}/edges/{edgeID}/delete", s.handleEdgeDelete)
		r.Post("/nodes/{id}/images", s.handleNodeImageUpload)
		r.Post("/nodes/{id}/videos", s.handleNodeVideoUpload)
		r.Get("/nodes/{id}/pin", s.handlePinForm)
		r.Get("/nodes/{id}/finding-picker", s.handleFindingPicker)
		r.Post("/nodes/{id}/pin", s.handlePinSet)
		r.Post("/nodes/{id}/unpin", s.handlePinDelete)
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
		r.Get("/lists", s.handleListsIndex)
		r.Post("/lists", s.handleListCreate)
		r.Get("/lists/{id}", s.handleListDetail)
		r.Post("/lists/{id}/members", s.handleListAddMember)
		r.Post("/lists/{id}/members/{userID}/delete", s.handleListRemoveMember)
		r.Post("/lists/{id}/delete", s.handleListDelete)
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
	})

	s.router = r
}
