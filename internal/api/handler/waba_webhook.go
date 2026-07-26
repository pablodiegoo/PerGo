package handler

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	"github.com/pablojhp.pergo/internal/channel"
	"github.com/pablojhp.pergo/internal/channel/whatsapp"
	"github.com/pablojhp.pergo/internal/inbound"
	"github.com/pablojhp.pergo/internal/media"
	"github.com/pablojhp.pergo/internal/repository"
)

// WABAWebhookHandler handles verification and inbound payloads for Meta's WhatsApp Cloud API (WABA).
type WABAWebhookHandler struct {
	connectionsRepo  *repository.ConnectionRepository
	templatesRepo    *repository.WABATemplateRepository
	inboundProcessor *inbound.InboundProcessor
	adapter          channel.InboundAdapter
}

func NewWABAWebhookHandler(
	connectionsRepo *repository.ConnectionRepository,
	inboundProcessor *inbound.InboundProcessor,
	mediaEngine media.Engine,
) *WABAWebhookHandler {
	return &WABAWebhookHandler{
		connectionsRepo:  connectionsRepo,
		inboundProcessor: inboundProcessor,
		adapter:          whatsapp.NewWABAInboundAdapter(mediaEngine),
	}
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

	// Load registered connections for the workspace
	conns, err := h.connectionsRepo.ListByWorkspace(c.Request().Context(), workspaceID)
	if err != nil {
		return c.NoContent(http.StatusForbidden)
	}

	var matchFound bool
	for _, conn := range conns {
		if conn.Channel != "whatsapp_cloud" {
			continue
		}

		var creds wabaVerifyCreds
		if err := json.Unmarshal(conn.Credentials, &creds); err == nil {
			if verifyToken != "" && (verifyToken == creds.VerifyToken || verifyToken == expectedVerifyToken) {
				matchFound = true
				break
			}
		}
	}

	if !matchFound {
		return c.NoContent(http.StatusForbidden)
	}

	return c.String(http.StatusOK, challenge)
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

	var matchingConn *repository.Connection
	for _, conn := range conns {
		if conn.Channel == "whatsapp_cloud" {
			matchingConn = conn
			break
		}
	}

	if matchingConn == nil {
		slog.Warn("waba webhook: no connection found", "workspace_id", workspaceID)
		return c.NoContent(http.StatusForbidden)
	}

	// Read raw request body
	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return c.NoContent(http.StatusBadRequest)
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
