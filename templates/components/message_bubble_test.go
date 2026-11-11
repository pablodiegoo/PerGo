package components_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/pablojhp.pergo/templates/components"
)

func TestMessageBubble_InboundOrderSummary(t *testing.T) {
	orderEv := domain.OrderCreatedEvent{
		OrderID:   "wamid_order_123",
		CatalogID: "cat_9999",
		Items: []domain.OrderProductItem{
			{
				ProductRetailerID: "PROD_A",
				Quantity:          2,
				ItemPrice:         49.90,
				Currency:          "BRL",
			},
			{
				ProductRetailerID: "PROD_B",
				Quantity:          1,
				ItemPrice:         50.10,
				Currency:          "BRL",
			},
		},
		TotalPrice: 149.90,
		Currency:   "BRL",
		Wamid:      "wamid_order_123",
	}

	orderJSON, err := json.Marshal(orderEv)
	if err != nil {
		t.Fatalf("failed to marshal order event: %v", err)
	}

	msg := repository.ThreadMessage{
		ID:        uuid.New(),
		TraceID:   "trace_order_001",
		Direction: "inbound",
		Body:      "🛒 Order Received (Catalog: cat_9999)\nNote: Por favor entregar no portão lateral.\nItems:\n- PROD_A: 2 x 49.90 BRL\n- PROD_B: 1 x 50.10 BRL\nTotal: 149.90 BRL",
		CreatedAt: time.Now().UTC(),
		Metadata: map[string]string{
			"type":          "order",
			"order_json":    string(orderJSON),
			"customer_note": "Por favor entregar no portão lateral.",
		},
	}

	var buf bytes.Buffer
	err = components.MessageBubble(msg).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("failed to render MessageBubble: %v", err)
	}

	html := buf.String()

	expectedSubstrings := []string{
		"Pedido do Catálogo",
		"cat_9999",
		"Nota do Cliente",
		"Por favor entregar no portão lateral.",
		"PROD_A",
		"PROD_B",
		"149.90",
		"BRL",
	}

	for _, expected := range expectedSubstrings {
		if !strings.Contains(html, expected) {
			t.Errorf("expected HTML output to contain %q, got:\n%s", expected, html)
		}
	}
}

func TestMessageBubble_OutboundProductCard_Single(t *testing.T) {
	productPayload := domain.ProductPayload{
		CatalogID:         "cat_1234",
		ProductRetailerID: "SKU_PROD_100",
		Header:            "Oferta Especial",
		Body:              "Confira este produto incrível!",
		Footer:            "Estoque limitado",
	}

	productJSON, err := json.Marshal(productPayload)
	if err != nil {
		t.Fatalf("failed to marshal product payload: %v", err)
	}

	sentStatus := "sent"
	msg := repository.ThreadMessage{
		ID:        uuid.New(),
		TraceID:   "trace_prod_001",
		Direction: "outbound",
		Body:      string(productJSON),
		CreatedAt: time.Now().UTC(),
		Status:    &sentStatus,
		Metadata: map[string]string{
			"type":         "product",
			"product_json": string(productJSON),
		},
	}

	var buf bytes.Buffer
	err = components.MessageBubble(msg).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("failed to render MessageBubble: %v", err)
	}

	html := buf.String()

	expectedSubstrings := []string{
		"Catálogo de Produtos",
		"cat_1234",
		"Oferta Especial",
		"Confira este produto incrível!",
		"SKU_PROD_100",
		"Estoque limitado",
	}

	for _, expected := range expectedSubstrings {
		if !strings.Contains(html, expected) {
			t.Errorf("expected HTML output to contain %q, got:\n%s", expected, html)
		}
	}
}

func TestMessageBubble_OutboundProductCard_Multi(t *testing.T) {
	productPayload := domain.ProductPayload{
		CatalogID: "cat_5678",
		Header:    "Nosso Cardápio",
		Body:      "Escolha seus itens favoritos abaixo:",
		Footer:    "Pedidos até as 22h",
		Sections: []domain.ProductSection{
			{
				Title: "Sobremesas",
				ProductItems: []domain.ProductItem{
					{ProductRetailerID: "SKU_BOLO", ItemPrice: 15.00, Currency: "BRL"},
					{ProductRetailerID: "SKU_TORTA", ItemPrice: 18.50, Currency: "BRL"},
				},
			},
			{
				Title: "Bebidas",
				ProductItems: []domain.ProductItem{
					{ProductRetailerID: "SKU_SUCO", ItemPrice: 8.00, Currency: "BRL"},
				},
			},
		},
	}

	productJSON, err := json.Marshal(productPayload)
	if err != nil {
		t.Fatalf("failed to marshal product payload: %v", err)
	}

	deliveredStatus := "delivered"
	msg := repository.ThreadMessage{
		ID:        uuid.New(),
		TraceID:   "trace_prod_list_001",
		Direction: "outbound",
		Body:      string(productJSON),
		CreatedAt: time.Now().UTC(),
		Status:    &deliveredStatus,
		Metadata: map[string]string{
			"type":         "product_list",
			"product_json": string(productJSON),
		},
	}

	var buf bytes.Buffer
	err = components.MessageBubble(msg).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("failed to render MessageBubble: %v", err)
	}

	html := buf.String()

	expectedSubstrings := []string{
		"Catálogo de Produtos",
		"cat_5678",
		"Nosso Cardápio",
		"2 Seção(ões) de Produtos",
		"Sobremesas",
		"SKU_BOLO",
		"SKU_TORTA",
		"Bebidas",
		"SKU_SUCO",
	}

	for _, expected := range expectedSubstrings {
		if !strings.Contains(html, expected) {
			t.Errorf("expected HTML output to contain %q, got:\n%s", expected, html)
		}
	}
}

func TestMessageBubble_StandardMessages(t *testing.T) {
	// Standard Inbound
	inboundMsg := repository.ThreadMessage{
		ID:        uuid.New(),
		Direction: "inbound",
		Body:      "Olá, gostaria de tirar uma dúvida.",
		CreatedAt: time.Now().UTC(),
	}

	var bufIn bytes.Buffer
	if err := components.MessageBubble(inboundMsg).Render(context.Background(), &bufIn); err != nil {
		t.Fatalf("failed to render inbound message: %v", err)
	}
	if !strings.Contains(bufIn.String(), "Olá, gostaria de tirar uma dúvida.") {
		t.Errorf("inbound message missing text body")
	}

	// Standard Outbound
	readStatus := "read"
	outboundMsg := repository.ThreadMessage{
		ID:        uuid.New(),
		Direction: "outbound",
		Body:      "Claro, como posso ajudar?",
		CreatedAt: time.Now().UTC(),
		Status:    &readStatus,
	}

	var bufOut bytes.Buffer
	if err := components.MessageBubble(outboundMsg).Render(context.Background(), &bufOut); err != nil {
		t.Fatalf("failed to render outbound message: %v", err)
	}
	if !strings.Contains(bufOut.String(), "Claro, como posso ajudar?") {
		t.Errorf("outbound message missing text body")
	}
}

func TestMessageBubble_InternalNote(t *testing.T) {
	noteMsg := repository.ThreadMessage{
		ID:        uuid.New(),
		UID:       "note_draft_uid_123",
		TraceID:   "note_draft_uid_123",
		Direction: "internal_note",
		Body:      "Proposta de resposta gerada pela IA aguardando aprovação humana.",
		CreatedAt: time.Now().UTC(),
		Metadata: map[string]string{
			"author_name": "Claude Agent",
			"is_private":  "true",
		},
	}

	var buf bytes.Buffer
	if err := components.MessageBubble(noteMsg).Render(context.Background(), &buf); err != nil {
		t.Fatalf("failed to render internal note: %v", err)
	}

	html := buf.String()
	expectedSubstrings := []string{
		"bg-amber-500/10",
		"border-amber-300",
		"Nota Interna",
		"Claude Agent",
		"Proposta de resposta gerada pela IA aguardando aprovação humana.",
		"Approve &amp; Send",
		"btn-approve-send",
		"btn-use-reply",
		"data-draft=\"Proposta de resposta gerada pela IA aguardando aprovação humana.\"",
		"data-draft-uid=\"note_draft_uid_123\"",
		"chat-draft-uid",
	}

	for _, expected := range expectedSubstrings {
		if !strings.Contains(html, expected) {
			t.Errorf("expected HTML to contain %q, but got:\n%s", expected, html)
		}
	}

	// Verify ChatPanel includes the hidden draft_uid input inside #chat-send-form
	contact := &domain.Contact{
		ID:   uuid.New(),
		Name: "Test Contact",
	}
	var panelBuf bytes.Buffer
	if err := components.ChatPanel(contact, nil, nil, []repository.ThreadMessage{noteMsg}, false).Render(context.Background(), &panelBuf); err != nil {
		t.Fatalf("failed to render ChatPanel: %v", err)
	}
	panelHTML := panelBuf.String()
	if !strings.Contains(panelHTML, `id="chat-draft-uid"`) || !strings.Contains(panelHTML, `name="draft_uid"`) {
		t.Errorf("expected ChatPanel #chat-send-form to contain hidden input id=\"chat-draft-uid\" name=\"draft_uid\", got:\n%s", panelHTML)
	}
}

func TestMessageBubble_MediaAttachmentsAndTranscript(t *testing.T) {
	wsID := uuid.New()
	audioURL := "/media/" + wsID.String() + "/hash_voice_01.ogg"
	audioType := "voice"

	// 1. Audio without transcript renders <audio>, /admin/media/ URL, "Transcrever áudio" HTMX button, and NO fallback placeholder
	untranscribed := repository.ThreadMessage{
		ID:        uuid.New(),
		UID:       "msg_voice_01",
		Direction: "inbound",
		Body:      "",
		MediaURL:  &audioURL,
		MediaType: &audioType,
		CreatedAt: time.Now().UTC(),
		Metadata: map[string]string{
			"duration_ms": "4200",
		},
	}

	var buf1 bytes.Buffer
	if err := components.MessageBubble(untranscribed).Render(context.Background(), &buf1); err != nil {
		t.Fatalf("render untranscribed audio bubble: %v", err)
	}
	html1 := buf1.String()
	for _, want := range []string{
		`<audio controls preload="metadata"`,
		`/admin/media/` + wsID.String() + `/hash_voice_01.ogg`,
		`Transcrever áudio`,
		`/admin/inbox/messages/msg_voice_01/transcribe`,
		`0:04`,
	} {
		if !strings.Contains(html1, want) {
			t.Errorf("expected untranscribed audio HTML to contain %q, got:\n%s", want, html1)
		}
	}
	if strings.Contains(html1, "[mídia ou mensagem especial]") {
		t.Errorf("expected empty-body audio bubble NOT to render fallback placeholder, got:\n%s", html1)
	}

	// 2. Audio with transcript renders inline transcript box and provider
	transcribed := untranscribed
	transcribed.Metadata = map[string]string{
		"transcript":          "Bom dia, preciso reagendar nossa reunião para quinta.",
		"transcript_provider": "gemini",
	}
	var buf2 bytes.Buffer
	if err := components.MessageBubble(transcribed).Render(context.Background(), &buf2); err != nil {
		t.Fatalf("render transcribed audio bubble: %v", err)
	}
	html2 := buf2.String()
	if !strings.Contains(html2, "Bom dia, preciso reagendar nossa reunião para quinta.") {
		t.Errorf("expected inline transcript in audio bubble, got:\n%s", html2)
	}
	if strings.Contains(html2, "Transcrever áudio") {
		t.Errorf("expected 'Transcrever áudio' button to be hidden when transcript is present")
	}

	// 3. Image bubble renders clickable thumbnail rewritten to /admin/media/
	imgURL := "/media/" + wsID.String() + "/photo_01.jpg"
	imgType := "image"
	imgMsg := repository.ThreadMessage{
		ID:        uuid.New(),
		UID:       "msg_img_01",
		Direction: "outbound",
		MediaURL:  &imgURL,
		MediaType: &imgType,
		CreatedAt: time.Now().UTC(),
	}
	var buf3 bytes.Buffer
	if err := components.MessageBubble(imgMsg).Render(context.Background(), &buf3); err != nil {
		t.Fatalf("render image bubble: %v", err)
	}
	html3 := buf3.String()
	if !strings.Contains(html3, `<img`) || !strings.Contains(html3, `/admin/media/`+wsID.String()+`/photo_01.jpg`) {
		t.Errorf("expected image thumbnail with /admin/media/ URL, got:\n%s", html3)
	}
	if strings.Contains(html3, "[mídia ou mensagem especial]") {
		t.Errorf("expected image-only bubble NOT to render fallback placeholder")
	}

	// 4. Document bubble renders Paper-Calm download chip
	docURL := "/media/" + wsID.String() + "/proposal.pdf"
	docType := "document"
	docMsg := repository.ThreadMessage{
		ID:        uuid.New(),
		UID:       "msg_doc_01",
		Direction: "inbound",
		MediaURL:  &docURL,
		MediaType: &docType,
		CreatedAt: time.Now().UTC(),
		Metadata:  map[string]string{"filename": "proposta-comercial.pdf"},
	}
	var buf4 bytes.Buffer
	if err := components.MessageBubble(docMsg).Render(context.Background(), &buf4); err != nil {
		t.Fatalf("render document bubble: %v", err)
	}
	html4 := buf4.String()
	if !strings.Contains(html4, "proposta-comercial.pdf") || !strings.Contains(html4, "download") {
		t.Errorf("expected document download chip with filename, got:\n%s", html4)
	}
}

func TestChatPanel_ComposerToggleAttachmentAndEditableCRM(t *testing.T) {
	contact := &domain.Contact{
		ID:          uuid.New(),
		Name:        "Ana Carolina",
		Attributes:  map[string]string{"plan": "solo"},
		CreatedAt:   time.Now().UTC(),
		LastMessage: time.Now().UTC(),
	}
	var buf bytes.Buffer
	if err := components.ChatPanel(contact, nil, nil, nil, false).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render ChatPanel: %v", err)
	}
	html := buf.String()

	for _, want := range []string{
		`Nota Privada`,
		`Alt+N`,
		`setChatComposerMode`,
		`name="contact_id"`,
		`hx-encoding="multipart/form-data"`,
		`name="attachment"`,
		`/admin/inbox/contacts/` + contact.ID.String() + `/update`,
		`/admin/inbox/contacts/` + contact.ID.String() + `/attributes`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("expected ChatPanel HTML to contain %q", want)
		}
	}
}



