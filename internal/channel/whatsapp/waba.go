package whatsapp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/google/uuid"
	"github.com/pablojhp.omnigo/internal/channel"
	"github.com/pablojhp.omnigo/internal/platform/postgres/tenant"
	"github.com/pablojhp.omnigo/internal/repository"
)

// WindowChecker defines the interface for checking if a customer service window is open.
type WindowChecker interface {
	IsWindowOpen(ctx context.Context, workspaceID uuid.UUID, recipientPhone string, channel string) (bool, error)
}

// WABAAdapter implements channel.Dispatcher for WhatsApp Cloud (WABA) REST API.
type WABAAdapter struct {
	credentialsRepo *repository.CredentialsRepository
	client          *http.Client
	baseURL         string
	windowChecker   WindowChecker
}

// WABAConfig represents the WABA credentials JSON payload.
type WABAConfig struct {
	PhoneNumberID string `json:"phone_number_id"`
	Token         string `json:"token"`
	WABAAccountID string `json:"waba_account_id"`
}


type wabaMessageRequest struct {
	MessagingProduct string        `json:"messaging_product"`
	RecipientType    string        `json:"recipient_type"`
	To               string        `json:"to"`
	Type             string        `json:"type"`
	Text             *wabaText     `json:"text,omitempty"`
	Template         *wabaTemplate `json:"template,omitempty"`
}

type wabaText struct {
	PreviewURL bool   `json:"preview_url"`
	Body       string `json:"body"`
}

type wabaTemplate struct {
	Name       string         `json:"name"`
	Language   wabaLanguage   `json:"language"`
	Components []wabaComponent `json:"components,omitempty"`
}

type wabaLanguage struct {
	Code string `json:"code"`
}

type wabaComponent struct {
	Type       string          `json:"type"`
	Parameters []wabaParameter `json:"parameters"`
}

type wabaParameter struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// MetaErrorResponse is the structure returned by Meta on API errors.
type MetaErrorResponse struct {
	Error struct {
		Message      string `json:"message"`
		Type         string `json:"type"`
		Code         int    `json:"code"`
		ErrorSubcode int    `json:"error_subcode"`
		FBTraceID    string `json:"fbtrace_id"`
	} `json:"error"`
}

// NewWABAAdapter creates a new WABAAdapter.
func NewWABAAdapter(credentialsRepo *repository.CredentialsRepository, client *http.Client, windowChecker WindowChecker) *WABAAdapter {
	if client == nil {
		client = http.DefaultClient
	}
	return &WABAAdapter{
		credentialsRepo: credentialsRepo,
		client:          client,
		baseURL:         "https://graph.facebook.com/v18.0",
		windowChecker:   windowChecker,
	}
}

// SetBaseURL overrides the base API URL (useful for testing).
func (a *WABAAdapter) SetBaseURL(url string) {
	a.baseURL = url
}

// Dispatch sends a message through the WhatsApp Cloud REST API.
func (a *WABAAdapter) Dispatch(ctx context.Context, m *channel.MessagePayload) error {
	workspaceID, err := tenant.RequireWorkspaceID(ctx)
	if err != nil {
		return channel.NewTerminalError(err)
	}

	credsBytes, err := a.credentialsRepo.Get(ctx, workspaceID, "whatsapp_cloud")
	if err != nil {
		if errors.Is(err, repository.ErrCredentialsNotFound) {
			return channel.NewTerminalError(fmt.Errorf("credentials not found: %w", err))
		}
		return err
	}

	var config WABAConfig
	if err := json.Unmarshal(credsBytes, &config); err != nil {
		return channel.NewTerminalError(fmt.Errorf("invalid credentials format: %w", err))
	}

	if config.PhoneNumberID == "" || config.Token == "" {
		return channel.NewTerminalError(errors.New("missing phone_number_id or token in credentials"))
	}

	reqPayload := wabaMessageRequest{
		MessagingProduct: "whatsapp",
		RecipientType:    "individual",
		To:               m.To,
	}

	templateName := m.TemplateName
	if templateName == "" && m.Metadata != nil {
		templateName = m.Metadata["template_name"]
	}

	if templateName == "" {
		if a.windowChecker != nil {
			open, err := a.windowChecker.IsWindowOpen(ctx, workspaceID, m.To, "whatsapp_cloud")
			if err != nil {
				return err
			}
			if !open {
				return channel.NewTerminalError(errors.New("customer service window expired"))
			}
		}
	}

	if templateName != "" {
		langCode := m.Language
		if langCode == "" && m.Metadata != nil {
			langCode = m.Metadata["template_language"]
		}
		if langCode == "" {
			langCode = "en_US"
		}
		tmpl := wabaTemplate{
			Name: templateName,
			Language: wabaLanguage{
				Code: langCode,
			},
		}

		if len(m.Components) > 0 {
			tmpl.Components = make([]wabaComponent, len(m.Components))
			for i, comp := range m.Components {
				tmpl.Components[i] = wabaComponent{
					Type: comp.Type,
				}
				if len(comp.Parameters) > 0 {
					tmpl.Components[i].Parameters = make([]wabaParameter, len(comp.Parameters))
					for j, param := range comp.Parameters {
						tmpl.Components[i].Parameters[j] = wabaParameter{
							Type: param.Type,
							Text: param.Text,
						}
					}
				}
			}
		} else if m.Metadata != nil {
			var params []wabaParameter
			for i := 1; ; i++ {
				paramKey := fmt.Sprintf("param%d", i)
				val, ok := m.Metadata[paramKey]
				if !ok {
					break
				}
				params = append(params, wabaParameter{
					Type: "text",
					Text: val,
				})
			}

			if len(params) > 0 {
				tmpl.Components = []wabaComponent{
					{
						Type:       "body",
						Parameters: params,
					},
				}
			}
		}

		reqPayload.Type = "template"
		reqPayload.Template = &tmpl
	} else {
		reqPayload.Type = "text"
		reqPayload.Text = &wabaText{
			PreviewURL: false,
			Body:       m.Body,
		}
	}


	bodyBytes, err := json.Marshal(reqPayload)
	if err != nil {
		return channel.NewTerminalError(fmt.Errorf("marshal request: %w", err))
	}

	url := fmt.Sprintf("%s/%s/messages", a.baseURL, config.PhoneNumberID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return channel.NewTerminalError(fmt.Errorf("create HTTP request: %w", err))
	}

	req.Header.Set("Authorization", "Bearer "+config.Token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	respBytes, _ := io.ReadAll(resp.Body)
	var errorResp MetaErrorResponse
	if err := json.Unmarshal(respBytes, &errorResp); err != nil {
		if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != http.StatusTooManyRequests {
			return channel.NewTerminalError(fmt.Errorf("HTTP error %d: %s", resp.StatusCode, string(respBytes)))
		}
		return fmt.Errorf("HTTP error %d: %s", resp.StatusCode, string(respBytes))
	}

	return a.classifyError(resp.StatusCode, &errorResp)
}

func (a *WABAAdapter) classifyError(statusCode int, errResp *MetaErrorResponse) error {
	metaErr := errResp.Error
	err := fmt.Errorf("Meta API error (code: %d, subcode: %d): %s", metaErr.Code, metaErr.ErrorSubcode, metaErr.Message)

	if metaErr.Code == 131030 ||
		metaErr.Code == 131047 ||
		metaErr.Code == 132000 ||
		metaErr.Code == 190 ||
		(metaErr.Code == 100 && metaErr.ErrorSubcode == 33) {
		return channel.NewTerminalError(err)
	}

	if metaErr.Code == 130429 || metaErr.Code == 131052 {
		return err
	}

	if statusCode == http.StatusTooManyRequests || statusCode >= 500 {
		return err
	}
	if statusCode >= 400 && statusCode < 500 {
		return channel.NewTerminalError(err)
	}

	return err
}
