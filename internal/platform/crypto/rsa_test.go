package crypto

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"os"
	"testing"
)

func TestLoadRSAPrivateKey(t *testing.T) {
	priv2048, _ := rsa.GenerateKey(rand.Reader, 2048)
	priv4096, _ := rsa.GenerateKey(rand.Reader, 4096)

	pem2048 := string(pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(priv2048),
	}))

	pem4096 := string(pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(priv4096),
	}))

	t.Run("Valid 2048 bit key from JSON", func(t *testing.T) {
		creds := map[string]string{"private_key": pem2048}
		raw, _ := json.Marshal(creds)
		key, err := LoadRSAPrivateKey(raw, nil)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if key.N.BitLen() != 2048 {
			t.Errorf("expected 2048 bit key")
		}
	})

	t.Run("Valid 2048 bit key from Env", func(t *testing.T) {
		os.Setenv("WABA_FLOWS_PRIVATE_KEY", pem2048)
		defer os.Unsetenv("WABA_FLOWS_PRIVATE_KEY")
		
		key, err := LoadRSAPrivateKey([]byte("{}"), nil)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if key.N.BitLen() != 2048 {
			t.Errorf("expected 2048 bit key")
		}
	})

	t.Run("Invalid 4096 bit key", func(t *testing.T) {
		creds := map[string]string{"private_key": pem4096}
		raw, _ := json.Marshal(creds)
		_, err := LoadRSAPrivateKey(raw, nil)
		if err == nil {
			t.Fatalf("expected error for non-2048 bit key")
		}
	})
}
