package breaker

import (
	"errors"
	"sync"
	"time"
)

var ErrCircuitOpen = errors.New("circuit breaker is open")

type EndpointBreaker struct {
	consecutiveFailures int
	openUntil           time.Time
}

type CircuitBreaker struct {
	mu           sync.Mutex
	endpoints    map[string]*EndpointBreaker
	maxFailures  int
	resetTimeout time.Duration
}

func NewCircuitBreaker(maxFailures int, resetTimeout time.Duration) *CircuitBreaker {
	if maxFailures <= 0 {
		maxFailures = 5
	}
	if resetTimeout <= 0 {
		resetTimeout = 5 * time.Minute
	}
	return &CircuitBreaker{
		endpoints:    make(map[string]*EndpointBreaker),
		maxFailures:  maxFailures,
		resetTimeout: resetTimeout,
	}
}

func (cb *CircuitBreaker) Allow(endpoint string) error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	ep, ok := cb.endpoints[endpoint]
	if !ok {
		return nil
	}

	if !ep.openUntil.IsZero() {
		if time.Now().Before(ep.openUntil) {
			return ErrCircuitOpen
		}
		ep.consecutiveFailures = 0
		ep.openUntil = time.Time{}
	}
	return nil
}

func (cb *CircuitBreaker) RecordSuccess(endpoint string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if ep, ok := cb.endpoints[endpoint]; ok {
		ep.consecutiveFailures = 0
		ep.openUntil = time.Time{}
	}
}

func (cb *CircuitBreaker) RecordFailure(endpoint string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	ep, ok := cb.endpoints[endpoint]
	if !ok {
		ep = &EndpointBreaker{}
		cb.endpoints[endpoint] = ep
	}

	ep.consecutiveFailures++
	if ep.consecutiveFailures >= cb.maxFailures {
		ep.openUntil = time.Now().Add(cb.resetTimeout)
	}
}
