package config

import (
	"os"
)

// Config holds all configuration for the Module Service.
type Config struct {
	Port string

	// MariaDB — modules, nodes, pairing state (primary store for onboarding)
	DBDSN string

	// TimescaleDB — time-series telemetry store.
	// Provisioned & hypertable-ready in this phase; telemetry ingest lands in the next phase.
	TimescaleDSN string

	// Redis — realtime node status / last-seen cache
	RedisAddr     string
	RedisPassword string
	RedisDB       int

	// NATS — audit + event bus
	NATSUrl string

	// JWT — shared secret with Auth Service for validating access tokens.
	// When empty (dev), auth is skipped; Kong still fronts the service.
	JWTSecret string

	// MQTT — Mosquitto broker (device onboarding signals)
	MQTTURL         string
	MQTTUser        string
	MQTTPass        string
	MQTTClientID    string
	MQTTTopicPrefix string
}

// Load reads configuration from environment variables with dev-friendly defaults.
func Load() (*Config, error) {
	cfg := &Config{
		Port:            getEnv("PORT", "8080"),
		DBDSN:           getEnv("DB_DSN", "module_user:module_pass@tcp(mariadb-module:3306)/module_db?parseTime=true&charset=utf8mb4"),
		TimescaleDSN:    getEnv("TIMESCALE_DSN", "postgres://module_user:module_pass@timescaledb-module:5432/module_ts?sslmode=disable"),
		RedisAddr:       getEnv("REDIS_ADDR", "redis-shared:6379"),
		RedisPassword:   getEnv("REDIS_PASSWORD", ""),
		RedisDB:         getEnvInt("REDIS_DB", 0),
		NATSUrl:         getEnv("NATS_URL", "nats://nats:4222"),
		JWTSecret:       getEnv("JWT_SECRET", ""),
		MQTTURL:         getEnv("MQTT_URL", "tcp://mosquitto:1883"),
		MQTTUser:        getEnv("MQTT_USER", ""),
		MQTTPass:        getEnv("MQTT_PASS", ""),
		MQTTClientID:    getEnv("MQTT_CLIENT_ID", "module-svc"),
		MQTTTopicPrefix: getEnv("MQTT_TOPIC_PREFIX", "smartfarm"),
	}
	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		n := 0
		for _, c := range v {
			if c < '0' || c > '9' {
				return fallback
			}
			n = n*10 + int(c-'0')
		}
		return n
	}
	return fallback
}
