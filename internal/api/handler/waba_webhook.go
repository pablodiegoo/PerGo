package handler

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	"github.com/pablojhp.pergo/internal/channel"
	"github.com/pablojhp.pergo/internal/channel/whatsapp"
	"github.com/pablojhp.pergo/internal/inbound"
	"github.com/pablojhp.pergo/internal/media"
	"github.com/pablojhp.pergo/internal/repository"
)

const (
	// MaxWABAPayloadSize limits WABA webhook payloads to 2MiB.
	MaxWABAPayloadSize = 2 * 1024 * 1024
)

// ErrPayloadTooLarge is returned when the request body exceeds MaxWABAPayloadSize.
var ErrPayloadTooLarge = errors.New("waba webhook: request body exceeds 2MiB limit")

// ReadLimitedBody reads up to limit bytes from r. If the body exceeds limit, returns ErrPayloadTooLarge.
func ReadLimitedBody(r io.Reader, limit int64) ([]byte, error) {
	if r == nil {
		return []byte{}, nil
	}
	lr := io.LimitReader(r, limit+1)
	body, err := io.ReadAll(lr)
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, ErrPayloadTooLarge
	}
	return body, nil
}

// VerifyWABASignature verifies the X-Hub-Signature-256 header against the payload body and app secret using HMAC-SHA256 and hmac.Equal.
func VerifyWABASignature(body []byte, secret string, signatureHeader string) bool {
	if secret == "" || signatureHeader == "" {
		return false
	}
	sigHex := strings.TrimPrefix(signatureHeader, "sha256=")
	sigBytes, err := hex.DecodeString(sigHex)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expectedMAC := mac.Sum(nil)
	return hmac.Equal(expectedMAC, sigBytes)
}

// WABAWebhookHandler handles verification and inbound payloads for Meta's WhatsApp Cloud API (WABA).
type WABAWebhookHandler struct {
	connectionsRepo  *repository.ConnectionRepository
	templatesRepo    *repository.WABATemplateRepository
	inboundProcessor *inbound.InboundProcessor
	adapter          channel.InboundAdapter
	verifySignature  bool
}

func NewWABAWebhookHandler(
	connectionsRepo *repository.ConnectionRepository,
	inboundProcessor *inbound.InboundProcessor,
	mediaEngine media.Engine,
) *WABAWebhookHandler {
	verify := true
	if val := strings.ToLower(strings.TrimSpace(os.Getenv("WABA_WEBHOOK_VERIFY_SIGNATURE"))); val == "false" || val == "0" || val == "off" || val == "no" {
		verify = false
	}
	return &WABAWebhookHandler{
		connectionsRepo:  connectionsRepo,
		inboundProcessor: inboundProcessor,
		adapter:          whatsapp.NewWABAInboundAdapter(mediaEngine),
		verifySignature:  verify,
	}
}

// SetVerifySignature enables or disables X-Hub-Signature-256 signature verification.
func (h *WABAWebhookHandler) SetVerifySignature(verify bool) {
	h.verifySignature = verify
}

// SetTemplatesRepo injects the WABATemplateRepository for template status updates.
func (h *WABAWebhookHandler) SetTemplatesRepo(repo *repository.WABATemplateRepository) {
	h.templatesRepo = repo
}

// SetBaseURL overrides the base Meta Graph API URL (useful for testing).
func (h *WABAWebhookHandler) SetBaseURL(url string) {
	if wa, ok := h.adapter.(*whatsapp.WABAInboundAdapter); ok {
		wa.SetBaseURL(url)
	}
}

type wabaVerifyCreds struct {
	VerifyToken string `json:"verify_token"`
}

// HandleGet verification from Meta
func (h *WABAWebhookHandler) HandleGet(c *echo.Context) error {
	workspaceIDStr, err := echo.PathParam[string](c, "workspace_id")
	if err != nil || workspaceIDStr == "" {
		return c.NoContent(http.StatusBadRequest)
	}
	workspaceID, err := uuid.Parse(workspaceIDStr)
	if err != nil {
		return c.NoContent(http.StatusBadRequest)
	}

	verifyToken := c.Request().URL.Query().Get("hub.verify_token")
	challenge := c.Request().URL.Query().Get("hub.challenge")

	expectedVerifyToken := "pergo_verify_token_" + workspaceIDStr

	if verifyToken != "" && (verifyToken == "pergo-verify-token" || verifyToken == expectedVerifyToken) {
		return c.String(http.StatusOK, challenge)
	}

	// Load registered connections for the workspace
	conns, err := h.connectionsRepo.ListByWorkspace(c.Request().Context(), workspaceID)
	if err == nil {
		for _, conn := range conns {
			if conn.Channel != "whatsapp_cloud" {
				continue
			}

			var creds wabaVerifyCreds
			if err := json.Unmarshal(conn.Credentials, &creds); err == nil {
				if verifyToken != "" && (verifyToken == creds.VerifyToken || verifyToken == expectedVerifyToken) {
					return c.String(http.StatusOK, challenge)
				}
			}
		}
	}

	return c.NoContent(http.StatusForbidden)
}

type wabaTemplateChangePayload struct {
	Entry []struct {
		ID      string `json:"id"`
		Changes []struct {
			Field string `json:"field"`
			Value struct {
				Event                   string `json:"event"`
				MessageTemplateID       string `json:"message_template_id"`
				MessageTemplateName     string `json:"message_template_name"`
				MessageTemplateLanguage string `json:"message_template_language"`
				Reason                  string `json:"reason"`
				NewCategory             string `json:"new_category"`
				NewQualityScore         string `json:"new_quality_score"`
			} `json:"value"`
		} `json:"changes"`
	} `json:"entry"`
}

// HandlePost ingests inbound messages and template status updates from Meta
func (h *WABAWebhookHandler) HandlePost(c *echo.Context) error {
	workspaceIDStr, err := echo.PathParam[string](c, "workspace_id")
	if err != nil || workspaceIDStr == "" {
		return c.NoContent(http.StatusBadRequest)
	}
	workspaceID, err := uuid.Parse(workspaceIDStr)
	if err != nil {
		return c.NoContent(http.StatusBadRequest)
	}

	// Load registered connections for the workspace
	conns, err := h.connectionsRepo.ListByWorkspace(c.Request().Context(), workspaceID)
	if err != nil {
		return c.NoContent(http.StatusForbidden)
	}

	// Read raw request body with 2MiB limit
	body, err := ReadLimitedBody(c.Request().Body, MaxWABAPayloadSize)
	if err != nil {
		if errors.Is(err, ErrPayloadTooLarge) {
			slog.Warn("waba webhook: body exceeds 2MiB limit", "workspace_id", workspaceID)
			return c.NoContent(http.StatusRequestEntityTooLarge)
		}
		return c.NoContent(http.StatusBadRequest)
	}

	var incomingPhoneID, incomingDisplayPhone string
	var wabaMetaExtract struct {
		Entry []struct {
			Changes []struct {
				Value struct {
					Metadata struct {
						DisplayPhoneNumber string `json:"display_phone_number"`
						PhoneNumberID      string `json:"phone_number_id"`
					} `json:"metadata"`
				} `json:"value"`
			} `json:"changes"`
		} `json:"entry"`
	}
	if json.Unmarshal(body, &wabaMetaExtract) == nil && len(wabaMetaExtract.Entry) > 0 {
		for _, entry := range wabaMetaExtract.Entry {
			for _, change := range entry.Changes {
				if change.Value.Metadata.PhoneNumberID != "" {
					incomingPhoneID = change.Value.Metadata.PhoneNumberID
				}
				if change.Value.Metadata.DisplayPhoneNumber != "" {
					incomingDisplayPhone = change.Value.Metadata.DisplayPhoneNumber
				}
			}
		}
	}

	var matchingConn *repository.Connection
	for _, conn := range conns {
		if conn.Channel != "whatsapp_cloud" {
			continue
		}
		if incomingPhoneID != "" || incomingDisplayPhone != "" {
			var creds struct {
				PhoneNumberID string `json:"phone_number_id"`
			}
			_ = json.Unmarshal(conn.Credentials, &creds)
			if (incomingPhoneID != "" && (conn.SenderIdentity == incomingPhoneID || creds.PhoneNumberID == incomingPhoneID)) ||
				(incomingDisplayPhone != "" && (conn.SenderIdentity == incomingDisplayPhone || strings.TrimPrefix(conn.SenderIdentity, "+") == strings.TrimPrefix(incomingDisplayPhone, "+"))) {
				matchingConn = conn
				break
			}
		}
		if matchingConn == nil {
			matchingConn = conn
		}
	}

	if matchingConn == nil {
		slog.Warn("waba webhook: no connection found", "workspace_id", workspaceID)
		return c.NoContent(http.StatusForbidden)
	}

	// Verify X-Hub-Signature-256 signature if enabled
	if h.verifySignature {
		sigHeader := c.Request().Header.Get("X-Hub-Signature-256")
		var creds struct {
			AppSecret   string `json:"app_secret"`
			VerifyToken string `json:"verify_token"`
			Token       string `json:"token"`
		}
		_ = json.Unmarshal(matchingConn.Credentials, &creds)

		secret := creds.AppSecret
		if secret == "" {
			secret = creds.VerifyToken
		}
		if secret == "" {
			secret = creds.Token
		}

		if sigHeader == "" || secret == "" || !VerifyWABASignature(body, secret, sigHeader) {
			slog.Warn("waba webhook: signature verification failed", "workspace_id", workspaceID)
			return c.NoContent(http.StatusUnauthorized)
		}
	}

	// Check for template status/quality update events
	if h.templatesRepo != nil {
		var tPayload wabaTemplateChangePayload
		if err := json.Unmarshal(body, &tPayload); err == nil {
			for _, entry := range tPayload.Entry {
				for _, change := range entry.Changes {
					if change.Field == "message_template_status_update" || change.Field == "message_template_quality_update" {
						val := change.Value
						if val.MessageTemplateName != "" && val.MessageTemplateLanguage != "" {
							tmpl, err := h.templatesRepo.GetByNameAndLanguage(c.Request().Context(), matchingConn.ID, val.MessageTemplateName, val.MessageTemplateLanguage)
							if err == nil && tmpl != nil {
								newStatus := tmpl.Status
								if val.Event != "" {
									newStatus = val.Event
								}
								reasonPtr := tmpl.RejectionReason
								if val.Reason != "" {
									rStr := val.Reason
									reasonPtr = &rStr
								}
								qualityPtr := tmpl.QualityScore
								if val.NewQualityScore != "" {
									oldQual := ""
									if tmpl.QualityScore != nil {
										oldQual = *tmpl.QualityScore
									}
									newQual := val.NewQualityScore
									if (oldQual == "GREEN" && (newQual == "YELLOW" || newQual == "RED")) || (oldQual == "YELLOW" && newQual == "RED") {
										slog.Warn("WABA template quality score drop detected",
											"template_id", tmpl.ID,
											"template_name", tmpl.Name,
											"old_quality", oldQual,
											"new_quality", newQual,
											"workspace_id", workspaceID,
										)
									}
									qualityPtr = &newQual
								}

								err := h.templatesRepo.UpdateStatus(c.Request().Context(), tmpl.ID, newStatus, reasonPtr, qualityPtr)
								if err != nil {
									slog.Error("failed to update template status from webhook", "error", err, "template_id", tmpl.ID)
								} else {
									slog.Info("updated template status from webhook", "template_name", tmpl.Name, "status", newStatus)
								}
							}
						}
					}
				}
			}
		}
	}

	events, err := h.adapter.Parse(c.Request().Context(), body, nil, matchingConn)
	if err != nil {
		slog.Warn("waba webhook: adapter failed to parse", "error", err)
		return c.NoContent(http.StatusForbidden)
	}

	ctx := c.Request().Context()
	for _, event := range events {
		if h.inboundProcessor != nil {
			err := h.inboundProcessor.Process(ctx, event)
			if err != nil {
				slog.Error("waba webhook: inbound processor failed", "error", err, "message_id", event.MessageID)
				return c.NoContent(http.StatusInternalServerError)
			}
		}
	}

	return c.NoContent(http.StatusOK)
}
