package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/zohrevand/splitflow/api"
	"github.com/zohrevand/splitflow/internal/handler"
	appmw "github.com/zohrevand/splitflow/internal/middleware"
	"github.com/zohrevand/splitflow/internal/store"

	"github.com/zohrevand/splitflow/internal/auth"
	"github.com/zohrevand/splitflow/internal/config"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	cfg := config.Load()

	// Connect to PostgreSQL using pgxpool
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("unable to create connection pool", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		logger.Error("unable to reach database", "error", err)
		os.Exit(1)
	}
	logger.Info("connected to PostgreSQL")

	// Wire dependencies (Store -> Service -> Handler)
	st := store.New(pool)
	authSvc := auth.NewService(cfg.JWTSecret)
	authHandler := handler.NewAuthHandler(st, authSvc)
	groupHandler := handler.NewGroupHandler(st)
	expenseHandler := handler.NewExpenseHandler(st)
	settlementHandler := handler.NewSettlementHandler(st)

	// Setup Router & Middleware
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.ClientIPFromRemoteAddr)
	r.Use(appmw.RequestLogger(logger))
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))

	// Serve the raw OpenAPI YAML spec
	r.Get("/openapi.yaml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(api.OpenAPISpec)
	})

	// Serve interactive API docs (Scalar UI)
	r.Get("/docs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<!doctype html>
<html>
  <head>
    <title>SplitFlow API Docs</title>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
  </head>
  <body>
    <script id="api-reference" data-url="/openapi.yaml"></script>
    <script src="https://cdn.jsdelivr.net/npm/@scalar/api-reference"></script>
  </body>
</html>`))
	})

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	authLimiter := appmw.NewRateLimiter(ctx, 0.5, 5)

	r.Route("/api/v1", func(r chi.Router) {
		// Rate-limited public auth routes
		r.Group(func(r chi.Router) {
			r.Use(authLimiter.Limit)

			r.Post("/auth/register", authHandler.Register)
			r.Post("/auth/login", authHandler.Login)
			r.Post("/auth/refresh", authHandler.Refresh)
			r.Post("/auth/logout", authHandler.Logout)
		})

		// Protected routes (JWT required)
		r.Group(func(r chi.Router) {
			r.Use(authSvc.RequireAuth)

			r.Post("/groups", groupHandler.CreateGroup)
			r.Get("/groups", groupHandler.ListGroups)
			r.Post("/groups/{groupID}/expenses", expenseHandler.CreateExpense)
			r.Get("/groups/{groupID}/balances", expenseHandler.GetBalances)
			r.Post("/groups/{groupID}/members", groupHandler.AddMember)
			r.Get("/groups/{groupID}/expenses", expenseHandler.ListExpenses)
			r.Post("/groups/{groupID}/settlements", settlementHandler.CreateSettlement)
			r.Get("/groups/{groupID}/settlements/plan", settlementHandler.GetSettlementPlan)
		})
	})

	//Start Server
	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      r,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		logger.Info("starting HTTP server", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	logger.Info("shutdown signal received, draining connections...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
		os.Exit(1)
	}
	logger.Info("server stopped cleanly")
}
