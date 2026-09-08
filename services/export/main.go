package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/almuzky/iot/services/export/internal/config"
	"github.com/almuzky/iot/services/export/internal/handler"
	"github.com/almuzky/iot/services/export/internal/middleware"
	"github.com/almuzky/iot/services/export/internal/service"
	"github.com/almuzky/iot/services/export/internal/tsdb"
	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	// ─── TimescaleDB (Module Service time-series store) ──────────────
	var store *tsdb.Store
	for i := 0; i < 10; i++ {
		store, err = tsdb.New(cfg.TimescaleDSN)
		if err == nil {
			break
		}
		log.Printf("timescaledb connect attempt %d/10 failed: %v", i+1, err)
		time.Sleep(3 * time.Second)
	}
	if store == nil {
		log.Fatalf("timescaledb unreachable: %v", err)
	}
	defer store.Close()
	log.Println("timescaledb connected")

	// ─── Wire dependencies ──────────────────────────────────────────
	svc := service.New(store)
	h := handler.New(svc)

	// ─── Router ─────────────────────────────────────────────────────
	r := chi.NewRouter()
	r.Use(chimw.RequestID)
	r.Use(chimw.Recoverer)
	r.Use(middleware.PrometheusMiddleware)

	r.Handle("/metrics", promhttp.Handler())
	r.Get("/health", handler.Health)
	r.Get("/export/health", handler.Health)

	// Auth + RBAC: Kong fronts the service, but the service itself enforces a
	// valid JWT and restricts exports to admin/operator (viewer cannot export).
	authMw := middleware.JWTAuth(cfg.JWTSecret)
	rbacMw := middleware.RequireRole(cfg.JWTSecret, "admin", "operator")

	h.Routes(r, authMw, rbacMw)

	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      r,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		log.Printf("export-svc listening on :%s", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	<-quit
	log.Println("shutting down export-svc...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("shutdown error: %v", err)
	}
	log.Println("export-svc stopped")
}
