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
		SELECT id, workspace_id, connection_id, contact_id, status, assigned_user_id, assigned_email,
		       tags, unread_count, ai_disabled, service_window_expires_at, last_message_at, metadata,
		       created_at, updated_at
		FROM chats
		WHERE workspace_id = $1 AND contact_id = $2 AND (connection_id = $3 OR (connection_id IS NULL AND $3 IS NULL))
	`, workspaceID, contactID, connectionID).Scan(
		&chat.ID, &chat.WorkspaceID, &chat.ConnectionID, &chat.ContactID, &chat.Status,
		&chat.AssignedUserID, &chat.AssignedEmail, &chat.Tags, &chat.UnreadCount, &chat.AIDisabled,
		&chat.ServiceWindowExpiresAt, &chat.LastMessageAt, &metaBytes, &chat.CreatedAt, &chat.UpdatedAt,
	)

	if err == nil {
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
		RETURNING id, workspace_id, connection_id, contact_id, status, assigned_user_id, assigned_email,
		          tags, unread_count, ai_disabled, service_window_expires_at, last_message_at, metadata,
		          created_at, updated_at
	`, workspaceID, connectionID, contactID).Scan(
		&chat.ID, &chat.WorkspaceID, &chat.ConnectionID, &chat.ContactID, &chat.Status,
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
		SELECT id, workspace_id, connection_id, contact_id, status, assigned_user_id, assigned_email,
		       tags, unread_count, ai_disabled, service_window_expires_at, last_message_at, metadata,
		       created_at, updated_at
		FROM chats
		WHERE workspace_id = $1 AND id = $2
	`, workspaceID, chatID).Scan(
		&chat.ID, &chat.WorkspaceID, &chat.ConnectionID, &chat.ContactID, &chat.Status,
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
		SELECT id, workspace_id, connection_id, contact_id, status, assigned_user_id, assigned_email,
		       tags, unread_count, ai_disabled, service_window_expires_at, last_message_at, metadata,
		       created_at, updated_at
		FROM chats
		WHERE workspace_id = $1 AND contact_id = $2
		ORDER BY last_message_at DESC
		LIMIT 1
	`, workspaceID, contactID).Scan(
		&chat.ID, &chat.WorkspaceID, &chat.ConnectionID, &chat.ContactID, &chat.Status,
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
		SELECT id, workspace_id, connection_id, contact_id, status, assigned_user_id, assigned_email,
		       tags, unread_count, ai_disabled, service_window_expires_at, last_message_at, metadata,
		       created_at, updated_at
		FROM chats
		WHERE id = $1
	`, chatID).Scan(
		&chat.ID, &chat.WorkspaceID, &chat.ConnectionID, &chat.ContactID, &chat.Status,
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
		SELECT id, workspace_id, connection_id, contact_id, status, assigned_user_id, assigned_email,
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
			&c.ID, &c.WorkspaceID, &c.ConnectionID, &c.ContactID, &c.Status,
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
	res, err := r.pool.Exec(ctx, `
		UPDATE chats SET status = $3, updated_at = NOW()
		WHERE workspace_id = $1 AND id = $2
	`, workspaceID, chatID, status)
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

// AddReaction appends a reaction to the message idempotently and returns the updated reactions.
func (r *ChatRepository) AddReaction(ctx context.Context, workspaceID uuid.UUID, uid string, reaction domain.Reaction) ([]domain.Reaction, error) {
	if workspaceID == uuid.Nil {
		return nil, ErrInvalidWorkspaceID
	}
	if uid == "" {
		return nil, errors.New("message uid is required")
	}
	if reaction.CreatedAt.IsZero() {
		reaction.CreatedAt = time.Now().UTC()
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var rxBytes []byte
	err = tx.QueryRow(ctx, `
		SELECT reactions
		FROM chat_messages
		WHERE workspace_id = $1 AND uid = $2
		FOR UPDATE
	`, workspaceID, uid).Scan(&rxBytes)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrChatMessageNotFound
		}
		return nil, fmt.Errorf("get message reactions: %w", err)
	}

	var reactions []domain.Reaction
	if len(rxBytes) > 0 {
		_ = json.Unmarshal(rxBytes, &reactions)
	}
	if reactions == nil {
		reactions = []domain.Reaction{}
	}

	// Idempotency: if already exists with same emoji and sender, return without duplicating
	found := false
	for _, ex := range reactions {
		if ex.Emoji == reaction.Emoji && ex.Sender == reaction.Sender {
			found = true
			break
		}
	}
	if !found {
		reactions = append(reactions, reaction)
		newRxJSON, err := json.Marshal(reactions)
		if err != nil {
			return nil, fmt.Errorf("marshal reactions: %w", err)
		}
		_, err = tx.Exec(ctx, `
			UPDATE chat_messages
			SET reactions = $3
			WHERE workspace_id = $1 AND uid = $2
		`, workspaceID, uid, newRxJSON)
		if err != nil {
			return nil, fmt.Errorf("update reactions: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}

	return reactions, nil
}

// RemoveReaction removes a reaction from the message and returns the updated reactions.
func (r *ChatRepository) RemoveReaction(ctx context.Context, workspaceID uuid.UUID, uid string, emoji string, sender string) ([]domain.Reaction, error) {
	if workspaceID == uuid.Nil {
		return nil, ErrInvalidWorkspaceID
	}
	if uid == "" {
		return nil, errors.New("message uid is required")
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var rxBytes []byte
	err = tx.QueryRow(ctx, `
		SELECT reactions
		FROM chat_messages
		WHERE workspace_id = $1 AND uid = $2
		FOR UPDATE
	`, workspaceID, uid).Scan(&rxBytes)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrChatMessageNotFound
		}
		return nil, fmt.Errorf("get message reactions: %w", err)
	}

	var reactions []domain.Reaction
	if len(rxBytes) > 0 {
		_ = json.Unmarshal(rxBytes, &reactions)
	}
	if reactions == nil {
		reactions = []domain.Reaction{}
	}

	updated := make([]domain.Reaction, 0, len(reactions))
	removed := false
	for _, ex := range reactions {
		match := true
		if emoji != "" && ex.Emoji != emoji {
			match = false
		}
		if sender != "" && ex.Sender != sender {
			match = false
		}
		if match {
			removed = true
		} else {
			updated = append(updated, ex)
		}
	}

	if removed {
		newRxJSON, err := json.Marshal(updated)
		if err != nil {
			return nil, fmt.Errorf("marshal reactions: %w", err)
		}
		_, err = tx.Exec(ctx, `
			UPDATE chat_messages
			SET reactions = $3
			WHERE workspace_id = $1 AND uid = $2
		`, workspaceID, uid, newRxJSON)
		if err != nil {
			return nil, fmt.Errorf("update reactions: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}

	return updated, nil
}
