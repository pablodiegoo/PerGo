// Package config provides 12-factor env-var configuration loading for PerGo.
package config

import (
	"encoding/base64"
	"os"
)

// Config holds all configuration for the PerGo server.
type Config struct {
	Env            string
	DatabaseURL    string
	NATSUrl        string
	ServerPort     string
	DebugPort      string
	KEKBase64      string
	KEKBytes       []byte // decoded from KEKBase64
	KEKDecodeErr   error  // error if KEKBase64 decoding fails
	AdminPassword  string
	MasterKey      string
	SessionSecret  string
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
		Env:            envOrDefault("PERGO_ENV", envOrDefault("ENV", "development")),
		DatabaseURL:    envOrDefault("PERGO_DATABASE_URL", "postgres://postgres:postgres@localhost:5432/pergo?sslmode=disable"),
		NATSUrl:        envOrDefault("PERGO_NATS_URL", "nats://localhost:4222"),
		ServerPort:     envOrDefault("PERGO_SERVER_PORT", "8080"),
		DebugPort:      envOrDefault("PERGO_DEBUG_PORT", "6060"),
		KEKBase64:      os.Getenv("PERGO_KEK_BASE64"),
		AdminPassword:  envOrDefault("PERGO_ADMIN_PASSWORD", "pergo-dev-2026"),
		MasterKey:      os.Getenv("PERGO_MASTER_KEY"),
		SessionSecret:  os.Getenv("PERGO_SESSION_SECRET"),
		S3Endpoint:     envOrDefault("PERGO_S3_ENDPOINT", envOrDefault("S3_ENDPOINT", "")),
		S3Bucket:       envOrDefault("PERGO_S3_BUCKET", envOrDefault("S3_BUCKET", "")),
		S3AccessKey:    envOrDefault("PERGO_S3_ACCESS_KEY", envOrDefault("S3_ACCESS_KEY", "")),
		S3SecretKey:    envOrDefault("PERGO_S3_SECRET_KEY", envOrDefault("S3_SECRET_KEY", "")),
		S3Region:       envOrDefault("PERGO_S3_REGION", envOrDefault("S3_REGION", "us-east-1")),
		S3UsePathStyle: os.Getenv("PERGO_S3_USE_PATH_STYLE") == "true" || os.Getenv("S3_USE_PATH_STYLE") == "true",
		ExternalURL:    envOrDefault("PERGO_EXTERNAL_URL", "http://localhost:8080"),
	}

	if cfg.KEKBase64 != "" {
		kek, err := base64.StdEncoding.DecodeString(cfg.KEKBase64)
		if err != nil {
			cfg.KEKDecodeErr = err
		} else {
			cfg.KEKBytes = kek
		}
	}

	return cfg
}

// IsProduction returns true if the environment is set to production.
func (c *Config) IsProduction() bool {
	return c.Env == "production" || c.Env == "prod"
}

// Validate checks that mandatory configuration values are sound.
func (c *Config) Validate() error {
	if c.KEKDecodeErr != nil {
		return c.KEKDecodeErr
	}
	if c.KEKBase64 != "" && len(c.KEKBytes) != 32 {
		return os.ErrInvalid
	}
	if c.IsProduction() && len(c.KEKBytes) != 32 {
		return os.ErrInvalid
	}
	return nil
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
