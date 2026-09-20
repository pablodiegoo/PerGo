package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/client"
	"github.com/pablojhp.pergo/internal/domain"
)

// ResolvedCredentials holds resolved Meta WABA credentials, indicating whether fallback mocks are active.
type ResolvedCredentials struct {
	PhoneNumberID string
	Token         string
	WABAAccountID string
	VerifyToken   string
	DisplayPhone  string
	IsMock        bool
}

// resolveWABACredentials resolves credentials from environment or provides deterministic zero-PII mocks.
func resolveWABACredentials(ctx context.Context) ResolvedCredentials {
	token := envOrDefault("ACCESS_TOKEN", "")
	phoneID := envOrDefault("PHONE_NUMBER_ID", "")
	wabaID := envOrDefault("WHATSAPP_BUSINESS_ACCOUNT_ID", "")
	verifyToken := envOrDefault("PERGO_VERIFY_TOKEN", "pergo-verify-token")
	displayPhone := envOrDefault("DISPLAY_PHONE_NUMBER", "")

	if token == "" || phoneID == "" || wabaID == "" {
		if displayPhone == "" {
			displayPhone = "5511987654321"
		}
		return ResolvedCredentials{
			PhoneNumberID: "109876543210123",
			Token:         "mock-waba-token-pergo-seed",
			WABAAccountID: "209876543210123",
			VerifyToken:   verifyToken,
			DisplayPhone:  displayPhone,
			IsMock:        true,
		}
	}

	if displayPhone == "" {
		metaClient := client.NewWABAMetaClient(nil, "")
		if details, err := metaClient.FetchPhoneNumberDetails(ctx, phoneID, token); err == nil && details != nil && details.DisplayPhoneNumber != "" {
			if clean, valid := domain.SanitizePhone(details.DisplayPhoneNumber); valid {
				displayPhone = clean
			} else {
				displayPhone = details.DisplayPhoneNumber
			}
			slog.Info("resolved WABA display phone number from Meta API", "display_phone", displayPhone, "verified_name", details.VerifiedName)
		}
	}
	if displayPhone == "" {
		displayPhone = phoneID
	}

	return ResolvedCredentials{
		PhoneNumberID: phoneID,
		Token:         token,
		WABAAccountID: wabaID,
		VerifyToken:   verifyToken,
		DisplayPhone:  displayPhone,
		IsMock:        false,
	}
}

// Fixture data structures
type TagFixture struct {
	Name  string
	Color string
}

type IdentityFixture struct {
	Channel        string
	SenderIdentity string
}

type ContactFixture struct {
	Name       string
	Email      string
	Identities []IdentityFixture
	Tags       []string
	Attributes map[string]string
	BotActive  bool
}

type TemplateFixture struct {
	Name           string
	Category       string
	Language       string
	Status         string
	QualityScore   string
	MetaTemplateID string
	Components     json.RawMessage
}

type CampaignRecipientFixture struct {
	ContactName string
	Phone       string
	Variables   map[string]string
	Status      domain.RecipientStatus
	SentAgo     time.Duration
}

type CampaignFixture struct {
	Name         string
	Channel      string
	TemplateName string
	Status       domain.CampaignStatus
	BatchSize    int
	DelaySeconds int
	TagNames     []string
	Recipients   []CampaignRecipientFixture
}

type MessageFixture struct {
	Direction     string // "inbound" or "outbound"
	Body          string
	OffsetMinutes int    // minutes prior to base time
	Status        string // "delivered", "read", "sent"
}

type ConversationFixture struct {
	ContactName       string
	Channel           string
	RecipientIdentity string // business side identity
	Messages          []MessageFixture
}

type FixtureData struct {
	Tags          []TagFixture
	Contacts      []ContactFixture
	Templates     []TemplateFixture
	Campaigns     []CampaignFixture
	Conversations []ConversationFixture
}

// getDeterministicFixtures returns rich, production-authentic mock data for PerGo with zero real PII.
func getDeterministicFixtures(displayPhone string) FixtureData {
	return FixtureData{
		Tags: []TagFixture{
			{Name: "VIP", Color: "#10B981"},
			{Name: "Lead Qualificado", Color: "#3B82F6"},
			{Name: "Cliente Ativo", Color: "#8B5CF6"},
			{Name: "Suporte N2", Color: "#F59E0B"},
			{Name: "Pesquisa 2026", Color: "#EC4899"},
		},
		Contacts: []ContactFixture{
			{
				Name:  "Ana Clara Silva",
				Email: "ana.silva@mock.pergo.io",
				Identities: []IdentityFixture{
					{Channel: "whatsapp_cloud", SenderIdentity: "5511981112233"},
				},
				Tags: []string{"VIP", "Cliente Ativo"},
				Attributes: map[string]string{
					"plano":      "Enterprise",
					"cidade":     "São Paulo",
					"contrato":   "PJ-9821",
					"vendedor":   "Rafael Souza",
					"recorrencia": "Mensal",
				},
				BotActive: false,
			},
			{
				Name:  "Carlos Eduardo Santos",
				Email: "carlos.santos@mock.pergo.io",
				Identities: []IdentityFixture{
					{Channel: "whatsapp_cloud", SenderIdentity: "5511982223344"},
				},
				Tags: []string{"Lead Qualificado"},
				Attributes: map[string]string{
					"empresa":  "Santos Tech",
					"cargo":    "CTO",
					"origem":   "Inbound Web",
					"interesse": "Meta Flows API",
				},
				BotActive: false,
			},
			{
				Name:  "Mariana Oliveira",
				Email: "mariana.oliveira@mock.pergo.io",
				Identities: []IdentityFixture{
					{Channel: "whatsapp_cloud", SenderIdentity: "5521983334455"},
					{Channel: "telegram", SenderIdentity: "mariana_oliveira"},
				},
				Tags: []string{"Cliente Ativo", "Suporte N2"},
				Attributes: map[string]string{
					"segmento":        "Fintech",
					"status_contrato": "vigente",
					"tier_suporte":    "SLA 1h",
				},
				BotActive: false,
			},
			{
				Name:  "Lucas Mendonça",
				Email: "lucas.mendonca@mock.pergo.io",
				Identities: []IdentityFixture{
					{Channel: "whatsapp", SenderIdentity: "5531984445566"},
				},
				Tags: []string{"Lead Qualificado"},
				Attributes: map[string]string{
					"origem":    "Webinar Março",
					"interesse": "WhatsApp Cloud Gateway",
					"cidade":    "Belo Horizonte",
				},
				BotActive: true,
			},
			{
				Name:  "Beatriz Costa",
				Email: "beatriz.costa@mock.pergo.io",
				Identities: []IdentityFixture{
					{Channel: "whatsapp_cloud", SenderIdentity: "5541985556677"},
				},
				Tags: []string{"Pesquisa 2026"},
				Attributes: map[string]string{
					"nps":          "10",
					"regiao":       "Sul",
					"ultima_compra": "2026-08-15",
				},
				BotActive: false,
			},
		},
		Templates: []TemplateFixture{
			{
				Name:           "boas_vindas_onboarding",
				Category:       "UTILITY",
				Language:       "pt_BR",
				Status:         "APPROVED",
				QualityScore:   "GREEN",
				MetaTemplateID: "meta_mock_1001",
				Components: json.RawMessage(`[
					{"type": "HEADER", "format": "TEXT", "text": "Bem-vindo ao PerGo CPaaS"},
					{"type": "BODY", "text": "Olá {{1}}, seu cadastro no PerGo foi concluído com sucesso. Nossa equipe está à disposição para apoiar sua integração."},
					{"type": "FOOTER", "text": "Equipe de Sucesso do Cliente"}
				]`),
			},
			{
				Name:           "confirmacao_agendamento",
				Category:       "UTILITY",
				Language:       "pt_BR",
				Status:         "APPROVED",
				QualityScore:   "GREEN",
				MetaTemplateID: "meta_mock_1002",
				Components: json.RawMessage(`[
					{"type": "BODY", "text": "Olá {{1}}, sua reunião sobre {{2}} está confirmada para {{3}}."},
					{"type": "BUTTONS", "buttons": [
						{"type": "QUICK_REPLY", "text": "Confirmar Presença"},
						{"type": "QUICK_REPLY", "text": "Reagendar Reunião"}
					]}
				]`),
			},
			{
				Name:           "promocao_upgrade",
				Category:       "MARKETING",
				Language:       "pt_BR",
				Status:         "APPROVED",
				QualityScore:   "YELLOW",
				MetaTemplateID: "meta_mock_1003",
				Components: json.RawMessage(`[
					{"type": "BODY", "text": "Olá {{1}}, temos uma condição especial para migração ao plano Enterprise com throughput ilimitado e alta disponibilidade."}
				]`),
			},
		},
		Campaigns: []CampaignFixture{
			{
				Name:         "Campanha Boas-Vindas Q3",
				Channel:      "whatsapp_cloud",
				TemplateName: "boas_vindas_onboarding",
				Status:       domain.CampaignStatusCompleted,
				BatchSize:    50,
				DelaySeconds: 2,
				TagNames:     []string{"Lead Qualificado"},
				Recipients: []CampaignRecipientFixture{
					{
						ContactName: "Carlos Eduardo Santos",
						Phone:       "5511982223344",
						Variables:   map[string]string{"1": "Carlos"},
						Status:      domain.RecipientStatusSent,
						SentAgo:     120 * time.Minute,
					},
					{
						ContactName: "Lucas Mendonça",
						Phone:       "5531984445566",
						Variables:   map[string]string{"1": "Lucas"},
						Status:      domain.RecipientStatusSent,
						SentAgo:     118 * time.Minute,
					},
				},
			},
			{
				Name:         "Pesquisa Satisfação 2026",
				Channel:      "whatsapp_cloud",
				TemplateName: "promocao_upgrade",
				Status:       domain.CampaignStatusRunning,
				BatchSize:    25,
				DelaySeconds: 5,
				TagNames:     []string{"Pesquisa 2026"},
				Recipients: []CampaignRecipientFixture{
					{
						ContactName: "Beatriz Costa",
						Phone:       "5541985556677",
						Variables:   map[string]string{"1": "Beatriz"},
						Status:      domain.RecipientStatusSent,
						SentAgo:     15 * time.Minute,
					},
					{
						ContactName: "Ana Clara Silva",
						Phone:       "5511981112233",
						Variables:   map[string]string{"1": "Ana Clara"},
						Status:      domain.RecipientStatusPending,
					},
				},
			},
		},
		Conversations: []ConversationFixture{
			{
				ContactName:       "Ana Clara Silva",
				Channel:           "whatsapp_cloud",
				RecipientIdentity: displayPhone,
				Messages: []MessageFixture{
					{
						Direction:     "inbound",
						Body:          "Olá! Gostaria de entender mais sobre os limites de envio da API WABA no PerGo.",
						OffsetMinutes: 120,
					},
					{
						Direction:     "outbound",
						Body:          "Olá Ana Clara! Nosso plano Enterprise oferece taxa com balanceamento dinâmico via NATS JetStream sem gargalos.",
						OffsetMinutes: 115,
						Status:        "read",
					},
					{
						Direction:     "inbound",
						Body:          "Excelente! Como configuro o webhook de retorno para o nosso bot no Typebot?",
						OffsetMinutes: 90,
					},
					{
						Direction:     "outbound",
						Body:          "Você pode cadastrar o webhook diretamente em Integrações ou pela API REST via POST /api/v1/webhooks.",
						OffsetMinutes: 85,
						Status:        "read",
					},
					{
						Direction:     "inbound",
						Body:          "Muito obrigada pela resposta ágil! Já configurei e a integração está funcionando com sucesso.",
						OffsetMinutes: 10,
					},
				},
			},
			{
				ContactName:       "Carlos Eduardo Santos",
				Channel:           "whatsapp_cloud",
				RecipientIdentity: displayPhone,
				Messages: []MessageFixture{
					{
						Direction:     "inbound",
						Body:          "Bom dia! O PerGo já suporta os novos Meta Flows interativos?",
						OffsetMinutes: 240,
					},
					{
						Direction:     "outbound",
						Body:          "Bom dia Carlos! Sim, temos suporte nativo com criptografia ponta a ponta RSA/AES-128-GCM e troca síncrona de telas.",
						OffsetMinutes: 235,
						Status:        "read",
					},
					{
						Direction:     "inbound",
						Body:          "Sensacional! Vou testar a exportação da chave pública PEM hoje mesmo no Meta Flow Builder.",
						OffsetMinutes: 180,
					},
				},
			},
			{
				ContactName:       "Mariana Oliveira",
				Channel:           "whatsapp_cloud",
				RecipientIdentity: displayPhone,
				Messages: []MessageFixture{
					{
						Direction:     "inbound",
						Body:          "Oi equipe, estou com uma dúvida sobre a retenção de mídias no S3/MinIO.",
						OffsetMinutes: 1440, // 24 hours ago
					},
					{
						Direction:     "outbound",
						Body:          "Oi Mariana! Você pode definir a retenção em dias nas configurações do workspace. Deseja configurar 30 ou 90 dias?",
						OffsetMinutes: 1430,
						Status:        "read",
					},
					{
						Direction:     "inbound",
						Body:          "90 dias atende perfeitamente à nossa política de conformidade bancária. Obrigado!",
						OffsetMinutes: 1400,
					},
				},
			},
		},
	}
}

// buildInboundAuditPayload serializes the JSON structure for an inbound_message audit log entry.
func buildInboundAuditPayload(wsID uuid.UUID, traceID, messageID, channel, from, to, body string, t time.Time) ([]byte, error) {
	return json.Marshal(map[string]any{
		"event":        "inbound_message",
		"trace_id":     traceID,
		"message_id":   messageID,
		"channel":      channel,
		"timestamp":    t.Format(time.RFC3339),
		"workspace_id": wsID.String(),
		"from":         from,
		"to":           to,
		"body":         body,
	})
}

// buildOutboundAuditPayload serializes the JSON structure for an outbound_message audit log entry.
func buildOutboundAuditPayload(wsID uuid.UUID, traceID, channel, senderIdentity, to, body string, t time.Time) ([]byte, error) {
	return json.Marshal(map[string]any{
		"event":        "outbound_message",
		"trace_id":     traceID,
		"channel":      channel,
		"timestamp":    t.Format(time.RFC3339),
		"workspace_id": wsID.String(),
		"request": map[string]any{
			"channel":         channel,
			"sender_identity": senderIdentity,
			"to":              to,
			"body":            body,
			"type":            "text",
		},
	})
}
