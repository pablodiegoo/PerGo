package instagram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/pablojhp.pergo/internal/channel"
	"github.com/pablojhp.pergo/internal/platform/postgres/tenant"
	"github.com/pablojhp.pergo/internal/repository"
)

// InstagramAdapter implements channel.Dispatcher for Instagram.
type InstagramAdapter struct {
	connectionsRepo *repository.ConnectionRepository
	client          *http.Client
	baseURL         string
	externalBaseURL string
}

// InstagramConfig represents the Instagram credentials JSON payload.
type InstagramConfig struct {
	InstagramAccountID string `json:"instagram_account_id"`
	Token              string `json:"token"`
	VerifyToken        string `json:"verify_token,omitempty"`
}

type instagramMessageRequest struct {
	Recipient struct {
		ID string `json:"id"`
	} `json:"recipient"`
	Message struct {
		Text        string                 `json:"text,omitempty"`
		Attachment  *instagramAttachment   `json:"attachment,omitempty"`
		Interactive *instagramInteractive  `json:"interactive,omitempty"`
	} `json:"message"`
	MessagingType string `json:"messaging_type,omitempty"`
}

type instagramAttachment struct {
	Type    string `json:"type"` // "image", "audio", "video", "file"
	Payload struct {
		URL string `json:"url"`
		IsReusable bool `json:"is_reusable,omitempty"`
	} `json:"payload"`
}

type instagramInteractive struct {
	Type   string                   `json:"type"` // "button", "list"
	Header *instagramInteractiveText `json:"header,omitempty"`
	Body   *instagramInteractiveText `json:"body,omitempty"`
	Footer *instagramInteractiveText `json:"footer,omitempty"`
	Action *instagramAction          `json:"action,omitempty"`
}

type instagramInteractiveText struct {
	Text string `json:"text"`
}

type instagramAction struct {
	Button   string                       `json:"button,omitempty"`
	Buttons  []instagramInteractiveReplyButton `json:"buttons,omitempty"`
	Sections []instagramInteractiveSection     `json:"sections,omitempty"`
}

type instagramInteractiveReplyButton struct {
	Type  string                     `json:"type"`
	Reply instagramInteractiveButtonReply `json:"reply"`
}

type instagramInteractiveButtonReply struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type instagramInteractiveSection struct {
	Title string                     `json:"title,omitempty"`
	Rows  []instagramInteractiveSectionRow `json:"rows"`
}

type instagramInteractiveSectionRow struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
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

// NewAdapter creates a new InstagramAdapter.
func NewAdapter(connectionsRepo *repository.ConnectionRepository, client *http.Client, externalBaseURL string) *InstagramAdapter {
	if client == nil {
		client = http.DefaultClient
	}
	return &InstagramAdapter{
		connectionsRepo: connectionsRepo,
		client:          client,
		baseURL:         "https://graph.facebook.com/v18.0",
		externalBaseURL: externalBaseURL,
	}
}

// Dispatch sends a message through the Instagram REST API.
func (a *InstagramAdapter) Dispatch(ctx context.Context, m *channel.MessagePayload) (string, error) {
	_, err := tenant.RequireWorkspaceID(ctx)
	if err != nil {
		return "", channel.NewTerminalError(err)
	}

	conn, err := a.connectionsRepo.GetByID(ctx, m.ConnectionID)
	if err != nil {
		if errors.Is(err, repository.ErrConnectionNotFound) {
			return "", channel.NewTerminalError(fmt.Errorf("connection credentials not found: %w", err))
		}
		return "", err
	}

	var config InstagramConfig
	if err := json.Unmarshal(conn.Credentials, &config); err != nil {
		return "", channel.NewTerminalError(fmt.Errorf("invalid credentials format: %w", err))
	}

	if config.InstagramAccountID == "" || config.Token == "" {
		return "", channel.NewTerminalError(errors.New("missing instagram_account_id or token in credentials"))
	}

	reqPayload := instagramMessageRequest{}
	reqPayload.Recipient.ID = m.To

	if m.Interactive != nil {
		reqPayload.Message.Interactive = &instagramInteractive{
			Type: m.Interactive.Type,
		}
		if m.Interactive.Header != nil {
			reqPayload.Message.Interactive.Header = &instagramInteractiveText{Text: m.Interactive.Header.Text}
		}
		reqPayload.Message.Interactive.Body = &instagramInteractiveText{Text: m.Interactive.Body.Text}
		if m.Interactive.Footer != nil {
			reqPayload.Message.Interactive.Footer = &instagramInteractiveText{Text: m.Interactive.Footer.Text}
		}
		
		action := instagramAction{
			Button: m.Interactive.Action.Button,
		}
		for _, b := range m.Interactive.Action.Buttons {
			action.Buttons = append(action.Buttons, instagramInteractiveReplyButton{
				Type: "reply",
				Reply: instagramInteractiveButtonReply{
					ID:    b.Reply.ID,
					Title: b.Reply.Title,
				},
			})
		}
		for _, s := range m.Interactive.Action.Sections {
			section := instagramInteractiveSection{Title: s.Title}
			for _, r := range s.Rows {
				section.Rows = append(section.Rows, instagramInteractiveSectionRow{
					ID:          r.ID,
					Title:       r.Title,
					Description: r.Description,
				})
			}
			action.Sections = append(action.Sections, section)
		}
		reqPayload.Message.Interactive.Action = &action
	} else if m.Media != nil {
		mediaURL := m.Media.MediaURL
		if len(mediaURL) > 0 && mediaURL[0] == '/' && a.externalBaseURL != "" {
			base := a.externalBaseURL
			if len(base) > 0 && base[len(base)-1] == '/' {
				base = base[:len(base)-1]
			}
			mediaURL = base + mediaURL
		}
		
		mediaType := m.Media.MediaType
		if mediaType == "document" {
			mediaType = "file"
		}
		
		reqPayload.Message.Attachment = &instagramAttachment{
			Type: mediaType,
		}
		reqPayload.Message.Attachment.Payload.URL = mediaURL
		
	} else {
		reqPayload.Message.Text = m.Body
	}

	bodyBytes, err := json.Marshal(reqPayload)
	if err != nil {
		return "", channel.NewTerminalError(fmt.Errorf("marshal request: %w", err))
	}

	return a.sendRequest(ctx, config.InstagramAccountID, config.Token, bodyBytes)
}

func (a *InstagramAdapter) sendRequest(ctx context.Context, accountID, token string, bodyBytes []byte) (string, error) {
	url := fmt.Sprintf("%s/%s/messages", a.baseURL, accountID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", channel.NewTerminalError(fmt.Errorf("create HTTP request: %w", err))
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		var successResp struct {
			MessageID string `json:"message_id"`
		}
		if err := json.Unmarshal(respBytes, &successResp); err == nil && successResp.MessageID != "" {
			return successResp.MessageID, nil
		}
		return string(respBytes), nil
	}

	var errorResp MetaErrorResponse
	if err := json.Unmarshal(respBytes, &errorResp); err != nil {
		if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != http.StatusTooManyRequests {
			return string(respBytes), channel.NewTerminalError(fmt.Errorf("HTTP error %d: %s", resp.StatusCode, string(respBytes)))
		}
		return string(respBytes), fmt.Errorf("HTTP error %d: %s", resp.StatusCode, string(respBytes))
	}

	return string(respBytes), a.classifyError(resp.StatusCode, &errorResp)
}

func (a *InstagramAdapter) classifyError(statusCode int, errResp *MetaErrorResponse) error {
	metaErr := errResp.Error
	err := fmt.Errorf("Meta API error (code: %d, subcode: %d): %s", metaErr.Code, metaErr.ErrorSubcode, metaErr.Message)

	if statusCode == http.StatusTooManyRequests || statusCode >= 500 {
		return err
	}
	if statusCode >= 400 && statusCode < 500 {
		return channel.NewTerminalError(err)
	}

	return err
}
