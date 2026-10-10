package domain

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Direction represents the trajectory of a message in a conversation.
type Direction string

const (
	DirectionInbound      Direction = "inbound"
	DirectionOutbound     Direction = "outbound"
	DirectionInternalNote Direction = "internal_note"
)

// SenderType identifies the origin actor of a message.
type SenderType string

const (
	SenderTypeContact    SenderType = "contact"
	SenderTypeHumanAgent SenderType = "human_agent"
	SenderTypeAIAgent    SenderType = "ai_agent"
	SenderTypeSystem     SenderType = "system"
)

// ChatStatus represents the lifecycle state of a conversation.
type ChatStatus string

const (
	ChatStatusOpen   ChatStatus = "open"
	ChatStatusClosed ChatStatus = "closed"
)

// Reaction represents an emoji reaction attached to a message.
type Reaction struct {
	Emoji     string    `json:"emoji"`
	Sender    string    `json:"sender"`
	CreatedAt time.Time `json:"created_at"`
}

// ReactionUpdatedPayload is the typed payload for reaction events published to NATS and webhooks.
type ReactionUpdatedPayload struct {
	Event       string     `json:"event"`
	WorkspaceID string     `json:"workspace_id"`
	ChatID      string     `json:"chat_id,omitempty"`
	MessageUID  string     `json:"message_uid"`
	Emoji       string     `json:"emoji"`
	Sender      string     `json:"sender"`
	Action      string     `json:"action"` // "add" | "remove"
	Reactions   []Reaction `json:"reactions"`
	Timestamp   string     `json:"timestamp"`
}

// Chat represents a conversational thread between a workspace connection and a contact.
type Chat struct {
	ID                     uuid.UUID              `json:"id"`
	WorkspaceID            uuid.UUID              `json:"workspace_id"`
	ConnectionID           *uuid.UUID             `json:"connection_id,omitempty"`
	ContactID              uuid.UUID              `json:"contact_id"`
	Status                 string                 `json:"status"` // "open", "closed"
	AssignedUserID         *uuid.UUID             `json:"assigned_user_id,omitempty"`
	AssignedEmail          *string                `json:"assigned_email,omitempty"`
	Tags                   []string               `json:"tags"`
	UnreadCount            int                    `json:"unread_count"`
	AIDisabled             bool                   `json:"ai_disabled"`
	ServiceWindowExpiresAt *time.Time             `json:"service_window_expires_at,omitempty"`
	ServiceWindowIsOpen    bool                   `json:"service_window_is_open"`
	LastMessageAt          time.Time              `json:"last_message_at"`
	Metadata               map[string]interface{} `json:"metadata"`
	CreatedAt              time.Time              `json:"created_at"`
	UpdatedAt              time.Time              `json:"updated_at"`
}

// IsServiceWindowOpen reports whether the customer service window is active.
func (c *Chat) IsServiceWindowOpen() bool {
	if c == nil || c.ServiceWindowExpiresAt == nil {
		return false
	}
	return time.Now().UTC().Before(*c.ServiceWindowExpiresAt)
}

// ChatMessage represents a single message, dispatch, or internal note within a Chat.
type ChatMessage struct {
	ID          uuid.UUID              `json:"id"`
	ChatID      uuid.UUID              `json:"chat_id"`
	WorkspaceID uuid.UUID              `json:"workspace_id"`
	UID         string                 `json:"uid"`
	Direction   string                 `json:"direction"`
	SenderType  string                 `json:"sender_type"`
	SenderName  string                 `json:"sender_name,omitempty"`
	SenderID    string                 `json:"sender_id,omitempty"`
	Body        string                 `json:"body"`
	MediaURL    *string                `json:"media_url,omitempty"`
	MediaType   *string                `json:"media_type,omitempty"`
	IsPrivate   bool                   `json:"is_private"`
	Reactions   []Reaction             `json:"reactions"`
	ReplyToUID  *string                `json:"reply_to_uid,omitempty"`
	Metadata    map[string]interface{} `json:"metadata"`
	CreatedAt   time.Time              `json:"created_at"`
}

// ChatFilter encapsulates querying criteria for listing workspace chats.
type ChatFilter struct {
	Status        string     `json:"status,omitempty"`
	Unread        *bool      `json:"unread,omitempty"`
	Phone         string     `json:"phone,omitempty"`
	ContactID     *uuid.UUID `json:"contact_id,omitempty"`
	AssignedEmail *string    `json:"assigned_email,omitempty"`
	Unassigned    *bool      `json:"unassigned,omitempty"`
	Tag           string     `json:"tag,omitempty"`
}

// ChatSynopsis represents a 3-bullet synopsis of customer issues, promises made, and current status.
type ChatSynopsis struct {
	CustomerIssues string `json:"customer_issues"`
	PromisesMade   string `json:"promises_made"`
	CurrentStatus  string `json:"current_status"`
}

// SummarizeMessages generates a structured 3-bullet synopsis: Customer Issues, Promises Made, and Current Status.
func SummarizeMessages(messages []ChatMessage, chat *Chat) ChatSynopsis {
	var inboundBodies []string
	var agentCommitments []string
	var allAgentBodies []string

	commitmentKeywords := []string{
		"proposta", "enviar", "envio", "retorno", "amanhã", "amanha", "prazo",
		"aguarde", "preço", "preco", "valor", "orçamento", "orcamento", "reunião",
		"reuniao", "link", "desconto", "confirm", "send", "promise", "proposal",
	}

	for _, msg := range messages {
		body := strings.TrimSpace(msg.Body)
		if body == "" {
			continue
		}

		if msg.Direction == string(DirectionInbound) || msg.SenderType == string(SenderTypeContact) {
			inboundBodies = append(inboundBodies, body)
		} else {
			allAgentBodies = append(allAgentBodies, body)
			lowerBody := strings.ToLower(body)
			for _, kw := range commitmentKeywords {
				if strings.Contains(lowerBody, kw) {
					agentCommitments = append(agentCommitments, body)
					break
				}
			}
		}
	}

	// 1. Customer Issues
	customerIssues := "Nenhuma demanda recente identificada na conversa."
	if len(inboundBodies) > 0 {
		if len(inboundBodies) == 1 {
			b := inboundBodies[0]
			if len(b) > 160 {
				b = b[:157] + "..."
			}
			customerIssues = fmt.Sprintf("Cliente solicitou: %q", b)
		} else {
			lastInbound := inboundBodies[len(inboundBodies)-1]
			firstInbound := inboundBodies[0]
			if len(firstInbound) > 80 {
				firstInbound = firstInbound[:77] + "..."
			}
			if len(lastInbound) > 80 {
				lastInbound = lastInbound[:77] + "..."
			}
			if firstInbound == lastInbound {
				customerIssues = fmt.Sprintf("Demanda do cliente: %q", lastInbound)
			} else {
				customerIssues = fmt.Sprintf("Demanda inicial: %q | Dúvida mais recente: %q", firstInbound, lastInbound)
			}
		}
	}

	// 2. Promises Made
	promisesMade := "Nenhum compromisso ou prazo pendente acordado pela equipe."
	if len(agentCommitments) > 0 {
		lastCommitment := agentCommitments[len(agentCommitments)-1]
		if len(lastCommitment) > 160 {
			lastCommitment = lastCommitment[:157] + "..."
		}
		promisesMade = fmt.Sprintf("Compromisso registrado pela equipe: %q", lastCommitment)
	} else if len(allAgentBodies) > 0 {
		lastAgentMsg := allAgentBodies[len(allAgentBodies)-1]
		if len(lastAgentMsg) > 160 {
			lastAgentMsg = lastAgentMsg[:157] + "..."
		}
		promisesMade = fmt.Sprintf("Atendimento em andamento; última resposta do operador: %q", lastAgentMsg)
	}

	// 3. Current Status
	currentStatus := "Conversa em aberto sem mensagens recentes."
	if chat != nil && chat.Status == "closed" {
		currentStatus = "Atendimento resolvido e fechado."
	} else if len(messages) > 0 {
		lastMsg := messages[len(messages)-1]
		if lastMsg.Direction == string(DirectionInbound) || lastMsg.SenderType == string(SenderTypeContact) {
			currentStatus = "Aguardando resposta da equipe (última mensagem recebida do cliente)."
		} else {
			currentStatus = "Aguardando retorno do cliente (equipe respondeu recentemente)."
		}
	}

	if chat != nil {
		if chat.IsServiceWindowOpen() {
			currentStatus += " [Janela 24h WABA ativa]"
		} else if chat.ServiceWindowExpiresAt != nil {
			currentStatus += " [Janela 24h WABA expirada]"
		}
	}

	return ChatSynopsis{
		CustomerIssues: customerIssues,
		PromisesMade:   promisesMade,
		CurrentStatus:  currentStatus,
	}
}


