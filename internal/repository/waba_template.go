// Package repository provides data access operations for WABA templates.
package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
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
	ID              uuid.UUID       `json:"id"`
	WorkspaceID     uuid.UUID       `json:"workspace_id"`
	ConnectionID    uuid.UUID       `json:"connection_id"`
	MetaTemplateID  string          `json:"meta_template_id"`
	Name            string          `json:"name"`
	Language        string          `json:"language"`
	Status          string          `json:"status"`           // e.g. "APPROVED", "PENDING", "REJECTED"
	Category        string          `json:"category"`         // e.g. "MARKETING", "UTILITY", "AUTHENTICATION"
	Components      json.RawMessage `json:"components"`       // JSON structure from Meta template components
	RejectionReason *string         `json:"rejection_reason"` // NULL unless template was rejected
	QualityScore     *string         `json:"quality_score"`    // e.g. "GREEN", "YELLOW", "RED", NULL
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

// WABATemplateRepository provides CRUD operations for WABA templates with an in-memory cache.
type WABATemplateRepository struct {
	pool        *pgxpool.Pool
	mu          sync.RWMutex
	cache       map[uuid.UUID]map[string]*WABATemplate // ConnectionID -> "name_language" -> *WABATemplate
	cacheLoaded bool
}

// NewWABATemplateRepository creates a new WABATemplateRepository.
func NewWABATemplateRepository(pool *pgxpool.Pool) *WABATemplateRepository {
	return &WABATemplateRepository{
		pool:  pool,
		cache: make(map[uuid.UUID]map[string]*WABATemplate),
	}
}

// LoadCache eagerly loads all waba_templates into the in-memory cache.
func (r *WABATemplateRepository) LoadCache(ctx context.Context) error {
	rows, err := r.pool.Query(ctx,
		`SELECT id, workspace_id, connection_id, meta_template_id, name, language, status, category, components, rejection_reason, quality_score, created_at, updated_at
		 FROM waba_templates`,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	newCache := make(map[uuid.UUID]map[string]*WABATemplate)

	for rows.Next() {
		var tmpl WABATemplate
		if err := rows.Scan(&tmpl.ID, &tmpl.WorkspaceID, &tmpl.ConnectionID, &tmpl.MetaTemplateID, &tmpl.Name, &tmpl.Language, &tmpl.Status, &tmpl.Category, &tmpl.Components, &tmpl.RejectionReason, &tmpl.QualityScore, &tmpl.CreatedAt, &tmpl.UpdatedAt); err != nil {
			return err
		}
		if _, exists := newCache[tmpl.ConnectionID]; !exists {
			newCache[tmpl.ConnectionID] = make(map[string]*WABATemplate)
		}
		key := fmt.Sprintf("%s_%s", tmpl.Name, tmpl.Language)
		tCopy := tmpl
		newCache[tmpl.ConnectionID][key] = &tCopy
	}
	if err := rows.Err(); err != nil {
		return err
	}

	r.mu.Lock()
	r.cache = newCache
	r.cacheLoaded = true
	r.mu.Unlock()
	return nil
}

func (r *WABATemplateRepository) putInCache(tmpl *WABATemplate) {
	if tmpl == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cache == nil {
		r.cache = make(map[uuid.UUID]map[string]*WABATemplate)
	}
	if _, exists := r.cache[tmpl.ConnectionID]; !exists {
		r.cache[tmpl.ConnectionID] = make(map[string]*WABATemplate)
	}
	key := fmt.Sprintf("%s_%s", tmpl.Name, tmpl.Language)
	tCopy := *tmpl
	r.cache[tmpl.ConnectionID][key] = &tCopy
}

func (r *WABATemplateRepository) removeFromCache(connectionID uuid.UUID, id uuid.UUID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if connMap, exists := r.cache[connectionID]; exists {
		for key, tmpl := range connMap {
			if tmpl.ID == id {
				delete(connMap, key)
				break
			}
		}
	}
}

// Create inserts a new template, updates cache, and returns it.
func (r *WABATemplateRepository) Create(ctx context.Context, tmpl *WABATemplate) (*WABATemplate, error) {
	if tmpl.Components == nil {
		tmpl.Components = json.RawMessage("[]")
	}
	var dbTmpl WABATemplate
	err := r.pool.QueryRow(ctx,
		`INSERT INTO waba_templates (workspace_id, connection_id, meta_template_id, name, language, status, category, components, rejection_reason, quality_score)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		 RETURNING id, workspace_id, connection_id, meta_template_id, name, language, status, category, components, rejection_reason, quality_score, created_at, updated_at`,
		tmpl.WorkspaceID, tmpl.ConnectionID, tmpl.MetaTemplateID, tmpl.Name, tmpl.Language, tmpl.Status, tmpl.Category, tmpl.Components, tmpl.RejectionReason, tmpl.QualityScore,
	).Scan(&dbTmpl.ID, &dbTmpl.WorkspaceID, &dbTmpl.ConnectionID, &dbTmpl.MetaTemplateID, &dbTmpl.Name, &dbTmpl.Language, &dbTmpl.Status, &dbTmpl.Category, &dbTmpl.Components, &dbTmpl.RejectionReason, &dbTmpl.QualityScore, &dbTmpl.CreatedAt, &dbTmpl.UpdatedAt)
	if err != nil {
		return nil, err
	}
	r.putInCache(&dbTmpl)
	return &dbTmpl, nil
}

// Upsert inserts a template or updates its fields if it already exists (matching connection_id, name, language).
func (r *WABATemplateRepository) Upsert(ctx context.Context, tmpl *WABATemplate) (*WABATemplate, error) {
	if tmpl.Components == nil {
		tmpl.Components = json.RawMessage("[]")
	}
	var dbTmpl WABATemplate
	err := r.pool.QueryRow(ctx,
		`INSERT INTO waba_templates (workspace_id, connection_id, meta_template_id, name, language, status, category, components, rejection_reason, quality_score)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		 ON CONFLICT (connection_id, name, language) DO UPDATE SET
			 meta_template_id = EXCLUDED.meta_template_id,
			 status = EXCLUDED.status,
			 category = EXCLUDED.category,
			 components = EXCLUDED.components,
			 rejection_reason = EXCLUDED.rejection_reason,
			 quality_score = EXCLUDED.quality_score,
			 updated_at = now()
		 RETURNING id, workspace_id, connection_id, meta_template_id, name, language, status, category, components, rejection_reason, quality_score, created_at, updated_at`,
		tmpl.WorkspaceID, tmpl.ConnectionID, tmpl.MetaTemplateID, tmpl.Name, tmpl.Language, tmpl.Status, tmpl.Category, tmpl.Components, tmpl.RejectionReason, tmpl.QualityScore,
	).Scan(&dbTmpl.ID, &dbTmpl.WorkspaceID, &dbTmpl.ConnectionID, &dbTmpl.MetaTemplateID, &dbTmpl.Name, &dbTmpl.Language, &dbTmpl.Status, &dbTmpl.Category, &dbTmpl.Components, &dbTmpl.RejectionReason, &dbTmpl.QualityScore, &dbTmpl.CreatedAt, &dbTmpl.UpdatedAt)
	if err != nil {
		return nil, err
	}
	r.putInCache(&dbTmpl)
	return &dbTmpl, nil
}

// GetByID retrieves a template by ID.
func (r *WABATemplateRepository) GetByID(ctx context.Context, id uuid.UUID) (*WABATemplate, error) {
	r.mu.RLock()
	if r.cacheLoaded {
		for _, connMap := range r.cache {
			for _, tmpl := range connMap {
				if tmpl.ID == id {
					tCopy := *tmpl
					r.mu.RUnlock()
					return &tCopy, nil
				}
			}
		}
	}
	r.mu.RUnlock()

	var tmpl WABATemplate
	err := r.pool.QueryRow(ctx,
		`SELECT id, workspace_id, connection_id, meta_template_id, name, language, status, category, components, rejection_reason, quality_score, created_at, updated_at
		 FROM waba_templates WHERE id = $1`,
		id,
	).Scan(&tmpl.ID, &tmpl.WorkspaceID, &tmpl.ConnectionID, &tmpl.MetaTemplateID, &tmpl.Name, &tmpl.Language, &tmpl.Status, &tmpl.Category, &tmpl.Components, &tmpl.RejectionReason, &tmpl.QualityScore, &tmpl.CreatedAt, &tmpl.UpdatedAt)
	if err != nil {
		return nil, err
	}
	r.putInCache(&tmpl)
	return &tmpl, nil
}

// GetByNameAndLanguage retrieves a template by connection, name, and language.
func (r *WABATemplateRepository) GetByNameAndLanguage(ctx context.Context, connectionID uuid.UUID, name, language string) (*WABATemplate, error) {
	r.mu.RLock()
	if r.cacheLoaded {
		if connMap, exists := r.cache[connectionID]; exists {
			key := fmt.Sprintf("%s_%s", name, language)
			if tmpl, ok := connMap[key]; ok {
				tCopy := *tmpl
				r.mu.RUnlock()
				return &tCopy, nil
			}
		}
	}
	r.mu.RUnlock()

	var tmpl WABATemplate
	err := r.pool.QueryRow(ctx,
		`SELECT id, workspace_id, connection_id, meta_template_id, name, language, status, category, components, rejection_reason, quality_score, created_at, updated_at
		 FROM waba_templates WHERE connection_id = $1 AND name = $2 AND language = $3`,
		connectionID, name, language,
	).Scan(&tmpl.ID, &tmpl.WorkspaceID, &tmpl.ConnectionID, &tmpl.MetaTemplateID, &tmpl.Name, &tmpl.Language, &tmpl.Status, &tmpl.Category, &tmpl.Components, &tmpl.RejectionReason, &tmpl.QualityScore, &tmpl.CreatedAt, &tmpl.UpdatedAt)
	if err != nil {
		return nil, err
	}
	r.putInCache(&tmpl)
	return &tmpl, nil
}

// ListByWorkspace returns all templates for a workspace, ordered by created_at descending.
func (r *WABATemplateRepository) ListByWorkspace(ctx context.Context, workspaceID uuid.UUID) ([]WABATemplate, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, workspace_id, connection_id, meta_template_id, name, language, status, category, components, rejection_reason, quality_score, created_at, updated_at
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
		if err := rows.Scan(&tmpl.ID, &tmpl.WorkspaceID, &tmpl.ConnectionID, &tmpl.MetaTemplateID, &tmpl.Name, &tmpl.Language, &tmpl.Status, &tmpl.Category, &tmpl.Components, &tmpl.RejectionReason, &tmpl.QualityScore, &tmpl.CreatedAt, &tmpl.UpdatedAt); err != nil {
			return nil, err
		}
		templates = append(templates, tmpl)
	}
	return templates, rows.Err()
}

// ListByConnection returns all templates for a specific connection, ordered by created_at descending.
func (r *WABATemplateRepository) ListByConnection(ctx context.Context, connectionID uuid.UUID) ([]WABATemplate, error) {
	r.mu.RLock()
	if r.cacheLoaded {
		if connMap, exists := r.cache[connectionID]; exists {
			var templates []WABATemplate
			for _, tmpl := range connMap {
				templates = append(templates, *tmpl)
			}
			sort.Slice(templates, func(i, j int) bool {
				return templates[i].CreatedAt.After(templates[j].CreatedAt)
			})
			r.mu.RUnlock()
			return templates, nil
		}
	}
	r.mu.RUnlock()

	rows, err := r.pool.Query(ctx,
		`SELECT id, workspace_id, connection_id, meta_template_id, name, language, status, category, components, rejection_reason, quality_score, created_at, updated_at
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
		if err := rows.Scan(&tmpl.ID, &tmpl.WorkspaceID, &tmpl.ConnectionID, &tmpl.MetaTemplateID, &tmpl.Name, &tmpl.Language, &tmpl.Status, &tmpl.Category, &tmpl.Components, &tmpl.RejectionReason, &tmpl.QualityScore, &tmpl.CreatedAt, &tmpl.UpdatedAt); err != nil {
			return nil, err
		}
		templates = append(templates, tmpl)
	}
	return templates, rows.Err()
}

// UpdateStatus updates the status, rejection_reason, and quality_score of a template by ID.
func (r *WABATemplateRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status string, rejectionReason *string, qualityScore *string) error {
	var dbTmpl WABATemplate
	err := r.pool.QueryRow(ctx,
		`UPDATE waba_templates
		 SET status = $1, rejection_reason = $2, quality_score = $3, updated_at = now()
		 WHERE id = $4
		 RETURNING id, workspace_id, connection_id, meta_template_id, name, language, status, category, components, rejection_reason, quality_score, created_at, updated_at`,
		status, rejectionReason, qualityScore, id,
	).Scan(&dbTmpl.ID, &dbTmpl.WorkspaceID, &dbTmpl.ConnectionID, &dbTmpl.MetaTemplateID, &dbTmpl.Name, &dbTmpl.Language, &dbTmpl.Status, &dbTmpl.Category, &dbTmpl.Components, &dbTmpl.RejectionReason, &dbTmpl.QualityScore, &dbTmpl.CreatedAt, &dbTmpl.UpdatedAt)
	if err != nil {
		return err
	}
	r.putInCache(&dbTmpl)
	return nil
}

// Delete removes a template by ID.
func (r *WABATemplateRepository) Delete(ctx context.Context, id uuid.UUID) error {
	tmpl, err := r.GetByID(ctx, id)
	if err == nil && tmpl != nil {
		r.removeFromCache(tmpl.ConnectionID, tmpl.ID)
	}
	_, err = r.pool.Exec(ctx, `DELETE FROM waba_templates WHERE id = $1`, id)
	return err
}
