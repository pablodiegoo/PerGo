<Plan 032-02 Summary: Session window fallback with default_template_name upgrade>
## What was built
- Implemented smart session window fallback for `whatsapp_cloud` messages outside the 24h window.
- When an expired session receives a freeform message, the system checks the connection's credentials for `default_template_name`.
- If a default template is configured, the request is automatically mutated into a template message, mapping the freeform text to the first body parameter (`{{1}}`).
- Retained the existing `session_window_expired` rejection behavior if no default template is configured.

## Files changed
- `internal/outbound/processor.go` — Added `Smart Session Window Fallback` logic to upgrade freeform payloads before template validation.
- `internal/outbound/processor_test.go` — Added `TestProcessor_SessionFallback` to verify the transformation and error paths.

## Tests
- `TestProcessor_SessionFallback` passing.
- All outbound package tests passing.

## Decisions made
- Chose to directly map `req.Body` to a `[]domain.TemplateParameter` array under a single `body` component rather than attempting dynamic positional binding, as `{{1}}` is strictly required by the requirements.
- Configured the fallback to default to `en_US` if `default_template_language` is not provided in connection credentials.
</Plan 032-02 Summary: Session window fallback with default_template_name upgrade>
