package crypto

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestHashAPIKey(t *testing.T) {
	key := "pergo_live_1234567890abcdef"
	hash, prefix := HashAPIKey(key)

	if len(prefix) != APIKeyPrefixLength {
		t.Errorf("expected prefix length %d, got %d", APIKeyPrefixLength, len(prefix))
	}
	if prefix != "pergo_li" {
		t.Errorf("expected prefix 'pergo_li', got %q", prefix)
	}

	expectedHash := sha256.Sum256([]byte(key))
	if hex.EncodeToString(hash) != hex.EncodeToString(expectedHash[:]) {
		t.Errorf("hash mismatch")
	}

	// Short key shorter than APIKeyPrefixLength
	shortKey := "abc"
	_, shortPrefix := HashAPIKey(shortKey)
	if shortPrefix != "abc" {
		t.Errorf("expected short prefix 'abc', got %q", shortPrefix)
	}
}

func TestVerifyAPIKey(t *testing.T) {
	key := "secret_api_key_123"
	hash, _ := HashAPIKey(key)

	if !VerifyAPIKey(key, hash) {
		t.Errorf("expected VerifyAPIKey to return true for matching key")
	}

	if VerifyAPIKey("wrong_key", hash) {
		t.Errorf("expected VerifyAPIKey to return false for wrong key")
	}

	if VerifyAPIKey(key, []byte("short")) {
		t.Errorf("expected VerifyAPIKey to return false for invalid length storedHash")
	}
}

func TestCompareHashConstantTime(t *testing.T) {
	tests := []struct {
		name     string
		a        string
		b        string
		expected bool
	}{
		{"identical strings", "sha256:1234567890abcdef", "sha256:1234567890abcdef", true},
		{"different characters", "sha256:1234567890abcdef", "sha256:1234567890abcdeg", false},
		{"different lengths", "sha256:123456", "sha256:1234567890abcdef", false},
		{"empty strings", "", "", true},
		{"one empty string", "something", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CompareHashConstantTime(tt.a, tt.b)
			if got != tt.expected {
				t.Errorf("CompareHashConstantTime(%q, %q) = %v; want %v", tt.a, tt.b, got, tt.expected)
			}
		})
	}
}

func TestHashSHA256(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected string
	}{
		{
			name:     "hello",
			input:    []byte("hello"),
			expected: "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824",
		},
		{
			name:     "empty input",
			input:    []byte(""),
			expected: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		},
		{
			name:     "pergo test vector",
			input:    []byte("PerGo Omnichannel CPaaS"),
			expected: "07aa5a94cdd90acf5a5ab6856b0ff7c4147c718aea64bbd9392ba10d89679faf",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HashSHA256(tt.input)
			if got != tt.expected {
				t.Errorf("HashSHA256(%q) = %q; want %q", string(tt.input), got, tt.expected)
			}
		})
	}
}
