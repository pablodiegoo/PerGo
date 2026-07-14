---
status: complete
---

# Summary of Action: Implement Dynamic Onboarding Logic for Admin Dashboard

I have completed the tasks outlined in the plan:

1. **Repository Layer Changes**:
   - Added `CountActive(ctx context.Context, workspaceID uuid.UUID) (int, error)` to `APIKeyRepository` in `internal/repository/apikey.go` to count only non-revoked API keys.
   - Added `CountActiveByWorkspace(ctx context.Context, workspaceID uuid.UUID) (int, error)` to `ConnectionRepository` in `internal/repository/connection.go` to count only active or connected connections.
   - Created `internal/repository/apikey_test.go` and updated `internal/repository/connection_test.go` to test these queries. All tests passed.

2. **Overview Handler & Dashboard Template Changes**:
   - Updated `Index` handler in `internal/api/handler/admin/dashboard.go` to query active counts of API keys and connections.
   - Changed `isOnboarded` computation logic to require at least one active API key and one active connection.
   - Modified `templates/pages/dashboard.templ` template signature to accept `activeConnectionsCount int` and passed it to `OnboardingChecklist` to correctly determine active state.
   - Compiled the templates via `make generate`.

3. **Verification & Tests**:
   - Added `TestDashboardHandler_Index_Onboarded` unit test to `internal/api/handler/admin/dashboard_test.go` to verify that fully-onboarded workspaces display the operational dashboard, while non-onboarded ones display the onboarding checklist.
   - Verified that the whole test suite passes successfully.

## Commit Details
- Repository changes: `a21a1f72cf93b0dfb925b42d7cd58b9f1d07cde6`
- Handler & template changes: `13f7295fb2430be348e3e4a2cd1c7849e7b2ee93`
