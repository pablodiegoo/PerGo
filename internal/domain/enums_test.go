package domain

import (
	"testing"
)

func TestChannel_Validation(t *testing.T) {
	validChannels := []Channel{
		ChannelWhatsApp,
		ChannelWhatsAppCloud,
		ChannelTelegram,
		ChannelInstagram,
		ChannelEmail,
		ChannelEmailSES,
		ChannelEmailSMTP,
		ChannelEmailMautic,
	}

	for _, ch := range validChannels {
		if !ch.IsValid() {
			t.Errorf("expected channel %q to be valid", ch)
		}
		if ch.String() != string(ch) {
			t.Errorf("expected channel string %q, got %q", string(ch), ch.String())
		}
		// Verify backward compatibility with ValidChannels map
		if !ValidChannels[string(ch)] {
			t.Errorf("expected channel %q to be present in ValidChannels map", ch)
		}
	}

	all := AllChannels()
	if len(all) != len(validChannels) {
		t.Errorf("expected %d channels in AllChannels(), got %d", len(validChannels), len(all))
	}

	invalid := Channel("sms_invalid")
	if invalid.IsValid() {
		t.Errorf("expected invalid channel to return false from IsValid()")
	}
}

func TestFallbackBehavior_Validation(t *testing.T) {
	if !FallbackBehaviorDegrade.IsValid() {
		t.Errorf("expected FallbackBehaviorDegrade to be valid")
	}
	if !FallbackBehaviorFail.IsValid() {
		t.Errorf("expected FallbackBehaviorFail to be valid")
	}
	if FallbackBehavior("retry").IsValid() {
		t.Errorf("expected unknown fallback behavior to be invalid")
	}
}

func TestMessageStatus_Validation(t *testing.T) {
	validStatuses := []MessageStatus{
		StatusQueued,
		StatusSent,
		StatusDelivered,
		StatusRead,
		StatusFailed,
	}
	for _, st := range validStatuses {
		if !st.IsValid() {
			t.Errorf("expected status %q to be valid", st)
		}
	}
	if MessageStatus("unknown").IsValid() {
		t.Errorf("expected unknown status to be invalid")
	}
}

func TestCampaignStatus_Validation(t *testing.T) {
	validCampaignStatuses := []CampaignStatus{
		CampaignStatusDraft,
		CampaignStatusScheduled,
		CampaignStatusSending,
		CampaignStatusRunning,
		CampaignStatusPaused,
		CampaignStatusCompleted,
		CampaignStatusFailed,
		CampaignStatusCancelled,
	}
	for _, cs := range validCampaignStatuses {
		if !cs.IsValid() {
			t.Errorf("expected campaign status %q to be valid", cs)
		}
	}
	if CampaignStatus("invalid_state").IsValid() {
		t.Errorf("expected invalid campaign status to return false")
	}
}

func TestRecipientStatus_Validation(t *testing.T) {
	validRecipientStatuses := []RecipientStatus{
		RecipientStatusPending,
		RecipientStatusProcessing,
		RecipientStatusSent,
		RecipientStatusFailed,
		RecipientStatusSkipped,
	}
	for _, rs := range validRecipientStatuses {
		if !rs.IsValid() {
			t.Errorf("expected recipient status %q to be valid", rs)
		}
	}
	if RecipientStatus("invalid").IsValid() {
		t.Errorf("expected invalid recipient status to return false")
	}
}

func TestEventType_Validation(t *testing.T) {
	if !EventTypeFlowCompleted.IsValid() {
		t.Errorf("expected EventTypeFlowCompleted to be valid")
	}
	if !EventTypeOrderCreated.IsValid() {
		t.Errorf("expected EventTypeOrderCreated to be valid")
	}
	if EventType("custom.event").IsValid() {
		t.Errorf("expected unknown EventType to return false")
	}
}
