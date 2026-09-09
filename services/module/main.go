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

	"github.com/almuzky/iot/services/module/internal/cache"
	"github.com/almuzky/iot/services/module/internal/config"
	"github.com/almuzky/iot/services/module/internal/handler"
	"github.com/almuzky/iot/services/module/internal/middleware"
	mqttsub "github.com/almuzky/iot/services/module/internal/mqtt"
	"github.com/almuzky/iot/services/module/internal/outbox"
	"github.com/almuzky/iot/services/module/internal/repository"
	"github.com/almuzky/iot/services/module/internal/service"
	"github.com/almuzky/iot/services/module/internal/tsdb"
	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	_ "github.com/go-sql-driver/mysql"
	"github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	// ─── MariaDB (modules, nodes) ──────────────────────────────────────
	db, err := openDB(cfg.DBDSN)
	if err != nil {
		log.Fatalf("db error: %v", err)
	}
	defer db.Close()
	log.Println("mariadb connected")

	if err := runMigrations(cfg.DBDSN); err != nil {
		log.Fatalf("migration error: %v", err)
	}

	// ─── Redis (realtime status cache) ─────────────────────────────────
	statusCache := cache.New(cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB)
	pctx, pcancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := statusCache.Ping(pctx); err != nil {
		log.Printf("WARN: redis ping failed: %v — status cache degraded", err)
	} else {
		log.Println("redis connected")
	}
	pcancel()
	defer statusCache.Close()

	// ─── NATS (audit + events) ─────────────────────────────────────────
	// Connecting once at startup is fragile: at container boot the `nats`
	// DNS name may not be resolvable yet (depends_on only waits for the
	// container to *start*, not for DNS/port readiness). A single failed
	// nats.Connect leaves natsConn nil forever, silently disabling live
	// streaming (PublishLive), core telemetry events and audit. So we retry
	// with backoff until a connection is established. Once connected, the
	// client's own MaxReconnects(-1) keeps it alive across later blips.
	var natsConn *nats.Conn
	for attempt := 1; ; attempt++ {
		natsConn, err = nats.Connect(cfg.NATSUrl,
			nats.Name("module-svc"),
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

	// Periodic health-check: surfaces a prolonged NATS outage so live telemetry
	// (PublishLive), core telemetry events and audit logs are not silently lost
	// while the connection is down.
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			if natsConn == nil || !natsConn.IsConnected() {
				log.Printf("WARN: NATS not connected — live telemetry/audit publish will buffer until reconnect")
			}
		}
	}()

	// ─── NATS JetStream (durable telemetry.batch) ────────────────────
	// The batch publisher emits one aggregate per minute on telemetry.batch.
	// Publishing via JetStream (with a durable consumer in Analytics) means a
	// restart/blip of Analytics no longer drops the window permanently.
	var jsPub service.JetStreamPublisher
	if natsConn != nil {
		if js, jerr := natsConn.JetStream(); jerr == nil {
			_, serr := js.AddStream(&nats.StreamConfig{
				Name:      "TELEMETRY_BATCH",
				Subjects:  []string{"telemetry.batch"},
				Retention: nats.LimitsPolicy,
				Storage:   nats.FileStorage,
				MaxAge:    24 * time.Hour,
				MaxMsgs:   1_000_000,
				Replicas:  1,
			})
			if serr != nil {
				log.Printf("WARN: NATS JetStream stream ensure failed: %v — batch falls back to core NATS", serr)
			} else {
				jsPub = &jsBatchPublisher{js: js}
				log.Println("NATS JetStream stream TELEMETRY_BATCH ready")
			}
		} else {
			log.Printf("WARN: NATS JetStream init failed: %v — batch falls back to core NATS", jerr)
		}
	}

	// ─── Wire dependencies ─────────────────────────────────────────────
	repo := repository.New(db)
	var natsPub service.NATSPublisher
	if natsConn != nil {
		natsPub = natsConn
	}

	// TimescaleDB — time-series telemetry store.
	tsStore, err := tsdb.New(cfg.TimescaleDSN)
	if err != nil {
		log.Printf("WARN: TimescaleDB connect failed: %v — telemetry ingest disabled", err)
	} else {
		defer tsStore.Close()
		log.Println("timescaledb connected")
	}

	svc := service.New(repo, statusCache, natsPub, jsPub, tsStore)
	h := handler.New(svc)

	// ─── Outbox relay (ADR-007) ───────────────────────────────────────
	// Drains the outbox table and publishes events to NATS JetStream with a
	// Nats-Msg-Id dedupe header. Relay created here; started (after bgCtx is
	// available) just before the HTTP server so no event written during
	// startup is lost.
	relay := outbox.New(repo, nil)
	if natsConn != nil {
		relay.SetNATS(natsConn)
	}

	// ─── MQTT subscriber (device onboarding + publish live to NATS) ───
	sub, err := mqttsub.New(mqttsub.Config{
		BrokerURL:   cfg.MQTTURL,
		Username:    cfg.MQTTUser,
		Password:    cfg.MQTTPass,
		ClientID:    cfg.MQTTClientID,
		TopicPrefix: cfg.MQTTTopicPrefix,
	}, svc)
	if err != nil {
		log.Printf("WARN: MQTT init failed: %v — onboarding disabled", err)
	} else {
		defer sub.Disconnect()
	}

	// ─── Telemetry batch publisher (telemetry.batch every 1 min) ────
	bgCtx, bgCancel := context.WithCancel(context.Background())
	defer bgCancel()
	go relay.Start(bgCtx)
	go svc.StartBatchPublisher(bgCtx, time.Minute)
	// Batched TouchNode flusher: collapses per-message last_seen writes into one
	// UPDATE per node per interval (default 30s).
	go svc.StartTouchFlusher(bgCtx, 30*time.Second)

	// ─── Router ────────────────────────────────────────────────────────
	r := chi.NewRouter()
	r.Use(chimw.Logger)
	r.Use(chimw.Recoverer)
	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(middleware.PrometheusMiddleware)

	r.Handle("/metrics", promhttp.Handler())
	r.Get("/health", handler.Health)

	// ─── Auth middleware ───────────────────────────────────────────────
	// Kong fronts the service and (optionally) rate-limits; the service itself
	// enforces a valid JWT and RBAC so protected resources are never reachable
	// unauthenticated. Reads require any valid user; writes require admin/operator.
	authMw := middleware.JWTAuth(cfg.JWTSecret)
	writeMw := middleware.RequireRole(cfg.JWTSecret, "admin", "operator")

	// Modules
	r.Route("/modules", func(r chi.Router) {
		r.With(authMw).Get("/", h.ListModules)
		r.With(authMw, writeMw).Post("/", h.CreateModule)
		r.With(authMw).Get("/{id}", h.GetModule)
		r.With(authMw, writeMw).Put("/{id}", h.UpdateModule)
		r.With(authMw, writeMw).Delete("/{id}", h.DeleteModule)
	})

	// Nodes
	r.Route("/nodes", func(r chi.Router) {
		r.With(authMw).Get("/", h.ListNodes)
		r.With(authMw).Get("/discovered", h.ListDiscovered)
		r.With(authMw).Get("/{node_id}", h.GetNode)
		r.With(authMw).Get("/{node_id}/live", h.GetLiveTelemetry)
		r.With(authMw, writeMw).Post("/{node_id}/pair", h.PairNode)
		r.With(authMw, writeMw).Post("/{node_id}/unpair", h.UnpairNode)
		r.With(authMw, writeMw).Delete("/{node_id}", h.DeleteNode)
		r.With(authMw).Get("/{node_id}/tags", h.GetNodeTags)
		r.With(authMw, writeMw).Put("/{node_id}/tags", h.SaveNodeTags)
		r.With(authMw, writeMw).Post("/{node_id}/tags", h.SaveNodeTag)
		r.With(authMw, writeMw).Delete("/{node_id}/tags/{id}", h.DeleteNodeTag)
		r.With(authMw).Get("/{node_id}/actuators", h.GetActuatorTags)
		r.With(authMw, writeMw).Post("/{node_id}/actuators", h.CreateActuatorTag)
		r.With(authMw, writeMw).Delete("/{node_id}/actuators/{id}", h.DeleteActuatorTag)
	})

	// ─── HTTP Server ───────────────────────────────────────────────────
	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      r,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		log.Printf("module-svc listening on :%s", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	<-quit
	log.Println("shutting down module-svc...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("shutdown error: %v", err)
	}
	log.Println("module-svc stopped")
}

func openDB(dsn string) (*sql.DB, error) {
	db, err := sql.Open("mysql", dsn)
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

// jsBatchPublisher adapts a NATS JetStream context to the service's
// JetStreamPublisher interface so telemetry.batch is published durably.
type jsBatchPublisher struct {
	js nats.JetStreamContext
}

func (p *jsBatchPublisher) Publish(subject string, data []byte) error {
	_, err := p.js.Publish(subject, data)
	return err
}
