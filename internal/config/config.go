// Package config provides 12-factor env-var configuration loading for PerGo.
package config

import (
	"encoding/base64"
	"os"

	"github.com/google/uuid"
)

// Config holds all configuration for the PerGo server.
type Config struct {
	DatabaseURL    string
	NATSUrl        string
	ServerPort     string
	DebugPort      string
	KEKBase64      string
	KEKBytes       []byte // decoded from KEKBase64
	AdminPassword  string
	MasterKey      string
	SessionSecret  string
	S3Endpoint     string
	S3Bucket       string
	S3AccessKey    string
	S3SecretKey    string
	S3Region       string
	S3UsePathStyle bool
	ExternalURL        string
	DefaultWorkspaceID string
}

// DefaultDevWorkspaceID is the standard deterministic UUID for the development workspace.
const DefaultDevWorkspaceID = "a0000000-0000-0000-0000-000000000001"

// Load reads configuration from environment variables with sensible defaults.
func Load() *Config {
	cfg := &Config{
		DatabaseURL:        envOrDefault("PERGO_DATABASE_URL", "postgres://postgres:postgres@localhost:5432/pergo?sslmode=disable"),
		NATSUrl:            envOrDefault("PERGO_NATS_URL", "nats://localhost:4222"),
		ServerPort:         envOrDefault("PERGO_SERVER_PORT", "8080"),
		DebugPort:          envOrDefault("PERGO_DEBUG_PORT", "6060"),
		KEKBase64:          os.Getenv("PERGO_KEK_BASE64"),
		AdminPassword:      envOrDefault("PERGO_ADMIN_PASSWORD", "pergo-dev-2026"),
		MasterKey:          os.Getenv("PERGO_MASTER_KEY"),
		SessionSecret:      os.Getenv("PERGO_SESSION_SECRET"),
		S3Endpoint:         envOrDefault("PERGO_S3_ENDPOINT", envOrDefault("S3_ENDPOINT", "")),
		S3Bucket:           envOrDefault("PERGO_S3_BUCKET", envOrDefault("S3_BUCKET", "")),
		S3AccessKey:        envOrDefault("PERGO_S3_ACCESS_KEY", envOrDefault("S3_ACCESS_KEY", "")),
		S3SecretKey:        envOrDefault("PERGO_S3_SECRET_KEY", envOrDefault("S3_SECRET_KEY", "")),
		S3Region:           envOrDefault("PERGO_S3_REGION", envOrDefault("S3_REGION", "us-east-1")),
		S3UsePathStyle:     os.Getenv("PERGO_S3_USE_PATH_STYLE") == "true" || os.Getenv("S3_USE_PATH_STYLE") == "true",
		ExternalURL:        envOrDefault("PERGO_EXTERNAL_URL", "http://localhost:8080"),
		DefaultWorkspaceID: envOrDefault("DEFAULT_WORKSPACE_ID", envOrDefault("PERGO_DEV_WORKSPACE_ID", DefaultDevWorkspaceID)),
	}

	if cfg.KEKBase64 != "" {
		kek, err := base64.StdEncoding.DecodeString(cfg.KEKBase64)
		if err == nil {
			cfg.KEKBytes = kek
		}
	}

	return cfg
}

// DefaultWorkspaceUUID returns the configured DefaultWorkspaceID as a parsed uuid.UUID, falling back to DefaultDevWorkspaceID.
func (c *Config) DefaultWorkspaceUUID() uuid.UUID {
	if c.DefaultWorkspaceID != "" {
		if id, err := uuid.Parse(c.DefaultWorkspaceID); err == nil && id != uuid.Nil {
			return id
		}
	}
	return uuid.MustParse(DefaultDevWorkspaceID)
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
