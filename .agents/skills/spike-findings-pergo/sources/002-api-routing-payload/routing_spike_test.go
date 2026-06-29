package repository

import (
	"errors"
	"testing"

	"github.com/google/uuid"
)

// MockConnection represents a connection in the DB
type MockConnection struct {
	ID             uuid.UUID
	WorkspaceID    uuid.UUID
	Channel        string
	SenderIdentity string
	IsDefault      bool
}

// Request payload matching our proposed API change
type MockCreateMessageRequest struct {
	Channel          string
	From             string
	To               string
	Body             string
	FallbackChannels []string
}

// ResolveRoute simulates the API validation and routing resolution logic.
func ResolveRoute(
	workspaceID uuid.UUID,
	req *MockCreateMessageRequest,
	connections []MockConnection,
) (primaryID uuid.UUID, fallbackIDs []uuid.UUID, err error) {
	if req.To == "" {
		return uuid.Nil, nil, errors.New("field 'to' is required")
	}

	var primary MockConnection
	foundPrimary := false

	if req.From != "" {
		// 1. Resolve primary by From (SenderIdentity)
		for _, conn := range connections {
			if conn.WorkspaceID == workspaceID && conn.SenderIdentity == req.From {
				primary = conn
				foundPrimary = true
				break
			}
		}
		if !foundPrimary {
			return uuid.Nil, nil, errors.New("sender identity 'from' not found in workspace")
		}
	} else if req.Channel != "" {
		// 2. Resolve primary by Channel (use default)
		for _, conn := range connections {
			if conn.WorkspaceID == workspaceID && conn.Channel == req.Channel && conn.IsDefault {
				primary = conn
				foundPrimary = true
				break
			}
		}
		if !foundPrimary {
			return uuid.Nil, nil, errors.New("no default connection found for the specified channel")
		}
	} else {
		return uuid.Nil, nil, errors.New("either 'from' or 'channel' is required")
	}

	primaryID = primary.ID

	// 3. Resolve Fallback Channels
	for _, fbChan := range req.FallbackChannels {
		if fbChan == primary.Channel {
			return uuid.Nil, nil, errors.New("fallback channel cannot be the same as the primary channel")
		}
		
		// Find default connection for fallback channel
		foundFB := false
		for _, conn := range connections {
			if conn.WorkspaceID == workspaceID && conn.Channel == fbChan && conn.IsDefault {
				fallbackIDs = append(fallbackIDs, conn.ID)
				foundFB = true
				break
			}
		}
		if !foundFB {
			return uuid.Nil, nil, errors.New("no default connection found for fallback channel: " + fbChan)
		}
	}

	return primaryID, fallbackIDs, nil
}

func TestRoutingSpike(t *testing.T) {
	workspaceID := uuid.New()

	// Mock DB connections
	connWaba1 := MockConnection{ID: uuid.New(), WorkspaceID: workspaceID, Channel: "whatsapp_cloud", SenderIdentity: "+5511999990001", IsDefault: true}
	connWaba2 := MockConnection{ID: uuid.New(), WorkspaceID: workspaceID, Channel: "whatsapp_cloud", SenderIdentity: "+5511999990002", IsDefault: false}
	connTele1 := MockConnection{ID: uuid.New(), WorkspaceID: workspaceID, Channel: "telegram", SenderIdentity: "@pergo_support_bot", IsDefault: true}
	
	connections := []MockConnection{connWaba1, connWaba2, connTele1}

	// Case 1: Route by Channel (uses default)
	req1 := &MockCreateMessageRequest{
		Channel: "whatsapp_cloud",
		To:      "+5511988888888",
		Body:    "Test 1",
	}
	p1, fb1, err := ResolveRoute(workspaceID, req1, connections)
	if err != nil {
		t.Fatalf("Case 1 failed: %v", err)
	}
	if p1 != connWaba1.ID {
		t.Errorf("Case 1 got primary = %s, want %s (WABA Primary)", p1, connWaba1.ID)
	}
	if len(fb1) != 0 {
		t.Errorf("Case 1 got fallback count = %d, want 0", len(fb1))
	}

	// Case 2: Route by From (WABA Secondary)
	req2 := &MockCreateMessageRequest{
		From: "+5511999990002",
		To:   "+5511988888888",
		Body: "Test 2",
	}
	p2, fb2, err := ResolveRoute(workspaceID, req2, connections)
	if err != nil {
		t.Fatalf("Case 2 failed: %v", err)
	}
	if p2 != connWaba2.ID {
		t.Errorf("Case 2 got primary = %s, want %s (WABA Secondary)", p2, connWaba2.ID)
	}
	if len(fb2) != 0 {
		t.Errorf("Case 2 got fallback count = %d, want 0", len(fb2))
	}

	// Case 3: Route by From with Fallback Channel
	req3 := &MockCreateMessageRequest{
		From:             "@pergo_support_bot",
		To:               "chat_id_123",
		Body:             "Test 3",
		FallbackChannels: []string{"whatsapp_cloud"},
	}
	p3, fb3, err := ResolveRoute(workspaceID, req3, connections)
	if err != nil {
		t.Fatalf("Case 3 failed: %v", err)
	}
	if p3 != connTele1.ID {
		t.Errorf("Case 3 got primary = %s, want %s (Telegram Support)", p3, connTele1.ID)
	}
	if len(fb3) != 1 || fb3[0] != connWaba1.ID {
		t.Errorf("Case 3 got fallback = %v, want [%s] (WABA Primary)", fb3, connWaba1.ID)
	}

	// Case 4: Invalid 'from'
	req4 := &MockCreateMessageRequest{
		From: "+5511999999999", // non-existent
		To:   "+5511988888888",
		Body: "Test 4",
	}
	_, _, err = ResolveRoute(workspaceID, req4, connections)
	if err == nil {
		t.Error("expected error for non-existent From sender, got nil")
	}

	// Case 5: Empty Request (neither from nor channel)
	req5 := &MockCreateMessageRequest{
		To:   "+5511988888888",
		Body: "Test 5",
	}
	_, _, err = ResolveRoute(workspaceID, req5, connections)
	if err == nil {
		t.Error("expected error for missing both from and channel, got nil")
	}

	// Case 6: Fallback to same channel type
	req6 := &MockCreateMessageRequest{
		From:             "+5511999990001",
		To:               "+5511988888888",
		Body:             "Test 6",
		FallbackChannels: []string{"whatsapp_cloud"},
	}
	_, _, err = ResolveRoute(workspaceID, req6, connections)
	if err == nil {
		t.Error("expected error when fallback channel type matches primary channel type, got nil")
	}
}
