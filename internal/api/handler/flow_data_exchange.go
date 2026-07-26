package handler

import (
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	"github.com/pablojhp.pergo/internal/platform/crypto"
	"github.com/pablojhp.pergo/internal/repository"
)

type FlowDataExchangeHandler struct {
	connectionsRepo *repository.ConnectionRepository
}

func NewFlowDataExchangeHandler(connectionsRepo *repository.ConnectionRepository) *FlowDataExchangeHandler {
	return &FlowDataExchangeHandler{
		connectionsRepo: connectionsRepo,
	}
}

type FlowDataExchangeRequest struct {
	EncryptedFlowData string `json:"encrypted_flow_data"`
	EncryptedAESKey   string `json:"encrypted_aes_key"`
	InitialVector     string `json:"initial_vector"`
}

func (h *FlowDataExchangeHandler) HandleFlowDataExchange(c *echo.Context) error {
	connectionIDStr := c.QueryParam("connection_id")
	if connectionIDStr == "" {
		return c.String(http.StatusBadRequest, "missing connection_id")
	}

	connectionID, err := uuid.Parse(connectionIDStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid connection_id")
	}

	conn, err := h.connectionsRepo.GetByID(c.Request().Context(), connectionID)
	if err != nil {
		return c.String(http.StatusNotFound, "connection not found")
	}

	privKey, err := crypto.LoadRSAPrivateKey(conn.Credentials, nil)
	if err != nil {
		slog.Error("failed to load RSA private key", "error", err, "connection_id", connectionID)
		return c.String(http.StatusInternalServerError, "internal error")
	}

	var req FlowDataExchangeRequest
	if err := json.NewDecoder(c.Request().Body).Decode(&req); err != nil {
		return c.String(http.StatusBadRequest, "invalid json")
	}

	encAESKey, err := base64.StdEncoding.DecodeString(req.EncryptedAESKey)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid encrypted_aes_key")
	}

	iv, err := base64.StdEncoding.DecodeString(req.InitialVector)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid initial_vector")
	}

	encFlowData, err := base64.StdEncoding.DecodeString(req.EncryptedFlowData)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid encrypted_flow_data")
	}

	// 4. Decrypt AES key
	aesKey, err := crypto.DecryptRSA(privKey, encAESKey)
	if err != nil {
		slog.Error("failed to decrypt AES key", "error", err)
		return c.String(http.StatusBadRequest, "decryption failed")
	}

	// 5. Decrypt flow data
	if len(encFlowData) < 16 {
		return c.String(http.StatusBadRequest, "flow data too short")
	}
	tagSize := 16
	ciphertext := encFlowData[:len(encFlowData)-tagSize]
	tag := encFlowData[len(encFlowData)-tagSize:]

	_, err = crypto.DecryptAES128GCM(aesKey, iv, ciphertext, tag)
	if err != nil {
		slog.Error("failed to decrypt flow data", "error", err)
		return c.String(http.StatusBadRequest, "decryption failed")
	}

	// 6. Process the flow data (pluggable handler interface for dynamic screen content).
	respPayload := []byte(`{"screen": "SUCCESS", "data": {}}`)

	// 7. Invert the IV via crypto.InvertIV.
	invIV := crypto.InvertIV(iv)

	// 8. Encrypt the JSON response payload via crypto.EncryptAES128GCM.
	respCiphertext, respTag, err := crypto.EncryptAES128GCM(aesKey, invIV, respPayload)
	if err != nil {
		slog.Error("failed to encrypt response", "error", err)
		return c.String(http.StatusInternalServerError, "encryption failed")
	}

	// 9. Return the base64-encoded encrypted response to Meta.
	finalEnc := append(respCiphertext, respTag...)
	encodedResp := base64.StdEncoding.EncodeToString(finalEnc)

	return c.String(http.StatusOK, encodedResp)
}
