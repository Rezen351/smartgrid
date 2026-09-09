package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds all configuration for the Auth Service.
type Config struct {
	Port string

	// Database
	DBDSN string

	// JWT
	JWTSecret     string
	JWTExpiry     time.Duration
	RefreshExpiry time.Duration

	// NATS
	NATSUrl string

	// Default admin (seeded on first startup)
	AdminUsername string
	AdminEmail    string
	AdminPassword string
}

// Load reads configuration from environment variables.
// All values have sensible defaults for local development.
func Load() (*Config, error) {
	cfg := &Config{
		Port:          getEnv("PORT", "8080"),
		DBDSN:         getEnv("DB_DSN", "postgresql://auth_user:auth_pass@postgres:5432/auth_db?sslmode=disable"),
		JWTSecret:     getEnv("JWT_SECRET", ""),
		NATSUrl:       getEnv("NATS_URL", "nats://nats:4222"),
		AdminUsername: getEnv("ADMIN_USERNAME", "admin"),
		AdminEmail:    getEnv("ADMIN_EMAIL", "admin@smartfarm.local"),
		AdminPassword: getEnv("ADMIN_PASSWORD", "admin1234"),
	}

	if cfg.JWTSecret == "" {
		return nil, fmt.Errorf("JWT_SECRET env variable is required")
	}

	var err error
	cfg.JWTExpiry, err = parseDuration(getEnv("JWT_EXPIRY", "15m"))
	if err != nil {
		return nil, fmt.Errorf("invalid JWT_EXPIRY: %w", err)
	}

	cfg.RefreshExpiry, err = parseDuration(getEnv("REFRESH_EXPIRY", "168h"))
	if err != nil {
		return nil, fmt.Errorf("invalid REFRESH_EXPIRY: %w", err)
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func parseDuration(s string) (time.Duration, error) {
	// Try Go duration format first (e.g. "15m", "168h")
	if d, err := time.ParseDuration(s); err == nil {
		return d, nil
	}
	// Fallback: interpret as seconds integer
	secs, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("cannot parse %q as duration", s)
	}
	return time.Duration(secs) * time.Second, nil
}
