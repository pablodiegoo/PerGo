package domain

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

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
// Phone holds the recipient destination address (phone number, channel handle, or contact identifier).
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
	RateLimitPerMin  *int                `json:"rate_limit_per_min,omitempty"`
	TemplateName     *string             `json:"template_name,omitempty"`
	MessageBody      *string             `json:"message_body,omitempty"`
	Channel          *string             `json:"channel,omitempty"`
	FallbackChannels []string            `json:"fallback_channels,omitempty"`
	Interactive      *Interactive        `json:"interactive,omitempty"`
	FallbackBehavior *string             `json:"fallback_behavior,omitempty"`
	TagID            *uuid.UUID          `json:"tag_id,omitempty"`
	TagIDs           []uuid.UUID         `json:"tag_ids,omitempty"`
	TotalRecipients  int                 `json:"total_recipients"`
	SentRecipients   int                 `json:"sent_recipients"`
	FailedRecipients int                 `json:"failed_recipients"`
	Recipients       []CampaignRecipient `json:"recipients,omitempty"`
	SkippedRows      []SkippedRow        `json:"skipped_rows,omitempty"`
	ScheduledAt      *time.Time          `json:"scheduled_at,omitempty"`
	CreatedAt        time.Time           `json:"created_at"`
	UpdatedAt        time.Time           `json:"updated_at"`
}

// CampaignStartTask represents the payload for starting a campaign with dynamic tag resolution.
// Published to the campaigns.start JetStream subject.
type CampaignStartTask struct {
	CampaignID  uuid.UUID `json:"campaign_id"`
	WorkspaceID uuid.UUID `json:"workspace_id"`
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

var varRegex = regexp.MustCompile(`\{\{(.+?)\}\}`)

// MergeVariables merges key-value pairs from src into dest. If dest is nil, a new map is initialized.
func MergeVariables(dest, src map[string]string) map[string]string {
	if dest == nil {
		dest = make(map[string]string, len(src))
	}
	for k, v := range src {
		dest[k] = v
	}
	return dest
}

// ResolveVariables replaces dynamic placeholders format `{{placeholder}}` with mapped values from the row.
func ResolveVariables(input string, row map[string]string) string {
	return varRegex.ReplaceAllStringFunc(input, func(match string) string {
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

// CalculateEstimatedDuration calculates estimated campaign dispatch duration in seconds,
// taking into account precision rate limiting if configured (> 0).
func CalculateEstimatedDuration(totalValid, batchSize, delaySeconds int, rateLimitPerMin *int) int {
	if totalValid <= 0 {
		return 0
	}
	if rateLimitPerMin != nil && *rateLimitPerMin > 0 {
		secs := (totalValid * 60) / *rateLimitPerMin
		if (totalValid*60)%*rateLimitPerMin != 0 {
			secs++
		}
		return secs
	}
	return CalculateDuration(totalValid, batchSize, delaySeconds)
}

// TagContactLister defines the interface for querying contacts associated with a specific tag.
type TagContactLister interface {
	ListContactsByTag(ctx context.Context, workspaceID, tagID uuid.UUID) ([]Contact, error)
}

// DeduplicateUUIDs returns a slice of unique non-nil UUIDs preserving input order.
func DeduplicateUUIDs(ids []uuid.UUID) []uuid.UUID {
	seen := make(map[uuid.UUID]bool, len(ids))
	result := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if id != uuid.Nil && !seen[id] {
			seen[id] = true
			result = append(result, id)
		}
	}
	return result
}

// DeduplicateStrings returns a slice of unique non-empty strings preserving input order.
func DeduplicateStrings(items []string) []string {
	seen := make(map[string]bool, len(items))
	result := make([]string, 0, len(items))
	for _, s := range items {
		s = strings.TrimSpace(s)
		if s != "" && !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	return result
}

// TagResolutionResult bundles the resolved recipient records (including pending and skipped),
// valid outbound recipients to be dispatched, and the set of seen recipient identities.
type TagResolutionResult struct {
	Records        []CampaignRecipientRecord
	Recipients     []CampaignRecipient
	SeenIdentities map[string]bool
}

// ResolveTagRecipients resolves contacts from specified tag IDs into campaign recipient records and recipients.
// Contacts matching the channel filter with a valid phone are added as RecipientStatusPending to records and included in recipients.
// Contacts lacking an identity for the channel filter are added as RecipientStatusSkipped to records and excluded from recipients.
func ResolveTagRecipients(
	ctx context.Context,
	lister TagContactLister,
	workspaceID uuid.UUID,
	tagIDs []uuid.UUID,
	channelFilter string,
) (TagResolutionResult, error) {
	seenIdentities := make(map[string]bool)
	seenContactIDs := make(map[uuid.UUID]bool)
	var records []CampaignRecipientRecord
	var recipients []CampaignRecipient

	uniqueIDs := DeduplicateUUIDs(tagIDs)
	if lister == nil || len(uniqueIDs) == 0 {
		return TagResolutionResult{
			Records:        records,
			Recipients:     recipients,
			SeenIdentities: seenIdentities,
		}, nil
	}

	for _, tagID := range uniqueIDs {
		contacts, err := lister.ListContactsByTag(ctx, workspaceID, tagID)
		if err != nil {
			return TagResolutionResult{}, fmt.Errorf("list contacts by tag: %w", err)
		}
		for _, contact := range contacts {
			if contact.ID != uuid.Nil {
				if seenContactIDs[contact.ID] {
					continue
				}
				seenContactIDs[contact.ID] = true
			}

			phone := ""
			for _, ident := range contact.Identities {
				if channelFilter != "" && !strings.EqualFold(ident.Channel, channelFilter) {
					continue
				}
				if ident.SenderIdentity != "" {
					if clean, valid := SanitizePhone(ident.SenderIdentity); valid {
						phone = clean
						break
					} else if channelFilter == "" || !strings.HasPrefix(strings.ToLower(channelFilter), "whatsapp") {
						// For non-phone or non-whatsapp channels when no sanitize phone is enforced
						phone = ident.SenderIdentity
						break
					}
				}
			}

			contactID := contact.ID

			vars := make(map[string]string, len(contact.Attributes)+1)
			for k, v := range contact.Attributes {
				vars[k] = v
			}
			vars["name"] = contact.Name

			if phone != "" {
				if seenIdentities[phone] {
					continue
				}
				seenIdentities[phone] = true

				records = append(records, CampaignRecipientRecord{
					ContactID: &contactID,
					Phone:     phone,
					Status:    RecipientStatusPending,
					Variables: copyVariables(vars),
				})
				recipients = append(recipients, CampaignRecipient{
					To:        phone,
					Variables: copyVariables(vars),
				})
			} else {
				// Contact lacked matching identity for the channel -> mark as skipped
				skippedIdentity := ""
				for _, ident := range contact.Identities {
					if ident.SenderIdentity != "" {
						if clean, valid := SanitizePhone(ident.SenderIdentity); valid {
							skippedIdentity = clean
						} else {
							skippedIdentity = ident.SenderIdentity
						}
						break
					}
				}
				if skippedIdentity == "" && contact.Email != nil && *contact.Email != "" {
					skippedIdentity = *contact.Email
				}
				if skippedIdentity == "" {
					skippedIdentity = contact.ID.String()
				}

				if seenIdentities[skippedIdentity] {
					continue
				}
				seenIdentities[skippedIdentity] = true

				records = append(records, CampaignRecipientRecord{
					ContactID: &contactID,
					Phone:     skippedIdentity,
					Status:    RecipientStatusSkipped,
					Variables: copyVariables(vars),
				})
			}
		}
	}

	return TagResolutionResult{
		Records:        records,
		Recipients:     recipients,
		SeenIdentities: seenIdentities,
	}, nil
}

// MergeTagAndCSVRecipients reconciles dynamic tag resolution results with static CSV recipients.
// Tag contact identity wins (ADR-0010), but CSV variables merge on top so campaign-specific data
// supplements the contact's stored attributes.
func MergeTagAndCSVRecipients(tagRes TagResolutionResult, csvRecipients []CampaignRecipient) ([]CampaignRecipientRecord, []CampaignRecipient) {
	allRecords := make([]CampaignRecipientRecord, 0, len(tagRes.Records)+len(csvRecipients))
	allRecords = append(allRecords, tagRes.Records...)

	mergedRecipients := make([]CampaignRecipient, 0, len(tagRes.Recipients)+len(csvRecipients))
	mergedRecipients = append(mergedRecipients, tagRes.Recipients...)

	seenIdentities := make(map[string]bool, len(tagRes.SeenIdentities))
	for k, v := range tagRes.SeenIdentities {
		seenIdentities[k] = v
	}

	for _, csvRec := range csvRecipients {
		phone := csvRec.To
		if clean, valid := SanitizePhone(phone); valid {
			phone = clean
		}
		if seenIdentities[phone] {
			foundPending := false
			for i, rec := range allRecords {
				if rec.Phone == phone {
					if rec.Status == RecipientStatusPending {
						foundPending = true
						allRecords[i].Variables = MergeVariables(allRecords[i].Variables, csvRec.Variables)
					} else if rec.Status == RecipientStatusSkipped {
						allRecords[i].Status = RecipientStatusPending
						allRecords[i].Variables = MergeVariables(allRecords[i].Variables, csvRec.Variables)
						mergedRecipients = append(mergedRecipients, CampaignRecipient{
							To:        phone,
							Variables: allRecords[i].Variables,
						})
					}
					break
				}
			}
			if foundPending {
				for i, mr := range mergedRecipients {
					if mr.To == phone {
						mergedRecipients[i].Variables = MergeVariables(mergedRecipients[i].Variables, csvRec.Variables)
						break
					}
				}
			}
			continue
		}
		seenIdentities[phone] = true
		allRecords = append(allRecords, CampaignRecipientRecord{
			Phone:     phone,
			Status:    RecipientStatusPending,
			Variables: csvRec.Variables,
		})
		mergedRecipients = append(mergedRecipients, CampaignRecipient{
			To:        phone,
			Variables: csvRec.Variables,
		})
	}

	return allRecords, mergedRecipients
}

// copyVariables creates a shallow copy of a string map.
func copyVariables(v map[string]string) map[string]string {
	if v == nil {
		return make(map[string]string)
	}
	cp := make(map[string]string, len(v))
	for k, val := range v {
		cp[k] = val
	}
	return cp
}

// WhatsApp and Meta interactive message character and structure limits.
const (
	MaxButtonTitleRunes        = 20
	MaxButtonIDRunes           = 256
	MaxButtonsCount            = 3
	MaxListButtonTextRunes     = 20
	MaxListSectionTitleRunes   = 24
	MaxListRowTitleRunes       = 24
	MaxListRowDescriptionRunes = 72
	MaxListRowIDRunes          = 200
	MaxListTotalRows           = 10
	MaxListSectionsCount       = 10
	MaxFlowCTARunes            = 20
	MaxHeaderTextRunes         = 60
	MaxBodyTextRunes           = 1024
	MaxFooterTextRunes         = 60
)

// InterpolateInteractive recursively resolves variables in an Interactive payload.
// Returns a new cloned Interactive object with all template placeholders {{key}}
// replaced by values from the vars map.
func InterpolateInteractive(src *Interactive, vars map[string]string) *Interactive {
	if src == nil {
		return nil
	}

	dst := &Interactive{
		Type: src.Type,
		Body: TextContent{
			Text: ResolveVariables(src.Body.Text, vars),
		},
	}

	if src.Header != nil {
		dst.Header = &TextContent{
			Text: ResolveVariables(src.Header.Text, vars),
		}
	}

	if src.Footer != nil {
		dst.Footer = &TextContent{
			Text: ResolveVariables(src.Footer.Text, vars),
		}
	}

	dst.Action = Action{
		Button:     ResolveVariables(src.Action.Button, vars),
		FlowToken:  ResolveVariables(src.Action.FlowToken, vars),
		FlowID:     ResolveVariables(src.Action.FlowID, vars),
		FlowCTA:    ResolveVariables(src.Action.FlowCTA, vars),
		FlowAction: ResolveVariables(src.Action.FlowAction, vars),
	}

	if len(src.Action.Buttons) > 0 {
		dst.Action.Buttons = make([]Button, len(src.Action.Buttons))
		for i, b := range src.Action.Buttons {
			dst.Action.Buttons[i] = Button{
				Type: b.Type,
				Reply: Reply{
					ID:    ResolveVariables(b.Reply.ID, vars),
					Title: ResolveVariables(b.Reply.Title, vars),
				},
			}
		}
	}

	if len(src.Action.Sections) > 0 {
		dst.Action.Sections = make([]Section, len(src.Action.Sections))
		for i, s := range src.Action.Sections {
			sec := Section{
				Title: ResolveVariables(s.Title, vars),
			}
			if len(s.Rows) > 0 {
				sec.Rows = make([]Row, len(s.Rows))
				for j, r := range s.Rows {
					sec.Rows[j] = Row{
						ID:          ResolveVariables(r.ID, vars),
						Title:       ResolveVariables(r.Title, vars),
						Description: ResolveVariables(r.Description, vars),
					}
				}
			}
			dst.Action.Sections[i] = sec
		}
	}

	if src.Action.FlowActionPayload != nil {
		dst.Action.FlowActionPayload = interpolateMap(src.Action.FlowActionPayload, vars)
	}

	return dst
}

func interpolateMap(src map[string]interface{}, vars map[string]string) map[string]interface{} {
	if src == nil {
		return nil
	}
	dst := make(map[string]interface{}, len(src))
	for k, v := range src {
		interpolatedKey := ResolveVariables(k, vars)
		dst[interpolatedKey] = interpolateValue(v, vars)
	}
	return dst
}

func interpolateValue(val interface{}, vars map[string]string) interface{} {
	switch v := val.(type) {
	case string:
		return ResolveVariables(v, vars)
	case map[string]interface{}:
		return interpolateMap(v, vars)
	case []interface{}:
		res := make([]interface{}, len(v))
		for i, item := range v {
			res[i] = interpolateValue(item, vars)
		}
		return res
	default:
		return val
	}
}

// ValidateInteractiveLimits checks whether an interactive message exceeds Meta / WhatsApp constraints post-interpolation.
func ValidateInteractiveLimits(i *Interactive) error {
	if i == nil {
		return nil
	}

	if i.Header != nil && utf8.RuneCountInString(i.Header.Text) > MaxHeaderTextRunes {
		return fmt.Errorf("interactive header exceeds maximum length of %d characters (%d)", MaxHeaderTextRunes, utf8.RuneCountInString(i.Header.Text))
	}

	if utf8.RuneCountInString(i.Body.Text) > MaxBodyTextRunes {
		return fmt.Errorf("interactive body exceeds maximum length of %d characters (%d)", MaxBodyTextRunes, utf8.RuneCountInString(i.Body.Text))
	}

	if i.Footer != nil && utf8.RuneCountInString(i.Footer.Text) > MaxFooterTextRunes {
		return fmt.Errorf("interactive footer exceeds maximum length of %d characters (%d)", MaxFooterTextRunes, utf8.RuneCountInString(i.Footer.Text))
	}

	if i.Type == "button" {
		if len(i.Action.Buttons) > MaxButtonsCount {
			return fmt.Errorf("interactive button message exceeds maximum of %d buttons (%d)", MaxButtonsCount, len(i.Action.Buttons))
		}
		for idx, b := range i.Action.Buttons {
			titleLen := utf8.RuneCountInString(b.Reply.Title)
			if titleLen > MaxButtonTitleRunes {
				return fmt.Errorf("button %d title exceeds maximum length of %d characters (%d)", idx+1, MaxButtonTitleRunes, titleLen)
			}
			if utf8.RuneCountInString(b.Reply.ID) > MaxButtonIDRunes {
				return fmt.Errorf("button %d ID exceeds maximum length of %d characters", idx+1, MaxButtonIDRunes)
			}
		}
	}

	if i.Type == "list" {
		if i.Action.Button != "" && utf8.RuneCountInString(i.Action.Button) > MaxListButtonTextRunes {
			return fmt.Errorf("list button title exceeds maximum length of %d characters (%d)", MaxListButtonTextRunes, utf8.RuneCountInString(i.Action.Button))
		}
		if len(i.Action.Sections) > MaxListSectionsCount {
			return fmt.Errorf("interactive list exceeds maximum of %d sections (%d)", MaxListSectionsCount, len(i.Action.Sections))
		}
		if i.TotalRows() > MaxListTotalRows {
			return fmt.Errorf("interactive list exceeds maximum of %d rows total (%d)", MaxListTotalRows, i.TotalRows())
		}
		for sIdx, sec := range i.Action.Sections {
			if utf8.RuneCountInString(sec.Title) > MaxListSectionTitleRunes {
				return fmt.Errorf("section %d title exceeds maximum length of %d characters (%d)", sIdx+1, MaxListSectionTitleRunes, utf8.RuneCountInString(sec.Title))
			}
			for rIdx, r := range sec.Rows {
				rowTitleLen := utf8.RuneCountInString(r.Title)
				if rowTitleLen > MaxListRowTitleRunes {
					return fmt.Errorf("section %d row %d title exceeds maximum length of %d characters (%d)", sIdx+1, rIdx+1, MaxListRowTitleRunes, rowTitleLen)
				}
				if utf8.RuneCountInString(r.Description) > MaxListRowDescriptionRunes {
					return fmt.Errorf("section %d row %d description exceeds maximum length of %d characters (%d)", sIdx+1, rIdx+1, MaxListRowDescriptionRunes, utf8.RuneCountInString(r.Description))
				}
				if utf8.RuneCountInString(r.ID) > MaxListRowIDRunes {
					return fmt.Errorf("section %d row %d ID exceeds maximum length of %d characters", sIdx+1, rIdx+1, MaxListRowIDRunes)
				}
			}
		}
	}

	if i.Type == "flow" {
		if i.Action.FlowCTA != "" && utf8.RuneCountInString(i.Action.FlowCTA) > MaxFlowCTARunes {
			return fmt.Errorf("flow CTA title exceeds maximum length of %d characters (%d)", MaxFlowCTARunes, utf8.RuneCountInString(i.Action.FlowCTA))
		}
	}

	return nil
}
