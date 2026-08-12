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

func TestCircuitBreaker_MultiCycleAccumulation(t *testing.T) {
	maxFailures := 3
	resetTimeout := 20 * time.Millisecond
	cb := breaker.NewCircuitBreaker(maxFailures, resetTimeout)
	endpoint := "test-service"

	// Trigger initial transition: Closed -> Open
	for i := 0; i < maxFailures; i++ {
		cb.RecordFailure(endpoint)
	}

	if err := cb.Allow(endpoint); err != breaker.ErrCircuitOpen {
		t.Fatalf("expected ErrCircuitOpen, got %v", err)
	}

	if count := cb.ConsecutiveFailures(endpoint); count != maxFailures {
		t.Fatalf("expected consecutiveFailures to be %d, got %d", maxFailures, count)
	}

	// Simulate 4 open -> half-open -> open cycles with probe failures
	for cycle := 1; cycle <= 4; cycle++ {
		time.Sleep(resetTimeout + 5*time.Millisecond)

		// 1. First call after openUntil should transition to HalfOpen and allow 1 probe
		err := cb.Allow(endpoint)
		if err != nil {
			t.Fatalf("cycle %d: expected probe allowed in HalfOpen state, got %v", cycle, err)
		}

		// 2. Probe fails
		cb.RecordFailure(endpoint)

		// 3. Circuit breaker should transition back to Open
		err = cb.Allow(endpoint)
		if err != breaker.ErrCircuitOpen {
			t.Fatalf("cycle %d: expected ErrCircuitOpen after probe failure, got %v", cycle, err)
		}

		// 4. Assert consecutiveFailures is capped at maxFailures (equals maxFailures, not accumulating)
		if count := cb.ConsecutiveFailures(endpoint); count != maxFailures {
			t.Fatalf("cycle %d: expected consecutiveFailures to equal maxFailures (%d), got %d", cycle, maxFailures, count)
		}
	}
}

func TestCircuitBreaker_RecordSuccess_HalfOpen(t *testing.T) {
	maxFailures := 2
	resetTimeout := 20 * time.Millisecond
	cb := breaker.NewCircuitBreaker(maxFailures, resetTimeout)
	endpoint := "test-success-service"

	// Closed -> Open
	for i := 0; i < maxFailures; i++ {
		cb.RecordFailure(endpoint)
	}

	if count := cb.ConsecutiveFailures(endpoint); count != maxFailures {
		t.Fatalf("expected consecutiveFailures to be %d, got %d", maxFailures, count)
	}

	// Open -> HalfOpen
	time.Sleep(resetTimeout + 5*time.Millisecond)
	err := cb.Allow(endpoint)
	if err != nil {
		t.Fatalf("expected probe allowed in HalfOpen, got %v", err)
	}

	// HalfOpen probe succeeds
	cb.RecordSuccess(endpoint)

	// Verify counter is zeroed
	if count := cb.ConsecutiveFailures(endpoint); count != 0 {
		t.Fatalf("expected consecutiveFailures to be 0 after RecordSuccess, got %d", count)
	}

	// Verify state is Closed (Allow returns nil)
	if err := cb.Allow(endpoint); err != nil {
		t.Fatalf("expected nil (Closed state) after RecordSuccess, got %v", err)
	}
}

