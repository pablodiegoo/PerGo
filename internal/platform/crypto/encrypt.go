package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"errors"
)

// Encryptor provides AES-256-GCM envelope encryption with a Key Encryption Key (KEK).
type Encryptor struct {
	kek []byte
}

// NewEncryptor creates a new Encryptor with the given KEK (must be 32 bytes).
func NewEncryptor(kek []byte) (*Encryptor, error) {
	if len(kek) != 32 {
		return nil, errors.New("kek must be 32 bytes")
	}
	return &Encryptor{kek: kek}, nil
}

// Encrypt encrypts plaintext using AES-256-GCM envelope encryption.
// Returns ciphertext, key_id, key_version, and error.
func (e *Encryptor) Encrypt(plaintext []byte) (ciphertext []byte, keyID string, keyVersion int, err error) {
	// Generate random DEK
	dek := make([]byte, 32)
	if _, err := rand.Read(dek); err != nil {
		return nil, "", 0, err
	}

	// Wrap DEK with KEK using AES-GCM
	block, err := aes.NewCipher(e.kek)
	if err != nil {
		return nil, "", 0, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, "", 0, err
	}

	// Fresh nonce for DEK wrapping
	dekNonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(dekNonce); err != nil {
		return nil, "", 0, err
	}
	wrappedDEK := gcm.Seal(nil, dekNonce, dek, nil)

	// Encrypt plaintext with DEK
	plainBlock, err := aes.NewCipher(dek)
	if err != nil {
		return nil, "", 0, err
	}
	plainGCM, err := cipher.NewGCM(plainBlock)
	if err != nil {
		return nil, "", 0, err
	}

	// Fresh nonce for plaintext encryption
	plainNonce := make([]byte, plainGCM.NonceSize())
	if _, err := rand.Read(plainNonce); err != nil {
		return nil, "", 0, err
	}
	encrypted := plainGCM.Seal(nil, plainNonce, plaintext, nil)

	// Envelope: dekNonce_len(1) + dekNonce + wrappedDEK + plainNonce + encrypted
	ciphertext = make([]byte, 0, 1+len(dekNonce)+len(wrappedDEK)+len(plainNonce)+len(encrypted))
	ciphertext = append(ciphertext, byte(len(dekNonce)))
	ciphertext = append(ciphertext, dekNonce...)
	ciphertext = append(ciphertext, wrappedDEK...)
	ciphertext = append(ciphertext, plainNonce...)
	ciphertext = append(ciphertext, encrypted...)

	return ciphertext, "default", 1, nil
}

// Decrypt decrypts ciphertext produced by Encrypt.
func (e *Encryptor) Decrypt(ciphertext []byte) (plaintext []byte, err error) {
	if len(ciphertext) < 1 {
		return nil, errors.New("ciphertext too short")
	}

	// Parse envelope
	dekNonceLen := int(ciphertext[0])
	if len(ciphertext) < 1+dekNonceLen {
		return nil, errors.New("ciphertext too short for DEK nonce")
	}
	dekNonce := ciphertext[1 : 1+dekNonceLen]
	rest := ciphertext[1+dekNonceLen:]

	// DEK wrapping uses the same GCM as encrypt (32-byte key → 16-byte tag)
	block, err := aes.NewCipher(e.kek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	wrappedDEKLen := gcm.Overhead() + 32 // nonce + ciphertext + tag = overhead + plaintext
	if len(rest) < wrappedDEKLen {
		return nil, errors.New("ciphertext too short for wrapped DEK")
	}
	wrappedDEK := rest[:wrappedDEKLen]
	rest = rest[wrappedDEKLen:]

	// Unwrap DEK
	dek, err := gcm.Open(nil, dekNonce, wrappedDEK, nil)
	if err != nil {
		return nil, err
	}

	// Parse plaintext nonce + encrypted data
	plainBlock, err := aes.NewCipher(dek)
	if err != nil {
		return nil, err
	}
	plainGCM, err := cipher.NewGCM(plainBlock)
	if err != nil {
		return nil, err
	}

	nonceSize := plainGCM.NonceSize()
	if len(rest) < nonceSize {
		return nil, errors.New("ciphertext too short for plaintext nonce")
	}
	plainNonce := rest[:nonceSize]
	encrypted := rest[nonceSize:]

	return plainGCM.Open(nil, plainNonce, encrypted, nil)
}

// DecryptRSA decrypts ciphertext using RSA-OAEP with SHA-256.
func DecryptRSA(privateKey *rsa.PrivateKey, ciphertext []byte) ([]byte, error) {
	return rsa.DecryptOAEP(sha256.New(), rand.Reader, privateKey, ciphertext, nil)
}

// DecryptRSAPem decrypts ciphertext using RSA-OAEP with SHA-256 from a PEM-encoded key.
func DecryptRSAPem(privateKeyPem []byte, ciphertext []byte) ([]byte, error) {
	priv, err := ParseRSAPrivateKeyFromPEM(privateKeyPem)
	if err != nil {
		return nil, err
	}
	return DecryptRSA(priv, ciphertext)
}

// DecryptAES128GCM decrypts ciphertext using AES-GCM.
func DecryptAES128GCM(key, iv, ciphertext, tag []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	ciphertextWithTag := append(ciphertext, tag...)
	return gcm.Open(nil, iv, ciphertextWithTag, nil)
}

// EncryptAES128GCM encrypts plaintext using AES-GCM.
func EncryptAES128GCM(key, iv, plaintext []byte) ([]byte, []byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, err
	}
	sealed := gcm.Seal(nil, iv, plaintext, nil)
	tagSize := gcm.Overhead()
	return sealed[:len(sealed)-tagSize], sealed[len(sealed)-tagSize:], nil
}

// InvertIV performs a bitwise XOR of every byte with 0xFF.
func InvertIV(iv []byte) []byte {
	inverted := make([]byte, len(iv))
	for i, b := range iv {
		inverted[i] = b ^ 0xFF
	}
	return inverted
}
