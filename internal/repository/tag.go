package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

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

func (r *TagRepository) Pool() *pgxpool.Pool {
	if r == nil {
		return nil
	}
	return r.pool
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

func cleanTagNames(names []string) []string {
	if len(names) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(names))
	out := make([]string, 0, len(names))
	for _, raw := range names {
		clean := strings.TrimSpace(raw)
		if clean == "" {
			continue
		}
		lower := strings.ToLower(clean)
		if _, ok := seen[lower]; ok {
			continue
		}
		seen[lower] = struct{}{}
		out = append(out, clean)
	}
	return out
}

func ensureTagID(ctx context.Context, tx pgx.Tx, workspaceID uuid.UUID, name string) (uuid.UUID, error) {
	var tagID uuid.UUID
	err := tx.QueryRow(ctx, `
		SELECT id FROM tags
		WHERE workspace_id = $1 AND LOWER(name) = LOWER($2)
		LIMIT 1
	`, workspaceID, name).Scan(&tagID)
	if err == nil {
		return tagID, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, fmt.Errorf("lookup tag %q: %w", name, err)
	}

	err = tx.QueryRow(ctx, `
		INSERT INTO tags (workspace_id, name, color)
		VALUES ($1, $2, '#6B7280')
		ON CONFLICT (workspace_id, LOWER(name)) DO UPDATE SET name = tags.name
		RETURNING id
	`, workspaceID, name).Scan(&tagID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("ensure tag %q: %w", name, err)
	}
	return tagID, nil
}

// MutateContactTags atomically replaces (when set != nil) or adds/removes (when set == nil)
// a contact's tags by name in the authoritative tags + contact_tags store, returning the
// contact's resulting tag names ordered by t.name ASC.
func (r *TagRepository) MutateContactTags(
	ctx context.Context,
	workspaceID, contactID uuid.UUID,
	set, add, remove []string,
) ([]string, error) {
	if workspaceID == uuid.Nil {
		return nil, ErrInvalidWorkspaceID
	}
	if contactID == uuid.Nil {
		return nil, ErrContactNotFound
	}
	if r == nil || r.pool == nil {
		return nil, errors.New("tag repository pool not configured")
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var lockedID uuid.UUID
	err = tx.QueryRow(ctx, `
		SELECT id FROM contacts
		WHERE workspace_id = $1 AND id = $2
		FOR UPDATE
	`, workspaceID, contactID).Scan(&lockedID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrContactNotFound
		}
		return nil, fmt.Errorf("verify contact in workspace: %w", err)
	}

	if set != nil {
		cleanedSet := cleanTagNames(set)
		tagIDs := make([]uuid.UUID, 0, len(cleanedSet))
		for _, name := range cleanedSet {
			tagID, err := ensureTagID(ctx, tx, workspaceID, name)
			if err != nil {
				return nil, err
			}
			tagIDs = append(tagIDs, tagID)
		}

		if len(tagIDs) == 0 {
			if _, err := tx.Exec(ctx, `DELETE FROM contact_tags WHERE contact_id = $1`, contactID); err != nil {
				return nil, fmt.Errorf("clear contact tags: %w", err)
			}
		} else {
			if _, err := tx.Exec(ctx, `
				DELETE FROM contact_tags
				WHERE contact_id = $1 AND NOT (tag_id = ANY($2))
			`, contactID, tagIDs); err != nil {
				return nil, fmt.Errorf("prune contact tags: %w", err)
			}
			for _, tagID := range tagIDs {
				if _, err := tx.Exec(ctx, `
					INSERT INTO contact_tags (contact_id, tag_id)
					VALUES ($1, $2)
					ON CONFLICT (contact_id, tag_id) DO NOTHING
				`, contactID, tagID); err != nil {
					return nil, fmt.Errorf("link contact tag: %w", err)
				}
			}
		}
	} else {
		cleanedRemove := cleanTagNames(remove)
		removeSet := make(map[string]struct{}, len(cleanedRemove))
		removeLower := make([]string, 0, len(cleanedRemove))
		for _, rem := range cleanedRemove {
			lower := strings.ToLower(rem)
			removeSet[lower] = struct{}{}
			removeLower = append(removeLower, lower)
		}

		cleanedAdd := cleanTagNames(add)
		for _, name := range cleanedAdd {
			if _, removed := removeSet[strings.ToLower(name)]; removed {
				continue
			}
			tagID, err := ensureTagID(ctx, tx, workspaceID, name)
			if err != nil {
				return nil, err
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO contact_tags (contact_id, tag_id)
				VALUES ($1, $2)
				ON CONFLICT (contact_id, tag_id) DO NOTHING
			`, contactID, tagID); err != nil {
				return nil, fmt.Errorf("link contact tag: %w", err)
			}
		}

		if len(removeLower) > 0 {
			if _, err := tx.Exec(ctx, `
				DELETE FROM contact_tags ct
				USING tags t
				WHERE ct.contact_id = $1
				  AND ct.tag_id = t.id
				  AND t.workspace_id = $2
				  AND LOWER(t.name) = ANY($3)
			`, contactID, workspaceID, removeLower); err != nil {
				return nil, fmt.Errorf("remove contact tags: %w", err)
			}
		}
	}

	if _, err := tx.Exec(ctx, `
		UPDATE contacts SET updated_at = NOW()
		WHERE workspace_id = $1 AND id = $2
	`, workspaceID, contactID); err != nil {
		return nil, fmt.Errorf("touch contact updated_at: %w", err)
	}

	var resultTags []string
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(ARRAY_AGG(t.name ORDER BY t.name ASC), '{}')
		FROM contact_tags ct
		JOIN tags t ON t.id = ct.tag_id
		WHERE ct.contact_id = $1 AND t.workspace_id = $2
	`, contactID, workspaceID).Scan(&resultTags); err != nil {
		return nil, fmt.Errorf("load contact tags: %w", err)
	}
	if resultTags == nil {
		resultTags = []string{}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit contact tags tx: %w", err)
	}

	return resultTags, nil
}

// AddTagsByName attaches the given tag names to a contact (auto-creating workspace tags when needed)
// and returns the contact's resulting sorted tag names.
func (r *TagRepository) AddTagsByName(ctx context.Context, workspaceID, contactID uuid.UUID, names []string) ([]string, error) {
	return r.MutateContactTags(ctx, workspaceID, contactID, nil, names, nil)
}

// RemoveTagsByName unlinks the given tag names (case-insensitively) from a contact without deleting
// the workspace tag catalog entries, and returns the contact's resulting sorted tag names.
func (r *TagRepository) RemoveTagsByName(ctx context.Context, workspaceID, contactID uuid.UUID, names []string) ([]string, error) {
	return r.MutateContactTags(ctx, workspaceID, contactID, nil, nil, names)
}

// SetTagsByName replaces the contact's tag set with the given tag names and returns the resulting sorted tag names.
func (r *TagRepository) SetTagsByName(ctx context.Context, workspaceID, contactID uuid.UUID, names []string) ([]string, error) {
	if names == nil {
		names = []string{}
	}
	return r.MutateContactTags(ctx, workspaceID, contactID, names, nil, nil)
}

// ListContactsByTag retrieves all contacts matching a given tag ID.
func (r *TagRepository) ListContactsByTag(ctx context.Context, workspaceID, tagID uuid.UUID) ([]domain.Contact, error) {
	if workspaceID == uuid.Nil {
		return nil, ErrInvalidWorkspaceID
	}
	rows, err := r.pool.Query(ctx, `
		SELECT c.id, c.workspace_id, c.name, c.email, c.attributes,
		       COALESCE((
		           SELECT ARRAY_AGG(t.name ORDER BY t.name ASC)
		           FROM contact_tags ct2
		           JOIN tags t ON t.id = ct2.tag_id
		           WHERE ct2.contact_id = c.id AND t.workspace_id = c.workspace_id
		       ), '{}') AS tags,
		       c.created_at, c.updated_at
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
		if err := rows.Scan(&c.ID, &c.WorkspaceID, &c.Name, &email, &attrsRaw, &tags, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		c.Email = email
		if len(attrsRaw) > 0 {
			_ = json.Unmarshal(attrsRaw, &c.Attributes)
		}
		if c.Attributes == nil {
			c.Attributes = make(map[string]string)
		}
		if tags == nil {
			tags = []string{}
		}
		c.Tags = tags
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
