---
status: complete
---

# Summary: Fix Test Connection Modal visibility and WhatsApp Web pairing closure issue

All tasks have been successfully resolved:
1. **Fixed Test Connection Modal Visibility**: Replaced the `.modal` class with `.custom-modal` in `TestConnectionModal` (inside `templates/pages/devices.templ`) and configured an inline style override (`style="max-width: 56rem;"`) so DaisyUI's global styles do not hide the modal, while keeping the responsive two-column activity layout fully intact.
2. **Fixed WhatsApp Web Modal Early Dismissal**: Modified the `hx-on::after-request` condition in `PairForm` (inside `templates/pages/devices.templ`) to verify that the active channel is NOT `'whatsapp'` before removing the modal. This ensures the QR pairing code and setup instructions are visible to the operator rather than disappearing immediately upon creation.

## Verification
- Ran `make generate` to compile the type-safe templates.
- Executed `go test ./...` and confirmed all unit/integration tests pass.
