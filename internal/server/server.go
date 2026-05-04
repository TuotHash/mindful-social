package server

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/mindful-social/mindful-social/internal/auth"
	"github.com/mindful-social/mindful-social/internal/config"
	"github.com/mindful-social/mindful-social/internal/db"
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
	router   chi.Router
}

func New(cfg config.Config, logger *slog.Logger) (*Server, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}

	// scs/postgresstore is built against database/sql; bridge from pgxpool.
	sqlDB := stdlib.OpenDBFromPool(pool)
	sm := auth.NewSessionManager(sqlDB)

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
	s.routes()
	return s, nil
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

	r.Get("/healthz", s.handleHealth)

	// Static assets are served from ./static. Cached lightly so dev edits
	// take effect on reload without a hard refresh.
	staticFS := http.FileServer(http.Dir("static"))
	r.Handle("/static/*", http.StripPrefix("/static/", staticFS))

	r.Get("/", s.handleHome)

	r.Get("/signup", s.handleSignupGet)
	r.Post("/signup", s.handleSignupPost)
	r.Get("/login", s.handleLoginGet)
	r.Post("/login", s.handleLoginPost)
	r.Post("/logout", s.handleLogout)
	r.Get("/auth/oauth/{provider}", s.handleOAuthStart)
	r.Get("/auth/callback/{provider}", s.handleOAuthCallback)

	r.Get("/nodes/{id}", s.handleNodeDetail)

	// Routes that require an authenticated user.
	r.Group(func(r chi.Router) {
		r.Use(s.requireUser)
		r.Get("/nodes/new", s.handleNodeNew)
		r.Post("/nodes", s.handleNodeCreate)
		r.Get("/nodes/{id}/edges/new", s.handleEdgeNew)
		r.Post("/nodes/{id}/edges", s.handleEdgeCreate)
	})

	s.router = r
}
