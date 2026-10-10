// Package repository provides data access operations for workspaces and API keys.
package repository

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Workspace represents a workspace entity.
type Workspace struct {
	ID                 uuid.UUID `json:"id"`
	Name               string    `json:"name"`
	PIIOptIn           bool      `json:"pii_opt_in"`
	WebhookSecret      *string   `json:"webhook_secret,omitempty"`
	FlowWebhookURL     *string   `json:"flow_webhook_url,omitempty"`
	MediaRetentionDays int       `json:"media_retention_days"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// WorkspaceRepository provides CRUD operations for workspaces.
type WorkspaceRepository struct {
	pool *pgxpool.Pool
}

// NewWorkspaceRepository creates a new WorkspaceRepository.
func NewWorkspaceRepository(pool *pgxpool.Pool) *WorkspaceRepository {
	return &WorkspaceRepository{pool: pool}
}

// Create inserts a new workspace and returns it.
func (r *WorkspaceRepository) Create(ctx context.Context, name string) (*Workspace, error) {
	var ws Workspace
	err := r.pool.QueryRow(ctx,
		`INSERT INTO workspaces (name) VALUES ($1) RETURNING id, name, pii_opt_in, webhook_secret, flow_webhook_url, media_retention_days, created_at, updated_at`,
		name,
	).Scan(&ws.ID, &ws.Name, &ws.PIIOptIn, &ws.WebhookSecret, &ws.FlowWebhookURL, &ws.MediaRetentionDays, &ws.CreatedAt, &ws.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &ws, nil
}

// CreateWithID inserts a new workspace with a specific ID, updating the name on conflict.
func (r *WorkspaceRepository) CreateWithID(ctx context.Context, id uuid.UUID, name string) (*Workspace, error) {
	if id == uuid.Nil {
		return nil, ErrInvalidWorkspaceID
	}
	var ws Workspace
	err := r.pool.QueryRow(ctx,
		`INSERT INTO workspaces (id, name) VALUES ($1, $2)
		 ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, updated_at = now()
		 RETURNING id, name, pii_opt_in, webhook_secret, flow_webhook_url, media_retention_days, created_at, updated_at`,
		id, name,
	).Scan(&ws.ID, &ws.Name, &ws.PIIOptIn, &ws.WebhookSecret, &ws.FlowWebhookURL, &ws.MediaRetentionDays, &ws.CreatedAt, &ws.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &ws, nil
}

// EnsureWorkspace returns an existing workspace if available, or creates a new workspace dynamically if the database contains zero workspaces.
func (r *WorkspaceRepository) EnsureWorkspace(ctx context.Context, defaultName string) (*Workspace, error) {
	ws, err := r.GetEarliest(ctx)
	if err == nil && ws != nil {
		return ws, nil
	}
	if defaultName == "" {
		defaultName = "Agora"
	}
	return r.Create(ctx, defaultName)
}

// GetEarliest returns the earliest created workspace in the database (ORDER BY created_at ASC LIMIT 1).
// Returns ErrWorkspaceNotFound if no workspaces exist.
func (r *WorkspaceRepository) GetEarliest(ctx context.Context) (*Workspace, error) {
	var ws Workspace
	err := r.pool.QueryRow(ctx,
		`SELECT id, name, pii_opt_in, webhook_secret, flow_webhook_url, media_retention_days, created_at, updated_at FROM workspaces ORDER BY created_at ASC LIMIT 1`,
	).Scan(&ws.ID, &ws.Name, &ws.PIIOptIn, &ws.WebhookSecret, &ws.FlowWebhookURL, &ws.MediaRetentionDays, &ws.CreatedAt, &ws.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrWorkspaceNotFound
		}
		return nil, err
	}
	return &ws, nil
}

// GetByID retrieves a workspace by ID.
func (r *WorkspaceRepository) GetByID(ctx context.Context, id uuid.UUID) (*Workspace, error) {
	if id == uuid.Nil {
		return nil, ErrInvalidWorkspaceID
	}
	var ws Workspace
	err := r.pool.QueryRow(ctx,
		`SELECT id, name, pii_opt_in, webhook_secret, flow_webhook_url, media_retention_days, created_at, updated_at FROM workspaces WHERE id = $1`,
		id,
	).Scan(&ws.ID, &ws.Name, &ws.PIIOptIn, &ws.WebhookSecret, &ws.FlowWebhookURL, &ws.MediaRetentionDays, &ws.CreatedAt, &ws.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrWorkspaceNotFound
		}
		return nil, err
	}
	return &ws, nil
}

// GetByName retrieves a workspace by unique name.
func (r *WorkspaceRepository) GetByName(ctx context.Context, name string) (*Workspace, error) {
	var ws Workspace
	err := r.pool.QueryRow(ctx,
		`SELECT id, name, pii_opt_in, webhook_secret, flow_webhook_url, media_retention_days, created_at, updated_at FROM workspaces WHERE name = $1`,
		name,
	).Scan(&ws.ID, &ws.Name, &ws.PIIOptIn, &ws.WebhookSecret, &ws.FlowWebhookURL, &ws.MediaRetentionDays, &ws.CreatedAt, &ws.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrWorkspaceNotFound
		}
		return nil, err
	}
	return &ws, nil
}

var (
	ErrWorkspaceNotFound = errors.New("workspace not found")
)

// SetWebhookSecret sets or updates a workspace's webhook secret key.
func (r *WorkspaceRepository) SetWebhookSecret(ctx context.Context, id uuid.UUID, secret string) error {
	if id == uuid.Nil {
		return ErrInvalidWorkspaceID
	}
	tag, err := r.pool.Exec(ctx,
		`UPDATE workspaces SET webhook_secret = $2, updated_at = NOW() WHERE id = $1`,
		id, secret,
	)
	if err != nil {
		return fmt.Errorf("failed to set webhook secret: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("workspace %s: %w", id, ErrWorkspaceNotFound)
	}
	return nil
}

// SetFlowWebhookURL sets or updates a workspace's Meta Flow webhook URL.
func (r *WorkspaceRepository) SetFlowWebhookURL(ctx context.Context, id uuid.UUID, flowWebhookURL *string) error {
	if id == uuid.Nil {
		return ErrInvalidWorkspaceID
	}
	tag, err := r.pool.Exec(ctx,
		`UPDATE workspaces SET flow_webhook_url = $2, updated_at = NOW() WHERE id = $1`,
		id, flowWebhookURL,
	)
	if err != nil {
		return fmt.Errorf("failed to set flow webhook url: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("workspace %s: %w", id, ErrWorkspaceNotFound)
	}
	return nil
}

// GenerateWebhookSecret generates a new 32-byte hex-encoded secret for a workspace.
func (r *WorkspaceRepository) GenerateWebhookSecret(ctx context.Context, id uuid.UUID) (string, error) {
	if id == uuid.Nil {
		return "", ErrInvalidWorkspaceID
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate random secret: %w", err)
	}
	secret := hex.EncodeToString(b)
	if err := r.SetWebhookSecret(ctx, id, secret); err != nil {
		return "", err
	}
	return secret, nil
}

// Count returns the total number of workspaces.
func (r *WorkspaceRepository) Count(ctx context.Context) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM workspaces`).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

// List returns all workspaces ordered by created_at descending.
func (r *WorkspaceRepository) List(ctx context.Context, limit int) ([]Workspace, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx,
		`SELECT id, name, pii_opt_in, webhook_secret, flow_webhook_url, media_retention_days, created_at, updated_at FROM workspaces ORDER BY created_at DESC LIMIT $1`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var workspaces []Workspace
	for rows.Next() {
		var ws Workspace
		if err := rows.Scan(&ws.ID, &ws.Name, &ws.PIIOptIn, &ws.WebhookSecret, &ws.FlowWebhookURL, &ws.MediaRetentionDays, &ws.CreatedAt, &ws.UpdatedAt); err != nil {
			return nil, err
		}
		workspaces = append(workspaces, ws)
	}
	return workspaces, rows.Err()
}

// SetMediaRetentionDays updates a workspace's data retention policy in days.
func (r *WorkspaceRepository) SetMediaRetentionDays(ctx context.Context, id uuid.UUID, days int) error {
	if id == uuid.Nil {
		return ErrInvalidWorkspaceID
	}
	tag, err := r.pool.Exec(ctx,
		`UPDATE workspaces SET media_retention_days = $2, updated_at = NOW() WHERE id = $1`,
		id, days,
	)
	if err != nil {
		return fmt.Errorf("failed to set media retention days: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("workspace %s: %w", id, ErrWorkspaceNotFound)
	}
	return nil
}

// ListActiveRetentionPolicies retrieves all workspaces with active retention policy (media_retention_days > 0).
func (r *WorkspaceRepository) ListActiveRetentionPolicies(ctx context.Context) ([]Workspace, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, name, pii_opt_in, webhook_secret, flow_webhook_url, media_retention_days, created_at, updated_at 
		 FROM workspaces 
		 WHERE media_retention_days > 0 
		 ORDER BY created_at ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var workspaces []Workspace
	for rows.Next() {
		var ws Workspace
		if err := rows.Scan(&ws.ID, &ws.Name, &ws.PIIOptIn, &ws.WebhookSecret, &ws.FlowWebhookURL, &ws.MediaRetentionDays, &ws.CreatedAt, &ws.UpdatedAt); err != nil {
			return nil, err
		}
		workspaces = append(workspaces, ws)
	}
	return workspaces, rows.Err()
}

// Delete removes a workspace by ID. Associated API keys cascade-delete via foreign key.
func (r *WorkspaceRepository) Delete(ctx context.Context, id uuid.UUID) error {
	if id == uuid.Nil {
		return ErrInvalidWorkspaceID
	}
	_, err := r.pool.Exec(ctx, `DELETE FROM workspaces WHERE id = $1`, id)
	return err
}

var (
	ErrMemberNotFound = errors.New("workspace member not found")
)

// WorkspaceMember represents a team member belonging to a workspace.
type WorkspaceMember struct {
	ID                    uuid.UUID   `json:"id"`
	WorkspaceID           uuid.UUID   `json:"workspace_id"`
	Name                  string      `json:"name"`
	Email                 string      `json:"email"`
	Role                  string      `json:"role"` // 'owner', 'admin', 'operator', 'agent'
	AssignedConnectionIDs []uuid.UUID `json:"assigned_connection_ids"`
	CreatedAt             time.Time   `json:"created_at"`
	UpdatedAt             time.Time   `json:"updated_at"`
}

// AddMember adds or updates a member in a workspace.
func (r *WorkspaceRepository) AddMember(ctx context.Context, member *WorkspaceMember) error {
	if member == nil {
		return errors.New("member cannot be nil")
	}
	if member.WorkspaceID == uuid.Nil {
		return ErrInvalidWorkspaceID
	}
	if strings.TrimSpace(member.Email) == "" {
		return errors.New("email is required")
	}
	if member.ID == uuid.Nil {
		member.ID = uuid.New()
	}
	if member.Role == "" {
		member.Role = "operator"
	}
	if member.Name == "" {
		member.Name = member.Email
	}
	if member.AssignedConnectionIDs == nil {
		member.AssignedConnectionIDs = []uuid.UUID{}
	}

	return r.pool.QueryRow(ctx, `
		INSERT INTO workspace_members (id, workspace_id, name, email, role, assigned_connection_ids, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW(), NOW())
		ON CONFLICT (workspace_id, email)
		DO UPDATE SET name = EXCLUDED.name, role = EXCLUDED.role, assigned_connection_ids = EXCLUDED.assigned_connection_ids, updated_at = NOW()
		RETURNING id, created_at, updated_at
	`, member.ID, member.WorkspaceID, member.Name, strings.TrimSpace(member.Email), member.Role, member.AssignedConnectionIDs).Scan(&member.ID, &member.CreatedAt, &member.UpdatedAt)
}

// ListMembers returns all team members for a workspace.
func (r *WorkspaceRepository) ListMembers(ctx context.Context, workspaceID uuid.UUID) ([]WorkspaceMember, error) {
	if workspaceID == uuid.Nil {
		return nil, ErrInvalidWorkspaceID
	}

	rows, err := r.pool.Query(ctx, `
		SELECT id, workspace_id, name, email, role, assigned_connection_ids, created_at, updated_at
		FROM workspace_members
		WHERE workspace_id = $1
		ORDER BY created_at ASC
	`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list workspace members: %w", err)
	}
	defer rows.Close()

	var members []WorkspaceMember
	for rows.Next() {
		var m WorkspaceMember
		if err := rows.Scan(&m.ID, &m.WorkspaceID, &m.Name, &m.Email, &m.Role, &m.AssignedConnectionIDs, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan workspace member: %w", err)
		}
		members = append(members, m)
	}
	return members, rows.Err()
}

// GetMemberByEmail retrieves a single workspace member by email (case-insensitive).
func (r *WorkspaceRepository) GetMemberByEmail(ctx context.Context, workspaceID uuid.UUID, email string) (*WorkspaceMember, error) {
	if workspaceID == uuid.Nil {
		return nil, ErrInvalidWorkspaceID
	}
	trimmedEmail := strings.TrimSpace(email)
	if trimmedEmail == "" {
		return nil, errors.New("email cannot be empty")
	}

	var m WorkspaceMember
	err := r.pool.QueryRow(ctx, `
		SELECT id, workspace_id, name, email, role, assigned_connection_ids, created_at, updated_at
		FROM workspace_members
		WHERE workspace_id = $1 AND LOWER(email) = LOWER($2)
	`, workspaceID, trimmedEmail).Scan(&m.ID, &m.WorkspaceID, &m.Name, &m.Email, &m.Role, &m.AssignedConnectionIDs, &m.CreatedAt, &m.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrMemberNotFound
		}
		return nil, fmt.Errorf("get member by email: %w", err)
	}
	return &m, nil
}

// RemoveMember removes a team member from a workspace by email.
func (r *WorkspaceRepository) RemoveMember(ctx context.Context, workspaceID uuid.UUID, email string) error {
	if workspaceID == uuid.Nil {
		return ErrInvalidWorkspaceID
	}
	res, err := r.pool.Exec(ctx, `
		DELETE FROM workspace_members
		WHERE workspace_id = $1 AND LOWER(email) = LOWER($2)
	`, workspaceID, strings.TrimSpace(email))
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return ErrMemberNotFound
	}
	return nil
}

