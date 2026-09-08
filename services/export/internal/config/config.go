package config

import (
	"os"
	"strconv"
)

// Config holds all configuration for the Export Service.
type Config struct {
	Port string

	// TimescaleDSN points at the Module Service's time-series store
	// (Database-per-Service: Export reads module_ts telemetry for export).
	TimescaleDSN string

	// RedisAddr is the query cache (currently optional / unused for correctness).
	RedisAddr string

	// RedisPassword / RedisDB — shared redis-shared instance (ADR-004),
	// export service owns logical DB 3.
	RedisPassword string
	RedisDB       int

	// JWTSecret validates the Bearer token issued by the Auth Service so
	// exports are never reachable unauthenticated.
	JWTSecret string
}

// Load reads configuration from environment variables with dev-friendly defaults.
func Load() (*Config, error) {
	cfg := &Config{
		Port: getEnv("PORT", "8080"),
		TimescaleDSN: getEnv("TIMESCALE_DSN", getEnv("TIMESCALEDB_MODULE_DSN",
			"postgres://app:app1234@timescaledb-module:5432/module_ts?sslmode=disable")),
		RedisAddr:     getEnv("REDIS_ADDR", "redis-shared:6379"),
		RedisPassword: getEnv("REDIS_PASSWORD", ""),
		RedisDB:       atoiDefault(getEnv("REDIS_DB", "3"), 3),
		JWTSecret:     getEnv("JWT_SECRET", ""),
	}
	return cfg, nil
}

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
