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
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pablojhp.pergo/internal/domain"
)

var (
	ErrTagNotFound      = errors.New("tag not found")
	ErrTagAlreadyExists = errors.New("tag with this name already exists in workspace")
)

type TagRepository struct {
	pool *pgxpool.Pool
}

func NewTagRepository(pool *pgxpool.Pool) *TagRepository {
	return &TagRepository{pool: pool}
}

// CreateTag creates a new workspace-scoped tag.
func (r *TagRepository) CreateTag(ctx context.Context, workspaceID uuid.UUID, name, color string) (*domain.Tag, error) {
	if workspaceID == uuid.Nil {
		return nil, ErrInvalidWorkspaceID
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("tag name cannot be empty")
	}
	if color == "" {
		color = "#6B7280"
	}

	var tag domain.Tag
	err := r.pool.QueryRow(ctx, `
		INSERT INTO tags (workspace_id, name, color)
		VALUES ($1, $2, $3)
		RETURNING id, workspace_id, name, color, created_at
	`, workspaceID, name, color).Scan(&tag.ID, &tag.WorkspaceID, &tag.Name, &tag.Color, &tag.CreatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique_violation
			return nil, ErrTagAlreadyExists
		}
		return nil, fmt.Errorf("failed to create tag: %w", err)
	}

	return &tag, nil
}

// GetTagByID retrieves a tag by ID within a workspace.
func (r *TagRepository) GetTagByID(ctx context.Context, workspaceID, tagID uuid.UUID) (*domain.Tag, error) {
	if workspaceID == uuid.Nil {
		return nil, ErrInvalidWorkspaceID
	}
	var tag domain.Tag
	err := r.pool.QueryRow(ctx, `
		SELECT id, workspace_id, name, color, created_at
		FROM tags
		WHERE workspace_id = $1 AND id = $2
	`, workspaceID, tagID).Scan(&tag.ID, &tag.WorkspaceID, &tag.Name, &tag.Color, &tag.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrTagNotFound
		}
		return nil, fmt.Errorf("failed to get tag: %w", err)
	}

	return &tag, nil
}

// ListTags lists all tags in a workspace ordered by name.
func (r *TagRepository) ListTags(ctx context.Context, workspaceID uuid.UUID) ([]domain.Tag, error) {
	if workspaceID == uuid.Nil {
		return nil, ErrInvalidWorkspaceID
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, workspace_id, name, color, created_at
		FROM tags
		WHERE workspace_id = $1
		ORDER BY name ASC
	`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("failed to list tags: %w", err)
	}
	defer rows.Close()

	var tags []domain.Tag
	for rows.Next() {
		var t domain.Tag
		if err := rows.Scan(&t.ID, &t.WorkspaceID, &t.Name, &t.Color, &t.CreatedAt); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	return tags, nil
}

// DeleteTag deletes a tag by ID within a workspace.
func (r *TagRepository) DeleteTag(ctx context.Context, workspaceID, tagID uuid.UUID) error {
	if workspaceID == uuid.Nil {
		return ErrInvalidWorkspaceID
	}
	res, err := r.pool.Exec(ctx, `
		DELETE FROM tags WHERE workspace_id = $1 AND id = $2
	`, workspaceID, tagID)
	if err != nil {
		return fmt.Errorf("failed to delete tag: %w", err)
	}
	if res.RowsAffected() == 0 {
		return ErrTagNotFound
	}
	return nil
}

// AddTagToContact links a tag to a contact.
func (r *TagRepository) AddTagToContact(ctx context.Context, workspaceID, contactID, tagID uuid.UUID) error {
	if workspaceID == uuid.Nil {
		return ErrInvalidWorkspaceID
	}
	// Verify tag belongs to workspace
	_, err := r.GetTagByID(ctx, workspaceID, tagID)
	if err != nil {
		return err
	}

	// Verify contact belongs to workspace
	var exists bool
	err = r.pool.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM contacts WHERE workspace_id = $1 AND id = $2)
	`, workspaceID, contactID).Scan(&exists)
	if err != nil {
		return err
	}
	if !exists {
		return errors.New("contact not found in workspace")
	}

	_, err = r.pool.Exec(ctx, `
		INSERT INTO contact_tags (contact_id, tag_id)
		VALUES ($1, $2)
		ON CONFLICT (contact_id, tag_id) DO NOTHING
	`, contactID, tagID)
	if err != nil {
		return fmt.Errorf("failed to add tag to contact: %w", err)
	}

	return nil
}

// RemoveTagFromContact unlinks a tag from a contact.
func (r *TagRepository) RemoveTagFromContact(ctx context.Context, workspaceID, contactID, tagID uuid.UUID) error {
	if workspaceID == uuid.Nil {
		return ErrInvalidWorkspaceID
	}
	res, err := r.pool.Exec(ctx, `
		DELETE FROM contact_tags
		WHERE contact_id = $1 AND tag_id = $2
		AND EXISTS (SELECT 1 FROM tags WHERE id = $2 AND workspace_id = $3)
	`, contactID, tagID, workspaceID)
	if err != nil {
		return fmt.Errorf("failed to remove tag from contact: %w", err)
	}
	if res.RowsAffected() == 0 {
		return errors.New("tag or association not found")
	}
	return nil
}

// GetContactTags retrieves all tags associated with a specific contact.
func (r *TagRepository) GetContactTags(ctx context.Context, workspaceID, contactID uuid.UUID) ([]domain.Tag, error) {
	if workspaceID == uuid.Nil {
		return nil, ErrInvalidWorkspaceID
	}
	rows, err := r.pool.Query(ctx, `
		SELECT t.id, t.workspace_id, t.name, t.color, t.created_at
		FROM tags t
		JOIN contact_tags ct ON ct.tag_id = t.id
		WHERE t.workspace_id = $1 AND ct.contact_id = $2
		ORDER BY t.name ASC
	`, workspaceID, contactID)
	if err != nil {
		return nil, fmt.Errorf("failed to get contact tags: %w", err)
	}
	defer rows.Close()

	var tags []domain.Tag
	for rows.Next() {
		var t domain.Tag
		if err := rows.Scan(&t.ID, &t.WorkspaceID, &t.Name, &t.Color, &t.CreatedAt); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	return tags, nil
}

// ListContactsByTag retrieves all contacts matching a given tag ID.
func (r *TagRepository) ListContactsByTag(ctx context.Context, workspaceID, tagID uuid.UUID) ([]domain.Contact, error) {
	if workspaceID == uuid.Nil {
		return nil, ErrInvalidWorkspaceID
	}
	rows, err := r.pool.Query(ctx, `
		SELECT c.id, c.workspace_id, c.name, c.email, c.attributes, c.tags, c.closed_at, c.created_at, c.updated_at, c.bot_active, c.bot_paused_at
		FROM contacts c
		JOIN contact_tags ct ON ct.contact_id = c.id
		WHERE c.workspace_id = $1 AND ct.tag_id = $2
		ORDER BY c.created_at DESC
	`, workspaceID, tagID)
	if err != nil {
		return nil, fmt.Errorf("failed to list contacts by tag: %w", err)
	}
	defer rows.Close()

	var contacts []domain.Contact
	var contactIDs []uuid.UUID
	for rows.Next() {
		var c domain.Contact
		var email *string
		var attrsRaw []byte
		var tags []string
		var closedAt, botPausedAt *time.Time
		if err := rows.Scan(&c.ID, &c.WorkspaceID, &c.Name, &email, &attrsRaw, &tags, &closedAt, &c.CreatedAt, &c.UpdatedAt, &c.BotActive, &botPausedAt); err != nil {
			return nil, err
		}
		c.Email = email
		if len(attrsRaw) > 0 {
			_ = json.Unmarshal(attrsRaw, &c.Attributes)
		}
		if c.Attributes == nil {
			c.Attributes = make(map[string]string)
		}
		c.Tags = tags
		c.ClosedAt = closedAt
		c.BotPausedAt = botPausedAt
		contacts = append(contacts, c)
		contactIDs = append(contactIDs, c.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(contacts) > 0 {
		identRows, err := r.pool.Query(ctx, `
			SELECT id, contact_id, workspace_id, channel, sender_identity, created_at
			FROM contact_identities
			WHERE workspace_id = $1 AND contact_id = ANY($2)
		`, workspaceID, contactIDs)
		if err != nil {
			return nil, fmt.Errorf("failed to list contact identities by tag: %w", err)
		}
		defer identRows.Close()

		identMap := make(map[uuid.UUID][]domain.ContactIdentity)
		for identRows.Next() {
			var ci domain.ContactIdentity
			if err := identRows.Scan(&ci.ID, &ci.ContactID, &ci.WorkspaceID, &ci.Channel, &ci.SenderIdentity, &ci.CreatedAt); err != nil {
				return nil, err
			}
			identMap[ci.ContactID] = append(identMap[ci.ContactID], ci)
		}
		if err := identRows.Err(); err != nil {
			return nil, err
		}

		for i := range contacts {
			contacts[i].Identities = identMap[contacts[i].ID]
		}
	}

	return contacts, nil
}
