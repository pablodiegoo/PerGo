package crypto

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"os"
)

type WABACredentials struct {
	PrivateKey string `json:"private_key"`
}

// LoadRSAPrivateKey extracts private_key field from the WABA connection's credentials JSON.
func LoadRSAPrivateKey(connectionCredentials json.RawMessage, encryptor *Encryptor) (*rsa.PrivateKey, error) {
	var creds WABACredentials
	if len(connectionCredentials) > 0 {
		if err := json.Unmarshal(connectionCredentials, &creds); err != nil {
			return nil, err
		}
	}

	pemString := creds.PrivateKey
	if pemString == "" {
		pemString = os.Getenv("WABA_FLOWS_PRIVATE_KEY")
	}

	if pemString == "" {
		return nil, errors.New("private_key not found in credentials or environment")
	}

	return ParseRSAPrivateKeyFromPEM([]byte(pemString))
}

// ParseRSAPrivateKeyFromPEM parses and validates a 2048-bit RSA private key from PEM bytes.
func ParseRSAPrivateKeyFromPEM(pemBytes []byte) (*rsa.PrivateKey, error) {

	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("failed to parse PEM block")
	}

	var priv *rsa.PrivateKey
	var err error

	if block.Type == "RSA PRIVATE KEY" {
		priv, err = x509.ParsePKCS1PrivateKey(block.Bytes)
	} else if block.Type == "PRIVATE KEY" {
		var key interface{}
		key, err = x509.ParsePKCS8PrivateKey(block.Bytes)
		if err == nil {
			var ok bool
			priv, ok = key.(*rsa.PrivateKey)
			if !ok {
				err = errors.New("not an RSA private key")
			}
		}
	} else {
		return nil, errors.New("unsupported PEM block type: " + block.Type)
	}

	if err != nil {
		return nil, err
	}

	if priv.N.BitLen() != 2048 {
		return nil, errors.New("RSA private key must be 2048 bits")
	}

	return priv, nil
}

// GenerateRSAKeyPair2048 generates a new 2048-bit RSA keypair and returns both in PEM encoding.
func GenerateRSAKeyPair2048() (privateKeyPEM string, publicKeyPEM string, err error) {
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", "", err
	}

	privBytes := x509.MarshalPKCS1PrivateKey(privKey)
	privPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: privBytes,
	})

	pubPEM, err := ExportRSAPublicKeyPEM(privKey)
	if err != nil {
		return "", "", err
	}

	return string(privPEM), pubPEM, nil
}

// ExportRSAPublicKeyPEM converts an RSA public key to PKIX PEM format ("PUBLIC KEY").
func ExportRSAPublicKeyPEM(privKey *rsa.PrivateKey) (string, error) {
	if privKey == nil {
		return "", errors.New("nil private key")
	}
	pubBytes, err := x509.MarshalPKIXPublicKey(&privKey.PublicKey)
	if err != nil {
		return "", err
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: pubBytes,
	})
	return string(pubPEM), nil
}

// ExportRSAPublicKeyPEMFromPrivatePEM parses a PEM private key and exports its public key in PKIX PEM format.
func ExportRSAPublicKeyPEMFromPrivatePEM(privKeyPEM string) (string, error) {
	privKey, err := ParseRSAPrivateKeyFromPEM([]byte(privKeyPEM))
	if err != nil {
		return "", err
	}
	return ExportRSAPublicKeyPEM(privKey)
}
