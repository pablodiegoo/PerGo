// Package domain defines core entities, value objects, and domain rules for PerGo.
package domain

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// ScopeCapability represents the tenant scoping capability of an authenticated principal.
type ScopeCapability uint8

const (
	// CapabilityWorkspaceScoped indicates the principal is bound strictly to a single workspace.
	CapabilityWorkspaceScoped ScopeCapability = iota
	// CapabilityOperatorScoped indicates the principal is a system operator capable of targeting any workspace.
	CapabilityOperatorScoped
)

// String returns the string representation of ScopeCapability.
func (c ScopeCapability) String() string {
	switch c {
	case CapabilityWorkspaceScoped:
		return "WorkspaceScoped"
	case CapabilityOperatorScoped:
		return "OperatorScoped"
	default:
		return "Unknown"
	}
}

// WorkspaceScope is the authoritative, tamper-proof security context bound to an HTTP request
// or asynchronous task that defines and enforces tenant isolation.
type WorkspaceScope struct {
	workspaceID uuid.UUID
	capability  ScopeCapability
}

// NewWorkspaceScope constructs an authoritative WorkspaceScope for a specific workspace and capability.
func NewWorkspaceScope(workspaceID uuid.UUID, capability ScopeCapability) WorkspaceScope {
	return WorkspaceScope{
		workspaceID: workspaceID,
		capability:  capability,
	}
}

// NewOperatorScope constructs a WorkspaceScope with CapabilityOperatorScoped.
// Target workspaceID can be uuid.Nil (unbound) or a specific workspace target.
func NewOperatorScope(target uuid.UUID) WorkspaceScope {
	return WorkspaceScope{
		workspaceID: target,
		capability:  CapabilityOperatorScoped,
	}
}

// WorkspaceID returns the tenant UUID associated with the scope.
func (s WorkspaceScope) WorkspaceID() uuid.UUID {
	return s.workspaceID
}

// Capability returns the ScopeCapability of the scope.
func (s WorkspaceScope) Capability() ScopeCapability {
	return s.capability
}

// IsOperator returns true if the scope has CapabilityOperatorScoped.
func (s WorkspaceScope) IsOperator() bool {
	return s.capability == CapabilityOperatorScoped
}

// Matches returns true if the scope authorizes access to the given target workspace UUID.
// For OperatorScoped, it matches any non-nil target.
// For WorkspaceScoped, it matches only if target equals WorkspaceID and target is non-nil.
func (s WorkspaceScope) Matches(target uuid.UUID) bool {
	if target == uuid.Nil {
		return false
	}
	if s.IsOperator() {
		return true
	}
	return s.workspaceID == target
}

// WithTarget returns a new WorkspaceScope with the updated target workspace UUID while preserving capability.
func (s WorkspaceScope) WithTarget(target uuid.UUID) WorkspaceScope {
	return WorkspaceScope{
		workspaceID: target,
		capability:  s.capability,
	}
}

// ErrMissingScope is returned by Require when no WorkspaceScope is present in context.
var ErrMissingScope = errors.New("workspace scope missing from context")

type scopeContextKey struct{}

// ContextWithScope attaches the authoritative WorkspaceScope to the context.
func ContextWithScope(ctx context.Context, scope WorkspaceScope) context.Context {
	return context.WithValue(ctx, scopeContextKey{}, scope)
}

// FromContext extracts the WorkspaceScope from context.
func FromContext(ctx context.Context) (WorkspaceScope, bool) {
	if scope, ok := ctx.Value(scopeContextKey{}).(WorkspaceScope); ok {
		return scope, true
	}
	return WorkspaceScope{}, false
}

// From is an alias to FromContext.
func From(ctx context.Context) (WorkspaceScope, bool) {
	return FromContext(ctx)
}

// Require extracts the WorkspaceScope from context or returns ErrMissingScope.
func Require(ctx context.Context) (WorkspaceScope, error) {
	scope, ok := FromContext(ctx)
	if !ok {
		return WorkspaceScope{}, ErrMissingScope
	}
	return scope, nil
}

// ContextWithWorkspaceID is a test helper that constructs a context with a CapabilityWorkspaceScoped scope.
func ContextWithWorkspaceID(ctx context.Context, id uuid.UUID) context.Context {
	return ContextWithScope(ctx, NewWorkspaceScope(id, CapabilityWorkspaceScoped))
}

// ContextWithOperatorScope is a test helper that constructs a context with a CapabilityOperatorScoped scope.
func ContextWithOperatorScope(ctx context.Context, target uuid.UUID) context.Context {
	return ContextWithScope(ctx, NewOperatorScope(target))
}
