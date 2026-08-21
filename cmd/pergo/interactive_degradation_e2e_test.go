package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/channel"
	tgpkg "github.com/pablojhp.pergo/internal/channel/telegram"
	wapkg "github.com/pablojhp.pergo/internal/channel/whatsapp"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/platform/crypto"
	"github.com/pablojhp.pergo/internal/platform/postgres/tenant"
	"github.com/pablojhp.pergo/internal/platform/queue"
	"github.com/pablojhp.pergo/internal/repository"
)

type fakeE2EDispatchMsg struct {
	acked bool
	naked bool
	data  []byte
}

func (m *fakeE2EDispatchMsg) Data() []byte {
	return m.data
}

func (m *fakeE2EDispatchMsg) Headers() map[string]string {
	return nil
}

func (m *fakeE2EDispatchMsg) Ack() error {
	m.acked = true
	return nil
}

func (m *fakeE2EDispatchMsg) NakWithDelay(delay time.Duration) error {
	m.naked = true
	return nil
}

func TestInteractiveDegradation_E2E(t *testing.T) {
	pool := getTestPool(t)
	if pool == nil {
		t.Skip("skipping: PostgreSQL not available")
	}

	ctx := context.Background()
	kek := make([]byte, 32)
	for i := range kek {
		kek[i] = byte(i + 1)
	}
	enc, err := crypto.NewEncryptor(kek)
	if err != nil {
		t.Fatalf("failed to create encryptor: %v", err)
	}

	wsRepo := repository.NewWorkspaceRepository(pool)
	connRepo := repository.NewConnectionRepository(pool, enc)
	contactRepo := repository.NewContactRepository(pool)
	dispatchRepo := repository.NewMessageDispatchRepository(pool)

	ws, err := wsRepo.Create(ctx, "interactive_e2e_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	// 1. Setup mock Meta Server for WABA
	var lastWABAReq map[string]any
	wabaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, _ := io.ReadAll(r.Body)
		var req map[string]any
		_ = json.Unmarshal(bodyBytes, &req)
		lastWABAReq = req

		w.WriteHeader(http.StatusOK)
		respJSON := fmt.Sprintf(`{"messages":[{"id":"wamid.mock_e2e_%s"}]}`, uuid.New().String())
		_, _ = w.Write([]byte(respJSON))
	}))
	defer wabaServer.Close()

	// 2. Setup mock Telegram Server
	var lastTGReq map[string]any
	tgServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, _ := io.ReadAll(r.Body)
		var req map[string]any
		_ = json.Unmarshal(bodyBytes, &req)
		lastTGReq = req

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":998877}}`))
	}))
	defer tgServer.Close()

	// 3. Create Connections in DB
	wabaSenderIdentity := fmt.Sprintf("+551188%06d", time.Now().UnixNano()%1000000)
	tgSenderIdentity := fmt.Sprintf("@pergo_e2e_%06d_bot", time.Now().UnixNano()%1000000)

	wabaCreds, _ := json.Marshal(wapkg.WABAConfig{
		PhoneNumberID: "123456789",
		Token:         "waba_token_e2e",
	})
	wabaConn := &repository.Connection{
		ID:             uuid.New(),
		WorkspaceID:    ws.ID,
		Name:           "WABA E2E",
		Channel:        "whatsapp_cloud",
		SenderIdentity: wabaSenderIdentity,
		Credentials:    wabaCreds,
		Status:         "connected",
	}
	if err := connRepo.Create(ctx, wabaConn); err != nil {
		t.Fatalf("failed to create WABA connection: %v", err)
	}

	tgCreds, _ := json.Marshal(tgpkg.TelegramConfig{
		Token: "123456:tg_token_e2e",
	})
	tgConn := &repository.Connection{
		ID:             uuid.New(),
		WorkspaceID:    ws.ID,
		Name:           "Telegram E2E",
		Channel:        "telegram",
		SenderIdentity: tgSenderIdentity,
		Credentials:    tgCreds,
		Status:         "connected",
	}
	if err := connRepo.Create(ctx, tgConn); err != nil {
		t.Fatalf("failed to create Telegram connection: %v", err)
	}

	// 4. Setup Adapters
	wabaAdapter := wapkg.NewWABAAdapter(connRepo, wabaServer.Client(), nil, "")
	wabaAdapter.SetBaseURL(wabaServer.URL)

	tgAdapter := tgpkg.NewTelegramAdapter(connRepo, tgServer.Client(), nil)
	tgAdapter.SetBaseURL(tgServer.URL)

	// Mock WhatsApp Web Adapter using fake Dispatcher
	var lastWAWebText string
	waWebMock := &mockWAWebDispatcher{
		onDispatch: func(m *channel.MessagePayload) (string, error) {
			if m.Interactive != nil {
				if m.Interactive.Type == "button" && len(m.Interactive.Action.Buttons) > 3 {
					if m.FallbackBehavior == string(domain.FallbackBehaviorFail) {
						return "", channel.NewTerminalError(fmt.Errorf("whatsapp: interactive message exceeds native limits (max 3 buttons) and fallback_behavior is fail"))
					}
					lastWAWebText = m.Interactive.DegradeToText()
					return `{"message_id":"wa_web_degraded_btn"}`, nil
				}
				if m.Interactive.Type == "list" && (m.Interactive.TotalRows() > 10 || len(m.Interactive.Action.Sections) > 10) {
					if m.FallbackBehavior == string(domain.FallbackBehaviorFail) {
						return "", channel.NewTerminalError(fmt.Errorf("whatsapp: interactive list exceeds native limits (max 10 rows) and fallback_behavior is fail"))
					}
					lastWAWebText = m.Interactive.DegradeToText()
					return `{"message_id":"wa_web_degraded_list"}`, nil
				}
				if m.Interactive.Type == "flow" {
					if m.FallbackBehavior == string(domain.FallbackBehaviorFail) {
						return "", channel.NewTerminalError(fmt.Errorf("whatsapp: interactive flow is not supported on whatsapp web and fallback_behavior is fail"))
					}
					lastWAWebText = m.Interactive.DegradeToText()
					return `{"message_id":"wa_web_degraded_flow"}`, nil
				}
			}
			return `{"message_id":"wa_web_msg_ok"}`, nil
		},
	}

	registry := channel.NewRegistry(map[string]channel.Dispatcher{
		"whatsapp_cloud": wabaAdapter,
		"whatsapp":       waWebMock,
		"telegram":       tgAdapter,
	})

	orchestrator := queue.NewDispatchOrchestrator(registry, dispatchRepo, nil, nil, nil, contactRepo, 3, 10*time.Second)

	// Setup Contact with multiple channel identities
	contact, err := contactRepo.CreateContact(ctx, ws.ID, "John Doe", nil, nil)
	if err != nil {
		t.Fatalf("failed to create contact: %v", err)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO contact_identities (contact_id, workspace_id, channel, sender_identity)
		VALUES ($1, $2, 'whatsapp_cloud', '5511999998888'),
		       ($1, $2, 'whatsapp', '5511999998888'),
		       ($1, $2, 'telegram', '12345678')
	`, contact.ID, ws.ID)
	if err != nil {
		t.Fatalf("failed to link identities: %v", err)
	}

	t.Run("WhatsApp Web Degrades >3 Buttons to Numbered Text Menu", func(t *testing.T) {
		lastWAWebText = ""
		traceID := "trace_wa_web_degrade_btn_" + uuid.New().String()
		qMsg := &domain.QueueMessage{
			WorkspaceID:      ws.ID,
			TraceID:          traceID,
			To:               "5511999998888",
			Channel:          "whatsapp",
			FallbackBehavior: "degrade",
			Interactive: &domain.Interactive{
				Type: "button",
				Body: domain.TextContent{Text: "Choose a tier:"},
				Action: domain.Action{
					Buttons: []domain.Button{
						{Type: "reply", Reply: domain.Reply{ID: "1", Title: "Bronze"}},
						{Type: "reply", Reply: domain.Reply{ID: "2", Title: "Silver"}},
						{Type: "reply", Reply: domain.Reply{ID: "3", Title: "Gold"}},
						{Type: "reply", Reply: domain.Reply{ID: "4", Title: "Platinum"}},
					},
				},
			},
		}

		msg := &fakeE2EDispatchMsg{}
		err := orchestrator.Process(tenant.WithWorkspaceID(ctx, ws.ID), msg, qMsg, 0)
		if err != nil {
			t.Fatalf("expected successful degrade on WhatsApp Web, got: %v", err)
		}
		if !strings.Contains(lastWAWebText, "4. Platinum") {
			t.Errorf("expected degraded text to contain 4. Platinum, got: %s", lastWAWebText)
		}
	})

	t.Run("WhatsApp Web Degrades >10 List Rows to Numbered Text Menu", func(t *testing.T) {
		lastWAWebText = ""
		var rows []domain.Row
		for i := 1; i <= 11; i++ {
			rows = append(rows, domain.Row{ID: fmt.Sprintf("row_%d", i), Title: fmt.Sprintf("Option %d", i)})
		}
		traceID := "trace_wa_web_degrade_list_" + uuid.New().String()
		qMsg := &domain.QueueMessage{
			WorkspaceID:      ws.ID,
			TraceID:          traceID,
			To:               "5511999998888",
			Channel:          "whatsapp",
			FallbackBehavior: "degrade",
			Interactive: &domain.Interactive{
				Type: "list",
				Body: domain.TextContent{Text: "Choose from list:"},
				Action: domain.Action{
					Button: "Options",
					Sections: []domain.Section{
						{Title: "Category 1", Rows: rows},
					},
				},
			},
		}

		msg := &fakeE2EDispatchMsg{}
		err := orchestrator.Process(tenant.WithWorkspaceID(ctx, ws.ID), msg, qMsg, 0)
		if err != nil {
			t.Fatalf("expected successful list degrade on WhatsApp Web, got: %v", err)
		}
		if !strings.Contains(lastWAWebText, "11. Option 11") {
			t.Errorf("expected degraded text to contain 11. Option 11, got: %s", lastWAWebText)
		}
	})

	t.Run("WABA Degrades >3 Buttons to Text Menu", func(t *testing.T) {
		lastWABAReq = nil
		traceID := "trace_waba_degrade_btn_" + uuid.New().String()
		qMsg := &domain.QueueMessage{
			WorkspaceID:      ws.ID,
			ConnectionID:     wabaConn.ID,
			TraceID:          traceID,
			To:               "5511999998888",
			Channel:          "whatsapp_cloud",
			FallbackBehavior: "degrade",
			Interactive: &domain.Interactive{
				Type: "button",
				Body: domain.TextContent{Text: "Select an option:"},
				Action: domain.Action{
					Buttons: []domain.Button{
						{Type: "reply", Reply: domain.Reply{ID: "1", Title: "One"}},
						{Type: "reply", Reply: domain.Reply{ID: "2", Title: "Two"}},
						{Type: "reply", Reply: domain.Reply{ID: "3", Title: "Three"}},
						{Type: "reply", Reply: domain.Reply{ID: "4", Title: "Four"}},
					},
				},
			},
		}

		msg := &fakeE2EDispatchMsg{}
		err := orchestrator.Process(tenant.WithWorkspaceID(ctx, ws.ID), msg, qMsg, 0)
		if err != nil {
			t.Fatalf("expected successful degrade on WABA, got: %v", err)
		}
		if !msg.acked {
			t.Error("expected msg to be acked")
		}
		if lastWABAReq["type"] != "text" {
			t.Errorf("expected WABA message type to be text, got: %v", lastWABAReq["type"])
		}
		textMap, _ := lastWABAReq["text"].(map[string]any)
		if textBody, _ := textMap["body"].(string); !strings.Contains(textBody, "4. Four") {
			t.Errorf("expected text body to contain 4. Four, got: %s", textBody)
		}
	})

	t.Run("Telegram Maps Interactive Reply Buttons to Inline Keyboard", func(t *testing.T) {
		lastTGReq = nil
		traceID := "trace_tg_buttons_" + uuid.New().String()
		qMsg := &domain.QueueMessage{
			WorkspaceID:  ws.ID,
			ConnectionID: tgConn.ID,
			TraceID:      traceID,
			To:           "12345678",
			Channel:      "telegram",
			Interactive: &domain.Interactive{
				Type: "button",
				Body: domain.TextContent{Text: "Choose a plan:"},
				Action: domain.Action{
					Buttons: []domain.Button{
						{Type: "reply", Reply: domain.Reply{ID: "plan_free", Title: "Free"}},
						{Type: "reply", Reply: domain.Reply{ID: "plan_pro", Title: "Pro"}},
					},
				},
			},
		}

		msg := &fakeE2EDispatchMsg{}
		err := orchestrator.Process(tenant.WithWorkspaceID(ctx, ws.ID), msg, qMsg, 0)
		if err != nil {
			t.Fatalf("expected successful send on Telegram, got: %v", err)
		}
		if !msg.acked {
			t.Error("expected msg to be acked")
		}

		replyMarkup, ok := lastTGReq["reply_markup"].(map[string]any)
		if !ok || replyMarkup == nil {
			t.Fatalf("expected reply_markup in Telegram request: %+v", lastTGReq)
		}
		inlineKb, ok := replyMarkup["inline_keyboard"].([]any)
		if !ok || len(inlineKb) != 2 {
			t.Fatalf("expected 2 inline keyboard buttons, got: %+v", inlineKb)
		}
	})

	t.Run("WABA Degrades >10 List Rows to Text Payload", func(t *testing.T) {
		lastWABAReq = nil
		var rows []domain.Row
		for i := 1; i <= 11; i++ {
			rows = append(rows, domain.Row{ID: fmt.Sprintf("id_%d", i), Title: fmt.Sprintf("Item %d", i)})
		}

		traceID := "trace_waba_degrade_list_" + uuid.New().String()
		qMsg := &domain.QueueMessage{
			WorkspaceID:      ws.ID,
			ConnectionID:     wabaConn.ID,
			TraceID:          traceID,
			To:               "5511999998888",
			Channel:          "whatsapp_cloud",
			FallbackBehavior: "degrade",
			Interactive: &domain.Interactive{
				Type: "list",
				Body: domain.TextContent{Text: "Choose item:"},
				Action: domain.Action{
					Button: "Menu",
					Sections: []domain.Section{
						{Title: "Section 1", Rows: rows},
					},
				},
			},
		}

		msg := &fakeE2EDispatchMsg{}
		err := orchestrator.Process(tenant.WithWorkspaceID(ctx, ws.ID), msg, qMsg, 0)
		if err != nil {
			t.Fatalf("expected successful degrade on WABA list, got: %v", err)
		}
		if lastWABAReq["type"] != "text" {
			t.Errorf("expected WABA message type to be text, got: %v", lastWABAReq["type"])
		}
	})

	t.Run("Telegram Degrades Interactive List to Inline Keyboard and Menu Text", func(t *testing.T) {
		lastTGReq = nil
		var rows []domain.Row
		for i := 1; i <= 3; i++ {
			rows = append(rows, domain.Row{ID: fmt.Sprintf("opt_%d", i), Title: fmt.Sprintf("Option %d", i)})
		}

		traceID := "trace_tg_list_degrade_" + uuid.New().String()
		qMsg := &domain.QueueMessage{
			WorkspaceID:      ws.ID,
			ConnectionID:     tgConn.ID,
			TraceID:          traceID,
			To:               "12345678",
			Channel:          "telegram",
			FallbackBehavior: "degrade",
			Interactive: &domain.Interactive{
				Type:   "list",
				Header: &domain.TextContent{Text: "Catalog"},
				Body:   domain.TextContent{Text: "Choose option:"},
				Action: domain.Action{
					Button: "Options",
					Sections: []domain.Section{
						{Title: "Category 1", Rows: rows},
					},
				},
			},
		}

		msg := &fakeE2EDispatchMsg{}
		err := orchestrator.Process(tenant.WithWorkspaceID(ctx, ws.ID), msg, qMsg, 0)
		if err != nil {
			t.Fatalf("expected successful send on Telegram list degrade, got: %v", err)
		}
		replyMarkup, ok := lastTGReq["reply_markup"].(map[string]any)
		if !ok || replyMarkup == nil {
			t.Fatalf("expected reply_markup in Telegram list degrade: %+v", lastTGReq)
		}
		inlineKb, ok := replyMarkup["inline_keyboard"].([]any)
		if !ok || len(inlineKb) != 3 {
			t.Fatalf("expected 3 inline keyboard buttons, got: %+v", inlineKb)
		}
	})

	t.Run("Telegram Fails on Interactive List when FallbackBehavior is fail", func(t *testing.T) {
		traceID := "trace_tg_list_fail_" + uuid.New().String()
		qMsg := &domain.QueueMessage{
			WorkspaceID:      ws.ID,
			ConnectionID:     tgConn.ID,
			TraceID:          traceID,
			To:               "12345678",
			Channel:          "telegram",
			FallbackBehavior: "fail",
			Interactive: &domain.Interactive{
				Type: "list",
				Body: domain.TextContent{Text: "Choose option:"},
				Action: domain.Action{
					Button: "Options",
					Sections: []domain.Section{
						{Title: "Category 1", Rows: []domain.Row{{ID: "1", Title: "Opt 1"}}},
					},
				},
			},
		}

		msg := &fakeE2EDispatchMsg{}
		err := orchestrator.Process(tenant.WithWorkspaceID(ctx, ws.ID), msg, qMsg, 0)
		if err == nil {
			t.Fatal("expected error on telegram list fail, got nil")
		}
	})

	t.Run("WhatsApp Web with >3 buttons and FallbackBehavior fail triggers Fallback Channel (Telegram)", func(t *testing.T) {
		lastTGReq = nil
		traceID := "trace_fallback_cascade_" + uuid.New().String()
		qMsg := &domain.QueueMessage{
			WorkspaceID:      ws.ID,
			ConnectionID:     tgConn.ID,
			TraceID:          traceID,
			To:               "5511999998888",
			Channel:          "whatsapp",
			FallbackChannels: []string{"telegram"},
			FallbackBehavior: "fail",
			Interactive: &domain.Interactive{
				Type: "button",
				Body: domain.TextContent{Text: "Select one:"},
				Action: domain.Action{
					Buttons: []domain.Button{
						{Type: "reply", Reply: domain.Reply{ID: "1", Title: "One"}},
						{Type: "reply", Reply: domain.Reply{ID: "2", Title: "Two"}},
						{Type: "reply", Reply: domain.Reply{ID: "3", Title: "Three"}},
						{Type: "reply", Reply: domain.Reply{ID: "4", Title: "Four"}},
					},
				},
			},
		}

		msg := &fakeE2EDispatchMsg{}
		err := orchestrator.Process(tenant.WithWorkspaceID(ctx, ws.ID), msg, qMsg, 0)
		if err != nil {
			t.Fatalf("expected successful fallback delivery to Telegram, got: %v", err)
		}
		if !msg.acked {
			t.Error("expected msg to be acked on fallback delivery")
		}

		// Verify Telegram received the message
		if lastTGReq == nil {
			t.Fatal("expected Telegram to receive fallback message")
		}
		if chatID, _ := lastTGReq["chat_id"].(string); chatID != "12345678" {
			t.Errorf("expected chat_id 12345678, got %s", chatID)
		}
	})

	t.Run("Terminal Failure when FallbackBehavior fail has no fallback channel", func(t *testing.T) {
		traceID := "trace_terminal_fail_" + uuid.New().String()
		qMsg := &domain.QueueMessage{
			WorkspaceID:      ws.ID,
			ConnectionID:     tgConn.ID,
			TraceID:          traceID,
			To:               "12345678",
			Channel:          "telegram",
			FallbackBehavior: "fail",
			Interactive: &domain.Interactive{
				Type: "flow",
				Body: domain.TextContent{Text: "Please fill this flow"},
				Action: domain.Action{
					FlowCTA: "Open Form",
					FlowID:  "flow_123",
				},
			},
		}

		msg := &fakeE2EDispatchMsg{}
		err := orchestrator.Process(tenant.WithWorkspaceID(ctx, ws.ID), msg, qMsg, 0)
		if err == nil {
			t.Fatal("expected terminal failure error, got nil")
		}
		if !msg.acked {
			t.Error("expected message to be acked (consumed) after exhausting fallback channels")
		}

		// Check dispatch status in DB
		d, err := dispatchRepo.GetOrCreateDispatch(ctx, ws.ID, traceID, "telegram", nil, nil, nil)
		if err != nil {
			t.Fatalf("failed to retrieve dispatch: %v", err)
		}
		if d.Status != "failed" {
			t.Errorf("expected dispatch status failed, got %s", d.Status)
		}
	})
}

type mockWAWebDispatcher struct {
	onDispatch func(m *channel.MessagePayload) (string, error)
}

func (m *mockWAWebDispatcher) Dispatch(ctx context.Context, p *channel.MessagePayload) (string, error) {
	if m.onDispatch != nil {
		return m.onDispatch(p)
	}
	return `{"message_id":"mock_wa_web"}`, nil
}
