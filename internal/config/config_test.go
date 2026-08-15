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
