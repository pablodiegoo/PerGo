package crypto

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"testing"
)

func TestFlowCrypto(t *testing.T) {
	t.Run("InvertIV", func(t *testing.T) {
		iv := []byte{0x00, 0xFF, 0xAA, 0x55}
		expected := []byte{0xFF, 0x00, 0x55, 0xAA}
		inverted := InvertIV(iv)
		if !bytes.Equal(inverted, expected) {
			t.Errorf("InvertIV expected %x, got %x", expected, inverted)
		}
	})

	t.Run("AESGCM", func(t *testing.T) {
		key := make([]byte, 16) // AES-128
		rand.Read(key)
		iv := make([]byte, 12)
		rand.Read(iv)
		plaintext := []byte("hello world")

		ciphertext, tag, err := EncryptAES128GCM(key, iv, plaintext)
		if err != nil {
			t.Fatalf("EncryptAES128GCM error: %v", err)
		}

		decrypted, err := DecryptAES128GCM(key, iv, ciphertext, tag)
		if err != nil {
			t.Fatalf("DecryptAES128GCM error: %v", err)
		}

		if !bytes.Equal(plaintext, decrypted) {
			t.Errorf("expected %s, got %s", plaintext, decrypted)
		}
	})

	t.Run("RSA", func(t *testing.T) {
		privKey, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatalf("GenerateKey error: %v", err)
		}

		plaintext := []byte("aes_key_data")
		ciphertext, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, &privKey.PublicKey, plaintext, nil)
		if err != nil {
			t.Fatalf("EncryptOAEP error: %v", err)
		}

		decrypted, err := DecryptRSA(privKey, ciphertext)
		if err != nil {
			t.Fatalf("DecryptRSA error: %v", err)
		}

		if !bytes.Equal(plaintext, decrypted) {
			t.Errorf("expected %s, got %s", plaintext, decrypted)
		}
	})
}
