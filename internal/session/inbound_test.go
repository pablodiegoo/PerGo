package session_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	waEvents "go.mau.fi/whatsmeow/types/events"

	"github.com/pablojhp.pergo/internal/inbound"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/pablojhp.pergo/internal/session"
)

type mockInboundProcessor struct {
	processedEvents []*inbound.InboundEvent
}

func (m *mockInboundProcessor) Process(ctx context.Context, ev *inbound.InboundEvent) error {
	m.processedEvents = append(m.processedEvents, ev)
	return nil
}

func TestWhatsAppInbound_GroupTextMessage(t *testing.T) {
	proc := &mockInboundProcessor{}
	mgr := session.NewManager(nil, nil, nil, nil, "2.3000.1025000000", proc)

	groupJID, _ := types.ParseJID("120363024823904@g.us")
	senderJID, _ := types.ParseJID("5511999991234@s.whatsapp.net")

	wsID := uuid.New()
	connID := uuid.New()
	conn := &repository.Connection{
		ID:             connID,
		WorkspaceID:    wsID,
		SenderIdentity: "+5511888880000",
		Channel:        "whatsapp",
	}

	text := "Hello everyone in the group!"
	evt := &waEvents.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat:     groupJID,
				Sender:   senderJID,
				IsGroup:  true,
				IsFromMe: false,
			},
			ID:       "wamid.group_msg_101",
			PushName: "Alice GroupMember",
		},
		Message: &waE2E.Message{
			Conversation: &text,
		},
	}

	mgr.HandleWhatsAppMessage(context.Background(), nil, conn, evt)

	if len(proc.processedEvents) != 1 {
		t.Fatalf("expected 1 processed event, got %d", len(proc.processedEvents))
	}

	ev := proc.processedEvents[0]
	if ev.WorkspaceID != wsID {
		t.Errorf("expected WorkspaceID %v, got %v", wsID, ev.WorkspaceID)
	}
	if ev.ConnectionID != connID {
		t.Errorf("expected ConnectionID %v, got %v", connID, ev.ConnectionID)
	}
	if ev.MessageID != "wamid.group_msg_101" {
		t.Errorf("expected MessageID 'wamid.group_msg_101', got %q", ev.MessageID)
	}
	if ev.Channel != "whatsapp" {
		t.Errorf("expected Channel 'whatsapp', got %q", ev.Channel)
	}
	// Acceptance criterion: IsGroup == true populates From with group JID
	if ev.From != "120363024823904@g.us" {
		t.Errorf("expected From to be group JID '120363024823904@g.us', got %q", ev.From)
	}
	if ev.To != "+5511888880000" {
		t.Errorf("expected To '+5511888880000', got %q", ev.To)
	}
	if ev.Body != text {
		t.Errorf("expected Body %q, got %q", text, ev.Body)
	}
	// Acceptance criterion: SenderName populated with push name
	if ev.SenderName != "Alice GroupMember" {
		t.Errorf("expected SenderName 'Alice GroupMember', got %q", ev.SenderName)
	}
	// Acceptance criterion: Metadata populated with is_group, participant, chat_jid, sender_push_name
	if ev.Metadata == nil {
		t.Fatal("expected Metadata map to be non-nil")
	}
	if ev.Metadata["is_group"] != "true" {
		t.Errorf("expected Metadata[is_group] == 'true', got %q", ev.Metadata["is_group"])
	}
	if ev.Metadata["participant"] != "5511999991234@s.whatsapp.net" {
		t.Errorf("expected Metadata[participant] == '5511999991234@s.whatsapp.net', got %q", ev.Metadata["participant"])
	}
	if ev.Metadata["chat_jid"] != "120363024823904@g.us" {
		t.Errorf("expected Metadata[chat_jid] == '120363024823904@g.us', got %q", ev.Metadata["chat_jid"])
	}
	if ev.Metadata["sender_push_name"] != "Alice GroupMember" {
		t.Errorf("expected Metadata[sender_push_name] == 'Alice GroupMember', got %q", ev.Metadata["sender_push_name"])
	}
}

func TestWhatsAppInbound_DirectMessage(t *testing.T) {
	proc := &mockInboundProcessor{}
	mgr := session.NewManager(nil, nil, nil, nil, "2.3000.1025000000", proc)

	senderJID, _ := types.ParseJID("5511999995555@s.whatsapp.net")

	wsID := uuid.New()
	connID := uuid.New()
	conn := &repository.Connection{
		ID:             connID,
		WorkspaceID:    wsID,
		SenderIdentity: "+5511888880000",
		Channel:        "whatsapp",
	}

	text := "Direct 1-on-1 hello"
	evt := &waEvents.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat:     senderJID,
				Sender:   senderJID,
				IsGroup:  false,
				IsFromMe: false,
			},
			ID:       "wamid.dm_msg_202",
			PushName: "Bob Direct",
		},
		Message: &waE2E.Message{
			Conversation: &text,
		},
	}

	mgr.HandleWhatsAppMessage(context.Background(), nil, conn, evt)

	if len(proc.processedEvents) != 1 {
		t.Fatalf("expected 1 processed event, got %d", len(proc.processedEvents))
	}

	ev := proc.processedEvents[0]
	// Acceptance criterion: 1-on-1 direct messages continue to populate From with individual sender JID
	if ev.From != "5511999995555@s.whatsapp.net" {
		t.Errorf("expected From '5511999995555@s.whatsapp.net', got %q", ev.From)
	}
	if ev.SenderName != "Bob Direct" {
		t.Errorf("expected SenderName 'Bob Direct', got %q", ev.SenderName)
	}
	// Direct messages should not have is_group == true
	if ev.Metadata != nil && ev.Metadata["is_group"] == "true" {
		t.Errorf("expected is_group not to be 'true' for direct messages, got %q", ev.Metadata["is_group"])
	}
}

func TestWhatsAppInbound_FromMeIgnored(t *testing.T) {
	proc := &mockInboundProcessor{}
	mgr := session.NewManager(nil, nil, nil, nil, "2.3000.1025000000", proc)

	groupJID, _ := types.ParseJID("120363024823904@g.us")
	senderJID, _ := types.ParseJID("5511888880000@s.whatsapp.net")

	conn := &repository.Connection{
		ID:             uuid.New(),
		WorkspaceID:    uuid.New(),
		SenderIdentity: "+5511888880000",
		Channel:        "whatsapp",
	}

	text := "Outgoing message from me"
	evt := &waEvents.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat:     groupJID,
				Sender:   senderJID,
				IsGroup:  true,
				IsFromMe: true, // from me!
			},
			ID: "wamid.outgoing_101",
		},
		Message: &waE2E.Message{
			Conversation: &text,
		},
	}

	mgr.HandleWhatsAppMessage(context.Background(), nil, conn, evt)

	if len(proc.processedEvents) != 0 {
		t.Errorf("expected 0 events for IsFromMe == true, got %d", len(proc.processedEvents))
	}
}

func TestWhatsAppInbound_GroupMediaMessage(t *testing.T) {
	proc := &mockInboundProcessor{}
	mgr := session.NewManager(nil, nil, nil, nil, "2.3000.1025000000", proc)

	groupJID, _ := types.ParseJID("120363024823904@g.us")
	senderJID, _ := types.ParseJID("5511999991234@s.whatsapp.net")

	wsID := uuid.New()
	connID := uuid.New()
	conn := &repository.Connection{
		ID:             connID,
		WorkspaceID:    wsID,
		SenderIdentity: "+5511888880000",
		Channel:        "whatsapp",
	}

	caption := "Group image caption"
	evt := &waEvents.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat:     groupJID,
				Sender:   senderJID,
				IsGroup:  true,
				IsFromMe: false,
			},
			ID:       "wamid.group_img_303",
			PushName: "Carol",
		},
		Message: &waE2E.Message{
			ImageMessage: &waE2E.ImageMessage{
				Caption: &caption,
			},
		},
	}

	mgr.HandleWhatsAppMessage(context.Background(), nil, conn, evt)

	if len(proc.processedEvents) != 1 {
		t.Fatalf("expected 1 processed event, got %d", len(proc.processedEvents))
	}

	ev := proc.processedEvents[0]
	if ev.From != "120363024823904@g.us" {
		t.Errorf("expected From '120363024823904@g.us', got %q", ev.From)
	}
	if ev.Body != caption {
		t.Errorf("expected Body %q, got %q", caption, ev.Body)
	}
	if ev.Media == nil {
		t.Fatal("expected Media to be non-nil")
	}
	if ev.Media.MediaType != "image" {
		t.Errorf("expected MediaType 'image', got %q", ev.Media.MediaType)
	}
	if ev.Media.Caption != caption {
		t.Errorf("expected Caption %q, got %q", caption, ev.Media.Caption)
	}
	if ev.Metadata["is_group"] != "true" {
		t.Errorf("expected Metadata[is_group] == 'true', got %q", ev.Metadata["is_group"])
	}
}

