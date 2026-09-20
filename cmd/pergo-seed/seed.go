package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/pkg/slug"
	"github.com/pablojhp.pergo/internal/platform/crypto"
	"github.com/pablojhp.pergo/internal/repository"
)

// SeedOptions configures the deterministic seeding process.
type SeedOptions struct {
	WorkspaceName string
	WorkspaceID   *uuid.UUID
	Deterministic bool
}

// SeedResult summarizes entities populated during seeding.
type SeedResult struct {
	Workspace         *repository.Workspace
	Connections       []*repository.Connection
	Tags              []domain.Tag
	Contacts          []domain.Contact
	Templates         []repository.WABATemplate
	Campaigns         []domain.Campaign
	APIKeyCount       int
	ConversationCount int
	MessageCount      int
}

// Seed populates the database deterministically with rich mock data and zero real PII.
func Seed(ctx context.Context, pool *pgxpool.Pool, encryptor *crypto.Encryptor, opts SeedOptions) (*SeedResult, error) {
	wsRepo := repository.NewWorkspaceRepository(pool)
	connRepo := repository.NewConnectionRepository(pool, encryptor)
	tagRepo := repository.NewTagRepository(pool)
	contactRepo := repository.NewContactRepository(pool)
	tmplRepo := repository.NewWABATemplateRepository(pool)
	campaignRepo := repository.NewCampaignRepository(pool)
	sessRepo := repository.NewRecipientSessionRepository(pool)

	// 1. Workspace
	wsName := opts.WorkspaceName
	if wsName == "" {
		wsName = "PerGo Demo"
	}
	var ws *repository.Workspace
	var err error
	if opts.WorkspaceID != nil && *opts.WorkspaceID != uuid.Nil {
		ws, err = wsRepo.CreateWithID(ctx, *opts.WorkspaceID, wsName)
	} else {
		existingWs, getErr := wsRepo.GetByName(ctx, wsName)
		if getErr == nil && existingWs != nil {
			ws = existingWs
		} else {
			ws, err = wsRepo.Create(ctx, wsName)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("ensure workspace: %w", err)
	}
	slog.Info("workspace ready", "id", ws.ID, "name", ws.Name)

	// 2. Resolve WABA credentials and ensure connections
	creds := resolveWABACredentials(ctx)
	wabaCredJSON, _ := json.Marshal(WABAConfig{
		PhoneNumberID: creds.PhoneNumberID,
		Token:         creds.Token,
		WABAAccountID: creds.WABAAccountID,
		VerifyToken:   creds.VerifyToken,
	})

	wabaConn, err := ensureConnectionByChannel(ctx, pool, connRepo, ws.ID, "WhatsApp Cloud Primary", "whatsapp_cloud", creds.DisplayPhone, true, wabaCredJSON)
	if err != nil {
		return nil, fmt.Errorf("ensure waba connection: %w", err)
	}

	waWebConn, err := ensureConnectionByChannel(ctx, pool, connRepo, ws.ID, "WhatsApp Web Atendimento", "whatsapp", "5511998765432", false, []byte(`{}`))
	if err != nil {
		return nil, fmt.Errorf("ensure whatsapp web connection: %w", err)
	}

	tgCredJSON, _ := json.Marshal(map[string]string{"bot_token": "mock-telegram-token-pergo-seed"})
	tgConn, err := ensureConnectionByChannel(ctx, pool, connRepo, ws.ID, "Telegram Suporte Bot", "telegram", "@PerGoDemoBot", false, tgCredJSON)
	if err != nil {
		return nil, fmt.Errorf("ensure telegram connection: %w", err)
	}

	connections := []*repository.Connection{wabaConn, waWebConn, tgConn}

	// 2.1 Ensure an active API key exists for this workspace
	apiKeyRepo := repository.NewAPIKeyRepository(pool)
	existingKeys, err := apiKeyRepo.ListByWorkspace(ctx, ws.ID)
	if err != nil {
		return nil, fmt.Errorf("list api keys: %w", err)
	}
	apiKeyCount := len(existingKeys)
	if apiKeyCount == 0 {
		_, _, err = apiKeyRepo.Create(ctx, ws.ID, "Produção - CRM Omnichannel Key")
		if err != nil {
			return nil, fmt.Errorf("ensure api key: %w", err)
		}
		apiKeyCount = 1
	}

	// 3. Load Fixtures
	fixtures := getDeterministicFixtures(wabaConn.SenderIdentity)

	// 4. Tags
	tagMap := make(map[string]domain.Tag)
	existingTags, err := tagRepo.ListTags(ctx, ws.ID)
	if err == nil {
		for _, t := range existingTags {
			tagMap[t.Name] = t
		}
	}
	var seededTags []domain.Tag
	for _, tf := range fixtures.Tags {
		if t, ok := tagMap[tf.Name]; ok {
			seededTags = append(seededTags, t)
			continue
		}
		t, err := tagRepo.CreateTag(ctx, ws.ID, tf.Name, tf.Color)
		if err != nil {
			return nil, fmt.Errorf("create tag %s: %w", tf.Name, err)
		}
		tagMap[t.Name] = *t
		seededTags = append(seededTags, *t)
	}

	// 5. Contacts, Identities & Tags
	contactMap := make(map[string]*domain.Contact)
	var seededContacts []domain.Contact

	for _, cf := range fixtures.Contacts {
		var contactID uuid.UUID
		err := pool.QueryRow(ctx, `SELECT id FROM contacts WHERE workspace_id = $1 AND name = $2`, ws.ID, cf.Name).Scan(&contactID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("check contact %s: %w", cf.Name, err)
		}

		attrsJSON, _ := json.Marshal(cf.Attributes)
		if errors.Is(err, pgx.ErrNoRows) {
			err = pool.QueryRow(ctx, `
				INSERT INTO contacts (workspace_id, name, email, tags, bot_active, attributes)
				VALUES ($1, $2, $3, $4, $5, $6)
				RETURNING id
			`, ws.ID, cf.Name, cf.Email, cf.Tags, cf.BotActive, attrsJSON).Scan(&contactID)
			if err != nil {
				return nil, fmt.Errorf("insert contact %s: %w", cf.Name, err)
			}
		} else {
			// Update attributes & tags
			_, _ = pool.Exec(ctx, `UPDATE contacts SET attributes = $1, tags = $2, email = $3 WHERE id = $4`, attrsJSON, cf.Tags, cf.Email, contactID)
		}

		// Identities
		for _, idf := range cf.Identities {
			_, err = pool.Exec(ctx, `
				INSERT INTO contact_identities (contact_id, workspace_id, channel, sender_identity)
				VALUES ($1, $2, $3, $4)
				ON CONFLICT (workspace_id, channel, sender_identity) 
				DO UPDATE SET contact_id = EXCLUDED.contact_id
			`, contactID, ws.ID, idf.Channel, idf.SenderIdentity)
			if err != nil {
				return nil, fmt.Errorf("insert contact identity %s: %w", idf.SenderIdentity, err)
			}
		}

		// Tag associations
		for _, tagName := range cf.Tags {
			if tag, ok := tagMap[tagName]; ok {
				_, _ = pool.Exec(ctx, `
					INSERT INTO contact_tags (contact_id, tag_id)
					VALUES ($1, $2)
					ON CONFLICT (contact_id, tag_id) DO NOTHING
				`, contactID, tag.ID)
			}
		}

		loadedContact, err := contactRepo.GetByID(ctx, ws.ID, contactID)
		if err == nil && loadedContact != nil {
			contactMap[loadedContact.Name] = loadedContact
			seededContacts = append(seededContacts, *loadedContact)
		}
	}

	// 6. WABA Templates
	var seededTemplates []repository.WABATemplate
	for _, tf := range fixtures.Templates {
		existingTmpl, err := tmplRepo.GetByNameAndLanguage(ctx, wabaConn.ID, tf.Name, tf.Language)
		if err == nil && existingTmpl != nil {
			seededTemplates = append(seededTemplates, *existingTmpl)
			continue
		}

		quality := tf.QualityScore
		newTmpl := &repository.WABATemplate{
			WorkspaceID:    ws.ID,
			ConnectionID:   wabaConn.ID,
			MetaTemplateID: tf.MetaTemplateID,
			Name:           tf.Name,
			Language:       tf.Language,
			Status:         tf.Status,
			Category:       tf.Category,
			Components:     tf.Components,
			QualityScore:   &quality,
		}
		created, err := tmplRepo.Create(ctx, newTmpl)
		if err != nil {
			return nil, fmt.Errorf("create template %s: %w", tf.Name, err)
		}
		seededTemplates = append(seededTemplates, *created)
	}

	// 7. Campaigns & Recipients
	var seededCampaigns []domain.Campaign
	for _, cf := range fixtures.Campaigns {
		var campaignID uuid.UUID
		err := pool.QueryRow(ctx, `SELECT id FROM campaigns WHERE workspace_id = $1 AND name = $2`, ws.ID, cf.Name).Scan(&campaignID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("check campaign %s: %w", cf.Name, err)
		}

		var targetTagIDs []uuid.UUID
		for _, tn := range cf.TagNames {
			if t, ok := tagMap[tn]; ok {
				targetTagIDs = append(targetTagIDs, t.ID)
			}
		}

		if errors.Is(err, pgx.ErrNoRows) {
			channelStr := cf.Channel
			tmplName := cf.TemplateName
			c := &domain.Campaign{
				WorkspaceID:     ws.ID,
				ConnectionID:    &wabaConn.ID,
				ConnectionSlug:  &wabaConn.Slug,
				Name:            cf.Name,
				Status:          cf.Status,
				BatchSize:       cf.BatchSize,
				DelaySeconds:    cf.DelaySeconds,
				TemplateName:    &tmplName,
				Channel:         &channelStr,
				TagIDs:          targetTagIDs,
				TotalRecipients: len(cf.Recipients),
			}
			createdCmp, err := campaignRepo.Create(ctx, c)
			if err != nil {
				return nil, fmt.Errorf("create campaign %s: %w", cf.Name, err)
			}
			campaignID = createdCmp.ID

			// Recipients
			var recipientRecords []domain.CampaignRecipientRecord
			sentCount := 0
			failedCount := 0
			now := time.Now().UTC()

			for _, rf := range cf.Recipients {
				var contactID *uuid.UUID
				if ct, ok := contactMap[rf.ContactName]; ok {
					id := ct.ID
					contactID = &id
				}
				var sentAt *time.Time
				if rf.Status == domain.RecipientStatusSent {
					t := now.Add(-rf.SentAgo)
					sentAt = &t
					sentCount++
				} else if rf.Status == domain.RecipientStatusFailed {
					failedCount++
				}

				recipientRecords = append(recipientRecords, domain.CampaignRecipientRecord{
					CampaignID: campaignID,
					ContactID:  contactID,
					Phone:      rf.Phone,
					Variables:  rf.Variables,
					Status:     rf.Status,
					SentAt:     sentAt,
				})
			}

			if err := campaignRepo.AddRecipients(ctx, campaignID, recipientRecords); err != nil {
				return nil, fmt.Errorf("add campaign recipients: %w", err)
			}

			// Update status and sent_at per recipient
			for _, rec := range recipientRecords {
				_, _ = pool.Exec(ctx, `
					UPDATE campaign_recipients 
					SET status = $1, sent_at = $2 
					WHERE campaign_id = $3 AND phone = $4
				`, rec.Status, rec.SentAt, campaignID, rec.Phone)
			}

			// Update campaign counters
			_, _ = pool.Exec(ctx, `
				UPDATE campaigns 
				SET sent_recipients = $1, failed_recipients = $2, updated_at = now()
				WHERE id = $3
			`, sentCount, failedCount, campaignID)
		}

		loadedCmp, err := campaignRepo.GetByID(ctx, campaignID)
		if err == nil && loadedCmp != nil {
			seededCampaigns = append(seededCampaigns, *loadedCmp)
		}
	}

	// 8. Conversations, Audit Logs, Dispatches, Sessions
	now := time.Now().UTC()
	conversationCount := 0
	messageCount := 0

	for _, conv := range fixtures.Conversations {
		contact, ok := contactMap[conv.ContactName]
		if !ok || len(contact.Identities) == 0 {
			continue
		}
		var contactPhone string
		for _, id := range contact.Identities {
			if id.Channel == conv.Channel {
				contactPhone = id.SenderIdentity
				break
			}
		}
		if contactPhone == "" {
			contactPhone = contact.Identities[0].SenderIdentity
		}

		conversationCount++
		var lastInboundAt, lastOutboundAt time.Time
		contactSlug := slug.Generate(conv.ContactName)

		for msgIdx, msg := range conv.Messages {
			msgTime := now.Add(-time.Duration(msg.OffsetMinutes) * time.Minute)
			traceID := fmt.Sprintf("trace-seed-%s-%s-%d", ws.ID.String()[:8], contactSlug, msgIdx)
			messageID := fmt.Sprintf("wamid.seed.%s.%d", contactSlug, msgIdx)
			messageCount++

			var alreadyExists bool
			_ = pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM audit_logs WHERE workspace_id = $1 AND trace_id = $2)`, ws.ID, traceID).Scan(&alreadyExists)

			if msg.Direction == "inbound" {
				if msgTime.After(lastInboundAt) {
					lastInboundAt = msgTime
				}
				if !alreadyExists {
					payload, _ := buildInboundAuditPayload(
						ws.ID,
						traceID,
						messageID,
						conv.Channel,
						contactPhone,
						conv.RecipientIdentity,
						msg.Body,
						msgTime,
					)
					_, err = pool.Exec(ctx, `
						INSERT INTO audit_logs (workspace_id, trace_id, event_type, payload, created_at)
						VALUES ($1, $2, 'inbound_message', $3, $4)
					`, ws.ID, traceID, payload, msgTime)
					if err != nil {
						return nil, fmt.Errorf("insert inbound audit log: %w", err)
					}
				}
			} else {
				if msgTime.After(lastOutboundAt) {
					lastOutboundAt = msgTime
				}
				if !alreadyExists {
					payload, _ := buildOutboundAuditPayload(
						ws.ID,
						traceID,
						conv.Channel,
						conv.RecipientIdentity,
						contactPhone,
						msg.Body,
						msgTime,
					)
					_, err = pool.Exec(ctx, `
						INSERT INTO audit_logs (workspace_id, trace_id, event_type, payload, created_at)
						VALUES ($1, $2, 'outbound_message', $3, $4)
					`, ws.ID, traceID, payload, msgTime)
					if err != nil {
						return nil, fmt.Errorf("insert outbound audit log: %w", err)
					}

					status := msg.Status
					if status == "" {
						status = "delivered"
					}
					_, err = pool.Exec(ctx, `
						INSERT INTO message_dispatches (workspace_id, trace_id, current_channel, status, created_at, updated_at)
						VALUES ($1, $2, $3, $4, $5, $5)
						ON CONFLICT (trace_id) DO NOTHING
					`, ws.ID, traceID, conv.Channel, status, msgTime)
					if err != nil {
						return nil, fmt.Errorf("insert message dispatch: %w", err)
					}
				}
			}
		}

		// Upsert recipient session
		sessKey := domain.NewSessionKey(ws.ID, contactPhone, conv.Channel, conv.RecipientIdentity)
		_ = sessRepo.Upsert(ctx, sessKey, lastInboundAt, "standard")
		if !lastOutboundAt.IsZero() {
			_, _ = pool.Exec(ctx, `
				UPDATE recipient_sessions 
				SET last_outbound_at = $1 
				WHERE workspace_id = $2 AND recipient_phone = $3 AND channel = $4 AND recipient_identity = $5
			`, lastOutboundAt, ws.ID, contactPhone, conv.Channel, conv.RecipientIdentity)
		}
	}

	// 9. User Action Logs (Operator audit trail)
	actionLogs := []struct {
		Action    string
		ActorName string
		Source    string
		Minutes   int
	}{
		{"workspace.initialized", "System Operator", "pergo-seed", 300},
		{"connection.configured", "System Operator", "pergo-seed", 280},
		{"templates.synced", "System Operator", "pergo-seed", 240},
		{"campaign.broadcast_completed", "System Operator", "pergo-seed", 120},
	}
	for _, al := range actionLogs {
		logTime := now.Add(-time.Duration(al.Minutes) * time.Minute)
		var actionExists bool
		_ = pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM user_action_logs WHERE workspace_id = $1 AND action = $2 AND source = 'pergo-seed')`, ws.ID, al.Action).Scan(&actionExists)
		if !actionExists {
			_, _ = pool.Exec(ctx, `
				INSERT INTO user_action_logs (workspace_id, actor_type, actor_id, actor_name, action, source, created_at)
				VALUES ($1, 'system_operator', 'seed-admin', $2, $3, $4, $5)
			`, ws.ID, al.ActorName, al.Action, al.Source, logTime)
		}
	}

	return &SeedResult{
		Workspace:         ws,
		Connections:       connections,
		Tags:              seededTags,
		Contacts:          seededContacts,
		Templates:         seededTemplates,
		Campaigns:         seededCampaigns,
		APIKeyCount:       apiKeyCount,
		ConversationCount: conversationCount,
		MessageCount:      messageCount,
	}, nil
}

func ensureConnectionByChannel(
	ctx context.Context,
	pool *pgxpool.Pool,
	repo *repository.ConnectionRepository,
	wsID uuid.UUID,
	name, channel, senderIdentity string,
	isDefault bool,
	credentials []byte,
) (*repository.Connection, error) {
	existing, err := repo.ListByWorkspace(ctx, wsID)
	if err == nil {
		for _, c := range existing {
			if c.Channel == channel && c.SenderIdentity == senderIdentity {
				return c, nil
			}
		}
	}

	// If sender_identity exists globally in any workspace, reassign to this workspace for seed consistency
	var existingID uuid.UUID
	err = pool.QueryRow(ctx, "SELECT id FROM connections WHERE sender_identity = $1", senderIdentity).Scan(&existingID)
	if err == nil {
		_, updateErr := pool.Exec(ctx, `
			UPDATE connections 
			SET workspace_id = $1, name = $2, status = 'connected', is_default = $3, updated_at = now() 
			WHERE id = $4
		`, wsID, name, isDefault, existingID)
		if updateErr == nil {
			return repo.GetByID(ctx, existingID)
		}
	}

	conn := &repository.Connection{
		WorkspaceID:    wsID,
		Name:           name,
		Channel:        channel,
		SenderIdentity: senderIdentity,
		Status:         "connected",
		IsDefault:      isDefault,
		Credentials:    credentials,
	}
	if err := repo.Create(ctx, conn); err != nil {
		return nil, err
	}
	return conn, nil
}
