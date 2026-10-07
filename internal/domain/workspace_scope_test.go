package domain

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestWorkspaceScope_Basics(t *testing.T) {
	wsID := uuid.New()

	scoped := NewWorkspaceScope(wsID, CapabilityWorkspaceScoped)
	if scoped.WorkspaceID() != wsID {
		t.Fatalf("expected workspace ID %s, got %s", wsID, scoped.WorkspaceID())
	}
	if scoped.Capability() != CapabilityWorkspaceScoped {
		t.Fatalf("expected CapabilityWorkspaceScoped, got %v", scoped.Capability())
	}
	if scoped.IsOperator() {
		t.Fatal("expected IsOperator to be false for CapabilityWorkspaceScoped")
	}
	if scoped.Capability().String() != "WorkspaceScoped" {
		t.Fatalf("expected 'WorkspaceScoped', got %s", scoped.Capability().String())
	}

	operator := NewOperatorScope(wsID)
	if operator.WorkspaceID() != wsID {
		t.Fatalf("expected workspace ID %s, got %s", wsID, operator.WorkspaceID())
	}
	if operator.Capability() != CapabilityOperatorScoped {
		t.Fatalf("expected CapabilityOperatorScoped, got %v", operator.Capability())
	}
	if !operator.IsOperator() {
		t.Fatal("expected IsOperator to be true for CapabilityOperatorScoped")
	}
	if operator.Capability().String() != "OperatorScoped" {
		t.Fatalf("expected 'OperatorScoped', got %s", operator.Capability().String())
	}

	unknownCap := ScopeCapability(99)
	if unknownCap.String() != "Unknown" {
		t.Fatalf("expected 'Unknown', got %s", unknownCap.String())
	}
}

func TestWorkspaceScope_Matches(t *testing.T) {
	ws1 := uuid.New()
	ws2 := uuid.New()

	scoped := NewWorkspaceScope(ws1, CapabilityWorkspaceScoped)
	if !scoped.Matches(ws1) {
		t.Fatal("expected scoped to match own workspace ID")
	}
	if scoped.Matches(ws2) {
		t.Fatal("expected scoped not to match different workspace ID")
	}
	if scoped.Matches(uuid.Nil) {
		t.Fatal("expected scoped not to match uuid.Nil")
	}

	operator := NewOperatorScope(ws1)
	if !operator.Matches(ws1) {
		t.Fatal("expected operator to match ws1")
	}
	if !operator.Matches(ws2) {
		t.Fatal("expected operator to match ws2")
	}
	if operator.Matches(uuid.Nil) {
		t.Fatal("expected operator not to match uuid.Nil")
	}

	unboundOp := NewOperatorScope(uuid.Nil)
	if !unboundOp.Matches(ws1) {
		t.Fatal("expected unbound operator to match ws1")
	}
	if unboundOp.Matches(uuid.Nil) {
		t.Fatal("expected unbound operator not to match uuid.Nil")
	}
}

func TestWorkspaceScope_WithTarget(t *testing.T) {
	ws1 := uuid.New()
	ws2 := uuid.New()

	scoped := NewWorkspaceScope(ws1, CapabilityWorkspaceScoped)
	newScoped := scoped.WithTarget(ws2)
	if newScoped.WorkspaceID() != ws2 {
		t.Fatalf("expected target %s, got %s", ws2, newScoped.WorkspaceID())
	}
	if newScoped.Capability() != CapabilityWorkspaceScoped {
		t.Fatalf("expected CapabilityWorkspaceScoped preserved")
	}

	operator := NewOperatorScope(uuid.Nil)
	newOperator := operator.WithTarget(ws2)
	if newOperator.WorkspaceID() != ws2 {
		t.Fatalf("expected target %s, got %s", ws2, newOperator.WorkspaceID())
	}
	if newOperator.Capability() != CapabilityOperatorScoped {
		t.Fatalf("expected CapabilityOperatorScoped preserved")
	}
}

func TestWorkspaceScope_Context(t *testing.T) {
	ctx := context.Background()

	// Missing scope
	_, ok := FromContext(ctx)
	if ok {
		t.Fatal("expected FromContext to return false on empty context")
	}
	_, ok = From(ctx)
	if ok {
		t.Fatal("expected From to return false on empty context")
	}
	_, err := Require(ctx)
	if !errors.Is(err, ErrMissingScope) {
		t.Fatalf("expected ErrMissingScope, got %v", err)
	}

	// Attached scope
	wsID := uuid.New()
	scope := NewWorkspaceScope(wsID, CapabilityWorkspaceScoped)
	ctx = ContextWithScope(ctx, scope)

	extracted, ok := FromContext(ctx)
	if !ok || extracted.WorkspaceID() != wsID {
		t.Fatalf("failed to extract scope via FromContext: ok=%v, extracted=%v", ok, extracted)
	}

	extractedFrom, ok := From(ctx)
	if !ok || extractedFrom.WorkspaceID() != wsID {
		t.Fatalf("failed to extract scope via From: ok=%v, extracted=%v", ok, extractedFrom)
	}

	reqScope, err := Require(ctx)
	if err != nil || reqScope.WorkspaceID() != wsID {
		t.Fatalf("failed to extract scope via Require: err=%v, reqScope=%v", err, reqScope)
	}

	// Test helper constructors
	wsID2 := uuid.New()
	ctxWorkspace := ContextWithWorkspaceID(context.Background(), wsID2)
	scopeWS, ok := FromContext(ctxWorkspace)
	if !ok || scopeWS.WorkspaceID() != wsID2 || scopeWS.Capability() != CapabilityWorkspaceScoped {
		t.Fatalf("unexpected result from ContextWithWorkspaceID: ok=%v, scope=%v", ok, scopeWS)
	}

	targetOp := uuid.New()
	ctxOp := ContextWithOperatorScope(context.Background(), targetOp)
	scopeOp, ok := FromContext(ctxOp)
	if !ok || scopeOp.WorkspaceID() != targetOp || scopeOp.Capability() != CapabilityOperatorScoped {
		t.Fatalf("unexpected result from ContextWithOperatorScope: ok=%v, scope=%v", ok, scopeOp)
	}
}
