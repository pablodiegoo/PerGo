package obs

import (
	"context"
	"log/slog"
	"strings"
)

// RedactionPlaceholder is the value used to replace sensitive attributes.
const RedactionPlaceholder = "[REDACTED]"

// DefaultSensitiveKeys lists the standard attribute keys redacted by default.
var DefaultSensitiveKeys = []string{
	// PII
	"phone",
	"phone_number",
	"recipient",
	"sender",
	"contact",
	"email",
	"body",
	"text",
	"content",
	"message_body",
	"payload",
	// Security credentials & tokens
	"password",
	"token",
	"secret",
	"credentials",
	"api_key",
	"key",
	"kek",
	"authorization",
	"access_key",
	"secret_key",
	"private_key",
	"bearer",
	"webhook_secret",
	"access_token",
	"client_secret",
	"refresh_token",
	"auth_token",
	"master_key",
	"session_secret",
}

// RedactingHandler wraps an slog.Handler to sanitize sensitive attributes.
type RedactingHandler struct {
	handler        slog.Handler
	sensitiveKeys  map[string]struct{}
	customOverride bool
}

// RedactingHandlerOption configures a RedactingHandler.
type RedactingHandlerOption func(*RedactingHandler)

// WithSensitiveKeys overrides the set of sensitive keys with the given keys.
func WithSensitiveKeys(keys ...string) RedactingHandlerOption {
	return func(h *RedactingHandler) {
		h.customOverride = true
		h.sensitiveKeys = make(map[string]struct{}, len(keys))
		for _, k := range keys {
			h.sensitiveKeys[strings.ToLower(k)] = struct{}{}
		}
	}
}

// WithExtraSensitiveKeys adds additional sensitive keys to the default or configured set.
func WithExtraSensitiveKeys(keys ...string) RedactingHandlerOption {
	return func(h *RedactingHandler) {
		for _, k := range keys {
			h.sensitiveKeys[strings.ToLower(k)] = struct{}{}
		}
	}
}

// NewRedactingHandler creates a new RedactingHandler wrapping inner.
func NewRedactingHandler(inner slog.Handler, opts ...RedactingHandlerOption) *RedactingHandler {
	h := &RedactingHandler{
		handler:       inner,
		sensitiveKeys: make(map[string]struct{}, len(DefaultSensitiveKeys)),
	}
	for _, k := range DefaultSensitiveKeys {
		h.sensitiveKeys[strings.ToLower(k)] = struct{}{}
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// Enabled delegates to the wrapped handler.
func (h *RedactingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.handler.Enabled(ctx, level)
}

// Handle redacts sensitive attributes from the record and delegates to the wrapped handler.
func (h *RedactingHandler) Handle(ctx context.Context, r slog.Record) error {
	newRecord := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	r.Attrs(func(a slog.Attr) bool {
		newRecord.AddAttrs(h.redactAttr(a))
		return true
	})
	return h.handler.Handle(ctx, newRecord)
}

// WithAttrs returns a new RedactingHandler with pre-redacted attributes attached to the inner handler.
func (h *RedactingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	redacted := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		redacted[i] = h.redactAttr(a)
	}
	return &RedactingHandler{
		handler:        h.handler.WithAttrs(redacted),
		sensitiveKeys:  h.sensitiveKeys,
		customOverride: h.customOverride,
	}
}

// WithGroup returns a new RedactingHandler wrapping the inner handler with group name added.
func (h *RedactingHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &RedactingHandler{
		handler:        h.handler.WithGroup(name),
		sensitiveKeys:  h.sensitiveKeys,
		customOverride: h.customOverride,
	}
}

func (h *RedactingHandler) isSensitive(key string) bool {
	if key == "" {
		return false
	}
	lower := strings.ToLower(key)
	if _, ok := h.sensitiveKeys[lower]; ok {
		return true
	}
	if !h.customOverride {
		if strings.Contains(lower, "password") ||
			strings.Contains(lower, "secret") ||
			strings.Contains(lower, "token") ||
			strings.Contains(lower, "credential") ||
			strings.Contains(lower, "private_key") ||
			strings.Contains(lower, "api_key") ||
			strings.Contains(lower, "auth_header") {
			return true
		}
	}
	return false
}

func (h *RedactingHandler) redactAttr(a slog.Attr) slog.Attr {
	a.Value = a.Value.Resolve()

	if h.isSensitive(a.Key) {
		return slog.String(a.Key, RedactionPlaceholder)
	}

	if a.Value.Kind() == slog.KindGroup {
		groupAttrs := a.Value.Group()
		if len(groupAttrs) == 0 {
			return a
		}
		redacted := make([]slog.Attr, len(groupAttrs))
		for i, gAttr := range groupAttrs {
			redacted[i] = h.redactAttr(gAttr)
		}
		return slog.Attr{
			Key:   a.Key,
			Value: slog.GroupValue(redacted...),
		}
	}

	return a
}
