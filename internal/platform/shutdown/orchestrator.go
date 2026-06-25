// Package shutdown provides an orchestrator for graceful shutdown sequences.
package shutdown

import "context"

// Orchestrator manages a set of cleanup functions that execute in reverse
// registration order during shutdown.
type Orchestrator struct{}

// NewOrchestrator creates a new empty orchestrator.
// TODO: implement — currently a stub for RED phase.
func NewOrchestrator() *Orchestrator {
	panic("shutdown: NewOrchestrator not implemented")
}

// Register appends a cleanup function.
// TODO: implement — currently a stub for RED phase.
func (o *Orchestrator) Register(fn func() error) {
	panic("shutdown: Register not implemented")
}

// Shutdown executes all registered cleanup functions in reverse order.
// TODO: implement — currently a stub for RED phase.
func (o *Orchestrator) Shutdown(ctx context.Context) error {
	panic("shutdown: Shutdown not implemented")
}
