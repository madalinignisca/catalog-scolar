package config

import (
	"log/slog"
	"os"
)

type Config struct {
	Env         string
	Port        string
	LogLevel    slog.Level
	DatabaseURL string
	RedisURL    string
	JWTSecret   string
	TOTPKey     string
	BaseURL     string
}

func Load() *Config {
	cfg := &Config{
		Env:         getEnv("ENV", "development"),
		Port:        getEnv("PORT", "8080"),
		DatabaseURL: getEnv("DATABASE_URL", "postgres://catalogro:catalogro@localhost:5432/catalogro?sslmode=disable"),
		RedisURL:    getEnv("REDIS_URL", "redis://localhost:6379/0"),
		JWTSecret:   getEnv("JWT_SECRET", "dev-secret-change-in-production-please"),
		TOTPKey:     getEnv("TOTP_ENCRYPTION_KEY", "dev-totp-key-change-in-production-please"),
		BaseURL:     getEnv("APP_BASE_URL", "http://localhost:3000"),
	}

	if cfg.Env == "production" {
		cfg.LogLevel = slog.LevelInfo
	} else {
		cfg.LogLevel = slog.LevelDebug
	}

	return cfg
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
