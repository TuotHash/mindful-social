package server

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mindful-social/mindful-social/internal/config"
)

type Server struct {
	cfg    config.Config
	logger *slog.Logger
	db     *pgxpool.Pool
	router chi.Router
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

	s := &Server{
		cfg:    cfg,
		logger: logger,
		db:     pool,
	}
	s.routes()
	return s, nil
}

func (s *Server) Handler() http.Handler { return s.router }

func (s *Server) Close() error {
	s.db.Close()
	return nil
}

func (s *Server) routes() {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))

	r.Get("/healthz", s.handleHealth)
	r.Get("/", s.handleHome)

	s.router = r
}
