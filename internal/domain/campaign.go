package domain

import (
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

// CampaignStatus represents the active lifecycle state of a campaign mailing.
type CampaignStatus string

const (
	CampaignStatusDraft     CampaignStatus = "draft"
	CampaignStatusScheduled CampaignStatus = "scheduled"
	CampaignStatusSending   CampaignStatus = "sending"
	CampaignStatusRunning   CampaignStatus = "running"
	CampaignStatusPaused    CampaignStatus = "paused"
	CampaignStatusCompleted CampaignStatus = "completed"
	CampaignStatusFailed    CampaignStatus = "failed"
	CampaignStatusCancelled CampaignStatus = "cancelled"
)

// RecipientStatus represents the status of an individual recipient message dispatch within a campaign.
type RecipientStatus string

const (
	RecipientStatusPending    RecipientStatus = "pending"
	RecipientStatusProcessing RecipientStatus = "processing"
	RecipientStatusSent       RecipientStatus = "sent"
	RecipientStatusFailed     RecipientStatus = "failed"
	RecipientStatusSkipped    RecipientStatus = "skipped"
)

// CampaignRecipient represents an in-memory recipient record from CSV/form inputs.
type CampaignRecipient struct {
	To        string            `json:"to"`
	Variables map[string]string `json:"variables"`
}

// CampaignRecipientRecord represents a persisted row in campaign_recipients table.
type CampaignRecipientRecord struct {
	ID           uuid.UUID         `json:"id"`
	CampaignID   uuid.UUID         `json:"campaign_id"`
	ContactID    *uuid.UUID        `json:"contact_id,omitempty"`
	Phone        string            `json:"phone"`
	Variables    map[string]string `json:"variables"`
	Status       RecipientStatus   `json:"status"`
	ErrorMessage *string           `json:"error_message,omitempty"`
	SentAt       *time.Time        `json:"sent_at,omitempty"`
	CreatedAt    time.Time         `json:"created_at"`
}

// SkippedRow details why a row from the mailing list CSV was ignored.
type SkippedRow struct {
	LineNumber int    `json:"line_number"`
	RawInput   string `json:"raw_input"`
	Reason     string `json:"reason"`
}

// Campaign represents a bulk mailing campaign model.
type Campaign struct {
	ID               uuid.UUID           `json:"id"`
	WorkspaceID      uuid.UUID           `json:"workspace_id"`
	ConnectionID     *uuid.UUID          `json:"connection_id,omitempty"`
	ConnectionSlug   *string             `json:"connection_slug,omitempty"`
	Name             string              `json:"name"`
	Status           CampaignStatus      `json:"status"`
	BatchSize        int                 `json:"batch_size"`
	DelaySeconds     int                 `json:"delay_seconds"`
	TemplateName     *string             `json:"template_name,omitempty"`
	MessageBody      *string             `json:"message_body,omitempty"`
	Channel          *string             `json:"channel,omitempty"`
	TagID            *uuid.UUID          `json:"tag_id,omitempty"`
	TotalRecipients  int                 `json:"total_recipients"`
	SentRecipients   int                 `json:"sent_recipients"`
	FailedRecipients int                 `json:"failed_recipients"`
	Recipients       []CampaignRecipient `json:"recipients,omitempty"`
	SkippedRows      []SkippedRow        `json:"skipped_rows,omitempty"`
	ScheduledAt      *time.Time          `json:"scheduled_at,omitempty"`
	CreatedAt        time.Time           `json:"created_at"`
	UpdatedAt        time.Time           `json:"updated_at"`
}

// SniffDelimiter checks the frequencies of commas, semicolons, and tabs to auto-detect a CSV delimiter.
func SniffDelimiter(firstLine string) rune {
	candidates := []rune{',', ';', '\t'}
	counts := make(map[rune]int)
	for _, char := range firstLine {
		for _, cand := range candidates {
			if char == cand {
				counts[cand]++
			}
		}
	}
	best := ','
	maxCount := 0
	for cand, count := range counts {
		if count > maxCount {
			maxCount = count
			best = cand
		}
	}
	return best
}

// SanitizePhone cleanses a phone number and validates that it falls within E.164 length constraints (10-15 digits).
func SanitizePhone(phone string) (string, bool) {
	// Strip non-digits
	var sb strings.Builder
	for _, r := range phone {
		if r >= '0' && r <= '9' {
			sb.WriteRune(r)
		}
	}
	cleaned := sb.String()
	length := len(cleaned)
	if length >= 10 && length <= 15 {
		return cleaned, true
	}
	return cleaned, false
}

// ResolveVariables replaces dynamic placeholders format `{{placeholder}}` with mapped values from the row.
func ResolveVariables(input string, row map[string]string) string {
	re := regexp.MustCompile(`\{\{(.+?)\}\}`)
	return re.ReplaceAllStringFunc(input, func(match string) string {
		colName := strings.TrimSpace(match[2 : len(match)-2])
		colKey := strings.ToLower(colName)
		if val, exists := row[colKey]; exists {
			return val
		}
		return match // Keep raw placeholder if column is missing
	})
}

// CalculateDuration calculates estimated campaign dispatch duration in seconds.
func CalculateDuration(totalValid, batchSize, delaySeconds int) int {
	if totalValid <= 0 || batchSize <= 0 {
		return 0
	}
	batches := totalValid / batchSize
	if totalValid%batchSize != 0 {
		batches++
	}
	return batches * delaySeconds
}
