package handler

import (
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	"github.com/pablojhp.pergo/internal/channel"
	"github.com/pablojhp.pergo/internal/channel/whatsapp"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/inbound"
	"github.com/pablojhp.pergo/internal/media"
	"github.com/pablojhp.pergo/internal/platform/crypto"
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

	type wabaRawPayload struct {
		Entry []struct {
			Changes []struct {
				Value struct {
					Messages []struct {
						ID          string `json:"id"`
						Interactive *struct {
							Type     string `json:"type"`
							NFMReply *struct {
								ResponseJSON string `json:"response_json"`
							} `json:"nfm_reply"`
						} `json:"interactive"`
					} `json:"messages"`
				} `json:"value"`
			} `json:"changes"`
		} `json:"entry"`
	}

	type flowResponseData struct {
		FlowToken         string                 `json:"flow_token"`
		EncryptedFlowData string                 `json:"encrypted_flow_data"`
		EncryptedAesKey   string                 `json:"encrypted_aes_key"`
		InitialVector     string                 `json:"initial_vector"`
		Screen            string                 `json:"screen"`
		Data              map[string]interface{} `json:"data"`
	}

	var rawPayload wabaRawPayload
	_ = json.Unmarshal(body, &rawPayload)
	nfmMap := make(map[string]flowResponseData)
	for _, entry := range rawPayload.Entry {
		for _, change := range entry.Changes {
			for _, msg := range change.Value.Messages {
				if msg.Interactive != nil && msg.Interactive.Type == "nfm_reply" && msg.Interactive.NFMReply != nil {
					var flowData flowResponseData
					if err := json.Unmarshal([]byte(msg.Interactive.NFMReply.ResponseJSON), &flowData); err == nil {
						nfmMap[msg.ID] = flowData
					}
				}
			}
		}
	}

	var privKey *rsa.PrivateKey
	if len(nfmMap) > 0 {
		privKey, _ = crypto.LoadRSAPrivateKey(matchingConn.Credentials, nil)
	}

	ctx := c.Request().Context()
	for _, event := range events {
		if event.Metadata != nil && event.Metadata["type"] == "order" {
			if h.inboundProcessor != nil && h.inboundProcessor.DedupRepo() != nil {
				unique, err := h.inboundProcessor.DedupRepo().InsertAndCheck(ctx, event.WorkspaceID, "whatsapp_cloud", event.MessageID)
				if err != nil {
					slog.Error("waba webhook: order dedup check failed", "error", err, "message_id", event.MessageID)
				} else if !unique {
					slog.Info("waba webhook: duplicate order message ignored", "message_id", event.MessageID)
					continue
				} else {
					event.Metadata["deduplicated"] = "true"
				}
			}

			if orderJSON, ok := event.Metadata["order_json"]; ok && orderJSON != "" {
				var orderEv domain.OrderCreatedEvent
				if err := json.Unmarshal([]byte(orderJSON), &orderEv); err == nil {
					if h.inboundProcessor != nil {
						_ = h.inboundProcessor.PublishOrderCreated(ctx, event.WorkspaceID, &orderEv)
					}
				} else {
					slog.Error("waba webhook: failed to unmarshal order_json", "error", err, "message_id", event.MessageID)
				}
			}
		}

		if flowData, ok := nfmMap[event.MessageID]; ok {
			var screen string
			var formData map[string]interface{}

			if flowData.EncryptedFlowData != "" && privKey != nil {
				aesKeyCipher, _ := base64.StdEncoding.DecodeString(flowData.EncryptedAesKey)
				aesKey, err := crypto.DecryptRSA(privKey, aesKeyCipher)
				if err == nil {
					flowCipher, _ := base64.StdEncoding.DecodeString(flowData.EncryptedFlowData)
					iv, _ := base64.StdEncoding.DecodeString(flowData.InitialVector)
					tagSize := 16
					if len(flowCipher) > tagSize {
						ciphertext := flowCipher[:len(flowCipher)-tagSize]
						tag := flowCipher[len(flowCipher)-tagSize:]
						plaintext, err := crypto.DecryptAES128GCM(aesKey, iv, ciphertext, tag)
						if err == nil {
							var dec map[string]interface{}
							if json.Unmarshal(plaintext, &dec) == nil {
								if s, ok := dec["screen"].(string); ok {
									screen = s
								}
								if d, ok := dec["data"].(map[string]interface{}); ok {
									formData = d
								}
							}
						}
					}
				}
			} else {
				screen = flowData.Screen
				formData = flowData.Data
			}

			var summaryBuilder strings.Builder
			summaryBuilder.WriteString("📄 *Form Submitted*\n")
			if screen != "" {
				summaryBuilder.WriteString(fmt.Sprintf("Screen: %s\n", screen))
			}
			if formData != nil {
				for k, v := range formData {
					summaryBuilder.WriteString(fmt.Sprintf("- %s: %v\n", k, v))
				}
			}
			event.Body = strings.TrimSpace(summaryBuilder.String())

			if h.inboundProcessor != nil {
				flowEv := &domain.FlowCompletedEvent{
					Screen:    screen,
					Data:      formData,
					FlowToken: flowData.FlowToken,
					ContactID: event.From,
					Wamid:     event.MessageID,
				}
				_ = h.inboundProcessor.PublishFlowCompleted(ctx, event.WorkspaceID, flowEv)
			}
		}

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
