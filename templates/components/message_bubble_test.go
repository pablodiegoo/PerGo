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
		"🛒 Pedido do Catálogo",
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
		"📦 Catálogo de Produtos",
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
		"📦 Catálogo de Produtos",
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
