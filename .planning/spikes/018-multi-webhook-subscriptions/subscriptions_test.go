package subscriptions

import (
	"testing"
)

// WebhookSubscription defines an endpoint subscribed to specific event types.
type WebhookSubscription struct {
	URL        string
	EventTypes []string // e.g. ["message.received", "message.sent", "*"]
}

// RouteEvent returns all subscription URLs that are registered for the given event type.
func RouteEvent(subs []WebhookSubscription, eventType string) []string {
	var urls []string
	for _, sub := range subs {
		matches := false
		for _, t := range sub.EventTypes {
			if t == eventType || t == "*" {
				matches = true
				break
			}
		}
		if matches {
			urls = append(urls, sub.URL)
		}
	}
	return urls
}

func TestRouteEvent(t *testing.T) {
	subs := []WebhookSubscription{
		{
			URL:        "https://api.crm.com/inbound-messages",
			EventTypes: []string{"message.received"},
		},
		{
			URL:        "https://api.crm.com/all-activity",
			EventTypes: []string{"*"},
		},
		{
			URL:        "https://api.crm.com/connection-status",
			EventTypes: []string{"connection.status"},
		},
	}

	t.Run("Routes only matching event types", func(t *testing.T) {
		urls := RouteEvent(subs, "message.received")
		if len(urls) != 2 {
			t.Fatalf("expected 2 URLs, got %d: %v", len(urls), urls)
		}
		if urls[0] != "https://api.crm.com/inbound-messages" || urls[1] != "https://api.crm.com/all-activity" {
			t.Errorf("got unexpected routed URLs: %v", urls)
		}
	})

	t.Run("Routes wildcard to any event", func(t *testing.T) {
		urls := RouteEvent(subs, "message.sent")
		if len(urls) != 1 {
			t.Fatalf("expected 1 URL (wildcard), got %d: %v", len(urls), urls)
		}
		if urls[0] != "https://api.crm.com/all-activity" {
			t.Errorf("got %s, want https://api.crm.com/all-activity", urls[0])
		}
	})

	t.Run("Routes connection status updates to correct target", func(t *testing.T) {
		urls := RouteEvent(subs, "connection.status")
		if len(urls) != 2 {
			t.Fatalf("expected 2 URLs, got %d: %v", len(urls), urls)
		}
		// Expect connection-status target and wildcard all-activity target
		foundStatus := false
		foundAll := false
		for _, url := range urls {
			if url == "https://api.crm.com/connection-status" {
				foundStatus = true
			}
			if url == "https://api.crm.com/all-activity" {
				foundAll = true
			}
		}
		if !foundStatus || !foundAll {
			t.Errorf("missing expected routed target(s) from: %v", urls)
		}
	})
}
