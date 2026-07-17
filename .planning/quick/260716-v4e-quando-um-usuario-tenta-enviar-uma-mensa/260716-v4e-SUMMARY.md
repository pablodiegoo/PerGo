---
status: complete
---
# Summary: WABA Default Templates for Test/New Messages (260716-v4e)

Successfully completed the quick task to default WABA cloud channels to template sends when sending new or test messages.

## Deliverables
- **New Chat Modal (`new_chat_modal.templ`)**:
  - Implemented automatic selection check for `whatsapp_cloud` and other channels.
  - Dynamically toggles CSS `hidden` / `flex` display classes for plaintext vs template fields depending on the pre-selected `channel` value on load.
- **Test Connection Modal (`devices.templ`)**:
  - Refactored `TestConnectionModal` signature to accept `templates []repository.WABATemplate`.
  - Added template drop-down picker and variables inputs for WABA channels.
  - Hidden plaintext message textarea for WABA channel tests.
  - Added `showTestTemplatePreview` JS function handler to toggle variables fields visibility.
- **Device Handler (`device.go`)**:
  - In `TestForm`, query and supply WABA templates to the modal when the connection channel is `"whatsapp_cloud"`.
  - In `RunTest`, check if `is_template` is true and construct the structured template variables payload dynamically to publish to JetStream.
- **Tests & Rebuild**:
  - Re-generated templ outputs and successfully verified clean compilation.
  - Executed all admin handler tests successfully.
  - Rebuilt the Docker compose production stack.
