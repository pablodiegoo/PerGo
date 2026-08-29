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

func TestRSAKeypairGenerationAndExport(t *testing.T) {
	t.Run("GenerateRSAKeyPair2048 produces valid 2048-bit keypair", func(t *testing.T) {
		privPEM, pubPEM, err := GenerateRSAKeyPair2048()
		if err != nil {
			t.Fatalf("unexpected error generating keypair: %v", err)
		}
		if privPEM == "" || pubPEM == "" {
			t.Fatalf("expected non-empty PEM strings")
		}

		privKey, err := ParseRSAPrivateKeyFromPEM([]byte(privPEM))
		if err != nil {
			t.Fatalf("failed to parse generated private key: %v", err)
		}
		if privKey.N.BitLen() != 2048 {
			t.Errorf("expected 2048 bit key, got %d", privKey.N.BitLen())
		}

		derivedPubPEM, err := ExportRSAPublicKeyPEM(privKey)
		if err != nil {
			t.Fatalf("failed to export public key PEM: %v", err)
		}
		if derivedPubPEM != pubPEM {
			t.Errorf("expected derived public key PEM to match generated public key PEM")
		}

		block, _ := pem.Decode([]byte(pubPEM))
		if block == nil || block.Type != "PUBLIC KEY" {
			t.Fatalf("expected PUBLIC KEY block type, got %v", block)
		}

		pubInterface, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			t.Fatalf("failed to parse PKIX public key: %v", err)
		}
		rsaPub, ok := pubInterface.(*rsa.PublicKey)
		if !ok {
			t.Fatalf("expected *rsa.PublicKey, got %T", pubInterface)
		}
		if rsaPub.N.BitLen() != 2048 {
			t.Errorf("expected 2048 bit public key, got %d", rsaPub.N.BitLen())
		}
	})

	t.Run("ParseRSAPrivateKeyFromPEM rejects invalid or non-2048 bit keys", func(t *testing.T) {
		_, err := ParseRSAPrivateKeyFromPEM([]byte("invalid pem data"))
		if err == nil {
			t.Errorf("expected error for garbage PEM")
		}

		priv4096, _ := rsa.GenerateKey(rand.Reader, 4096)
		pem4096 := pem.EncodeToMemory(&pem.Block{
			Type:  "RSA PRIVATE KEY",
			Bytes: x509.MarshalPKCS1PrivateKey(priv4096),
		})
		_, err = ParseRSAPrivateKeyFromPEM(pem4096)
		if err == nil {
			t.Errorf("expected error for 4096-bit key")
		}
	})
}
