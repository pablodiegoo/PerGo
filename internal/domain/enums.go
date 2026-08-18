package domain

// Channel represents an omnichannel messaging provider type.
type Channel string

const (
	ChannelWhatsApp      Channel = "whatsapp"
	ChannelWhatsAppCloud Channel = "whatsapp_cloud"
	ChannelTelegram      Channel = "telegram"
	ChannelInstagram     Channel = "instagram"
	ChannelEmail         Channel = "email"
	ChannelEmailSES      Channel = "email_ses"
	ChannelEmailSMTP     Channel = "email_smtp"
	ChannelEmailMautic   Channel = "email_mautic"
)

// AllChannels returns the list of all supported channels.
func AllChannels() []Channel {
	return []Channel{
		ChannelWhatsApp,
		ChannelWhatsAppCloud,
		ChannelTelegram,
		ChannelInstagram,
		ChannelEmail,
		ChannelEmailSES,
		ChannelEmailSMTP,
		ChannelEmailMautic,
	}
}

// String returns the string representation of the channel.
func (c Channel) String() string {
	return string(c)
}

// IsValid reports whether the channel is recognized by PerGo.
func (c Channel) IsValid() bool {
	switch c {
	case ChannelWhatsApp,
		ChannelWhatsAppCloud,
		ChannelTelegram,
		ChannelInstagram,
		ChannelEmail,
		ChannelEmailSES,
		ChannelEmailSMTP,
		ChannelEmailMautic:
		return true
	default:
		return false
	}
}

// FallbackBehavior defines strategy when a channel dispatch fails.
type FallbackBehavior string

const (
	FallbackBehaviorDegrade FallbackBehavior = "degrade"
	FallbackBehaviorFail    FallbackBehavior = "fail"
)

// String returns string representation of FallbackBehavior.
func (f FallbackBehavior) String() string {
	return string(f)
}

// IsValid reports whether the fallback behavior is supported.
func (f FallbackBehavior) IsValid() bool {
	return f == FallbackBehaviorDegrade || f == FallbackBehaviorFail
}

// IsValid reports whether the MessageStatus is a known state.
func (s MessageStatus) IsValid() bool {
	switch s {
	case StatusQueued, StatusSent, StatusDelivered, StatusRead, StatusFailed:
		return true
	default:
		return false
	}
}

// IsValid reports whether the CampaignStatus is a valid lifecycle state.
func (s CampaignStatus) IsValid() bool {
	switch s {
	case CampaignStatusDraft,
		CampaignStatusScheduled,
		CampaignStatusSending,
		CampaignStatusRunning,
		CampaignStatusPaused,
		CampaignStatusCompleted,
		CampaignStatusFailed,
		CampaignStatusCancelled:
		return true
	default:
		return false
	}
}

// IsValid reports whether the RecipientStatus is a valid recipient dispatch status.
func (s RecipientStatus) IsValid() bool {
	switch s {
	case RecipientStatusPending,
		RecipientStatusProcessing,
		RecipientStatusSent,
		RecipientStatusFailed,
		RecipientStatusSkipped:
		return true
	default:
		return false
	}
}

// IsValid reports whether the EventType is a known event type.
func (e EventType) IsValid() bool {
	switch e {
	case EventTypeFlowCompleted, EventTypeOrderCreated:
		return true
	default:
		return false
	}
}
