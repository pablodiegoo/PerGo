package domain

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestSniffDelimiter(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected rune
	}{
		{"comma", "nome,cidade,idade\n", ','},
		{"semicolon", "nome;cidade;idade\n", ';'},
		{"tab", "nome\tcidade\tidade\n", '\t'},
		{"comma fallback", "nome-cidade-idade\n", ','},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SniffDelimiter(tt.input); got != tt.expected {
				t.Errorf("SniffDelimiter() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestSanitizePhone(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantClean  string
		wantValid  bool
	}{
		{"valid standard", "+55 (11) 99999-8888", "5511999998888", true},
		{"valid raw", "5511999998888", "5511999998888", true},
		{"short invalid", "99999-8888", "999998888", false},
		{"long invalid", "5511999998888777", "5511999998888777", false},
		{"alphabetic noise", "551199abc998888", "551199998888", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotClean, gotValid := SanitizePhone(tt.input)
			if gotClean != tt.wantClean || gotValid != tt.wantValid {
				t.Errorf("SanitizePhone() = (%q, %t), want (%q, %t)", gotClean, gotValid, tt.wantClean, tt.wantValid)
			}
		})
	}
}

func TestResolveVariables(t *testing.T) {
	row := map[string]string{
		"nome":   "João",
		"cidade": "São Paulo",
		"0":      "Primeiro",
		"1":      "Segundo",
	}

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"standard", "Olá {{nome}} de {{cidade}}!", "Olá João de São Paulo!"},
		{"case insensitive", "Olá {{Nome}} de {{Cidade}}!", "Olá João de São Paulo!"},
		{"whitespace", "Olá {{  nome  }}!", "Olá João!"},
		{"missing column", "Olá {{nome}} de {{pais}}!", "Olá João de {{pais}}!"},
		{"index based", "Item {{0}} e {{1}}", "Item Primeiro e Segundo"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ResolveVariables(tt.input, row); got != tt.expected {
				t.Errorf("ResolveVariables() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestCalculateDuration(t *testing.T) {
	tests := []struct {
		name         string
		totalValid   int
		batchSize    int
		delaySeconds int
		expected     int
	}{
		{"exact division", 100, 50, 5, 10},
		{"with remainder", 101, 50, 5, 15},
		{"zero valid", 0, 50, 5, 0},
		{"zero batch", 100, 0, 5, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CalculateDuration(tt.totalValid, tt.batchSize, tt.delaySeconds); got != tt.expected {
				t.Errorf("CalculateDuration() = %d, want %d", got, tt.expected)
			}
		})
	}
}

func TestCalculateEstimatedDuration(t *testing.T) {
	rate60 := 60
	rate120 := 120
	rate0 := 0

	tests := []struct {
		name            string
		totalValid      int
		batchSize       int
		delaySeconds    int
		rateLimitPerMin *int
		expected        int
	}{
		{"exact division with rate limit 60", 60, 50, 5, &rate60, 60},
		{"with rate limit 120 (2 msgs/sec)", 100, 50, 5, &rate120, 50},
		{"rate limit with remainder", 61, 50, 5, &rate60, 61},
		{"zero valid with rate limit", 0, 50, 5, &rate60, 0},
		{"nil rate limit falls back to batch calculation", 100, 50, 5, nil, 10},
		{"zero rate limit falls back to batch calculation", 100, 50, 5, &rate0, 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CalculateEstimatedDuration(tt.totalValid, tt.batchSize, tt.delaySeconds, tt.rateLimitPerMin); got != tt.expected {
				t.Errorf("CalculateEstimatedDuration() = %d, want %d", got, tt.expected)
			}
		})
	}
}

func TestDeduplicateUUIDs(t *testing.T) {
	id1 := uuid.New()
	id2 := uuid.New()
	id3 := uuid.New()

	input := []uuid.UUID{id1, id2, uuid.Nil, id1, id3, id2, uuid.Nil}
	expected := []uuid.UUID{id1, id2, id3}

	result := DeduplicateUUIDs(input)
	if len(result) != len(expected) {
		t.Fatalf("expected length %d, got %d", len(expected), len(result))
	}
	for i := range expected {
		if result[i] != expected[i] {
			t.Errorf("at index %d: expected %s, got %s", i, expected[i], result[i])
		}
	}
}

func TestDeduplicateStrings(t *testing.T) {
	input := []string{"whatsapp", "telegram", "", "whatsapp", "  email ", "telegram", "   "}
	expected := []string{"whatsapp", "telegram", "email"}

	result := DeduplicateStrings(input)
	if len(result) != len(expected) {
		t.Fatalf("expected length %d, got %d", len(expected), len(result))
	}
	for i := range expected {
		if result[i] != expected[i] {
			t.Errorf("at index %d: expected %s, got %s", i, expected[i], result[i])
		}
	}
}

type mockTagLister struct {
	contacts map[uuid.UUID][]Contact
	err      error
}

func (m *mockTagLister) ListContactsByTag(ctx context.Context, workspaceID, tagID uuid.UUID) ([]Contact, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.contacts[tagID], nil
}

func TestResolveTagRecipients(t *testing.T) {
	ctx := context.Background()
	wsID := uuid.New()
	tag1 := uuid.New()
	tag2 := uuid.New()

	c1ID := uuid.New()
	c2ID := uuid.New()
	c3ID := uuid.New()

	mock := &mockTagLister{
		contacts: map[uuid.UUID][]Contact{
			tag1: {
				{
					ID:   c1ID,
					Name: "Alice",
					Identities: []ContactIdentity{
						{Channel: "whatsapp", SenderIdentity: "+5511999998888"},
					},
				},
				{
					ID:         c2ID,
					Name:       "5511977776666", // Name looks like phone, but no identities
					Identities: nil,
				},
			},
			tag2: {
				{
					ID:   c3ID,
					Name: "Alice Duplicate Phone",
					Identities: []ContactIdentity{
						{Channel: "whatsapp", SenderIdentity: "5511999998888"}, // Duplicate phone of c1
					},
				},
			},
		},
	}

	res, err := ResolveTagRecipients(ctx, mock, wsID, []uuid.UUID{tag1, tag2}, "whatsapp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// c1 should be resolved as pending
	// c2 should be resolved as skipped (no identities)
	// c3 should be skipped by deduplication against c1
	if len(res.Records) != 2 {
		t.Fatalf("expected 2 records resolved (1 pending, 1 skipped), got %d", len(res.Records))
	}
	if len(res.Recipients) != 1 {
		t.Fatalf("expected 1 recipient resolved, got %d", len(res.Recipients))
	}
	if res.Records[0].Phone != "5511999998888" || res.Records[0].Status != RecipientStatusPending {
		t.Errorf("expected c1 to be pending with 5511999998888, got phone %s, status %s", res.Records[0].Phone, res.Records[0].Status)
	}
	if res.Records[1].Status != RecipientStatusSkipped {
		t.Errorf("expected c2 to be skipped, got status %s", res.Records[1].Status)
	}
	if !res.SeenIdentities["5511999998888"] {
		t.Errorf("expected seenIdentities['5511999998888'] to be true")
	}
}

func TestResolveTagRecipients_ChannelFilter(t *testing.T) {
	ctx := context.Background()
	wsID := uuid.New()
	tag1 := uuid.New()

	c1ID := uuid.New()
	c2ID := uuid.New()

	mock := &mockTagLister{
		contacts: map[uuid.UUID][]Contact{
			tag1: {
				{
					ID:   c1ID,
					Name: "Alice",
					Identities: []ContactIdentity{
						{Channel: "whatsapp", SenderIdentity: "+5511999998888"},
						{Channel: "telegram", SenderIdentity: "alice_tg"},
					},
				},
				{
					ID:   c2ID,
					Name: "Bob",
					Identities: []ContactIdentity{
						{Channel: "telegram", SenderIdentity: "5521988887777"},
					},
				},
			},
		},
	}

	// Filter by whatsapp:
	// - Alice matches whatsapp -> Status: RecipientStatusPending
	// - Bob lacks whatsapp identity -> Status: RecipientStatusSkipped
	// - recipients slice contains ONLY Alice
	res, err := ResolveTagRecipients(ctx, mock, wsID, []uuid.UUID{tag1}, "whatsapp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(res.Records) != 2 {
		t.Fatalf("expected 2 records (1 pending, 1 skipped), got %d", len(res.Records))
	}
	if res.Records[0].Phone != "5511999998888" || res.Records[0].Status != RecipientStatusPending {
		t.Errorf("expected records[0] to be Alice pending, got phone %s, status %s", res.Records[0].Phone, res.Records[0].Status)
	}
	if res.Records[1].Phone != "5521988887777" || res.Records[1].Status != RecipientStatusSkipped {
		t.Errorf("expected records[1] to be Bob skipped, got phone %s, status %s", res.Records[1].Phone, res.Records[1].Status)
	}
	if len(res.Recipients) != 1 {
		t.Fatalf("expected 1 recipient for batch dispatch, got %d", len(res.Recipients))
	}
	if res.Recipients[0].To != "5511999998888" {
		t.Errorf("expected recipient Alice, got %s", res.Recipients[0].To)
	}
	if !res.SeenIdentities["5511999998888"] || !res.SeenIdentities["5521988887777"] {
		t.Errorf("expected seenIdentities to contain both Alice and Bob's identities")
	}

	// Empty filter — both should match as pending if they have valid phones
	res2, err := ResolveTagRecipients(ctx, mock, wsID, []uuid.UUID{tag1}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res2.Records) != 2 {
		t.Fatalf("expected 2 records for empty filter, got %d", len(res2.Records))
	}
	if len(res2.Recipients) != 2 {
		t.Fatalf("expected 2 recipients for empty filter, got %d", len(res2.Recipients))
	}
}

func TestResolveTagRecipients_SkippedNoIdentities(t *testing.T) {
	ctx := context.Background()
	wsID := uuid.New()
	tag1 := uuid.New()

	c1ID := uuid.New()
	email := "carol@example.com"

	mock := &mockTagLister{
		contacts: map[uuid.UUID][]Contact{
			tag1: {
				{
					ID:         c1ID,
					Name:       "Carol",
					Email:      &email,
					Identities: nil,
				},
			},
		},
	}

	res, err := ResolveTagRecipients(ctx, mock, wsID, []uuid.UUID{tag1}, "whatsapp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(res.Records) != 1 {
		t.Fatalf("expected 1 skipped record, got %d", len(res.Records))
	}
	if res.Records[0].Status != RecipientStatusSkipped {
		t.Errorf("expected status skipped, got %s", res.Records[0].Status)
	}
	if res.Records[0].Phone != "carol@example.com" {
		t.Errorf("expected identity carol@example.com, got %s", res.Records[0].Phone)
	}
	if len(res.Recipients) != 0 {
		t.Errorf("expected 0 recipients, got %d", len(res.Recipients))
	}
	if !res.SeenIdentities["carol@example.com"] {
		t.Errorf("expected seenIdentities to contain carol@example.com")
	}
}

func TestResolveTagRecipients_CustomAttributes(t *testing.T) {
	ctx := context.Background()
	wsID := uuid.New()
	tag1 := uuid.New()

	c1ID := uuid.New()
	c2ID := uuid.New()

	mock := &mockTagLister{
		contacts: map[uuid.UUID][]Contact{
			tag1: {
				{
					ID:   c1ID,
					Name: "Alice",
					Attributes: map[string]string{
						"city":     "São Paulo",
						"tier":     "VIP",
						"discount": "20%",
					},
					Identities: []ContactIdentity{
						{Channel: "whatsapp", SenderIdentity: "+5511999998888"},
					},
				},
				{
					ID:   c2ID,
					Name: "Bob",
					Attributes: map[string]string{
						"plan": "Enterprise",
					},
					Identities: nil, // Skipped
				},
			},
		},
	}

	res, err := ResolveTagRecipients(ctx, mock, wsID, []uuid.UUID{tag1}, "whatsapp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(res.Records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(res.Records))
	}
	if len(res.Recipients) != 1 {
		t.Fatalf("expected 1 recipient, got %d", len(res.Recipients))
	}

	// Verify Alice (pending) variables merge name and attributes
	aliceRecord := res.Records[0]
	if aliceRecord.Variables["name"] != "Alice" {
		t.Errorf("expected variable name 'Alice', got %q", aliceRecord.Variables["name"])
	}
	if aliceRecord.Variables["city"] != "São Paulo" {
		t.Errorf("expected variable city 'São Paulo', got %q", aliceRecord.Variables["city"])
	}
	if aliceRecord.Variables["tier"] != "VIP" {
		t.Errorf("expected variable tier 'VIP', got %q", aliceRecord.Variables["tier"])
	}
	if aliceRecord.Variables["discount"] != "20%" {
		t.Errorf("expected variable discount '20%%', got %q", aliceRecord.Variables["discount"])
	}

	aliceRecipient := res.Recipients[0]
	if aliceRecipient.Variables["name"] != "Alice" || aliceRecipient.Variables["city"] != "São Paulo" {
		t.Errorf("expected alice recipient variables to have name and city, got %v", aliceRecipient.Variables)
	}

	// Verify Bob (skipped) variables also merge name and attributes
	bobRecord := res.Records[1]
	if bobRecord.Variables["name"] != "Bob" {
		t.Errorf("expected variable name 'Bob', got %q", bobRecord.Variables["name"])
	}
	if bobRecord.Variables["plan"] != "Enterprise" {
		t.Errorf("expected variable plan 'Enterprise', got %q", bobRecord.Variables["plan"])
	}
}

func TestMergeTagAndCSVRecipients(t *testing.T) {
	tagRes := TagResolutionResult{
		Records: []CampaignRecipientRecord{
			{
				Phone:     "5511999998888",
				Status:    RecipientStatusPending,
				Variables: map[string]string{"name": "Alice", "city": "SP"},
			},
			{
				Phone:     "5511977776666",
				Status:    RecipientStatusSkipped,
				Variables: map[string]string{"name": "Bob"},
			},
		},
		Recipients: []CampaignRecipient{
			{
				To:        "5511999998888",
				Variables: map[string]string{"name": "Alice", "city": "SP"},
			},
		},
		SeenIdentities: map[string]bool{
			"5511999998888": true,
			"5511977776666": true,
		},
	}

	csvRecipients := []CampaignRecipient{
		// CSV overrides Alice's discount variable
		{
			To:        "5511999998888",
			Variables: map[string]string{"discount": "30%"},
		},
		// CSV provides valid phone for skipped Bob
		{
			To:        "5511977776666",
			Variables: map[string]string{"coupon": "WELCOME"},
		},
		// CSV introduces new recipient Carol
		{
			To:        "+55 (11) 95555-4444",
			Variables: map[string]string{"name": "Carol"},
		},
	}

	allRecords, mergedRecipients := MergeTagAndCSVRecipients(tagRes, csvRecipients)

	if len(allRecords) != 3 {
		t.Fatalf("expected 3 total records, got %d", len(allRecords))
	}
	if len(mergedRecipients) != 3 {
		t.Fatalf("expected 3 merged active recipients, got %d", len(mergedRecipients))
	}

	// Alice: variables merged
	if allRecords[0].Variables["discount"] != "30%" || allRecords[0].Variables["city"] != "SP" {
		t.Errorf("Alice variables not merged properly: %v", allRecords[0].Variables)
	}

	// Bob: status changed from Skipped to Pending
	if allRecords[1].Status != RecipientStatusPending || allRecords[1].Variables["coupon"] != "WELCOME" {
		t.Errorf("Bob status or variables not updated properly: %v, status: %s", allRecords[1].Variables, allRecords[1].Status)
	}

	// Carol: added as pending
	if allRecords[2].Phone != "5511955554444" || allRecords[2].Status != RecipientStatusPending {
		t.Errorf("Carol not added as sanitized pending recipient: %v", allRecords[2])
	}
}

func TestInterpolateInteractive_Buttons(t *testing.T) {
	tmpl := &Interactive{
		Type: "button",
		Header: &TextContent{Text: "Aviso {{nome}}"},
		Body:   TextContent{Text: "Olá {{nome}}, seu plano {{plano}} está ativo."},
		Footer: &TextContent{Text: "Cupom: {{cupom}}"},
		Action: Action{
			Buttons: []Button{
				{
					Type: "reply",
					Reply: Reply{
						ID:    "btn_{{id}}",
						Title: "Ver {{plano}}",
					},
				},
				{
					Type: "reply",
					Reply: Reply{
						ID:    "opt_out",
						Title: "Cancelar",
					},
				},
			},
		},
	}

	vars := map[string]string{
		"nome":  "Carlos",
		"plano": "Premium",
		"cupom": "OFF50",
		"id":    "123",
	}

	interpolated := InterpolateInteractive(tmpl, vars)

	if interpolated.Header == nil || interpolated.Header.Text != "Aviso Carlos" {
		t.Errorf("expected header 'Aviso Carlos', got %v", interpolated.Header)
	}
	if interpolated.Body.Text != "Olá Carlos, seu plano Premium está ativo." {
		t.Errorf("expected body interpolation, got %s", interpolated.Body.Text)
	}
	if interpolated.Footer == nil || interpolated.Footer.Text != "Cupom: OFF50" {
		t.Errorf("expected footer 'Cupom: OFF50', got %v", interpolated.Footer)
	}
	if len(interpolated.Action.Buttons) != 2 {
		t.Fatalf("expected 2 buttons, got %d", len(interpolated.Action.Buttons))
	}
	if interpolated.Action.Buttons[0].Reply.ID != "btn_123" {
		t.Errorf("expected button 1 ID 'btn_123', got %s", interpolated.Action.Buttons[0].Reply.ID)
	}
	if interpolated.Action.Buttons[0].Reply.Title != "Ver Premium" {
		t.Errorf("expected button 1 Title 'Ver Premium', got %s", interpolated.Action.Buttons[0].Reply.Title)
	}

	// Verify original tmpl was not mutated
	if tmpl.Body.Text != "Olá {{nome}}, seu plano {{plano}} está ativo." {
		t.Errorf("original template was mutated: %s", tmpl.Body.Text)
	}
}

func TestInterpolateInteractive_Lists(t *testing.T) {
	tmpl := &Interactive{
		Type: "list",
		Header: &TextContent{Text: "Cardápio para {{nome}}"},
		Body:   TextContent{Text: "Escolha seu item em {{cidade}}:"},
		Action: Action{
			Button: "Menu {{dia}}",
			Sections: []Section{
				{
					Title: "Pratos {{categoria}}",
					Rows: []Row{
						{
							ID:          "item_{{codigo}}",
							Title:       "Combo {{nome_combo}}",
							Description: "Preço especial para {{cidade}}: R$ {{preco}}",
						},
					},
				},
			},
		},
	}

	vars := map[string]string{
		"nome":       "Beatriz",
		"cidade":     "Curitiba",
		"dia":        "Hoje",
		"categoria":  "Almoço",
		"codigo":     "42",
		"nome_combo": "Executivo",
		"preco":      "29,90",
	}

	interpolated := InterpolateInteractive(tmpl, vars)

	if interpolated.Header.Text != "Cardápio para Beatriz" {
		t.Errorf("expected header 'Cardápio para Beatriz', got %s", interpolated.Header.Text)
	}
	if interpolated.Action.Button != "Menu Hoje" {
		t.Errorf("expected action button 'Menu Hoje', got %s", interpolated.Action.Button)
	}
	sec := interpolated.Action.Sections[0]
	if sec.Title != "Pratos Almoço" {
		t.Errorf("expected section title 'Pratos Almoço', got %s", sec.Title)
	}
	row := sec.Rows[0]
	if row.ID != "item_42" || row.Title != "Combo Executivo" || row.Description != "Preço especial para Curitiba: R$ 29,90" {
		t.Errorf("row not interpolated correctly: %+v", row)
	}
}

func TestInterpolateInteractive_FlowPayload(t *testing.T) {
	tmpl := &Interactive{
		Type: "flow",
		Body: TextContent{Text: "Preencha o formulário {{nome}}"},
		Action: Action{
			FlowID:     "flow_12345",
			FlowToken:  "token_{{user_id}}",
			FlowCTA:    "Iniciar {{servico}}",
			FlowAction: "data_exchange",
			FlowActionPayload: map[string]interface{}{
				"screen": "REGISTER",
				"data": map[string]interface{}{
					"customer_name": "{{nome}}",
					"account_id":    "acc_{{user_id}}",
					"tags":          []interface{}{"vip", "{{tag}}"},
					"numeric_val":   100,
				},
			},
		},
	}

	vars := map[string]string{
		"nome":    "Diego",
		"user_id": "9988",
		"servico": "Cadastro",
		"tag":     "gold_tier",
	}

	interpolated := InterpolateInteractive(tmpl, vars)

	if interpolated.Action.FlowToken != "token_9988" {
		t.Errorf("expected flow token 'token_9988', got %s", interpolated.Action.FlowToken)
	}
	if interpolated.Action.FlowCTA != "Iniciar Cadastro" {
		t.Errorf("expected flow CTA 'Iniciar Cadastro', got %s", interpolated.Action.FlowCTA)
	}

	payload := interpolated.Action.FlowActionPayload
	if payload["screen"] != "REGISTER" {
		t.Errorf("expected screen REGISTER, got %v", payload["screen"])
	}
	data, ok := payload["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data map, got %T", payload["data"])
	}
	if data["customer_name"] != "Diego" {
		t.Errorf("expected customer_name Diego, got %v", data["customer_name"])
	}
	if data["account_id"] != "acc_9988" {
		t.Errorf("expected account_id acc_9988, got %v", data["account_id"])
	}
	if data["numeric_val"] != 100 {
		t.Errorf("expected numeric_val 100, got %v", data["numeric_val"])
	}
	tags, ok := data["tags"].([]interface{})
	if !ok || len(tags) != 2 || tags[0] != "vip" || tags[1] != "gold_tier" {
		t.Errorf("expected tags ['vip', 'gold_tier'], got %v", tags)
	}
}

func TestValidateInteractiveLimits(t *testing.T) {
	t.Run("Valid Button Message", func(t *testing.T) {
		msg := &Interactive{
			Type: "button",
			Body: TextContent{Text: "Texto válido"},
			Action: Action{
				Buttons: []Button{
					{Type: "reply", Reply: Reply{ID: "1", Title: "Aceitar"}},
					{Type: "reply", Reply: Reply{ID: "2", Title: "12345678901234567890"}}, // 20 chars
				},
			},
		}
		if err := ValidateInteractiveLimits(msg); err != nil {
			t.Errorf("expected valid, got %v", err)
		}
	})

	t.Run("Exceeded Button Title Limit (>20 chars)", func(t *testing.T) {
		msg := &Interactive{
			Type: "button",
			Body: TextContent{Text: "Texto válido"},
			Action: Action{
				Buttons: []Button{
					{Type: "reply", Reply: Reply{ID: "1", Title: "123456789012345678901"}}, // 21 chars
				},
			},
		}
		if err := ValidateInteractiveLimits(msg); err == nil {
			t.Error("expected error for button title > 20 chars, got nil")
		}
	})

	t.Run("Exceeded Button Count (>3 buttons)", func(t *testing.T) {
		msg := &Interactive{
			Type: "button",
			Body: TextContent{Text: "Texto válido"},
			Action: Action{
				Buttons: []Button{
					{Type: "reply", Reply: Reply{ID: "1", Title: "B1"}},
					{Type: "reply", Reply: Reply{ID: "2", Title: "B2"}},
					{Type: "reply", Reply: Reply{ID: "3", Title: "B3"}},
					{Type: "reply", Reply: Reply{ID: "4", Title: "B4"}},
				},
			},
		}
		if err := ValidateInteractiveLimits(msg); err == nil {
			t.Error("expected error for button count > 3, got nil")
		}
	})

	t.Run("Exceeded List Row Title Limit (>24 chars)", func(t *testing.T) {
		msg := &Interactive{
			Type: "list",
			Body: TextContent{Text: "Texto"},
			Action: Action{
				Button: "Ver",
				Sections: []Section{
					{
						Title: "Seção",
						Rows: []Row{
							{ID: "1", Title: "1234567890123456789012345"}, // 25 chars
						},
					},
				},
			},
		}
		if err := ValidateInteractiveLimits(msg); err == nil {
			t.Error("expected error for list row title > 24 chars, got nil")
		}
	})

	t.Run("Exceeded Flow CTA Limit (>20 chars)", func(t *testing.T) {
		msg := &Interactive{
			Type: "flow",
			Body: TextContent{Text: "Texto"},
			Action: Action{
				FlowID:  "123",
				FlowCTA: "Abrir Formulário Agora Mesmo", // 28 chars
			},
		}
		if err := ValidateInteractiveLimits(msg); err == nil {
			t.Error("expected error for flow CTA > 20 chars, got nil")
		}
	})
}

