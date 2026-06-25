// Package tenant provides tenant-context wrapper helpers for workspace isolation.
// Every query carries workspace_id scoped via context.Context.
package tenant

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

type contextKey struct{}

// WithWorkspaceID stores the workspace_id in context.
func WithWorkspaceID(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, contextKey{}, id)
}

// WorkspaceIDFrom retrieves the workspace_id from context.
// Returns false if the workspace_id is not present.
func WorkspaceIDFrom(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(contextKey{}).(uuid.UUID)
	return id, ok
}

// RequireWorkspaceID retrieves the workspace_id from context.
// Returns an error if the workspace_id is not present.
func RequireWorkspaceID(ctx context.Context) (uuid.UUID, error) {
	id, ok := WorkspaceIDFrom(ctx)
	if !ok {
		return uuid.Nil, errors.New("workspace_id missing from context")
	}
	return id, nil
}
