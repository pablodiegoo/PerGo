package domain_test

import (
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
