---
status: complete
---

# Summary: Fix Device Modal, Remove Inboxes section, Add Inbox Action, and Fix ConnectedSince

Completed all tasks defined in the plan:
1. **Resolved DaisyUI Conflict**: Renamed the `.modal` class used in `PairForm` of `devices.templ` to `.custom-modal` and updated `static/css/admin.css` to alias `.modal` to `.custom-modal` to inherit our custom styles without DaisyUI overriding them to `opacity: 0`.
2. **Discontinued Inboxes Grid**: Deleted the "Caixas de Entrada por Canal (Inboxes)" section card grid from `devices.templ`.
3. **Targeted Modal Container**: Added `<div id="modal-container"></div>` to the bottom of the page content in `devices.templ` and pointed the "Nova Conexão" button to target it (`hx-target="#modal-container"` and `hx-swap="innerHTML"`) instead of appending directly to the body.
4. **Inbox Action Button**: Added an "Inbox" action anchor button to each row in the connection list table in `devices.templ` that links directly to the specific channel inbox (`/admin/inbox?channel=<channel>`).
5. **Populated ConnectedSince for WABA/Telegram**:
   - Initialized `ConnectedSince` to current UTC time in `internal/api/handler/admin/device.go` during connection creation.
   - Added a safe default timestamp check in `internal/repository/connection.go`'s `Create` method if status is `"connected"` or `"active"` and `ConnectedSince` is nil.

## Verified
- Ran `make generate` and `go build ./...` successfully.
- Verified all unit and integration tests pass successfully (`go test ./...`).
