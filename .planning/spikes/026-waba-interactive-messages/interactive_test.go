package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestInteractiveButtons_Valid(t *testing.T) {
	payload := &InteractivePayload{
		Type: "button",
		Header: &InteractiveHeader{
			Type: "text",
			Text: "Promoção Exclusiva",
		},
		Body:   "Olá! Escolha uma das opções abaixo para continuar seu atendimento:",
		Footer: "Ecoar CPaaS Gateway",
		Buttons: []InteractiveButton{
			{ID: "btn_sales", Title: "Falar com Vendas"},
			{ID: "btn_support", Title: "Suporte Técnico"},
			{ID: "btn_cancel", Title: "Cancelar"},
		},
	}

	meta, err := payload.ToMetaJSON("+5511999999999")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	if meta.MessagingProduct != "whatsapp" {
		t.Errorf("messaging_product = %q, want whatsapp", meta.MessagingProduct)
	}
	if meta.Interactive.Type != "button" {
		t.Errorf("interactive.type = %q, want button", meta.Interactive.Type)
	}
	if len(meta.Interactive.Action.Buttons) != 3 {
		t.Fatalf("expected 3 reply buttons, got %d", len(meta.Interactive.Action.Buttons))
	}
	if meta.Interactive.Action.Buttons[0].Reply.ID != "btn_sales" {
		t.Errorf("button[0].id = %q, want btn_sales", meta.Interactive.Action.Buttons[0].Reply.ID)
	}

	jsonBytes, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		t.Fatalf("marshal meta json: %v", err)
	}
	if !strings.Contains(string(jsonBytes), `"type": "button"`) {
		t.Errorf("generated json missing type button: %s", string(jsonBytes))
	}
}

func TestInteractiveButtons_LimitExceeded(t *testing.T) {
	payload := &InteractivePayload{
		Type: "button",
		Body: "Selecione uma opção:",
		Buttons: []InteractiveButton{
			{ID: "b1", Title: "Opção 1"},
			{ID: "b2", Title: "Opção 2"},
			{ID: "b3", Title: "Opção 3"},
			{ID: "b4", Title: "Opção 4"}, // Meta limit is 3 buttons
		},
	}

	_, err := payload.ToMetaJSON("+5511999999999")
	if err == nil {
		t.Fatal("expected error for >3 buttons, got nil")
	}
	if !strings.Contains(err.Error(), "maximum 3 buttons") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestInteractiveList_Valid(t *testing.T) {
	payload := &InteractivePayload{
		Type:       "list",
		ButtonText: "Ver Menu de Planos",
		Body:       "Confira os nossos planos disponíveis e selecione o ideal para sua empresa:",
		Footer:     "Planos Mensais e Anuais",
		Sections: []InteractiveSection{
			{
				Title: "Planos Starter",
				Rows: []InteractiveRow{
					{ID: "plan_basic", Title: "Plano Basic", Description: "Até 1.000 msgs/mês"},
					{ID: "plan_pro", Title: "Plano Pro", Description: "Até 10.000 msgs/mês"},
				},
			},
			{
				Title: "Enterprise",
				Rows: []InteractiveRow{
					{ID: "plan_enterprise", Title: "Plano Enterprise", Description: "Volume customizado + suporte 24/7"},
				},
			},
		},
	}

	meta, err := payload.ToMetaJSON("+5511999999999")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	if meta.Interactive.Type != "list" {
		t.Errorf("interactive.type = %q, want list", meta.Interactive.Type)
	}
	if meta.Interactive.Action.Button != "Ver Menu de Planos" {
		t.Errorf("action.button = %q, want 'Ver Menu de Planos'", meta.Interactive.Action.Button)
	}
	if len(meta.Interactive.Action.Sections) != 2 {
		t.Fatalf("expected 2 sections, got %d", len(meta.Interactive.Action.Sections))
	}
	if len(meta.Interactive.Action.Sections[0].Rows) != 2 {
		t.Errorf("section 0 rows = %d, want 2", len(meta.Interactive.Action.Sections[0].Rows))
	}
}

func TestInteractiveCTA_Valid(t *testing.T) {
	payload := &InteractivePayload{
		Type: "cta_url",
		Body: "Seu boleto já está disponível para pagamento. Clique no link abaixo para visualizar:",
		CTA: &InteractiveCTA{
			DisplayText: "Baixar Boleto PDF",
			URL:         "https://pagamento.ecoar.com.br/boletos/12345",
		},
	}

	meta, err := payload.ToMetaJSON("+5511999999999")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	if meta.Interactive.Type != "cta_url" {
		t.Errorf("interactive.type = %q, want cta_url", meta.Interactive.Type)
	}
	if meta.Interactive.Action.Name != "cta_url" {
		t.Errorf("action.name = %q, want cta_url", meta.Interactive.Action.Name)
	}
	params := meta.Interactive.Action.Parameters
	if params["display_text"] != "Baixar Boleto PDF" {
		t.Errorf("display_text = %v, want 'Baixar Boleto PDF'", params["display_text"])
	}
}

func TestInteractiveLocationRequest_Valid(t *testing.T) {
	payload := &InteractivePayload{
		Type: "location_request",
		Body: "Para encontrarmos a loja mais próxima de você, por favor compartilhe sua localização atual:",
	}

	meta, err := payload.ToMetaJSON("+5511999999999")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	if meta.Interactive.Type != "location_request_message" {
		t.Errorf("interactive.type = %q, want location_request_message", meta.Interactive.Type)
	}
	if meta.Interactive.Action.Name != "send_location" {
		t.Errorf("action.name = %q, want send_location", meta.Interactive.Action.Name)
	}
}

func TestInteractiveFlow_Valid(t *testing.T) {
	payload := &InteractivePayload{
		Type: "flow",
		Body: "Agende sua consulta online diretamente pelo WhatsApp:",
		Flow: &InteractiveFlow{
			FlowID:     "1234567890",
			FlowToken:  "appointment_token_xyz",
			FlowCTA:    "Agendar Agora",
			FlowAction: "navigate",
			FlowScreen: "SELECT_DATE_SCREEN",
		},
	}

	meta, err := payload.ToMetaJSON("+5511999999999")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	if meta.Interactive.Type != "flow" {
		t.Errorf("interactive.type = %q, want flow", meta.Interactive.Type)
	}
	params := meta.Interactive.Action.Parameters
	if params["flow_id"] != "1234567890" {
		t.Errorf("flow_id = %v, want 1234567890", params["flow_id"])
	}
	payloadObj, ok := params["flow_action_payload"].(map[string]interface{})
	if !ok || payloadObj["screen"] != "SELECT_DATE_SCREEN" {
		t.Errorf("flow screen = %v, want SELECT_DATE_SCREEN", payloadObj)
	}
}

func TestParseInteractiveWebhook(t *testing.T) {
	rawJSON := []byte(`{
		"type": "button_reply",
		"button_reply": {
			"id": "btn_sales",
			"title": "Falar com Vendas"
		}
	}`)

	resp, err := ParseInteractiveWebhook(rawJSON)
	if err != nil {
		t.Fatalf("parse interactive webhook: %v", err)
	}

	if resp.Type != "button_reply" {
		t.Errorf("type = %q, want button_reply", resp.Type)
	}
	if resp.ButtonReply == nil || resp.ButtonReply.ID != "btn_sales" {
		t.Errorf("button_reply.id = %v, want btn_sales", resp.ButtonReply)
	}
}

func TestInteractiveAutoChunkingButtonsWithWarning(t *testing.T) {
	// 5 buttons -> Chunks into 2 button messages (3 + 2) and returns recommendation warning
	payload := &InteractivePayload{
		Type: "button",
		Body: "Selecione a sua região:",
		Buttons: []InteractiveButton{
			{ID: "r1", Title: "Norte"},
			{ID: "r2", Title: "Nordeste"},
			{ID: "r3", Title: "Centro-Oeste"},
			{ID: "r4", Title: "Sudeste"},
			{ID: "r5", Title: "Sul"},
		},
	}

	chunks, warning, err := payload.ToMetaJSONChunks("+5511999999999")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(chunks) != 2 {
		t.Fatalf("expected 2 button message chunks, got %d", len(chunks))
	}

	if chunks[0].Interactive.Type != "button" || chunks[1].Interactive.Type != "button" {
		t.Errorf("expected button types, got %s and %s", chunks[0].Interactive.Type, chunks[1].Interactive.Type)
	}

	if warning == "" || !strings.Contains(warning, "Recommendation") {
		t.Errorf("expected warning recommendation, got %q", warning)
	}
}

func TestInteractiveAutoChunkingListRows(t *testing.T) {
	// 15 list rows -> Auto-chunk into 2 list messages (10 rows + 5 rows)
	rows := make([]InteractiveRow, 15)
	for i := 0; i < 15; i++ {
		rows[i] = InteractiveRow{
			ID:    "item_" + string(rune('a'+i)),
			Title: "Opção " + string(rune('A'+i)),
		}
	}

	payload := &InteractivePayload{
		Type:       "list",
		ButtonText: "Ver Cardápio",
		Body:       "Escolha seus pratos:",
		Sections:   []InteractiveSection{{Title: "Pratos", Rows: rows}},
	}

	chunks, warning, err := payload.ToMetaJSONChunks("+5511999999999")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(chunks) != 2 {
		t.Fatalf("expected 2 list message chunks, got %d", len(chunks))
	}

	if warning == "" || !strings.Contains(warning, "Notice") {
		t.Errorf("expected warning notice for list chunking, got %q", warning)
	}

	if len(chunks[0].Interactive.Action.Sections[0].Rows) != 10 {
		t.Errorf("chunk 0 rows = %d, want 10", len(chunks[0].Interactive.Action.Sections[0].Rows))
	}
	if len(chunks[1].Interactive.Action.Sections[0].Rows) != 5 {
		t.Errorf("chunk 1 rows = %d, want 5", len(chunks[1].Interactive.Action.Sections[0].Rows))
	}
}
