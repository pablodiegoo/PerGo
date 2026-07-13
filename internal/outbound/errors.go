package outbound

import (
	"errors"

	"github.com/pablojhp.pergo/internal/domain"
)

// ErrQueueFull is returned when a workspace's active queue limit is exceeded.
var ErrQueueFull = errors.New("queue_full")

// ValidationError wraps a payload validation error from the domain package.
type ValidationError struct {
	Response *domain.ErrorResponse
}

func (e *ValidationError) Error() string {
	if e.Response != nil {
		return e.Response.Message
	}
	return "validation failed"
}

// MediaError indicates a failure downloading or validating inbound/outbound media.
type MediaError struct {
	Code    string
	Message string
	Field   string
	Err     error
}

func (e *MediaError) Error() string {
	return e.Message
}

// RouteError is returned when a connection or workspace routing lookup fails.
type RouteError struct {
	Message string
	Err     error
}

func (e *RouteError) Error() string {
	return e.Message
}
