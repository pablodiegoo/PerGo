package domain

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

func TestSessionKey(t *testing.T) {
	wsID := uuid.New()
	key := NewSessionKey(wsID, "+5511999990001", "whatsapp_cloud", "+5511888880001")

	if key.WorkspaceID != wsID {
		t.Errorf("got WorkspaceID %v, want %v", key.WorkspaceID, wsID)
	}
	if key.RecipientPhone != "+5511999990001" {
		t.Errorf("got RecipientPhone %s, want +5511999990001", key.RecipientPhone)
	}
	if key.Channel != "whatsapp_cloud" {
		t.Errorf("got Channel %s, want whatsapp_cloud", key.Channel)
	}
	if key.RecipientIdentity != "+5511888880001" {
		t.Errorf("got RecipientIdentity %s, want +5511888880001", key.RecipientIdentity)
	}

	// Test JSON serialization
	data, err := json.Marshal(key)
	if err != nil {
		t.Fatalf("failed to marshal SessionKey: %v", err)
	}

	var unmarshaled SessionKey
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("failed to unmarshal SessionKey: %v", err)
	}

	if unmarshaled != key {
		t.Errorf("unmarshaled %+v does not match original %+v", unmarshaled, key)
	}
}
