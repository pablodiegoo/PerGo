package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
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
