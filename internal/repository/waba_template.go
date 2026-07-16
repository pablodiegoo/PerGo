// Package repository provides data access operations for WABA templates.
package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	// ErrTemplateNotFound is returned when a WABA template is not found.
	ErrTemplateNotFound = errors.New("waba template not found")
)

// WABATemplate represents a stored WABA message template.
type WABATemplate struct {
	ID             uuid.UUID       `json:"id"`
	WorkspaceID    uuid.UUID       `json:"workspace_id"`
	ConnectionID   uuid.UUID       `json:"connection_id"`
	MetaTemplateID string          `json:"meta_template_id"`
	Name           string          `json:"name"`
	Language       string          `json:"language"`
	Status         string          `json:"status"`     // e.g. "APPROVED", "PENDING", "REJECTED"
	Category       string          `json:"category"`   // e.g. "MARKETING", "UTILITY", "AUTHENTICATION"
	Components     json.RawMessage `json:"components"` // JSON structure from Meta template components
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

// WABATemplateRepository provides CRUD operations for WABA templates.
type WABATemplateRepository struct {
	pool *pgxpool.Pool
}

// NewWABATemplateRepository creates a new WABATemplateRepository.
func NewWABATemplateRepository(pool *pgxpool.Pool) *WABATemplateRepository {
	return &WABATemplateRepository{pool: pool}
}

// Create inserts a new template and returns it.
func (r *WABATemplateRepository) Create(ctx context.Context, tmpl *WABATemplate) (*WABATemplate, error) {
	if tmpl.Components == nil {
		tmpl.Components = json.RawMessage("[]")
	}
	var dbTmpl WABATemplate
	err := r.pool.QueryRow(ctx,
		`INSERT INTO waba_templates (workspace_id, connection_id, meta_template_id, name, language, status, category, components)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 RETURNING id, workspace_id, connection_id, meta_template_id, name, language, status, category, components, created_at, updated_at`,
		tmpl.WorkspaceID, tmpl.ConnectionID, tmpl.MetaTemplateID, tmpl.Name, tmpl.Language, tmpl.Status, tmpl.Category, tmpl.Components,
	).Scan(&dbTmpl.ID, &dbTmpl.WorkspaceID, &dbTmpl.ConnectionID, &dbTmpl.MetaTemplateID, &dbTmpl.Name, &dbTmpl.Language, &dbTmpl.Status, &dbTmpl.Category, &dbTmpl.Components, &dbTmpl.CreatedAt, &dbTmpl.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &dbTmpl, nil
}

// Upsert inserts a template or updates its fields if it already exists (matching connection_id, name, language).
func (r *WABATemplateRepository) Upsert(ctx context.Context, tmpl *WABATemplate) (*WABATemplate, error) {
	if tmpl.Components == nil {
		tmpl.Components = json.RawMessage("[]")
	}
	var dbTmpl WABATemplate
	err := r.pool.QueryRow(ctx,
		`INSERT INTO waba_templates (workspace_id, connection_id, meta_template_id, name, language, status, category, components)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 ON CONFLICT (connection_id, name, language) DO UPDATE SET
			 meta_template_id = EXCLUDED.meta_template_id,
			 status = EXCLUDED.status,
			 category = EXCLUDED.category,
			 components = EXCLUDED.components,
			 updated_at = now()
		 RETURNING id, workspace_id, connection_id, meta_template_id, name, language, status, category, components, created_at, updated_at`,
		tmpl.WorkspaceID, tmpl.ConnectionID, tmpl.MetaTemplateID, tmpl.Name, tmpl.Language, tmpl.Status, tmpl.Category, tmpl.Components,
	).Scan(&dbTmpl.ID, &dbTmpl.WorkspaceID, &dbTmpl.ConnectionID, &dbTmpl.MetaTemplateID, &dbTmpl.Name, &dbTmpl.Language, &dbTmpl.Status, &dbTmpl.Category, &dbTmpl.Components, &dbTmpl.CreatedAt, &dbTmpl.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &dbTmpl, nil
}

// GetByID retrieves a template by ID.
func (r *WABATemplateRepository) GetByID(ctx context.Context, id uuid.UUID) (*WABATemplate, error) {
	var tmpl WABATemplate
	err := r.pool.QueryRow(ctx,
		`SELECT id, workspace_id, connection_id, meta_template_id, name, language, status, category, components, created_at, updated_at
		 FROM waba_templates WHERE id = $1`,
		id,
	).Scan(&tmpl.ID, &tmpl.WorkspaceID, &tmpl.ConnectionID, &tmpl.MetaTemplateID, &tmpl.Name, &tmpl.Language, &tmpl.Status, &tmpl.Category, &tmpl.Components, &tmpl.CreatedAt, &tmpl.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &tmpl, nil
}

// GetByNameAndLanguage retrieves a template by connection, name, and language.
func (r *WABATemplateRepository) GetByNameAndLanguage(ctx context.Context, connectionID uuid.UUID, name, language string) (*WABATemplate, error) {
	var tmpl WABATemplate
	err := r.pool.QueryRow(ctx,
		`SELECT id, workspace_id, connection_id, meta_template_id, name, language, status, category, components, created_at, updated_at
		 FROM waba_templates WHERE connection_id = $1 AND name = $2 AND language = $3`,
		connectionID, name, language,
	).Scan(&tmpl.ID, &tmpl.WorkspaceID, &tmpl.ConnectionID, &tmpl.MetaTemplateID, &tmpl.Name, &tmpl.Language, &tmpl.Status, &tmpl.Category, &tmpl.Components, &tmpl.CreatedAt, &tmpl.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &tmpl, nil
}

// ListByWorkspace returns all templates for a workspace, ordered by created_at descending.
func (r *WABATemplateRepository) ListByWorkspace(ctx context.Context, workspaceID uuid.UUID) ([]WABATemplate, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, workspace_id, connection_id, meta_template_id, name, language, status, category, components, created_at, updated_at
		 FROM waba_templates WHERE workspace_id = $1 ORDER BY created_at DESC`,
		workspaceID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var templates []WABATemplate
	for rows.Next() {
		var tmpl WABATemplate
		if err := rows.Scan(&tmpl.ID, &tmpl.WorkspaceID, &tmpl.ConnectionID, &tmpl.MetaTemplateID, &tmpl.Name, &tmpl.Language, &tmpl.Status, &tmpl.Category, &tmpl.Components, &tmpl.CreatedAt, &tmpl.UpdatedAt); err != nil {
			return nil, err
		}
		templates = append(templates, tmpl)
	}
	return templates, rows.Err()
}

// ListByConnection returns all templates for a specific connection, ordered by created_at descending.
func (r *WABATemplateRepository) ListByConnection(ctx context.Context, connectionID uuid.UUID) ([]WABATemplate, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, workspace_id, connection_id, meta_template_id, name, language, status, category, components, created_at, updated_at
		 FROM waba_templates WHERE connection_id = $1 ORDER BY created_at DESC`,
		connectionID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var templates []WABATemplate
	for rows.Next() {
		var tmpl WABATemplate
		if err := rows.Scan(&tmpl.ID, &tmpl.WorkspaceID, &tmpl.ConnectionID, &tmpl.MetaTemplateID, &tmpl.Name, &tmpl.Language, &tmpl.Status, &tmpl.Category, &tmpl.Components, &tmpl.CreatedAt, &tmpl.UpdatedAt); err != nil {
			return nil, err
		}
		templates = append(templates, tmpl)
	}
	return templates, rows.Err()
}

// UpdateStatus updates the status of a template by ID.
func (r *WABATemplateRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE waba_templates SET status = $1, updated_at = now() WHERE id = $2`,
		status, id,
	)
	return err
}

// Delete removes a template by ID.
func (r *WABATemplateRepository) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM waba_templates WHERE id = $1`, id)
	return err
}
