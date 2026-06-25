// Package crypto provides cryptographic primitives for OmniGo.
// SHA-256 hashing for API keys and AES-256-GCM envelope encryption.
package crypto

import "crypto/sha256"

// HashAPIKey hashes an API key with SHA-256 and returns the hash and prefix.
func HashAPIKey(key string) (hash []byte, prefix string) {
	h := sha256.Sum256([]byte(key))
	return h[:], key[:min(8, len(key))]
}

// VerifyAPIKey verifies that the provided key matches the stored hash.
func VerifyAPIKey(key string, storedHash []byte) bool {
	hash := sha256.Sum256([]byte(key))
	return hmacEqual(hash[:], storedHash)
}

func hmacEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
