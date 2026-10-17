package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pablojhp.pergo/internal/domain"
)

var (
	ErrChatNotFound        = errors.New("chat not found")
	ErrChatMessageNotFound = errors.New("chat message not found")
)

type ChatRepository struct {
	pool *pgxpool.Pool
}

func NewChatRepository(pool *pgxpool.Pool) *ChatRepository {
	return &ChatRepository{pool: pool}
}

// FindOrCreateChat retrieves an existing chat for the given (workspaceID, connectionID, contactID) or creates one.
func (r *ChatRepository) FindOrCreateChat(
	ctx context.Context,
	workspaceID uuid.UUID,
	connectionID *uuid.UUID,
	contactID uuid.UUID,
) (*domain.Chat, error) {
	if workspaceID == uuid.Nil {
		return nil, ErrInvalidWorkspaceID
	}
	if contactID == uuid.Nil {
		return nil, errors.New("contact_id is required")
	}

	var chat domain.Chat
	var metaBytes []byte

	err := r.pool.QueryRow(ctx, `
		SELECT id, workspace_id, connection_id, contact_id, status, snoozed_until, assigned_user_id, assigned_email,
		       tags, unread_count, ai_disabled, service_window_expires_at, last_message_at, metadata,
		       created_at, updated_at
		FROM chats
		WHERE workspace_id = $1 AND contact_id = $2
		  AND ($3::uuid IS NULL OR connection_id = $3 OR connection_id IS NULL)
		ORDER BY (connection_id = $3) DESC NULLS LAST, (connection_id IS NOT NULL) DESC, last_message_at DESC
		LIMIT 1
	`, workspaceID, contactID, connectionID).Scan(
		&chat.ID, &chat.WorkspaceID, &chat.ConnectionID, &chat.ContactID, &chat.Status, &chat.SnoozedUntil,
		&chat.AssignedUserID, &chat.AssignedEmail, &chat.Tags, &chat.UnreadCount, &chat.AIDisabled,
		&chat.ServiceWindowExpiresAt, &chat.LastMessageAt, &metaBytes, &chat.CreatedAt, &chat.UpdatedAt,
	)

	if err == nil {
		if connectionID != nil && *connectionID != uuid.Nil && chat.ConnectionID == nil {
			_, _ = r.pool.Exec(ctx, `
				UPDATE chats SET connection_id = $3, updated_at = NOW()
				WHERE id = $1 AND workspace_id = $2
				  AND NOT EXISTS (
				      SELECT 1 FROM chats WHERE workspace_id = $2 AND connection_id = $3 AND contact_id = $4
				  )
			`, chat.ID, workspaceID, connectionID, contactID)
			chat.ConnectionID = connectionID
		}
		if len(metaBytes) > 0 {
			_ = json.Unmarshal(metaBytes, &chat.Metadata)
		}
		if chat.Metadata == nil {
			chat.Metadata = make(map[string]interface{})
		}
		chat.ServiceWindowIsOpen = chat.IsServiceWindowOpen()
		return &chat, nil
	}

	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("find chat: %w", err)
	}

	// Insert on conflict
	err = r.pool.QueryRow(ctx, `
		INSERT INTO chats (workspace_id, connection_id, contact_id, status, tags, unread_count, ai_disabled, metadata)
		VALUES ($1, $2, $3, 'open', '{}', 0, FALSE, '{}')
		ON CONFLICT (workspace_id, connection_id, contact_id)
		DO UPDATE SET updated_at = NOW()
		RETURNING id, workspace_id, connection_id, contact_id, status, snoozed_until, assigned_user_id, assigned_email,
		          tags, unread_count, ai_disabled, service_window_expires_at, last_message_at, metadata,
		          created_at, updated_at
	`, workspaceID, connectionID, contactID).Scan(
		&chat.ID, &chat.WorkspaceID, &chat.ConnectionID, &chat.ContactID, &chat.Status, &chat.SnoozedUntil,
		&chat.AssignedUserID, &chat.AssignedEmail, &chat.Tags, &chat.UnreadCount, &chat.AIDisabled,
		&chat.ServiceWindowExpiresAt, &chat.LastMessageAt, &metaBytes, &chat.CreatedAt, &chat.UpdatedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("create chat: %w", err)
	}

	if len(metaBytes) > 0 {
		_ = json.Unmarshal(metaBytes, &chat.Metadata)
	}
	if chat.Metadata == nil {
		chat.Metadata = make(map[string]interface{})
	}
	chat.ServiceWindowIsOpen = chat.IsServiceWindowOpen()

	return &chat, nil
}

// GetChat fetches a single chat by workspace and chat ID.
func (r *ChatRepository) GetChat(ctx context.Context, workspaceID, chatID uuid.UUID) (*domain.Chat, error) {
	if workspaceID == uuid.Nil {
		return nil, ErrInvalidWorkspaceID
	}
	var chat domain.Chat
	var metaBytes []byte

	err := r.pool.QueryRow(ctx, `
		SELECT id, workspace_id, connection_id, contact_id, status, snoozed_until, assigned_user_id, assigned_email,
		       tags, unread_count, ai_disabled, service_window_expires_at, last_message_at, metadata,
		       created_at, updated_at
		FROM chats
		WHERE workspace_id = $1 AND id = $2
	`, workspaceID, chatID).Scan(
		&chat.ID, &chat.WorkspaceID, &chat.ConnectionID, &chat.ContactID, &chat.Status, &chat.SnoozedUntil,
		&chat.AssignedUserID, &chat.AssignedEmail, &chat.Tags, &chat.UnreadCount, &chat.AIDisabled,
		&chat.ServiceWindowExpiresAt, &chat.LastMessageAt, &metaBytes, &chat.CreatedAt, &chat.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrChatNotFound
		}
		return nil, fmt.Errorf("get chat: %w", err)
	}

	if len(metaBytes) > 0 {
		_ = json.Unmarshal(metaBytes, &chat.Metadata)
	}
	if chat.Metadata == nil {
		chat.Metadata = make(map[string]interface{})
	}
	chat.ServiceWindowIsOpen = chat.IsServiceWindowOpen()

	return &chat, nil
}

// GetChatByContact retrieves the most recently active chat for a contact in a workspace.
func (r *ChatRepository) GetChatByContact(ctx context.Context, workspaceID, contactID uuid.UUID) (*domain.Chat, error) {
	if workspaceID == uuid.Nil {
		return nil, ErrInvalidWorkspaceID
	}
	if contactID == uuid.Nil {
		return nil, errors.New("contact_id is required")
	}

	var chat domain.Chat
	var metaBytes []byte

	err := r.pool.QueryRow(ctx, `
		SELECT id, workspace_id, connection_id, contact_id, status, snoozed_until, assigned_user_id, assigned_email,
		       tags, unread_count, ai_disabled, service_window_expires_at, last_message_at, metadata,
		       created_at, updated_at
		FROM chats
		WHERE workspace_id = $1 AND contact_id = $2
		ORDER BY (connection_id IS NOT NULL) DESC, last_message_at DESC
		LIMIT 1
	`, workspaceID, contactID).Scan(
		&chat.ID, &chat.WorkspaceID, &chat.ConnectionID, &chat.ContactID, &chat.Status, &chat.SnoozedUntil,
		&chat.AssignedUserID, &chat.AssignedEmail, &chat.Tags, &chat.UnreadCount, &chat.AIDisabled,
		&chat.ServiceWindowExpiresAt, &chat.LastMessageAt, &metaBytes, &chat.CreatedAt, &chat.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrChatNotFound
		}
		return nil, fmt.Errorf("get chat by contact: %w", err)
	}

	if len(metaBytes) > 0 {
		_ = json.Unmarshal(metaBytes, &chat.Metadata)
	}
	if chat.Metadata == nil {
		chat.Metadata = make(map[string]interface{})
	}
	chat.ServiceWindowIsOpen = chat.IsServiceWindowOpen()

	return &chat, nil
}

// GetChatByID fetches a single chat by chat ID across any workspace.
func (r *ChatRepository) GetChatByID(ctx context.Context, chatID uuid.UUID) (*domain.Chat, error) {
	if chatID == uuid.Nil {
		return nil, errors.New("chat_id is required")
	}

	var chat domain.Chat
	var metaBytes []byte

	err := r.pool.QueryRow(ctx, `
		SELECT id, workspace_id, connection_id, contact_id, status, snoozed_until, assigned_user_id, assigned_email,
		       tags, unread_count, ai_disabled, service_window_expires_at, last_message_at, metadata,
		       created_at, updated_at
		FROM chats
		WHERE id = $1
	`, chatID).Scan(
		&chat.ID, &chat.WorkspaceID, &chat.ConnectionID, &chat.ContactID, &chat.Status, &chat.SnoozedUntil,
		&chat.AssignedUserID, &chat.AssignedEmail, &chat.Tags, &chat.UnreadCount, &chat.AIDisabled,
		&chat.ServiceWindowExpiresAt, &chat.LastMessageAt, &metaBytes, &chat.CreatedAt, &chat.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrChatNotFound
		}
		return nil, fmt.Errorf("get chat by id: %w", err)
	}

	if len(metaBytes) > 0 {
		_ = json.Unmarshal(metaBytes, &chat.Metadata)
	}
	if chat.Metadata == nil {
		chat.Metadata = make(map[string]interface{})
	}
	chat.ServiceWindowIsOpen = chat.IsServiceWindowOpen()

	return &chat, nil
}

// ListChats retrieves chats for a workspace with filtering and pagination.
func (r *ChatRepository) ListChats(
	ctx context.Context,
	workspaceID uuid.UUID,
	filter domain.ChatFilter,
	limit, offset int,
) ([]domain.Chat, error) {
	if workspaceID == uuid.Nil {
		return nil, ErrInvalidWorkspaceID
	}

	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	query := `
		SELECT id, workspace_id, connection_id, contact_id, status, snoozed_until, assigned_user_id, assigned_email,
		       tags, unread_count, ai_disabled, service_window_expires_at, last_message_at, metadata,
		       created_at, updated_at
		FROM chats
		WHERE workspace_id = $1
	`
	args := []interface{}{workspaceID}
	argIdx := 2

	if filter.Status != "" {
		query += fmt.Sprintf(" AND status = $%d", argIdx)
		args = append(args, filter.Status)
		argIdx++
	}

	if filter.Channel != "" {
		query += fmt.Sprintf(` AND (
			connection_id IN (SELECT id FROM connections WHERE workspace_id = $1 AND channel = $%d)
			OR contact_id IN (SELECT contact_id FROM contact_identities WHERE workspace_id = $1 AND channel = $%d)
		)`, argIdx, argIdx)
		args = append(args, filter.Channel)
		argIdx++
	}

	if filter.Unread != nil {
		if *filter.Unread {
			query += " AND unread_count > 0"
		} else {
			query += " AND unread_count = 0"
		}
	}

	if filter.ContactID != nil && *filter.ContactID != uuid.Nil {
		query += fmt.Sprintf(" AND contact_id = $%d", argIdx)
		args = append(args, *filter.ContactID)
		argIdx++
	}

	if filter.ConnectionID != nil && *filter.ConnectionID != uuid.Nil {
		query += fmt.Sprintf(" AND connection_id = $%d", argIdx)
		args = append(args, *filter.ConnectionID)
		argIdx++
	}

	if filter.Phone != "" {
		query += fmt.Sprintf(` AND contact_id IN (
			SELECT contact_id FROM contact_identities 
			WHERE workspace_id = $1 AND sender_identity ILIKE '%%' || $%d || '%%'
		)`, argIdx)
		args = append(args, filter.Phone)
		argIdx++
	}

	if filter.AssignedEmail != nil && *filter.AssignedEmail != "" {
		query += fmt.Sprintf(" AND assigned_email = $%d", argIdx)
		args = append(args, *filter.AssignedEmail)
		argIdx++
	}

	if filter.Unassigned != nil && *filter.Unassigned {
		query += " AND (assigned_email IS NULL OR assigned_email = '')"
	}

	if filter.Tag != "" {
		query += fmt.Sprintf(" AND $%d = ANY(tags)", argIdx)
		args = append(args, filter.Tag)
		argIdx++
	}

	query += fmt.Sprintf(" ORDER BY last_message_at DESC LIMIT $%d OFFSET $%d", argIdx, argIdx+1)
	args = append(args, limit, offset)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list chats: %w", err)
	}
	defer rows.Close()

	var chats []domain.Chat
	for rows.Next() {
		var c domain.Chat
		var metaBytes []byte
		if err := rows.Scan(
			&c.ID, &c.WorkspaceID, &c.ConnectionID, &c.ContactID, &c.Status, &c.SnoozedUntil,
			&c.AssignedUserID, &c.AssignedEmail, &c.Tags, &c.UnreadCount, &c.AIDisabled,
			&c.ServiceWindowExpiresAt, &c.LastMessageAt, &metaBytes, &c.CreatedAt, &c.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan chat: %w", err)
		}
		if len(metaBytes) > 0 {
			_ = json.Unmarshal(metaBytes, &c.Metadata)
		}
		if c.Metadata == nil {
			c.Metadata = make(map[string]interface{})
		}
		c.ServiceWindowIsOpen = c.IsServiceWindowOpen()
		chats = append(chats, c)
	}

	return chats, rows.Err()
}

// UpdateChatStatus updates the lifecycle state of a chat.
func (r *ChatRepository) UpdateChatStatus(ctx context.Context, workspaceID, chatID uuid.UUID, status string) error {
	var query string
	if status == string(domain.ChatStatusSnoozed) {
		query = `UPDATE chats SET status = $3, updated_at = NOW() WHERE workspace_id = $1 AND id = $2`
	} else {
		query = `UPDATE chats SET status = $3, snoozed_until = NULL, updated_at = NOW() WHERE workspace_id = $1 AND id = $2`
	}
	res, err := r.pool.Exec(ctx, query, workspaceID, chatID, status)
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return ErrChatNotFound
	}
	return nil
}

// SnoozeChat sets the chat status to 'snoozed' with an optional snooze expiration timestamp.
func (r *ChatRepository) SnoozeChat(ctx context.Context, workspaceID, chatID uuid.UUID, snoozedUntil *time.Time) error {
	res, err := r.pool.Exec(ctx, `
		UPDATE chats SET status = 'snoozed', snoozed_until = $3, updated_at = NOW()
		WHERE workspace_id = $1 AND id = $2
	`, workspaceID, chatID, snoozedUntil)
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return ErrChatNotFound
	}
	return nil
}

// AssignChat assigns a chat to a user/email.
func (r *ChatRepository) AssignChat(ctx context.Context, workspaceID, chatID uuid.UUID, userID *uuid.UUID, email *string) error {
	res, err := r.pool.Exec(ctx, `
		UPDATE chats SET assigned_user_id = $3, assigned_email = $4, updated_at = NOW()
		WHERE workspace_id = $1 AND id = $2
	`, workspaceID, chatID, userID, email)
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return ErrChatNotFound
	}
	return nil
}

// SetChatTags sets tags on a chat.
func (r *ChatRepository) SetChatTags(ctx context.Context, workspaceID, chatID uuid.UUID, tags []string) error {
	if tags == nil {
		tags = []string{}
	}
	res, err := r.pool.Exec(ctx, `
		UPDATE chats SET tags = $3, updated_at = NOW()
		WHERE workspace_id = $1 AND id = $2
	`, workspaceID, chatID, tags)
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return ErrChatNotFound
	}
	return nil
}

// UpdateChatUnreadCount updates the unread count of a chat.
func (r *ChatRepository) UpdateChatUnreadCount(ctx context.Context, workspaceID, chatID uuid.UUID, unreadCount int) error {
	res, err := r.pool.Exec(ctx, `
		UPDATE chats SET unread_count = $3, updated_at = NOW()
		WHERE workspace_id = $1 AND id = $2
	`, workspaceID, chatID, unreadCount)
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return ErrChatNotFound
	}
	return nil
}

// IncrementUnreadCount increases unread_count by 1.
func (r *ChatRepository) IncrementUnreadCount(ctx context.Context, workspaceID, chatID uuid.UUID) error {
	res, err := r.pool.Exec(ctx, `
		UPDATE chats SET unread_count = unread_count + 1, updated_at = NOW()
		WHERE workspace_id = $1 AND id = $2
	`, workspaceID, chatID)
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return ErrChatNotFound
	}
	return nil
}

// TouchLastMessageAt updates last_message_at and updated_at.
func (r *ChatRepository) TouchLastMessageAt(ctx context.Context, workspaceID, chatID uuid.UUID, t time.Time) error {
	if t.IsZero() {
		t = time.Now().UTC()
	}
	res, err := r.pool.Exec(ctx, `
		UPDATE chats SET last_message_at = $3, updated_at = NOW()
		WHERE workspace_id = $1 AND id = $2
	`, workspaceID, chatID, t)
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return ErrChatNotFound
	}
	return nil
}

// SetAIDisabled toggles AI responders on a chat.
func (r *ChatRepository) SetAIDisabled(ctx context.Context, workspaceID, chatID uuid.UUID, disabled bool) error {
	res, err := r.pool.Exec(ctx, `
		UPDATE chats SET ai_disabled = $3, updated_at = NOW()
		WHERE workspace_id = $1 AND id = $2
	`, workspaceID, chatID, disabled)
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return ErrChatNotFound
	}
	return nil
}

// UpdateServiceWindow sets the WhatsApp customer service window expiration.
func (r *ChatRepository) UpdateServiceWindow(ctx context.Context, workspaceID, chatID uuid.UUID, expiresAt *time.Time) error {
	res, err := r.pool.Exec(ctx, `
		UPDATE chats SET service_window_expires_at = $3, updated_at = NOW()
		WHERE workspace_id = $1 AND id = $2
	`, workspaceID, chatID, expiresAt)
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return ErrChatNotFound
	}
	return nil
}

// AddChatMessage appends a new message to the chat and updates chat's last_message_at.
func (r *ChatRepository) AddChatMessage(ctx context.Context, msg *domain.ChatMessage) error {
	if msg == nil {
		return errors.New("chat message is nil")
	}
	if msg.WorkspaceID == uuid.Nil {
		return ErrInvalidWorkspaceID
	}
	if msg.ChatID == uuid.Nil {
		return errors.New("chat_id is required")
	}
	if msg.UID == "" {
		return errors.New("uid is required")
	}
	if msg.ID == uuid.Nil {
		msg.ID = uuid.New()
	}
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now().UTC()
	}
	if msg.Reactions == nil {
		msg.Reactions = []domain.Reaction{}
	}
	if msg.Metadata == nil {
		msg.Metadata = make(map[string]interface{})
	}

	rxJSON, err := json.Marshal(msg.Reactions)
	if err != nil {
		return fmt.Errorf("marshal reactions: %w", err)
	}

	metaJSON, err := json.Marshal(msg.Metadata)
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}

	_, err = r.pool.Exec(ctx, `
		INSERT INTO chat_messages (
			id, chat_id, workspace_id, uid, direction, sender_type, sender_name, sender_id,
			body, media_url, media_type, is_private, reactions, reply_to_uid, metadata, created_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8,
			$9, $10, $11, $12, $13, $14, $15, $16
		)
		ON CONFLICT (uid) DO NOTHING
	`, msg.ID, msg.ChatID, msg.WorkspaceID, msg.UID, msg.Direction, msg.SenderType, msg.SenderName, msg.SenderID,
		msg.Body, msg.MediaURL, msg.MediaType, msg.IsPrivate, rxJSON, msg.ReplyToUID, metaJSON, msg.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert chat message: %w", err)
	}

	// Touch last_message_at on the parent chat
	_ = r.TouchLastMessageAt(ctx, msg.WorkspaceID, msg.ChatID, msg.CreatedAt)

	return nil
}

// CreateInternalNote appends a private internal note to a chat thread.
// It sets direction = DirectionInternalNote ("internal_note") and is_private = true.
func (r *ChatRepository) CreateInternalNote(
	ctx context.Context,
	workspaceID, chatID uuid.UUID,
	authorName, authorID, body string,
) (*domain.ChatMessage, error) {
	if chatID == uuid.Nil {
		return nil, errors.New("chat_id is required")
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, errors.New("body cannot be empty")
	}

	var chat *domain.Chat
	var err error
	if workspaceID != uuid.Nil {
		chat, err = r.GetChat(ctx, workspaceID, chatID)
	} else {
		chat, err = r.GetChatByID(ctx, chatID)
	}
	if err != nil {
		return nil, err
	}
	workspaceID = chat.WorkspaceID

	noteUID := fmt.Sprintf("note_%s", uuid.New().String())
	if authorName == "" {
		authorName = "AI Assistant"
	}
	senderType := domain.SenderTypeAIAgent
	if authorID == "human" || authorID == "human_agent" {
		senderType = domain.SenderTypeHumanAgent
	}

	msg := &domain.ChatMessage{
		ID:          uuid.New(),
		ChatID:      chatID,
		WorkspaceID: workspaceID,
		UID:         noteUID,
		Direction:   domain.DirectionInternalNote,
		SenderType:  senderType,
		SenderName:  authorName,
		SenderID:    authorID,
		Body:        body,
		IsPrivate:   true,
		Reactions:   []domain.Reaction{},
		Metadata: map[string]interface{}{
			"is_private":  true,
			"author_name": authorName,
		},
		CreatedAt: time.Now().UTC(),
	}

	if err := r.AddChatMessage(ctx, msg); err != nil {
		return nil, err
	}

	return msg, nil
}

// ListChatMessages returns messages for a given chat with deterministic cursor pagination.
// Messages are returned in chronological order (oldest to newest).
func (r *ChatRepository) ListChatMessages(
	ctx context.Context,
	workspaceID, chatID uuid.UUID,
	beforeUID, afterUID string,
	limit int,
) ([]domain.ChatMessage, error) {
	if workspaceID == uuid.Nil {
		return nil, ErrInvalidWorkspaceID
	}
	if chatID == uuid.Nil {
		return nil, errors.New("chat_id is required")
	}

	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}

	var query string
	var args []interface{}
	needReverse := false

	if beforeUID != "" {
		// Fetch messages strictly older than beforeUID, ordered newest-first, then reverse to chronological order
		query = `
			SELECT id, chat_id, workspace_id, uid, direction, sender_type, sender_name, sender_id,
			       body, media_url, media_type, is_private, reactions, reply_to_uid, metadata, created_at
			FROM chat_messages
			WHERE workspace_id = $1 AND chat_id = $2
			  AND (created_at, id) < (SELECT created_at, id FROM chat_messages WHERE uid = $3 AND workspace_id = $1)
			ORDER BY created_at DESC, id DESC
			LIMIT $4
		`
		args = []interface{}{workspaceID, chatID, beforeUID, limit}
		needReverse = true
	} else if afterUID != "" {
		// Fetch messages strictly newer than afterUID, ordered oldest-first
		query = `
			SELECT id, chat_id, workspace_id, uid, direction, sender_type, sender_name, sender_id,
			       body, media_url, media_type, is_private, reactions, reply_to_uid, metadata, created_at
			FROM chat_messages
			WHERE workspace_id = $1 AND chat_id = $2
			  AND (created_at, id) > (SELECT created_at, id FROM chat_messages WHERE uid = $3 AND workspace_id = $1)
			ORDER BY created_at ASC, id ASC
			LIMIT $4
		`
		args = []interface{}{workspaceID, chatID, afterUID, limit}
		needReverse = false
	} else {
		// Default: return most recent messages up to limit in chronological order
		query = `
			SELECT id, chat_id, workspace_id, uid, direction, sender_type, sender_name, sender_id,
			       body, media_url, media_type, is_private, reactions, reply_to_uid, metadata, created_at
			FROM (
				SELECT id, chat_id, workspace_id, uid, direction, sender_type, sender_name, sender_id,
				       body, media_url, media_type, is_private, reactions, reply_to_uid, metadata, created_at
				FROM chat_messages
				WHERE workspace_id = $1 AND chat_id = $2
				ORDER BY created_at DESC, id DESC
				LIMIT $3
			) sub
			ORDER BY created_at ASC, id ASC
		`
		args = []interface{}{workspaceID, chatID, limit}
		needReverse = false
	}

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list chat messages: %w", err)
	}
	defer rows.Close()

	var messages []domain.ChatMessage
	for rows.Next() {
		var m domain.ChatMessage
		var rxBytes, metaBytes []byte
		if err := rows.Scan(
			&m.ID, &m.ChatID, &m.WorkspaceID, &m.UID, &m.Direction, &m.SenderType,
			&m.SenderName, &m.SenderID, &m.Body, &m.MediaURL, &m.MediaType, &m.IsPrivate,
			&rxBytes, &m.ReplyToUID, &metaBytes, &m.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan chat message: %w", err)
		}
		if len(rxBytes) > 0 {
			_ = json.Unmarshal(rxBytes, &m.Reactions)
		}
		if m.Reactions == nil {
			m.Reactions = []domain.Reaction{}
		}
		if len(metaBytes) > 0 {
			_ = json.Unmarshal(metaBytes, &m.Metadata)
		}
		if m.Metadata == nil {
			m.Metadata = make(map[string]interface{})
		}
		messages = append(messages, m)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	if needReverse {
		for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
			messages[i], messages[j] = messages[j], messages[i]
		}
	}

	return messages, nil
}

// GetChatMessageByUID retrieves a single message by UID.
func (r *ChatRepository) GetChatMessageByUID(ctx context.Context, workspaceID uuid.UUID, uid string) (*domain.ChatMessage, error) {
	if workspaceID == uuid.Nil {
		return nil, ErrInvalidWorkspaceID
	}
	var m domain.ChatMessage
	var rxBytes, metaBytes []byte

	err := r.pool.QueryRow(ctx, `
		SELECT id, chat_id, workspace_id, uid, direction, sender_type, sender_name, sender_id,
		       body, media_url, media_type, is_private, reactions, reply_to_uid, metadata, created_at
		FROM chat_messages
		WHERE workspace_id = $1 AND uid = $2
	`, workspaceID, uid).Scan(
		&m.ID, &m.ChatID, &m.WorkspaceID, &m.UID, &m.Direction, &m.SenderType,
		&m.SenderName, &m.SenderID, &m.Body, &m.MediaURL, &m.MediaType, &m.IsPrivate,
		&rxBytes, &m.ReplyToUID, &metaBytes, &m.CreatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrChatMessageNotFound
		}
		return nil, fmt.Errorf("get chat message: %w", err)
	}

	if len(rxBytes) > 0 {
		_ = json.Unmarshal(rxBytes, &m.Reactions)
	}
	if m.Reactions == nil {
		m.Reactions = []domain.Reaction{}
	}
	if len(metaBytes) > 0 {
		_ = json.Unmarshal(metaBytes, &m.Metadata)
	}
	if m.Metadata == nil {
		m.Metadata = make(map[string]interface{})
	}

	return &m, nil
}

// FindMessageByUID retrieves a single message by UID across workspaces.
func (r *ChatRepository) FindMessageByUID(ctx context.Context, uid string) (*domain.ChatMessage, error) {
	if uid == "" {
		return nil, errors.New("uid is required")
	}
	var m domain.ChatMessage
	var rxBytes, metaBytes []byte

	err := r.pool.QueryRow(ctx, `
		SELECT id, chat_id, workspace_id, uid, direction, sender_type, sender_name, sender_id,
		       body, media_url, media_type, is_private, reactions, reply_to_uid, metadata, created_at
		FROM chat_messages
		WHERE uid = $1
	`, uid).Scan(
		&m.ID, &m.ChatID, &m.WorkspaceID, &m.UID, &m.Direction, &m.SenderType,
		&m.SenderName, &m.SenderID, &m.Body, &m.MediaURL, &m.MediaType, &m.IsPrivate,
		&rxBytes, &m.ReplyToUID, &metaBytes, &m.CreatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrChatMessageNotFound
		}
		return nil, fmt.Errorf("find chat message: %w", err)
	}

	if len(rxBytes) > 0 {
		_ = json.Unmarshal(rxBytes, &m.Reactions)
	}
	if m.Reactions == nil {
		m.Reactions = []domain.Reaction{}
	}
	if len(metaBytes) > 0 {
		_ = json.Unmarshal(metaBytes, &m.Metadata)
	}
	if m.Metadata == nil {
		m.Metadata = make(map[string]interface{})
	}

	return &m, nil
}

// GetReactions returns the current list of reactions for a message.
func (r *ChatRepository) GetReactions(ctx context.Context, workspaceID uuid.UUID, uid string) ([]domain.Reaction, error) {
	msg, err := r.GetChatMessageByUID(ctx, workspaceID, uid)
	if err != nil {
		return nil, err
	}
	if msg.Reactions == nil {
		return []domain.Reaction{}, nil
	}
	return msg.Reactions, nil
}

// UpdateMessageReactions updates the JSONB reactions array on a message.
func (r *ChatRepository) UpdateMessageReactions(ctx context.Context, workspaceID uuid.UUID, uid string, reactions []domain.Reaction) error {
	if reactions == nil {
		reactions = []domain.Reaction{}
	}
	rxJSON, err := json.Marshal(reactions)
	if err != nil {
		return fmt.Errorf("marshal reactions: %w", err)
	}

	res, err := r.pool.Exec(ctx, `
		UPDATE chat_messages SET reactions = $3
		WHERE workspace_id = $1 AND uid = $2
	`, workspaceID, uid, rxJSON)
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return ErrChatMessageNotFound
	}
	return nil
}

// MutateReaction atomically adds, removes, or toggles a reaction on a message under a row lock
// and returns (chatID, resolvedAction, updatedReactions, error).
func (r *ChatRepository) MutateReaction(ctx context.Context, workspaceID uuid.UUID, messageUID, emoji, sender, action string) (uuid.UUID, string, []domain.Reaction, error) {
	if workspaceID == uuid.Nil {
		return uuid.Nil, "", nil, ErrInvalidWorkspaceID
	}
	messageUID = strings.TrimSpace(messageUID)
	if messageUID == "" {
		return uuid.Nil, "", nil, errors.New("message uid is required")
	}

	sender = strings.TrimSpace(sender)
	if sender == "" {
		sender = "operator"
	}

	normAction := strings.ToLower(strings.TrimSpace(action))
	if normAction != "" && normAction != "add" && normAction != "remove" && normAction != "toggle" {
		return uuid.Nil, "", nil, fmt.Errorf("invalid reaction action %q: must be add, remove, or toggle", action)
	}
	if emoji == "" {
		normAction = "remove"
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return uuid.Nil, "", nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var chatID uuid.UUID
	var rxBytes []byte
	err = tx.QueryRow(ctx, `
		SELECT chat_id, reactions
		FROM chat_messages
		WHERE workspace_id = $1 AND uid = $2
		FOR UPDATE
	`, workspaceID, messageUID).Scan(&chatID, &rxBytes)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, "", nil, ErrChatMessageNotFound
		}
		return uuid.Nil, "", nil, fmt.Errorf("get message reactions: %w", err)
	}

	var reactions []domain.Reaction
	if len(rxBytes) > 0 {
		_ = json.Unmarshal(rxBytes, &reactions)
	}
	if reactions == nil {
		reactions = []domain.Reaction{}
	}

	var resolvedAction string
	mutated := false

	switch normAction {
	case "", "toggle":
		hasReacted := false
		for _, rx := range reactions {
			if rx.Emoji == emoji && (rx.Sender == sender || rx.Sender == "operator") {
				hasReacted = true
				break
			}
		}
		if hasReacted {
			resolvedAction = "remove"
			filtered := make([]domain.Reaction, 0, len(reactions))
			for _, rx := range reactions {
				if rx.Emoji == emoji && (rx.Sender == sender || rx.Sender == "operator") {
					mutated = true
					continue
				}
				filtered = append(filtered, rx)
			}
			reactions = filtered
		} else {
			resolvedAction = "add"
			reactions = append(reactions, domain.Reaction{
				Emoji:     emoji,
				Sender:    sender,
				CreatedAt: time.Now().UTC(),
			})
			mutated = true
		}

	case "add":
		resolvedAction = "add"
		found := false
		for _, rx := range reactions {
			if rx.Emoji == emoji && rx.Sender == sender {
				found = true
				break
			}
		}
		if !found {
			reactions = append(reactions, domain.Reaction{
				Emoji:     emoji,
				Sender:    sender,
				CreatedAt: time.Now().UTC(),
			})
			mutated = true
		}

	case "remove":
		resolvedAction = "remove"
		filtered := make([]domain.Reaction, 0, len(reactions))
		for _, rx := range reactions {
			remove := false
			if emoji == "" {
				if rx.Sender == sender || sender == "operator" {
					remove = true
				}
			} else if rx.Emoji == emoji && (rx.Sender == sender || sender == "operator" || rx.Sender == "operator") {
				remove = true
			}
			if remove {
				mutated = true
				continue
			}
			filtered = append(filtered, rx)
		}
		reactions = filtered
	}

	if reactions == nil {
		reactions = []domain.Reaction{}
	}

	if mutated {
		newRxJSON, err := json.Marshal(reactions)
		if err != nil {
			return uuid.Nil, "", nil, fmt.Errorf("marshal reactions: %w", err)
		}
		_, err = tx.Exec(ctx, `
			UPDATE chat_messages
			SET reactions = $3::jsonb
			WHERE workspace_id = $1 AND uid = $2
		`, workspaceID, messageUID, newRxJSON)
		if err != nil {
			return uuid.Nil, "", nil, fmt.Errorf("update reactions: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return uuid.Nil, "", nil, fmt.Errorf("commit tx: %w", err)
	}

	return chatID, resolvedAction, reactions, nil
}

// AddReaction appends a reaction to the message idempotently and returns the updated reactions.
func (r *ChatRepository) AddReaction(ctx context.Context, workspaceID uuid.UUID, uid string, reaction domain.Reaction) ([]domain.Reaction, error) {
	_, _, reactions, err := r.MutateReaction(ctx, workspaceID, uid, reaction.Emoji, reaction.Sender, "add")
	return reactions, err
}

// RemoveReaction removes a reaction from the message and returns the updated reactions.
func (r *ChatRepository) RemoveReaction(ctx context.Context, workspaceID uuid.UUID, uid string, emoji string, sender string) ([]domain.Reaction, error) {
	_, _, reactions, err := r.MutateReaction(ctx, workspaceID, uid, emoji, sender, "remove")
	return reactions, err
}

// ResetUnreadByContact resets unread_count = 0 across all chats belonging to a contact in a workspace.
func (r *ChatRepository) ResetUnreadByContact(ctx context.Context, workspaceID, contactID uuid.UUID) error {
	if workspaceID == uuid.Nil {
		return ErrInvalidWorkspaceID
	}
	if contactID == uuid.Nil {
		return errors.New("contact_id is required")
	}
	_, err := r.pool.Exec(ctx, `
		UPDATE chats
		SET unread_count = 0, updated_at = NOW()
		WHERE workspace_id = $1 AND contact_id = $2 AND unread_count > 0
	`, workspaceID, contactID)
	return err
}

// ConversationSummary represents a contact-level conversation card.
type ConversationSummary struct {
	ChatID            uuid.UUID  `json:"chat_id,omitempty"`
	ContactID         uuid.UUID  `json:"contact_id"`
	ContactName       string     `json:"contact_name"`
	LastMessageBody   string     `json:"last_message_body"`
	LastMessageTime   time.Time  `json:"last_message_time"`
	TotalMessageCount int64      `json:"total_message_count"`
	Channel           string     `json:"channel"`            // channel of last message
	RecipientIdentity string     `json:"recipient_identity"` // recipient identity of last message
	Status            string     `json:"status,omitempty"`
	SnoozedUntil      *time.Time `json:"snoozed_until,omitempty"`
	AssignedEmail     *string    `json:"assigned_email,omitempty"`
	Tags              []string   `json:"tags,omitempty"`
	UnreadCount       int        `json:"unread_count,omitempty"`
	Unread            bool       `json:"unread,omitempty"`
}

// ThreadMessage represents a single message in a chronological conversation thread.
type ThreadMessage struct {
	ID        uuid.UUID         `json:"id"`
	UID       string            `json:"uid,omitempty"`
	TraceID   string            `json:"trace_id"`
	Direction string            `json:"direction"` // "inbound", "outbound", or "internal_note"
	Body      string            `json:"body"`
	CreatedAt time.Time         `json:"created_at"`
	Status    *string           `json:"status"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	Reactions []domain.Reaction `json:"reactions,omitempty"`
}

// ListConversations lists unified conversations grouped by contact_id in a single query from chats and chat_messages.
func (r *ChatRepository) ListConversations(ctx context.Context, workspaceID uuid.UUID, channelFilter string) ([]ConversationSummary, error) {
	if workspaceID == uuid.Nil {
		return nil, ErrInvalidWorkspaceID
	}

	query := `
		WITH ContactChats AS (
			SELECT
				c.id AS chat_id,
				c.workspace_id,
				c.contact_id,
				c.connection_id,
				c.status,
				c.snoozed_until,
				c.assigned_email,
				c.tags,
				c.unread_count,
				c.last_message_at,
				ROW_NUMBER() OVER (
					PARTITION BY c.contact_id
					ORDER BY (c.connection_id IS NOT NULL) DESC, c.last_message_at DESC, c.updated_at DESC
				) AS chat_rn,
				SUM(c.unread_count) OVER (PARTITION BY c.contact_id) AS total_unread_count
			FROM chats c
			WHERE c.workspace_id = $1
		),
		AllMessages AS (
			SELECT
				cm.id,
				cm.created_at,
				COALESCE(cm.body, '') AS body,
				c.contact_id,
				COALESCE(ct.name, '') AS contact_name,
				COALESCE(
					NULLIF(cm.metadata->>'channel', ''),
					conn.channel,
					(SELECT ci.channel FROM contact_identities ci WHERE ci.workspace_id = cm.workspace_id AND ci.contact_id = c.contact_id LIMIT 1),
					''
				) AS channel,
				CASE
					WHEN cm.direction = 'outbound' THEN COALESCE(
						NULLIF(cm.metadata->>'sender_identity', ''),
						NULLIF(cm.sender_id, ''),
						conn.sender_identity,
						''
					)
					ELSE COALESCE(
						NULLIF(cm.metadata->>'to', ''),
						NULLIF(cm.metadata->>'sender_identity', ''),
						conn.sender_identity,
						''
					)
				END AS recipient_identity
			FROM chat_messages cm
			JOIN chats c ON c.id = cm.chat_id AND c.workspace_id = cm.workspace_id
			JOIN contacts ct ON ct.id = c.contact_id AND ct.workspace_id = cm.workspace_id
			LEFT JOIN connections conn ON conn.id = c.connection_id AND conn.workspace_id = cm.workspace_id
			WHERE cm.workspace_id = $1
			  AND NOT cm.is_private
			  AND cm.direction IN ('inbound', 'outbound')
		),
		FilteredMessages AS (
			SELECT * FROM AllMessages
			WHERE ($2 = '' OR recipient_identity = $2 OR channel = $2)
		),
		RankedConversations AS (
			SELECT
				fm.contact_id,
				fm.contact_name,
				fm.channel,
				fm.recipient_identity,
				fm.body,
				fm.created_at,
				ROW_NUMBER() OVER (PARTITION BY fm.contact_id ORDER BY fm.created_at DESC, fm.id DESC) AS rn,
				COUNT(*) OVER (PARTITION BY fm.contact_id) AS total_count
			FROM FilteredMessages fm
		)
		SELECT
			cc.chat_id,
			rc.contact_id,
			COALESCE(rc.contact_name, ''),
			COALESCE(rc.channel, ''),
			COALESCE(rc.recipient_identity, ''),
			COALESCE(rc.body, ''),
			rc.created_at,
			rc.total_count,
			COALESCE(cc.status, 'open'),
			cc.snoozed_until,
			cc.assigned_email,
			COALESCE(cc.tags, '{}'),
			COALESCE(cc.total_unread_count, 0)::int
		FROM RankedConversations rc
		JOIN ContactChats cc ON cc.contact_id = rc.contact_id AND cc.chat_rn = 1
		WHERE rc.rn = 1
		ORDER BY rc.created_at DESC, rc.contact_id ASC
	`

	rows, err := r.pool.Query(ctx, query, workspaceID, channelFilter)
	if err != nil {
		return nil, fmt.Errorf("query conversations list: %w", err)
	}
	defer rows.Close()

	var summaries []ConversationSummary
	for rows.Next() {
		var s ConversationSummary
		if err := rows.Scan(
			&s.ChatID,
			&s.ContactID,
			&s.ContactName,
			&s.Channel,
			&s.RecipientIdentity,
			&s.LastMessageBody,
			&s.LastMessageTime,
			&s.TotalMessageCount,
			&s.Status,
			&s.SnoozedUntil,
			&s.AssignedEmail,
			&s.Tags,
			&s.UnreadCount,
		); err != nil {
			return nil, fmt.Errorf("scan conversation summary: %w", err)
		}
		s.Unread = s.UnreadCount > 0
		summaries = append(summaries, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate conversations: %w", err)
	}

	return summaries, nil
}

// ListThreadByContact returns chronological thread messages for a contact in a workspace from chat_messages and message_dispatches.
func (r *ChatRepository) ListThreadByContact(ctx context.Context, workspaceID uuid.UUID, contactID uuid.UUID, afterID *uuid.UUID) ([]ThreadMessage, error) {
	if workspaceID == uuid.Nil {
		return nil, ErrInvalidWorkspaceID
	}

	query := `
		SELECT
			cm.id,
			cm.uid AS trace_id,
			cm.direction,
			COALESCE(cm.body, '') AS body,
			cm.created_at,
			CASE WHEN cm.direction = 'outbound' THEN md.status ELSE NULL END AS status,
			jsonb_strip_nulls(
				jsonb_build_object(
					'sender_name', NULLIF(cm.sender_name, ''),
					'sender_type', NULLIF(cm.sender_type, ''),
					'author_name', CASE WHEN cm.is_private OR cm.direction = 'internal_note' THEN COALESCE(NULLIF(cm.sender_name, ''), 'AI Assistant') ELSE NULLIF(cm.sender_name, '') END,
					'is_private', CASE WHEN cm.is_private THEN 'true' ELSE NULL END,
					'reply_to_uid', cm.reply_to_uid,
					'media_url', cm.media_url,
					'media_type', cm.media_type
				)
			) || COALESCE(cm.metadata, '{}'::jsonb) AS metadata,
			cm.uid AS uid,
			COALESCE(cm.reactions, '[]'::jsonb) AS reactions
		FROM chat_messages cm
		JOIN chats c ON c.id = cm.chat_id AND c.workspace_id = cm.workspace_id
		LEFT JOIN LATERAL (
			SELECT d.status
			FROM message_dispatches d
			WHERE d.workspace_id = cm.workspace_id AND d.trace_id = cm.uid
			ORDER BY d.updated_at DESC
			LIMIT 1
		) md ON cm.direction = 'outbound'
		WHERE cm.workspace_id = $1
		  AND c.contact_id = $2
		  AND (
			  $3::uuid IS NULL
			  OR (cm.created_at, cm.id) > (
				  SELECT m2.created_at, m2.id
				  FROM chat_messages m2
				  WHERE m2.workspace_id = $1 AND m2.id = $3::uuid
			  )
		  )
		ORDER BY cm.created_at ASC, cm.id ASC
	`

	rows, err := r.pool.Query(ctx, query, workspaceID, contactID, afterID)
	if err != nil {
		return nil, fmt.Errorf("query thread messages: %w", err)
	}
	defer rows.Close()

	var messages []ThreadMessage
	for rows.Next() {
		var m ThreadMessage
		var metaBytes []byte
		var reactionsBytes []byte
		if err := rows.Scan(&m.ID, &m.TraceID, &m.Direction, &m.Body, &m.CreatedAt, &m.Status, &metaBytes, &m.UID, &reactionsBytes); err != nil {
			return nil, fmt.Errorf("scan thread message: %w", err)
		}
		if len(reactionsBytes) > 0 {
			_ = json.Unmarshal(reactionsBytes, &m.Reactions)
		}
		if len(metaBytes) > 0 {
			var metaAny map[string]any
			if err := json.Unmarshal(metaBytes, &metaAny); err == nil && len(metaAny) > 0 {
				m.Metadata = make(map[string]string)
				for k, v := range metaAny {
					switch val := v.(type) {
					case string:
						m.Metadata[k] = val
					default:
						if b, err := json.Marshal(val); err == nil {
							m.Metadata[k] = string(b)
						}
					}
				}
			}
		}
		messages = append(messages, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate thread messages: %w", err)
	}

	return messages, nil
}

