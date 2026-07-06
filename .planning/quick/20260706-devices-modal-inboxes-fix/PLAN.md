# Plan: Fix Device Modal, Remove Inboxes section, Add Inbox Action, and Fix ConnectedSince

## Context
1. **Device Modal Hidden**: DaisyUI's `.modal` class overrides custom styles setting `opacity: 0` and `pointer-events: none`. Rename class `.modal` in the PairForm to `.custom-modal` and alias it in `admin.css`.
2. **Inboxes Section Discontinued**: The "Caixas de Entrada por Canal (Inboxes)" section under the connections table is retired. Delete it from `devices.templ`.
3. **Inbox Action Button**: Add an "Inbox" link button in the Actions column of each connection row to jump directly to that channel's inbox.
4. **Connected Since Null**: WABA/Telegram connections are created with `"connected"` status but their `ConnectedSince` timestamp remains `NULL` because `Create()` doesn't populate it.

## Tasks

### Task 1: Update CSS and Templates for Custom Modal Class
- File: `static/css/admin.css`
  - Alias `.modal` to `.custom-modal` to inherit styles without DaisyUI conflicts.
- File: `templates/pages/devices.templ`
  - Rename `.modal` in `PairForm` to `.custom-modal`.
  - Add `<div id="modal-container"></div>` to the end of `DeviceListContent` to act as the target container.
  - Modify the "Nova Conexão" button to target `#modal-container` with `hx-swap="innerHTML"`.

### Task 2: Remove Inboxes Section and Add Inbox Action Column Button
- File: `templates/pages/devices.templ`
  - Remove the "Caixas de Entrada por Canal (Inboxes)" grid section.
  - Add the "Inbox" button to `ConnectionRow` actions.

### Task 3: Fix ConnectedSince field on Connection Creation
- File: `internal/api/handler/admin/device.go`
  - Set `ConnectedSince` on WABA/Telegram connection initialization.
- File: `internal/repository/connection.go`
  - Add fallback default to `ConnectedSince` if status is `"connected"` or `"active"` during connection creation.
