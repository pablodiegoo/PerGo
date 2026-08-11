// Package crypto provides cryptographic primitives for PerGo.
// SHA-256 hashing for API keys and AES-256-GCM envelope encryption.
package crypto

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
)

// APIKeyPrefixLength is the length of the prefix stored and displayed for API keys.
const APIKeyPrefixLength = 8

// HashAPIKey hashes an API key with SHA-256 and returns the hash and prefix.
func HashAPIKey(key string) (hash []byte, prefix string) {
	h := sha256.Sum256([]byte(key))
	return h[:], key[:min(APIKeyPrefixLength, len(key))]
}

// VerifyAPIKey verifies that the provided key matches the stored hash using constant-time comparison.
func VerifyAPIKey(key string, storedHash []byte) bool {
	hash := sha256.Sum256([]byte(key))
	return subtle.ConstantTimeCompare(hash[:], storedHash) == 1
}

// CompareHashConstantTime compares two string hashes in constant time to prevent timing attacks.
func CompareHashConstantTime(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// HashSHA256 computes the SHA-256 digest of data and returns it as a hex-encoded string.
func HashSHA256(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
