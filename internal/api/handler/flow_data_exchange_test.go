package handler

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/pablojhp.pergo/internal/platform/crypto"
)

type mockProvider struct{}
func (m *mockProvider) Encrypt(plaintext []byte) ([]byte, string, int, error) { return plaintext, "mock", 1, nil }
func (m *mockProvider) Decrypt(ciphertext []byte) ([]byte, error) { return ciphertext, nil }

func TestHandleFlowDataExchange(t *testing.T) {
	// Setup keys
	privKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	
	aesKey := make([]byte, 16)
	rand.Read(aesKey)

	iv := make([]byte, 12)
	rand.Read(iv)

	flowData := []byte(`{"action":"ping"}`)
	encFlowData, tag, _ := crypto.EncryptAES128GCM(aesKey, iv, flowData)
	encFlowDataWithTag := append(encFlowData, tag...)

	encAESKey, _ := rsa.EncryptOAEP(sha256.New(), rand.Reader, &privKey.PublicKey, aesKey, nil)

	reqPayload := FlowDataExchangeRequest{
		EncryptedFlowData: base64.StdEncoding.EncodeToString(encFlowDataWithTag),
		EncryptedAESKey:   base64.StdEncoding.EncodeToString(encAESKey),
		InitialVector:     base64.StdEncoding.EncodeToString(iv),
	}
	reqBody, _ := json.Marshal(reqPayload)

	// Setup Echo and Handler
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/waba/flows/data-exchange", bytes.NewReader(reqBody))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	_ = e.NewContext(req, rec)

	t.Skip("Skipping because of DB dependency in repository.ConnectionRepository")
}
