package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/domain"
)

func TestAIEngagement_Effective(t *testing.T) {
	t0 := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	future := t0.Add(12 * time.Hour)
	past := t0.Add(-1 * time.Minute)

	tests := []struct {
		name        string
		state       domain.AIEngagement
		now         time.Time
		wantMode    domain.AIMode
		wantUntil   *time.Time
		wantEngaged bool
	}{
		{
			name:        "zero value is active and engaged",
			state:       domain.AIEngagement{},
			now:         t0,
			wantMode:    domain.AIModeActive,
			wantUntil:   nil,
			wantEngaged: true,
		},
		{
			name:        "explicit active is active and engaged",
			state:       domain.AIEngagement{Mode: domain.AIModeActive},
			now:         t0,
			wantMode:    domain.AIModeActive,
			wantUntil:   nil,
			wantEngaged: true,
		},
		{
			name:        "paused in future remains paused",
			state:       domain.AIEngagement{Mode: domain.AIModePaused, PausedUntil: &future},
			now:         t0,
			wantMode:    domain.AIModePaused,
			wantUntil:   &future,
			wantEngaged: false,
		},
		{
			name:        "paused at exact expiry reads as active",
			state:       domain.AIEngagement{Mode: domain.AIModePaused, PausedUntil: &future},
			now:         future,
			wantMode:    domain.AIModeActive,
			wantUntil:   nil,
			wantEngaged: true,
		},
		{
			name:        "paused in past reads as active without write",
			state:       domain.AIEngagement{Mode: domain.AIModePaused, PausedUntil: &past},
			now:         t0,
			wantMode:    domain.AIModeActive,
			wantUntil:   nil,
			wantEngaged: true,
		},
		{
			name:        "off stays off indefinitely past 12h",
			state:       domain.AIEngagement{Mode: domain.AIModeOff},
			now:         t0.Add(100 * time.Hour),
			wantMode:    domain.AIModeOff,
			wantUntil:   nil,
			wantEngaged: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			eff := tc.state.Effective(tc.now)
			if eff.Mode != tc.wantMode {
				t.Fatalf("Effective().Mode = %q, want %q", eff.Mode, tc.wantMode)
			}
			if (eff.PausedUntil == nil) != (tc.wantUntil == nil) {
				t.Fatalf("Effective().PausedUntil = %v, want %v", eff.PausedUntil, tc.wantUntil)
			}
			if eff.PausedUntil != nil && !eff.PausedUntil.Equal(*tc.wantUntil) {
				t.Fatalf("Effective().PausedUntil = %v, want %v", *eff.PausedUntil, *tc.wantUntil)
			}
			if eff.IsEngaged() != tc.wantEngaged {
				t.Fatalf("Effective().IsEngaged() = %v, want %v", eff.IsEngaged(), tc.wantEngaged)
			}
		})
	}
}

func TestApply_OutboundActivity_AIEngagementAndStateTransitions(t *testing.T) {
	wsID := uuid.New()
	chatID := uuid.New()
	contactID := uuid.New()
	t0 := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)

	baseState := domain.ChatState{
		ID:          chatID,
		WorkspaceID: wsID,
		ContactID:   contactID,
		Status:      domain.ChatStatusOpen,
		AI:          domain.AIEngagement{Mode: domain.AIModeActive},
	}

	t.Run("human_agent from active pauses for 12h and emits cancel_debounce + publish_handoff", func(t *testing.T) {
		next, msg, intents := domain.Apply(baseState, domain.OutboundActivity{
			At:         t0,
			UID:        "trace-human-1",
			SenderType: domain.SenderTypeHumanAgent,
			SenderName: "Agent Lois",
			SenderID:   "+551100009999",
			Body:       "Hello from Lois",
			Channel:    "whatsapp_cloud",
			To:         "+5511999991111",
		})

		wantUntil := t0.Add(12 * time.Hour)
		if next.AI.Mode != domain.AIModePaused || next.AI.PausedUntil == nil || !next.AI.PausedUntil.Equal(wantUntil) {
			t.Fatalf("next.AI = %+v, want paused until %v", next.AI, wantUntil)
		}
		if !next.LastMessageAt.Equal(t0) {
			t.Fatalf("next.LastMessageAt = %v, want %v", next.LastMessageAt, t0)
		}
		if msg == nil {
			t.Fatal("expected non-nil ChatMessage")
		}
		if msg.Direction != domain.DirectionOutbound || msg.SenderType != domain.SenderTypeHumanAgent {
			t.Fatalf("unexpected msg direction/sender_type: %+v", msg)
		}
		if msg.Metadata["status"] != "queued" {
			t.Fatalf("expected msg.Metadata[status] = 'queued', got %v", msg.Metadata["status"])
		}
		if !hasIntent(intents, domain.IntentCancelDebounce) || !hasIntent(intents, domain.IntentPublishHandoff) {
			t.Fatalf("expected both cancel_debounce and publish_handoff intents, got %+v", intents)
		}
	})

	t.Run("second human_agent reply while paused slides T to at+12h without re-emitting publish_handoff", func(t *testing.T) {
		initialUntil := t0.Add(12 * time.Hour)
		pausedState := baseState
		pausedState.AI = domain.AIEngagement{Mode: domain.AIModePaused, PausedUntil: &initialUntil}

		t1 := t0.Add(2 * time.Hour)
		next, _, intents := domain.Apply(pausedState, domain.OutboundActivity{
			At:         t1,
			UID:        "trace-human-2",
			SenderType: domain.SenderTypeHumanAgent,
			SenderName: "Agent Lois",
			Body:       "Follow-up from Lois",
		})

		wantUntil := t1.Add(12 * time.Hour)
		if next.AI.Mode != domain.AIModePaused || next.AI.PausedUntil == nil || !next.AI.PausedUntil.Equal(wantUntil) {
			t.Fatalf("next.AI = %+v, want paused until %v", next.AI, wantUntil)
		}
		if hasIntent(intents, domain.IntentPublishHandoff) {
			t.Fatalf("expected NO publish_handoff intent when already paused, got %+v", intents)
		}
	})

	t.Run("human_agent reply during 48h pause does not shorten T", func(t *testing.T) {
		pause48h := t0.Add(48 * time.Hour)
		paused48State := baseState
		paused48State.AI = domain.AIEngagement{Mode: domain.AIModePaused, PausedUntil: &pause48h}

		t1 := t0.Add(1 * time.Hour)
		next, _, intents := domain.Apply(paused48State, domain.OutboundActivity{
			At:         t1,
			UID:        "trace-human-48h",
			SenderType: domain.SenderTypeHumanAgent,
			Body:       "Checking in",
		})

		if next.AI.Mode != domain.AIModePaused || next.AI.PausedUntil == nil || !next.AI.PausedUntil.Equal(pause48h) {
			t.Fatalf("48h pause was shortened: got %+v, want paused until %v", next.AI, pause48h)
		}
		if hasIntent(intents, domain.IntentPublishHandoff) {
			t.Fatalf("expected no publish_handoff when already paused, got %+v", intents)
		}
	})

	t.Run("human_agent reply after pause expired fires handoff again", func(t *testing.T) {
		expiredUntil := t0.Add(12 * time.Hour)
		expiredState := baseState
		expiredState.AI = domain.AIEngagement{Mode: domain.AIModePaused, PausedUntil: &expiredUntil}

		tAfter := t0.Add(13 * time.Hour)
		next, _, intents := domain.Apply(expiredState, domain.OutboundActivity{
			At:         tAfter,
			UID:        "trace-human-after-expiry",
			SenderType: domain.SenderTypeHumanAgent,
			Body:       "New takeover after 13h",
		})

		wantUntil := tAfter.Add(12 * time.Hour)
		if next.AI.Mode != domain.AIModePaused || next.AI.PausedUntil == nil || !next.AI.PausedUntil.Equal(wantUntil) {
			t.Fatalf("next.AI = %+v, want paused until %v", next.AI, wantUntil)
		}
		if !hasIntent(intents, domain.IntentPublishHandoff) {
			t.Fatalf("expected publish_handoff on expired->paused edge, got %+v", intents)
		}
	})

	t.Run("human_agent reply when AI is off keeps off (off dominates) and emits no handoff", func(t *testing.T) {
		offState := baseState
		offState.AI = domain.AIEngagement{Mode: domain.AIModeOff}

		next, _, intents := domain.Apply(offState, domain.OutboundActivity{
			At:         t0,
			UID:        "trace-human-off",
			SenderType: domain.SenderTypeHumanAgent,
			Body:       "Reply while off",
		})

		if next.AI.Mode != domain.AIModeOff || next.AI.PausedUntil != nil {
			t.Fatalf("expected off to dominate, got %+v", next.AI)
		}
		if hasIntent(intents, domain.IntentPublishHandoff) {
			t.Fatalf("expected no publish_handoff when already off, got %+v", intents)
		}
	})

	t.Run("ai_agent reply reopens snoozed or closed chat, keeps AI active, and formats template body", func(t *testing.T) {
		snoozeUntil := t0.Add(4 * time.Hour)
		snoozedState := baseState
		snoozedState.Status = domain.ChatStatusSnoozed
		snoozedState.SnoozedUntil = &snoozeUntil

		replyUID := "parent-uid-1"
		next, msg, intents := domain.Apply(snoozedState, domain.OutboundActivity{
			At:           t0,
			UID:          "trace-ai-1",
			SenderType:   domain.SenderTypeAIAgent,
			SenderName:   "AI Bot",
			ReplyToUID:   &replyUID,
			TemplateName: "order_update",
			Language:     "pt_BR",
			MessageType:  "template",
		})

		if next.Status != domain.ChatStatusOpen || next.SnoozedUntil != nil {
			t.Fatalf("expected snoozed chat to reopen with nil SnoozedUntil, got status=%q snoozedUntil=%v", next.Status, next.SnoozedUntil)
		}
		if !next.AI.Effective(t0).IsEngaged() {
			t.Fatalf("expected AI to remain engaged on ai_agent send, got %+v", next.AI)
		}
		if len(intents) != 0 {
			t.Fatalf("expected 0 intents for ai_agent send, got %+v", intents)
		}
		if msg.Body != "[Template: order_update (pt_BR)]" {
			t.Fatalf("msg.Body = %q, want '[Template: order_update (pt_BR)]'", msg.Body)
		}
		if msg.ReplyToUID == nil || *msg.ReplyToUID != replyUID {
			t.Fatalf("msg.ReplyToUID = %v, want %q", msg.ReplyToUID, replyUID)
		}
	})
}

func TestApply_AIChangeActivity(t *testing.T) {
	wsID := uuid.New()
	chatID := uuid.New()
	contactID := uuid.New()
	t0 := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)

	baseState := domain.ChatState{
		ID:          chatID,
		WorkspaceID: wsID,
		ContactID:   contactID,
		Status:      domain.ChatStatusOpen,
		AI:          domain.AIEngagement{Mode: domain.AIModeActive},
	}

	t.Run("AIOff from active emits cancel_debounce and publish_handoff", func(t *testing.T) {
		next, msg, intents := domain.Apply(baseState, domain.AIChangeActivity{
			At:   t0,
			Mode: domain.AIModeOff,
		})
		if msg != nil {
			t.Fatalf("expected nil message for AIChangeActivity, got %+v", msg)
		}
		if next.AI.Mode != domain.AIModeOff || next.AI.PausedUntil != nil {
			t.Fatalf("expected AIModeOff, got %+v", next.AI)
		}
		if !hasIntent(intents, domain.IntentCancelDebounce) || !hasIntent(intents, domain.IntentPublishHandoff) {
			t.Fatalf("expected cancel_debounce and publish_handoff, got %+v", intents)
		}
	})

	t.Run("AIPauseUntil when off keeps off", func(t *testing.T) {
		offState := baseState
		offState.AI = domain.AIEngagement{Mode: domain.AIModeOff}

		until := t0.Add(2 * time.Hour)
		next, _, intents := domain.Apply(offState, domain.AIChangeActivity{
			At:          t0,
			Mode:        domain.AIModePaused,
			PausedUntil: &until,
		})
		if next.AI.Mode != domain.AIModeOff || next.AI.PausedUntil != nil {
			t.Fatalf("expected off to dominate AIPauseUntil, got %+v", next.AI)
		}
		if hasIntent(intents, domain.IntentPublishHandoff) {
			t.Fatalf("expected no publish_handoff when already off, got %+v", intents)
		}
	})

	t.Run("AIResume from off sets active", func(t *testing.T) {
		offState := baseState
		offState.AI = domain.AIEngagement{Mode: domain.AIModeOff}

		next, _, intents := domain.Apply(offState, domain.AIChangeActivity{
			At:   t0,
			Mode: domain.AIModeActive,
		})
		if next.AI.Mode != domain.AIModeActive || next.AI.PausedUntil != nil {
			t.Fatalf("expected AIModeActive, got %+v", next.AI)
		}
		if len(intents) != 0 {
			t.Fatalf("expected 0 intents on AIResume, got %+v", intents)
		}
	})
}

func hasIntent(intents []domain.Intent, kind domain.IntentKind) bool {
	for _, in := range intents {
		if in.Kind == kind {
			return true
		}
	}
	return false
}

func TestEffectiveStatus(t *testing.T) {
	t0 := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	future := t0.Add(2 * time.Hour)
	past := t0.Add(-1 * time.Minute)

	tests := []struct {
		name         string
		status       domain.ChatStatus
		snoozedUntil *time.Time
		now          time.Time
		want         domain.ChatStatus
	}{
		{
			name:         "empty status defaults to open",
			status:       "",
			snoozedUntil: nil,
			now:          t0,
			want:         domain.ChatStatusOpen,
		},
		{
			name:         "open stays open",
			status:       domain.ChatStatusOpen,
			snoozedUntil: nil,
			now:          t0,
			want:         domain.ChatStatusOpen,
		},
		{
			name:         "closed stays closed regardless of snoozed_until",
			status:       domain.ChatStatusClosed,
			snoozedUntil: &past,
			now:          t0,
			want:         domain.ChatStatusClosed,
		},
		{
			name:         "snoozed in future remains snoozed",
			status:       domain.ChatStatusSnoozed,
			snoozedUntil: &future,
			now:          t0,
			want:         domain.ChatStatusSnoozed,
		},
		{
			name:         "snoozed at exact expiry is open",
			status:       domain.ChatStatusSnoozed,
			snoozedUntil: &t0,
			now:          t0,
			want:         domain.ChatStatusOpen,
		},
		{
			name:         "snoozed in past is open",
			status:       domain.ChatStatusSnoozed,
			snoozedUntil: &past,
			now:          t0,
			want:         domain.ChatStatusOpen,
		},
		{
			name:         "snoozed with nil snoozed_until remains snoozed",
			status:       domain.ChatStatusSnoozed,
			snoozedUntil: nil,
			now:          t0,
			want:         domain.ChatStatusSnoozed,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := domain.EffectiveStatus(tc.status, tc.snoozedUntil, tc.now)
			if got != tc.want {
				t.Fatalf("EffectiveStatus(%q, %v, %v) = %q, want %q", tc.status, tc.snoozedUntil, tc.now, got, tc.want)
			}
		})
	}
}

func TestApply_InboundTurn(t *testing.T) {
	wsID := uuid.New()
	chatID := uuid.New()
	contactID := uuid.New()
	t0 := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)

	t.Run("reopens closed chat, increments unread by 1, advances timestamps and service window, preserves AI", func(t *testing.T) {
		pauseUntil := t0.Add(-1 * time.Hour) // expired pause
		state := domain.ChatState{
			ID:            chatID,
			WorkspaceID:   wsID,
			ContactID:     contactID,
			Status:        domain.ChatStatusClosed,
			UnreadCount:   2,
			LastMessageAt: t0.Add(-2 * time.Hour),
			AI:            domain.AIEngagement{Mode: domain.AIModePaused, PausedUntil: &pauseUntil},
		}

		replyTo := "msg-prev-1"
		next, msg, intents := domain.Apply(state, domain.InboundTurn{
			At:         t0,
			UID:        "wamid.inbound.1",
			SenderName: "Alice",
			SenderID:   "+5511999990000",
			ReplyToUID: &replyTo,
			Body:       "Need help!",
			Channel:    "whatsapp_cloud",
		})

		if next.Status != domain.ChatStatusOpen {
			t.Fatalf("next.Status = %q, want %q", next.Status, domain.ChatStatusOpen)
		}
		if next.SnoozedUntil != nil {
			t.Fatalf("next.SnoozedUntil = %v, want nil", next.SnoozedUntil)
		}
		if next.UnreadCount != 3 {
			t.Fatalf("next.UnreadCount = %d, want 3", next.UnreadCount)
		}
		if !next.LastMessageAt.Equal(t0) {
			t.Fatalf("next.LastMessageAt = %v, want %v", next.LastMessageAt, t0)
		}
		if next.LastInboundAt == nil || !next.LastInboundAt.Equal(t0) {
			t.Fatalf("next.LastInboundAt = %v, want %v", next.LastInboundAt, t0)
		}
		wantWindow := t0.Add(24 * time.Hour)
		if next.ServiceWindowExpiresAt == nil || !next.ServiceWindowExpiresAt.Equal(wantWindow) {
			t.Fatalf("next.ServiceWindowExpiresAt = %v, want %v", next.ServiceWindowExpiresAt, wantWindow)
		}
		// AI state must be preserved as-is (no reset write on inbound; resume is derived from time)
		if next.AI.Mode != domain.AIModePaused || next.AI.PausedUntil == nil || !next.AI.PausedUntil.Equal(pauseUntil) {
			t.Fatalf("expected stored AI to be preserved without reset write, got %+v", next.AI)
		}
		if !next.AI.Effective(t0).IsEngaged() {
			t.Fatalf("expected Effective(t0) on expired pause to be engaged")
		}
		if len(intents) != 0 {
			t.Fatalf("expected 0 intents for InboundTurn, got %+v", intents)
		}
		if msg == nil {
			t.Fatal("expected non-nil ChatMessage")
		}
		if msg.Direction != domain.DirectionInbound || msg.SenderType != domain.SenderTypeContact {
			t.Fatalf("msg direction/sender = %q/%q, want inbound/contact", msg.Direction, msg.SenderType)
		}
		if msg.UID != "wamid.inbound.1" || msg.Body != "Need help!" || msg.SenderName != "Alice" || msg.SenderID != "+5511999990000" {
			t.Fatalf("unexpected msg fields: %+v", msg)
		}
		if msg.ReplyToUID == nil || *msg.ReplyToUID != replyTo {
			t.Fatalf("msg.ReplyToUID = %v, want %q", msg.ReplyToUID, replyTo)
		}
	})

	t.Run("reopens snoozed chat and clears snoozed_until", func(t *testing.T) {
		snoozeFuture := t0.Add(6 * time.Hour)
		state := domain.ChatState{
			ID:           chatID,
			WorkspaceID:  wsID,
			ContactID:    contactID,
			Status:       domain.ChatStatusSnoozed,
			SnoozedUntil: &snoozeFuture,
			UnreadCount:  0,
			AI:           domain.AIEngagement{Mode: domain.AIModeOff},
		}

		next, _, _ := domain.Apply(state, domain.InboundTurn{
			At:   t0,
			UID:  "wamid.inbound.2",
			Body: "Wake up",
		})

		if next.Status != domain.ChatStatusOpen || next.SnoozedUntil != nil {
			t.Fatalf("expected snoozed chat to reopen with nil SnoozedUntil, got status=%q snoozed=%v", next.Status, next.SnoozedUntil)
		}
		if next.UnreadCount != 1 {
			t.Fatalf("next.UnreadCount = %d, want 1", next.UnreadCount)
		}
		if next.AI.Mode != domain.AIModeOff {
			t.Fatalf("expected AI off to be preserved, got %+v", next.AI)
		}
	})

	t.Run("AIChangeActivity normalizes expired snooze on transition", func(t *testing.T) {
		expiredSnooze := t0.Add(-10 * time.Minute)
		state := domain.ChatState{
			ID:           chatID,
			WorkspaceID:  wsID,
			ContactID:    contactID,
			Status:       domain.ChatStatusSnoozed,
			SnoozedUntil: &expiredSnooze,
			AI:           domain.AIEngagement{Mode: domain.AIModeActive},
		}

		next, _, _ := domain.Apply(state, domain.AIChangeActivity{
			At:   t0,
			Mode: domain.AIModeOff,
		})

		if next.Status != domain.ChatStatusOpen || next.SnoozedUntil != nil {
			t.Fatalf("expected expired snooze to be normalized to open on AIChangeActivity, got status=%q snoozed=%v", next.Status, next.SnoozedUntil)
		}
	})

	t.Run("CTWA inbound sets 72h window and subsequent standard inbound 1h later never shortens it", func(t *testing.T) {
		state := domain.ChatState{
			ID:          chatID,
			WorkspaceID: wsID,
			ContactID:   contactID,
			Status:      domain.ChatStatusOpen,
			AI:          domain.AIEngagement{Mode: domain.AIModeActive},
		}

		afterCTWA, _, _ := domain.Apply(state, domain.InboundTurn{
			At:             t0,
			UID:            "wamid.ctwa.1",
			Body:           "Clicked your ad",
			Channel:        "whatsapp_cloud",
			EntryPointType: domain.EntryPointCTWA,
		})

		want72 := t0.Add(72 * time.Hour)
		if afterCTWA.ServiceWindowExpiresAt == nil || !afterCTWA.ServiceWindowExpiresAt.Equal(want72) {
			t.Fatalf("after CTWA ServiceWindowExpiresAt = %v, want %v", afterCTWA.ServiceWindowExpiresAt, want72)
		}
		if afterCTWA.EntryPointType != domain.EntryPointCTWA {
			t.Fatalf("after CTWA EntryPointType = %q, want %q", afterCTWA.EntryPointType, domain.EntryPointCTWA)
		}

		// Standard message 1 hour later must not shorten the 72h CTWA window
		afterStandard, _, _ := domain.Apply(afterCTWA, domain.InboundTurn{
			At:             t0.Add(1 * time.Hour),
			UID:            "wamid.std.2",
			Body:           "Follow up",
			Channel:        "whatsapp_cloud",
			EntryPointType: domain.EntryPointStandard,
		})

		if afterStandard.ServiceWindowExpiresAt == nil || !afterStandard.ServiceWindowExpiresAt.Equal(want72) {
			t.Fatalf("after standard 1h later ServiceWindowExpiresAt = %v, want preserved %v", afterStandard.ServiceWindowExpiresAt, want72)
		}
		if afterStandard.EntryPointType != domain.EntryPointCTWA {
			t.Fatalf("after standard 1h later EntryPointType = %q, want preserved %q", afterStandard.EntryPointType, domain.EntryPointCTWA)
		}
	})

	t.Run("OutboundActivity sets LastOutboundAt without modifying ServiceWindowExpiresAt", func(t *testing.T) {
		state := domain.ChatState{
			ID:          chatID,
			WorkspaceID: wsID,
			ContactID:   contactID,
			Status:      domain.ChatStatusOpen,
			AI:          domain.AIEngagement{Mode: domain.AIModeActive},
		}

		next, _, _ := domain.Apply(state, domain.OutboundActivity{
			At:           t0,
			UID:          "trace-tmpl-1",
			SenderType:   domain.SenderTypeAIAgent,
			TemplateName: "welcome",
			Language:     "pt_BR",
			MessageType:  "template",
			Channel:      "whatsapp_cloud",
		})

		if next.ServiceWindowExpiresAt != nil {
			t.Fatalf("expected OutboundActivity not to open service window, got %v", next.ServiceWindowExpiresAt)
		}
		if next.LastOutboundAt == nil || !next.LastOutboundAt.Equal(t0) {
			t.Fatalf("expected LastOutboundAt = %v, got %v", t0, next.LastOutboundAt)
		}
	})
}

func TestExtendServiceWindow(t *testing.T) {
	t0 := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	prev72 := t0.Add(72 * time.Hour)
	prev10 := t0.Add(10 * time.Hour)

	tests := []struct {
		name       string
		prev       *time.Time
		at         time.Time
		entryPoint string
		want       time.Time
	}{
		{
			name:       "standard entry point with no prior window gives 24h",
			prev:       nil,
			at:         t0,
			entryPoint: "standard",
			want:       t0.Add(24 * time.Hour),
		},
		{
			name:       "empty entry point defaults to 24h",
			prev:       nil,
			at:         t0,
			entryPoint: "",
			want:       t0.Add(24 * time.Hour),
		},
		{
			name:       "ctwa entry point gives 72h",
			prev:       nil,
			at:         t0,
			entryPoint: "ctwa",
			want:       t0.Add(72 * time.Hour),
		},
		{
			name:       "standard turn 1h after ctwa never shortens 72h window",
			prev:       &prev72,
			at:         t0.Add(1 * time.Hour),
			entryPoint: "standard",
			want:       prev72,
		},
		{
			name:       "standard turn extends shorter prior window",
			prev:       &prev10,
			at:         t0,
			entryPoint: "standard",
			want:       t0.Add(24 * time.Hour),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := domain.ExtendServiceWindow(tc.prev, tc.at, tc.entryPoint)
			if !got.Equal(tc.want) {
				t.Fatalf("ExtendServiceWindow(%v, %v, %q) = %v, want %v", tc.prev, tc.at, tc.entryPoint, got, tc.want)
			}
		})
	}
}

func TestWindowAndRequireOpenWindow(t *testing.T) {
	t0 := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	futureExp := t0.Add(20 * time.Hour)
	pastExp := t0.Add(-2 * time.Hour)

	t.Run("no chat (exists=false) on whatsapp_cloud is closed and requires template", func(t *testing.T) {
		w := domain.Window(domain.ChatState{Channel: "whatsapp_cloud"}, false, t0)
		if w.Open {
			t.Fatalf("expected closed window when exists=false, got %+v", w)
		}
		err := domain.RequireOpenWindow(w, "text")
		var tmplErr *domain.TemplateRequiredError
		if !errors.As(err, &tmplErr) {
			t.Fatalf("expected *domain.TemplateRequiredError, got %T: %v", err, err)
		}
		if err := domain.RequireOpenWindow(w, "template"); err != nil {
			t.Fatalf("expected template message to be exempt, got %v", err)
		}
	})

	t.Run("existing chat with nil ServiceWindowExpiresAt is closed and requires template", func(t *testing.T) {
		w := domain.Window(domain.ChatState{Channel: "whatsapp_cloud"}, true, t0)
		if w.Open {
			t.Fatalf("expected closed window when ServiceWindowExpiresAt is nil, got %+v", w)
		}
		var tmplErr *domain.TemplateRequiredError
		if err := domain.RequireOpenWindow(w, "text"); !errors.As(err, &tmplErr) {
			t.Fatalf("expected *domain.TemplateRequiredError, got %T: %v", err, err)
		}
	})

	t.Run("existing chat with future ServiceWindowExpiresAt is open", func(t *testing.T) {
		w := domain.Window(domain.ChatState{
			Channel:                "whatsapp_cloud",
			ServiceWindowExpiresAt: &futureExp,
			EntryPointType:         domain.EntryPointCTWA,
		}, true, t0)
		if !w.Open {
			t.Fatalf("expected open window, got %+v", w)
		}
		if w.WindowDuration != 72*time.Hour {
			t.Fatalf("expected 72h duration for ctwa, got %v", w.WindowDuration)
		}
		if w.Remaining != 20*time.Hour {
			t.Fatalf("expected 20h remaining, got %v", w.Remaining)
		}
		if err := domain.RequireOpenWindow(w, "text"); err != nil {
			t.Fatalf("expected nil error for open window, got %v", err)
		}
	})

	t.Run("existing chat with past ServiceWindowExpiresAt is closed for text and exempt for template", func(t *testing.T) {
		w := domain.Window(domain.ChatState{
			Channel:                "whatsapp_cloud",
			ServiceWindowExpiresAt: &pastExp,
			EntryPointType:         domain.EntryPointStandard,
		}, true, t0)
		if w.Open {
			t.Fatalf("expected closed window, got %+v", w)
		}
		var tmplErr *domain.TemplateRequiredError
		if err := domain.RequireOpenWindow(w, "text"); !errors.As(err, &tmplErr) {
			t.Fatalf("expected *domain.TemplateRequiredError, got %T: %v", err, err)
		}
		if !tmplErr.Window.ExpiresAt.Equal(pastExp) {
			t.Fatalf("expected ExpiresAt %v on TemplateRequiredError, got %v", pastExp, tmplErr.Window.ExpiresAt)
		}
		if err := domain.RequireOpenWindow(w, "template"); err != nil {
			t.Fatalf("expected template message to be exempt, got %v", err)
		}
	})

	t.Run("non-whatsapp_cloud channels are always exempt", func(t *testing.T) {
		for _, ch := range []string{"whatsapp", "telegram"} {
			w := domain.Window(domain.ChatState{Channel: ch}, false, t0)
			if !w.Open {
				t.Fatalf("expected channel %q to report open window, got %+v", ch, w)
			}
			if err := domain.RequireOpenWindow(w, "text"); err != nil {
				t.Fatalf("expected channel %q to be exempt, got %v", ch, err)
			}
		}
	})
}

