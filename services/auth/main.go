package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/almuzky/iot/services/auth/internal/config"
	"github.com/almuzky/iot/services/auth/internal/cron"
	"github.com/almuzky/iot/services/auth/internal/handler"
	"github.com/almuzky/iot/services/auth/internal/middleware"
	"github.com/almuzky/iot/services/auth/internal/repository"
	"github.com/almuzky/iot/services/auth/internal/service"
	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	// ─── Config ────────────────────────────────────────────────────────
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	// ─── Database ──────────────────────────────────────────────────────
	db, err := openDB(cfg.DBDSN)
	if err != nil {
		log.Fatalf("db error: %v", err)
	}
	defer db.Close()
	log.Println("database connected")

	// ─── Schema Migration (GORM AutoMigrate) ───────────────────────────
	// Ensures auth_db tables exist/are up-to-date. Idempotent and safe to
	// run alongside infra/mariadb/auth/init.sql.
	if err := runMigrations(cfg.DBDSN, AdminSeed{
		Username: cfg.AdminUsername,
		Email:    cfg.AdminEmail,
		Password: cfg.AdminPassword,
	}); err != nil {
		log.Fatalf("migration error: %v", err)
	}

	// ─── NATS ──────────────────────────────────────────────────────────
	// Connecting once at startup is fragile: at container boot the `nats`
	// DNS name may not be resolvable yet (depends_on only waits for the
	// container to *start*, not for DNS/port readiness). A single failed
	// nats.Connect leaves natsConn nil forever, silently disabling live
	// streaming, telemetry events and audit. So we retry with backoff
	// until a connection is established. Once connected, the client's own
	// MaxReconnects(-1) keeps it alive across later blips.
	var natsConn *nats.Conn
	for attempt := 1; ; attempt++ {
		natsConn, err = nats.Connect(cfg.NATSUrl,
			nats.Name("auth-svc"),
			nats.MaxReconnects(-1),
			nats.ReconnectWait(3*time.Second),
			nats.DisconnectErrHandler(func(c *nats.Conn, e error) {
				log.Printf("WARN: NATS disconnected: %v", e)
			}),
			nats.ReconnectHandler(func(c *nats.Conn) {
				log.Printf("NATS reconnected -> %s", c.ConnectedUrl())
			}),
			nats.ClosedHandler(func(c *nats.Conn) {
				log.Printf("WARN: NATS connection closed")
			}),
			nats.ErrorHandler(func(c *nats.Conn, sub *nats.Subscription, e error) {
				if e != nil {
					log.Printf("WARN: NATS async error: %v", e)
				}
			}),
		)
		if err == nil {
			break
		}
		backoff := time.Duration(attempt) * time.Second
		if backoff > 15*time.Second {
			backoff = 15 * time.Second
		}
		log.Printf("WARN: NATS connect attempt %d failed: %v — retrying in %s", attempt, err, backoff)
		time.Sleep(backoff)
	}
	defer natsConn.Drain()
	log.Println("NATS connected")

	// ─── Wire dependencies ─────────────────────────────────────────────
	repo := repository.NewUserRepository(db)
	var natsPub service.NATSPublisher
	if natsConn != nil {
		natsPub = natsConn
	}
	svc := service.NewAuthService(repo, cfg, natsPub)
	h := handler.NewAuthHandler(svc)

	// ─── Retention Cron ────────────────────────────────────────────────
	rc := cron.NewRetentionCron(repo)
	rc.Start()
	defer rc.Stop()

	// ─── Router ────────────────────────────────────────────────────────
	r := chi.NewRouter()
	r.Use(chimw.Logger)
	r.Use(chimw.Recoverer)
	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(middleware.PrometheusMiddleware)

	// Prometheus metrics endpoint (scraped by Prometheus server)
	r.Handle("/metrics", promhttp.Handler())
	// Health (no auth)
	r.Get("/health", handler.Health)

	r.Route("/auth", func(r chi.Router) {
		// ── Public routes ──────────────────────────────────────────────
		r.Post("/register", h.Register)
		r.Post("/login", h.Login)
		r.Post("/refresh", h.Refresh)

		// ── Protected routes (JWT required) ───────────────────────────
		r.Group(func(r chi.Router) {
			r.Use(middleware.JWTAuth(svc))

			// Profile & account management
			r.Get("/me", h.Me)
			r.Put("/me", h.UpdateProfile)
			r.Put("/password", h.ChangePassword)
			r.Delete("/account", h.DeleteAccount)

			// Session management
			r.Get("/sessions", h.GetSessions)

			// Logout (revokes all tokens)
			r.Post("/logout", h.Logout)

			// Admin-only — user management
			r.Group(func(r chi.Router) {
				r.Use(middleware.RequireRole("admin"))
				r.Get("/users", h.ListUsers)
				r.Get("/roles", h.ListRoles)
				r.Get("/users/{id}", h.GetUser)
				r.Put("/users/{id}", h.UpdateUser)
				r.Delete("/users/{id}", h.DeleteUser)
			})
		})
	})

	// ─── HTTP Server ───────────────────────────────────────────────────
	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      r,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		log.Printf("auth-svc listening on :%s", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	<-quit
	log.Println("shutting down auth-svc...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("shutdown error: %v", err)
	}
	log.Println("auth-svc stopped")
}

// openDB opens and pings the PostgreSQL database with retry.
func openDB(dsn string) (*sql.DB, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(5 * time.Minute)

	for i := range 10 {
		if err = db.Ping(); err == nil {
			return db, nil
		}
		log.Printf("db ping attempt %d/10 failed: %v", i+1, err)
		time.Sleep(3 * time.Second)
	}
	return nil, fmt.Errorf("database unreachable after 10 attempts: %w", err)
}
