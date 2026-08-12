package breaker_test

import (
	"testing"
	"time"

	"github.com/pablojhp.pergo/internal/platform/breaker"
)

func TestCircuitBreaker_Transitions(t *testing.T) {
	t.Run("closed stays closed on success", func(t *testing.T) {
		cb := breaker.NewCircuitBreaker(2, 50*time.Millisecond)
		endpoint := "test"
		
		err := cb.Allow(endpoint)
		if err != nil {
			t.Fatalf("expected nil, got %v", err)
		}
		
		cb.RecordSuccess(endpoint)
		err = cb.Allow(endpoint)
		if err != nil {
			t.Fatalf("expected nil, got %v", err)
		}
	})

	t.Run("closed to open to halfOpen to closed", func(t *testing.T) {
		cb := breaker.NewCircuitBreaker(2, 50*time.Millisecond)
		endpoint := "test"
		
		// 1. closed -> open
		cb.RecordFailure(endpoint)
		cb.RecordFailure(endpoint)
		
		err := cb.Allow(endpoint)
		if err != breaker.ErrCircuitOpen {
			t.Fatalf("expected ErrCircuitOpen, got %v", err)
		}

		// 2. open stays open before timeout
		time.Sleep(10 * time.Millisecond)
		err = cb.Allow(endpoint)
		if err != breaker.ErrCircuitOpen {
			t.Fatalf("expected ErrCircuitOpen before timeout, got %v", err)
		}

		// 3. open -> halfOpen
		time.Sleep(50 * time.Millisecond)
		err = cb.Allow(endpoint) // 1st probe allowed
		if err != nil {
			t.Fatalf("expected nil for half-open probe, got %v", err)
		}
		
		err = cb.Allow(endpoint) // 2nd probe should fail
		if err != breaker.ErrCircuitOpen {
			t.Fatalf("expected ErrCircuitOpen for 2nd probe in halfOpen, got %v", err)
		}

		// 4. halfOpen -> closed
		cb.RecordSuccess(endpoint)
		err = cb.Allow(endpoint)
		if err != nil {
			t.Fatalf("expected nil after success in halfOpen, got %v", err)
		}
	})

	t.Run("halfOpen to open", func(t *testing.T) {
		cb := breaker.NewCircuitBreaker(1, 50*time.Millisecond)
		endpoint := "test"

		cb.RecordFailure(endpoint)
		
		time.Sleep(60 * time.Millisecond)
		
		err := cb.Allow(endpoint)
		if err != nil {
			t.Fatalf("expected nil for half-open probe, got %v", err)
		}
		
		cb.RecordFailure(endpoint) // failure in halfOpen
		
		err = cb.Allow(endpoint)
		if err != breaker.ErrCircuitOpen {
			t.Fatalf("expected ErrCircuitOpen after failure in halfOpen, got %v", err)
		}
	})
}
