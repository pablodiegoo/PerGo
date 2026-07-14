---
quick_id: "260713-uuo"
slug: "implement-dynamic-onboarding-logic"
type: quick
status: executing
must_haves:
  - "CountActive query in APIKeyRepository that counts only non-revoked API keys."
  - "CountActiveByWorkspace query in ConnectionRepository that counts only active or connected connections."
  - "OverviewHandler modified to fetch count of active API keys and connections to compute ShowOnboarding/isOnboarded."
  - "Dashboard template updated to accept activeConnectionsCount and conditionally render checklist vs operational metrics."
---

# Plan: Implement Dynamic Onboarding Logic for Admin Dashboard

## Objective
Modify the admin handler logic to dynamically determine if the workspace has completed the onboarding requirements (having at least 1 active API key and 1 active connection) and display the correct panel layout.

## Tasks

### Task 1: Add Optimized Count Queries to Repositories
- **Files**: [internal/repository/apikey.go](file:///home/pablo/Coding/PerGo/internal/repository/apikey.go), [internal/repository/connection.go](file:///home/pablo/Coding/PerGo/internal/repository/connection.go)
- **Action**: 
  - Add `CountActive(ctx context.Context, workspaceID uuid.UUID) (int, error)` to `APIKeyRepository` returning the count of keys where `workspace_id = $1 AND revoked_at IS NULL`.
  - Add `CountActiveByWorkspace(ctx context.Context, workspaceID uuid.UUID) (int, error)` to `ConnectionRepository` returning the count of connections where `workspace_id = $1 AND status IN ('active', 'connected')`.
- **Verify**: Implement unit tests in `apikey_test.go` and `connection_test.go` and run them: `go test ./internal/repository -run TestAPIKeyRepository_CountActive` and `go test ./internal/repository -run TestConnectionRepository_CountActiveByWorkspace`.
- **Done**: Repositories expose optimized DB COUNT queries filtering by workspace ID and active states.

### Task 2: Update OverviewHandler & Dashboard Template
- **Files**: [internal/api/handler/admin/dashboard.go](file:///home/pablo/Coding/PerGo/internal/api/handler/admin/dashboard.go), [templates/pages/dashboard.templ](file:///home/pablo/Coding/PerGo/templates/pages/dashboard.templ)
- **Action**:
  - In `Index` handler, replace the repository listing counts with `CountActive` and `CountActiveByWorkspace`.
  - Compute `isOnboarded = (activeKeysCount > 0 && activeConnectionsCount > 0)`.
  - Update `templates/pages/dashboard.templ`'s `Dashboard` parameters to accept `activeConnectionsCount int`, and pass it to `OnboardingChecklist`.
- **Verify**: Run `make generate` and check template compilation.
- **Done**: Active counts drive `isOnboarded` status, and the template conditionally renders checklist vs operational widgets using the active connection count.

### Task 3: Adjust Callers and Verification
- **Files**: [internal/api/handler/admin/dashboard_test.go](file:///home/pablo/Coding/PerGo/internal/api/handler/admin/dashboard_test.go) (and other callers of the template if any)
- **Action**:
  - Adjust any test callers of `pages.Dashboard` to pass the new `activeConnectionsCount` parameter.
  - Run all project tests to ensure everything is correct.
- **Verify**: `go test ./... -race -count=1`
- **Done**: All callers and tests are updated to match the new template signature, and verification passes.
