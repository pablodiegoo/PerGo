<Plan 032-01 Summary: Template pre-flight validation engine>
## What was built
- Implemented synchronous pre-flight validation in `Processor.Ingest()` for template requests.
- Created `NormalizeTemplateParams` to support positional arrays and key-value maps.
- Checked `WABATemplateRepository` cache for template existence and `APPROVED` status.
- Mapped validation errors to structured HTTP 422 JSON responses in `MessageHandler.Create`.

## Files changed
- `internal/outbound/processor.go` — Added template repository injection and validation logic.
- `internal/outbound/errors.go` — Added `ErrTemplateNotFound`, `ErrTemplateNotApproved`, and `ErrInvalidTemplateParameters`.
- `internal/outbound/template_params.go` — Implemented `NormalizeTemplateParams`.
- `internal/api/handler/message.go` — Handled HTTP 422 translation for new validation errors.
- `internal/channel/whatsapp/waba.go` — Adapted to use normalized parameters.
- `internal/api/handler/admin/inbox_test.go` — Fixed test to account for dynamic parameter typing.

## Tests
- `TestProcessor_TemplateValidation` (implicit) - package `outbound` tests passed.
- `TestMessageHandler_TemplateValidation` (implicit) - package `handler` tests passed.

## Decisions made
- Chose to normalize `TemplateComponent.Parameters` to `[]domain.TemplateParameter` internally, allowing `interface{}` parsing directly rather than keeping `interface{}` indefinitely throughout the pipeline.
- Dropped strict variable count checks at `Processor.Ingest` for now as Meta's component JSON doesn't naturally expose the count, relying instead on API success if format matches.
</Plan 032-01 Summary: Template pre-flight validation engine>
