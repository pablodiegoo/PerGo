package instagram

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/pablojhp.pergo/internal/inbound"
	"github.com/pablojhp.pergo/internal/media"
	"github.com/pablojhp.pergo/internal/repository"
)

// InstagramInboundAdapter implements channel.InboundAdapter for Instagram.
type InstagramInboundAdapter struct {
	downloader media.Downloader
	client     *http.Client
}

// NewInboundAdapter creates a new InstagramInboundAdapter.
func NewInboundAdapter(downloader media.Downloader) *InstagramInboundAdapter {
	return &InstagramInboundAdapter{
		downloader: downloader,
		client:     &http.Client{Timeout: 30 * time.Second},
	}
}

type igPayload struct {
	Object string `json:"object"`
	Entry  []struct {
		ID        string `json:"id"`
		Messaging []struct {
			Sender struct {
				ID string `json:"id"`
			} `json:"sender"`
			Recipient struct {
				ID string `json:"id"`
			} `json:"recipient"`
			Message *struct {
				Mid  string `json:"mid"`
				Text string `json:"text"`
				QuickReply *struct {
					Payload string `json:"payload"`
				} `json:"quick_reply"`
				ReplyTo *struct {
					Story *struct {
						URL string `json:"url"`
						ID  string `json:"id"`
					} `json:"story"`
				} `json:"reply_to"`
				Attachments []struct {
					Type    string `json:"type"`
					Payload struct {
						URL string `json:"url"`
					} `json:"payload"`
				} `json:"attachments"`
			} `json:"message"`
		} `json:"messaging"`
		Changes []struct {
			Value struct {
				MessagingProduct string `json:"messaging_product"`
				Messages []struct {
					From string `json:"from"`
					ID   string `json:"id"`
					Text *struct {
						Body string `json:"body"`
					} `json:"text"`
					Interactive *struct {
						Type       string `json:"type"`
						ButtonReply *struct {
							ID    string `json:"id"`
							Title string `json:"title"`
						} `json:"button_reply"`
					} `json:"interactive"`
				} `json:"messages"`
				Metadata struct {
					DisplayPhoneNumber string `json:"display_phone_number"`
				} `json:"metadata"`
			} `json:"value"`
		} `json:"changes"`
	} `json:"entry"`
}

// Parse decodes the Instagram payload and returns unified events.
func (a *InstagramInboundAdapter) Parse(
	ctx context.Context,
	payload []byte,
	headers map[string]string,
	conn *repository.Connection,
) ([]*inbound.InboundEvent, error) {
	var ig igPayload
	if err := json.Unmarshal(payload, &ig); err != nil {
		return nil, fmt.Errorf("failed to parse instagram payload: %w", err)
	}

	var events []*inbound.InboundEvent

	for _, entry := range ig.Entry {
		// Handle standard Messenger/Instagram format
		for _, msg := range entry.Messaging {
			if msg.Message == nil {
				continue
			}

			ev := &inbound.InboundEvent{
				WorkspaceID:  conn.WorkspaceID,
				ConnectionID: conn.ID,
				MessageID:    msg.Message.Mid,
				Channel:      "instagram",
				From:         msg.Sender.ID,
				To:           msg.Recipient.ID,
				Body:         msg.Message.Text,
			}

			if msg.Message.QuickReply != nil {
				ev.Interactive = &inbound.InboundInteractive{
					Type: "button_reply",
					ButtonReply: &inbound.InboundButtonReply{
						ID:    msg.Message.QuickReply.Payload,
						Title: msg.Message.Text,
					},
				}
			}

			if msg.Message.ReplyTo != nil && msg.Message.ReplyTo.Story != nil {
				ev.Story = &inbound.InboundStoryEvent{
					Subtype:  "reply",
					MediaURL: msg.Message.ReplyTo.Story.URL,
				}
			}

			for _, att := range msg.Message.Attachments {
				if att.Type == "story_mention" {
					ev.Story = &inbound.InboundStoryEvent{
						Subtype:  "mention",
						MediaURL: att.Payload.URL,
					}
				}
			}

			events = append(events, ev)
		}

		// Handle WABA-like format if messaging_product == instagram
		for _, change := range entry.Changes {
			if change.Value.MessagingProduct == "instagram" {
				for _, msg := range change.Value.Messages {
					ev := &inbound.InboundEvent{
						WorkspaceID:  conn.WorkspaceID,
						ConnectionID: conn.ID,
						MessageID:    msg.ID,
						Channel:      "instagram",
						From:         msg.From,
						To:           change.Value.Metadata.DisplayPhoneNumber,
					}
					
					if msg.Text != nil {
						ev.Body = msg.Text.Body
					}
					
					if msg.Interactive != nil && msg.Interactive.ButtonReply != nil {
						ev.Interactive = &inbound.InboundInteractive{
							Type: "button_reply",
							ButtonReply: &inbound.InboundButtonReply{
								ID:    msg.Interactive.ButtonReply.ID,
								Title: msg.Interactive.ButtonReply.Title,
							},
						}
					}
					
					events = append(events, ev)
				}
			}
		}
	}

	return events, nil
}
