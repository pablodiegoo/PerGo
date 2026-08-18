package config

import (
	"os"
	"testing"
)

func TestConfig_Load_MasterKey(t *testing.T) {
	// Test when PERGO_MASTER_KEY is set
	t.Run("with PERGO_MASTER_KEY set", func(t *testing.T) {
		orig := os.Getenv("PERGO_MASTER_KEY")
		defer os.Setenv("PERGO_MASTER_KEY", orig)

		os.Setenv("PERGO_MASTER_KEY", "test-master-key-xyz")
		cfg := Load()

		if cfg.MasterKey != "test-master-key-xyz" {
			t.Fatalf("expected MasterKey to be 'test-master-key-xyz', got %q", cfg.MasterKey)
		}
	})

	// Test when PERGO_MASTER_KEY is not set
	t.Run("with PERGO_MASTER_KEY unset", func(t *testing.T) {
		orig := os.Getenv("PERGO_MASTER_KEY")
		defer os.Setenv("PERGO_MASTER_KEY", orig)

		os.Unsetenv("PERGO_MASTER_KEY")
		cfg := Load()

		if cfg.MasterKey != "" {
			t.Fatalf("expected MasterKey to be empty when unset, got %q", cfg.MasterKey)
		}
	})
}

func TestConfig_KEK_Validation(t *testing.T) {
	t.Run("valid 32-byte KEKBase64", func(t *testing.T) {
		origKEK := os.Getenv("PERGO_KEK_BASE64")
		origEnv := os.Getenv("PERGO_ENV")
		defer func() {
			os.Setenv("PERGO_KEK_BASE64", origKEK)
			os.Setenv("PERGO_ENV", origEnv)
		}()

		// 32 zero bytes in base64: AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=
		os.Setenv("PERGO_KEK_BASE64", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
		os.Setenv("PERGO_ENV", "production")
		cfg := Load()

		if !cfg.IsProduction() {
			t.Errorf("expected IsProduction to be true")
		}
		if len(cfg.KEKBytes) != 32 {
			t.Fatalf("expected 32 bytes KEK, got %d", len(cfg.KEKBytes))
		}
		if err := cfg.Validate(); err != nil {
			t.Fatalf("expected valid config, got error: %v", err)
		}
	})

	t.Run("invalid base64 KEK", func(t *testing.T) {
		origKEK := os.Getenv("PERGO_KEK_BASE64")
		defer os.Setenv("PERGO_KEK_BASE64", origKEK)

		os.Setenv("PERGO_KEK_BASE64", "not-valid-base64!!!")
		cfg := Load()

		if cfg.KEKDecodeErr == nil {
			t.Fatalf("expected KEKDecodeErr to be non-nil")
		}
		if err := cfg.Validate(); err == nil {
			t.Fatalf("expected Validate() to fail on invalid base64")
		}
	})

	t.Run("valid base64 but not 32 bytes", func(t *testing.T) {
		origKEK := os.Getenv("PERGO_KEK_BASE64")
		defer os.Setenv("PERGO_KEK_BASE64", origKEK)

		// 4 bytes base64: AAAA
		os.Setenv("PERGO_KEK_BASE64", "AAAA")
		cfg := Load()

		if cfg.KEKDecodeErr != nil {
			t.Fatalf("unexpected decode error: %v", cfg.KEKDecodeErr)
		}
		if err := cfg.Validate(); err == nil {
			t.Fatalf("expected Validate() to fail on non-32 byte KEK")
		}
	})

	t.Run("production without KEK fails validation", func(t *testing.T) {
		origKEK := os.Getenv("PERGO_KEK_BASE64")
		origEnv := os.Getenv("PERGO_ENV")
		defer func() {
			os.Setenv("PERGO_KEK_BASE64", origKEK)
			os.Setenv("PERGO_ENV", origEnv)
		}()

		os.Unsetenv("PERGO_KEK_BASE64")
		os.Setenv("PERGO_ENV", "production")
		cfg := Load()

		if !cfg.IsProduction() {
			t.Errorf("expected IsProduction to be true")
		}
		if err := cfg.Validate(); err == nil {
			t.Fatalf("expected Validate() to fail in production without KEK")
		}
	})

	t.Run("development without KEK passes validation (uses fallback)", func(t *testing.T) {
		origKEK := os.Getenv("PERGO_KEK_BASE64")
		origEnv := os.Getenv("PERGO_ENV")
		defer func() {
			os.Setenv("PERGO_KEK_BASE64", origKEK)
			os.Setenv("PERGO_ENV", origEnv)
		}()

		os.Unsetenv("PERGO_KEK_BASE64")
		os.Setenv("PERGO_ENV", "development")
		cfg := Load()

		if cfg.IsProduction() {
			t.Errorf("expected IsProduction to be false")
		}
		if err := cfg.Validate(); err != nil {
			t.Fatalf("expected development without KEK to pass Validate(), got %v", err)
		}
	})
}
