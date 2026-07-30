package outbound

import (
	"errors"

	"github.com/pablojhp.pergo/internal/domain"
)

// ErrQueueFull is returned when a workspace's active queue limit is exceeded.
var ErrQueueFull = errors.New("queue_full")

// ErrMissingCatalogID is returned when catalog_id is required for product messages and no default_catalog_id is configured for connection.
var ErrMissingCatalogID = errors.New("catalog_id is required for product messages and no default_catalog_id is configured for connection")

// ErrInvalidProductPayload is returned when a product message fails structural bounds checks.
var ErrInvalidProductPayload = errors.New("invalid product payload parameters")

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

func (e *ValidationError) Unwrap() error {
	if e.Response != nil && e.Response.Code == "invalid_product_payload" {
		return ErrInvalidProductPayload
	}
	return nil
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

var ErrTemplateNotFound = errors.New("template_not_found")

type ErrTemplateNotApproved struct {
	Status          string
	RejectionReason *string
}

func (e *ErrTemplateNotApproved) Error() string {
	return "template_not_approved"
}

type ErrInvalidTemplateParameters struct {
	Message string
}

func (e *ErrInvalidTemplateParameters) Error() string {
	return e.Message
}
