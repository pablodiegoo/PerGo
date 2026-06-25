// Package shutdown provides an orchestrator for graceful shutdown sequences.
// It ensures cleanup functions execute in reverse registration order (LIFO)
// and respects context deadlines.
package shutdown

import (
	"context"
	"sync"
)

// Orchestrator manages a set of cleanup functions that execute in reverse
// registration order during shutdown.
type Orchestrator struct {
	functions []func() error
	mu        sync.Mutex
	once      sync.Once
}

// NewOrchestrator creates a new empty orchestrator.
func NewOrchestrator() *Orchestrator {
	return &Orchestrator{}
}

// Register appends a cleanup function to the list. Functions will be executed
// in reverse registration order (LIFO) during Shutdown.
func (o *Orchestrator) Register(fn func() error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.functions = append(o.functions, fn)
}

// Shutdown executes all registered cleanup functions in reverse order (LIFO).
// It is idempotent — the second and subsequent calls are no-ops returning nil.
// The context's deadline is respected; functions receive the context so they
// can check for cancellation.
func (o *Orchestrator) Shutdown(ctx context.Context) error {
	var err error
	o.once.Do(func() {
		o.mu.Lock()
		fns := make([]func() error, len(o.functions))
		copy(fns, o.functions)
		o.mu.Unlock()

		// Execute in reverse order (LIFO)
		for i := len(fns) - 1; i >= 0; i-- {
			if ctx.Err() != nil {
				break
			}
			if fnErr := fns[i](); fnErr != nil && err == nil {
				err = fnErr
			}
		}
	})
	return err
}
