package crypto

import (
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

	block, _ := pem.Decode([]byte(pemString))
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
