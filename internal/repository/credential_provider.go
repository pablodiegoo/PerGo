// Package repository provides the CredentialProvider port for enveloped
// encryption at rest. Production uses crypto.Encryptor (AES-256-GCM);
// tests use a no-op provider returning plain bytes.
package repository

// CredentialProvider is the port for encrypting and decrypting sensitive
// payloads before storage. Implementations must be safe for concurrent use.
type CredentialProvider interface {
	Encrypt(plaintext []byte) (ciphertext []byte, keyID string, keyVersion int, err error)
	Decrypt(ciphertext []byte) (plaintext []byte, err error)
}
