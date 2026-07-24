package inbound

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"

	"github.com/labstack/echo/v5"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/repository"
)

// Transparent 1x1 GIF bytes
var transparentGIF = []byte{
	0x47, 0x49, 0x46, 0x38, 0x39, 0x61, 0x01, 0x00, 0x01, 0x00,
	0x80, 0x00, 0x00, 0xff, 0xff, 0xff, 0x00, 0x00, 0x00, 0x21,
	0xf9, 0x04, 0x01, 0x00, 0x00, 0x00, 0x00, 0x2c, 0x00, 0x00,
	0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00, 0x02, 0x02, 0x44,
	0x01, 0x00, 0x3b,
}

// EmailTrackingHandler manages email open/click tracking and webhook event normalization.
type EmailTrackingHandler struct {
	secretKey    string
	dispatchRepo *repository.MessageDispatchRepository
}

// NewEmailTrackingHandler creates a new EmailTrackingHandler.
func NewEmailTrackingHandler(secret string, repo *repository.MessageDispatchRepository) *EmailTrackingHandler {
	return &EmailTrackingHandler{
		secretKey:    secret,
		dispatchRepo: repo,
	}
}

func generateTrackingHMAC(messageID, payload, secret string) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(messageID + ":" + payload))
	return hex.EncodeToString(h.Sum(nil))
}

func verifyTrackingHMAC(messageID, payload, sig, secret string) bool {
	expected := generateTrackingHMAC(messageID, payload, secret)
	return hmac.Equal([]byte(expected), []byte(sig))
}

// HandleOpen processes the 1x1 GIF open tracking request.
func (h *EmailTrackingHandler) HandleOpen(c *echo.Context) error {
	msgID := c.QueryParam("m")
	sig := c.QueryParam("sig")

	if msgID != "" && sig != "" {
		if verifyTrackingHMAC(msgID, "open", sig, h.secretKey) {
			if h.dispatchRepo != nil {
				_ = h.dispatchRepo.UpdateStatusByProviderMessageID(c.Request().Context(), msgID, string(domain.StatusRead))
			}
		}
	}

	c.Response().Header().Set("Content-Type", "image/gif")
	c.Response().Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	return c.Blob(http.StatusOK, "image/gif", transparentGIF)
}

// HandleClick processes click tracking requests and redirects to target URL.
func (h *EmailTrackingHandler) HandleClick(c *echo.Context) error {
	msgID := c.QueryParam("m")
	targetURL := c.QueryParam("url")
	sig := c.QueryParam("sig")

	if msgID != "" && targetURL != "" && sig != "" {
		if verifyTrackingHMAC(msgID, targetURL, sig, h.secretKey) {
			if h.dispatchRepo != nil {
				_ = h.dispatchRepo.UpdateStatusByProviderMessageID(c.Request().Context(), msgID, string(domain.StatusRead))
			}
			return c.Redirect(http.StatusFound, targetURL)
		}
	}

	if targetURL != "" {
		return c.Redirect(http.StatusFound, targetURL)
	}

	return c.NoContent(http.StatusBadRequest)
}

// SESNotificationPayload matches AWS SNS notification envelope for SES.
type SESNotificationPayload struct {
	Type    string `json:"Type"`
	Message string `json:"Message"`
}

type SESEventDetail struct {
	EventType string `json:"eventType"`
	Mail      struct {
		MessageID string `json:"messageId"`
	} `json:"mail"`
}

// HandleSESWebhook processes Amazon SES SNS webhook events (Delivery, Bounce, Complaint).
func (h *EmailTrackingHandler) HandleSESWebhook(c *echo.Context) error {
	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return c.NoContent(http.StatusBadRequest)
	}

	var snsPayload SESNotificationPayload
	if err := json.Unmarshal(body, &snsPayload); err != nil {
		return c.NoContent(http.StatusBadRequest)
	}

	var event SESEventDetail
	if snsPayload.Message != "" {
		_ = json.Unmarshal([]byte(snsPayload.Message), &event)
	} else {
		_ = json.Unmarshal(body, &event)
	}

	if event.Mail.MessageID != "" && h.dispatchRepo != nil {
		switch event.EventType {
		case "Delivery":
			_ = h.dispatchRepo.UpdateStatusByProviderMessageID(c.Request().Context(), event.Mail.MessageID, string(domain.StatusDelivered))
		case "Bounce", "Reject":
			_ = h.dispatchRepo.UpdateStatusByProviderMessageID(c.Request().Context(), event.Mail.MessageID, string(domain.StatusFailed))
		case "Open":
			_ = h.dispatchRepo.UpdateStatusByProviderMessageID(c.Request().Context(), event.Mail.MessageID, string(domain.StatusRead))
		}
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}
