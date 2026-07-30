package domain

import (
	"testing"
)

func TestEventTypes(t *testing.T) {
	if EventTypeFlowCompleted != "flow.completed" {
		t.Errorf("Expected EventTypeFlowCompleted to be 'flow.completed', got '%s'", EventTypeFlowCompleted)
	}

	event := FlowCompletedEvent{
		Screen:    "test_screen",
		Data:      map[string]interface{}{"key": "value"},
		FlowToken: "token_123",
		ContactID: "contact_123",
		Wamid:     "wamid_123",
	}

	if event.Screen != "test_screen" {
		t.Errorf("Expected Screen to be 'test_screen', got '%s'", event.Screen)
	}
}

func TestOrderCreated_Config(t *testing.T) {
	if EventTypeOrderCreated != "order.created" {
		t.Errorf("Expected EventTypeOrderCreated to be 'order.created', got '%s'", EventTypeOrderCreated)
	}

	orderEvent := OrderCreatedEvent{
		OrderID:   "order_123",
		CatalogID: "cat_456",
		Items: []OrderProductItem{
			{
				ProductRetailerID: "sku_1",
				Quantity:          2,
				ItemPrice:         50.0,
				Currency:          "BRL",
			},
		},
		TotalPrice: 100.0,
		Currency:   "BRL",
		Wamid:      "wamid_789",
		ContactID:  "contact_123",
		TraceID:    "trace_abc",
	}

	if orderEvent.OrderID != "order_123" || len(orderEvent.Items) != 1 || orderEvent.Items[0].ProductRetailerID != "sku_1" {
		t.Errorf("unexpected OrderCreatedEvent layout: %+v", orderEvent)
	}
}

