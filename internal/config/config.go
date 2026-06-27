// Package config provides 12-factor env-var configuration loading for OmniGo.
package config

import (
	"encoding/base64"
	"os"
)

// Config holds all configuration for the OmniGo server.
type Config struct {
	DatabaseURL string
	NATSUrl     string
	ServerPort  string
	DebugPort   string
	KEKBase64   string
	KEKBytes    []byte // decoded from KEKBase64
	S3Endpoint     string
	S3Bucket       string
	S3AccessKey    string
	S3SecretKey    string
	S3Region       string
	S3UsePathStyle bool
	ExternalURL    string
}

// Load reads configuration from environment variables with sensible defaults.
func Load() *Config {
	cfg := &Config{
		DatabaseURL: envOrDefault("OMNIGO_DATABASE_URL", "postgres://postgres:postgres@localhost:5432/omnigo?sslmode=disable"),
		NATSUrl:     envOrDefault("OMNIGO_NATS_URL", "nats://localhost:4222"),
		ServerPort:  envOrDefault("OMNIGO_SERVER_PORT", "8080"),
		DebugPort:   envOrDefault("OMNIGO_DEBUG_PORT", "6060"),
		KEKBase64:   os.Getenv("OMNIGO_KEK_BASE64"),
		S3Endpoint:     envOrDefault("OMNIGO_S3_ENDPOINT", envOrDefault("S3_ENDPOINT", "")),
		S3Bucket:       envOrDefault("OMNIGO_S3_BUCKET", envOrDefault("S3_BUCKET", "")),
		S3AccessKey:    envOrDefault("OMNIGO_S3_ACCESS_KEY", envOrDefault("S3_ACCESS_KEY", "")),
		S3SecretKey:    envOrDefault("OMNIGO_S3_SECRET_KEY", envOrDefault("S3_SECRET_KEY", "")),
		S3Region:       envOrDefault("OMNIGO_S3_REGION", envOrDefault("S3_REGION", "us-east-1")),
		S3UsePathStyle: os.Getenv("OMNIGO_S3_USE_PATH_STYLE") == "true" || os.Getenv("S3_USE_PATH_STYLE") == "true",
		ExternalURL:    envOrDefault("OMNIGO_EXTERNAL_URL", "http://localhost:8080"),
	}

	if cfg.KEKBase64 != "" {
		kek, err := base64.StdEncoding.DecodeString(cfg.KEKBase64)
		if err == nil {
			cfg.KEKBytes = kek
		}
	}

	return cfg
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
