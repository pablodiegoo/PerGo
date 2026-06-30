package whatsapp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"

	"github.com/pablojhp.pergo/internal/channel"
	"github.com/pablojhp.pergo/internal/platform/storage"
)

// minStagger and maxStagger define the random delay range for
// ban-risk mitigation on WhatsApp Web sends.
const (
	minStagger = 1 * time.Second
	maxStagger = 3 * time.Second
)

// SessionFinder defines the lookup interface for active WhatsApp client sessions.
type SessionFinder interface {
	GetClient(jid string) *WhatsAppClient
}

// WhatsAppAdapter implements channel.Dispatcher for WhatsApp Web via whatsmeow.
// It adds staggered dispatch (1-3s random delay) before each send to
// minimize account suspension risk.
type WhatsAppAdapter struct {
	client        *WhatsAppClient
	sessionFinder SessionFinder
	log           *slog.Logger
	s3Client      *storage.S3Client
}

// NewWhatsAppAdapter creates a dispatcher backed by the given WhatsApp client.
func NewWhatsAppAdapter(client *WhatsAppClient, s3Client *storage.S3Client) *WhatsAppAdapter {
	return &WhatsAppAdapter{
		client:   client,
		log:      slog.With("component", "whatsapp-adapter"),
		s3Client: s3Client,
	}
}

// SetSessionFinder sets the session finder for dynamic client routing.
func (a *WhatsAppAdapter) SetSessionFinder(finder SessionFinder) {
	a.sessionFinder = finder
}

// Dispatch sends a text or media message via WhatsApp Web with staggered delay.
// The recipient in payload.To should be a phone number (digits only or
// with country code). It's converted to a JID with @s.whatsapp.net suffix.
//
// Returns channel.TerminalError for 403/logged-out errors (non-retryable).
// Returns regular error for transient failures (retryable).
func (a *WhatsAppAdapter) Dispatch(ctx context.Context, m *channel.MessagePayload) (string, error) {
	var wc *WhatsAppClient
	if a.sessionFinder != nil {
		wc = a.sessionFinder.GetClient(m.SenderIdentity)
	} else {
		wc = a.client
	}

	if wc == nil || wc.Client() == nil {
		if m.SenderIdentity == "" {
			return "", fmt.Errorf("whatsapp: client not connected")
		}
		return "", fmt.Errorf("whatsapp: client not connected for sender identity: %s", m.SenderIdentity)
	}

	// Staggered dispatch: random delay before send
	stagger := time.Duration(rand.Int64N(int64(maxStagger-minStagger))) + minStagger
	select {
	case <-time.After(stagger):
	case <-ctx.Done():
		return "", ctx.Err()
	}

	// Convert phone number to JID
	recipientJID, parseErr := types.ParseJID(phoneToJID(m.To))
	if parseErr != nil {
		return "", fmt.Errorf("whatsapp: invalid recipient %q: %w", m.To, parseErr)
	}

	a.log.Info("whatsapp: dispatching message",
		"trace_id", m.TraceID,
		"to", recipientJID.String(),
		"stagger", stagger,
	)

	var msg waE2E.Message

	if m.Media != nil {
		if a.s3Client == nil {
			return "", channel.NewTerminalError(fmt.Errorf("whatsapp: media storage client not configured"))
		}

		// Rewrite internal proxy URL to key format: {workspace_id}/{hash}.{ext}
		// m.Media.MediaURL looks like: /media/{workspace_id}/{hash}.{ext}
		parts := strings.Split(m.Media.MediaURL, "/")
		if len(parts) < 3 {
			return "", channel.NewTerminalError(fmt.Errorf("whatsapp: invalid media URL format: %s", m.Media.MediaURL))
		}
		workspaceIDStr := parts[len(parts)-2]
		hashWithExt := parts[len(parts)-1]
		key := workspaceIDStr + "/" + hashWithExt

		bodyRC, contentType, err := a.s3Client.Download(ctx, key)
		if err != nil {
			return "", fmt.Errorf("whatsapp media download from S3 failed: %w", err)
		}
		defer bodyRC.Close()

		dataBytes, err := io.ReadAll(bodyRC)
		if err != nil {
			return "", fmt.Errorf("whatsapp media read failed: %w", err)
		}

		var uploadType whatsmeow.MediaType
		switch m.Media.MediaType {
		case "image":
			uploadType = whatsmeow.MediaImage
		case "document":
			uploadType = whatsmeow.MediaDocument
		case "audio":
			uploadType = whatsmeow.MediaAudio
		case "video":
			uploadType = whatsmeow.MediaVideo
		default:
			return "", channel.NewTerminalError(fmt.Errorf("whatsapp: unsupported media type %s", m.Media.MediaType))
		}

		resp, err := wc.Client().Upload(ctx, dataBytes, uploadType)
		if err != nil {
			return "", fmt.Errorf("whatsapp upload to CDN failed: %w", err)
		}

		var caption *string
		if m.Media.Caption != "" {
			caption = &m.Media.Caption
		}

		switch m.Media.MediaType {
		case "image":
			msg.ImageMessage = &waE2E.ImageMessage{
				URL:           &resp.URL,
				DirectPath:    &resp.DirectPath,
				MediaKey:      resp.MediaKey,
				Mimetype:      &contentType,
				FileLength:    &resp.FileLength,
				FileSHA256:    resp.FileSHA256,
				FileEncSHA256: resp.FileEncSHA256,
				Caption:       caption,
			}
		case "document":
			filename := m.Media.Filename
			if filename == "" {
				filename = "document"
			}
			msg.DocumentMessage = &waE2E.DocumentMessage{
				URL:           &resp.URL,
				DirectPath:    &resp.DirectPath,
				MediaKey:      resp.MediaKey,
				Mimetype:      &contentType,
				FileLength:    &resp.FileLength,
				FileSHA256:    resp.FileSHA256,
				FileEncSHA256: resp.FileEncSHA256,
				Title:         &filename,
				FileName:      &filename,
				Caption:       caption,
			}
		case "audio":
			msg.AudioMessage = &waE2E.AudioMessage{
				URL:           &resp.URL,
				DirectPath:    &resp.DirectPath,
				MediaKey:      resp.MediaKey,
				Mimetype:      &contentType,
				FileLength:    &resp.FileLength,
				FileSHA256:    resp.FileSHA256,
				FileEncSHA256: resp.FileEncSHA256,
			}
		case "video":
			msg.VideoMessage = &waE2E.VideoMessage{
				URL:           &resp.URL,
				DirectPath:    &resp.DirectPath,
				MediaKey:      resp.MediaKey,
				Mimetype:      &contentType,
				FileLength:    &resp.FileLength,
				FileSHA256:    resp.FileSHA256,
				FileEncSHA256: resp.FileEncSHA256,
				Caption:       caption,
			}
		}
	} else {
		body := m.Body
		msg.Conversation = &body
	}

	respSend, err := wc.Client().SendMessage(ctx, recipientJID, &msg)
	if err != nil {
		a.log.Error("whatsapp: send failed",
			"error", err,
			"trace_id", m.TraceID,
			"to", recipientJID.String(),
		)
		if isTerminalWhatsAppError(err) {
			return "", channel.NewTerminalError(fmt.Errorf("whatsapp terminal: %w", err))
		}
		return "", fmt.Errorf("whatsapp send: %w", err)
	}

	a.log.Info("whatsapp: message sent",
		"trace_id", m.TraceID,
		"to", recipientJID.String(),
	)

	respJSON, _ := json.Marshal(map[string]any{
		"message_id": respSend.ID,
		"timestamp":  respSend.Timestamp,
	})
	return string(respJSON), nil
}

// phoneToJID converts a phone number string to a WhatsApp JID.
// Strips non-digit characters and appends @s.whatsapp.net.
func phoneToJID(phone string) string {
	digits := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, phone)

	return digits + "@s.whatsapp.net"
}

// isTerminalWhatsAppError classifies whatsmeow errors as terminal
// (non-retryable) vs transient.
func isTerminalWhatsAppError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "403") ||
		strings.Contains(msg, "logged out") ||
		strings.Contains(msg, "unpaired")
}
