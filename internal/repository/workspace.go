// Package repository provides data access operations for workspaces and API keys.
package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Workspace represents a workspace entity.
type Workspace struct {
	ID        uuid.UUID
	Name      string
	CreatedAt time.Time
	UpdatedAt time.Time
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
		`INSERT INTO workspaces (name) VALUES ($1) RETURNING id, name, created_at, updated_at`,
		name,
	).Scan(&ws.ID, &ws.Name, &ws.CreatedAt, &ws.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &ws, nil
}

// GetByID retrieves a workspace by ID.
func (r *WorkspaceRepository) GetByID(ctx context.Context, id uuid.UUID) (*Workspace, error) {
	var ws Workspace
	err := r.pool.QueryRow(ctx,
		`SELECT id, name, created_at, updated_at FROM workspaces WHERE id = $1`,
		id,
	).Scan(&ws.ID, &ws.Name, &ws.CreatedAt, &ws.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &ws, nil
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
		`SELECT id, name, created_at, updated_at FROM workspaces ORDER BY created_at DESC LIMIT $1`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var workspaces []Workspace
	for rows.Next() {
		var ws Workspace
		if err := rows.Scan(&ws.ID, &ws.Name, &ws.CreatedAt, &ws.UpdatedAt); err != nil {
			return nil, err
		}
		workspaces = append(workspaces, ws)
	}
	return workspaces, rows.Err()
}
