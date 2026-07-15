# Validation Strategy: Phase 14 - User & API Action Logs

This validation strategy defines the checklist, tests, and scenarios to verify correct implementation of user logging.

## Automated Verification Gates

We will run the following tests:
```bash
# Run repository tests
go test -v ./internal/repository/...
# Run middleware and routing tests
go test -v ./internal/api/middleware/...
# Run project-wide tests
go test ./...
```

## Must-Have Scenarios

| Must-Have Scenario | Verification Steps | Expected Outcome |
|--------------------|--------------------|------------------|
| Database Migration | Run `make dev` or `goose` migration command | Table `user_action_logs` and index are created without errors. |
| API Logger Middleware | Submit a message request via `POST /api/v1/messages` | A new row is asynchronously written to `user_action_logs` with type `api_key`, source `api`, and metadata containing channel and recipient details. |
| Dashboard Log capture | Trigger a settings edit or campaign creation | A new row is written to `user_action_logs` with type `user`, source `dashboard`, and the user's login email. |
| Logs UI View | Navigate to `/admin/workspaces/:workspace_id/settings/user-logs` | The page renders a list of logs with correct source badges (Console / API REST) and a clickable details button. |
| Metadata Modal inspection | Click on "View Metadata" details button | The DaisyUI dialog modal pops up displaying the raw JSON payload correctly formatted. |
