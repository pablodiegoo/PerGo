# 02 — Global Modal Infrastructure & Inbox Template Triggering

**What to build:** Mount the modal container element in the global base layout so all views support dynamic modals. Ensure the Inbox "Templates" and "Novo Chat" buttons trigger interactive compose dialogs that dynamically parse template components, render exact variable input fields, and submit the template's designated language rather than a hardcoded default.

**Blocked by:** None — can start immediately

**Status:** ready-for-agent

- [ ] `#modal-container` is present in the global base HTML layout, accessible to all admin pages.
- [ ] Clicking "Templates" in the Inbox chat panel renders the template modal via HTMX without DOM target errors.
- [ ] Template selection in `NewChatModal` dynamically inspects the selected template's component definition and displays exact parameter inputs (`{{1}}`, `{{2}}`, etc.) or an informational note when static.
- [ ] Submitting a template via `NewMessageSend` captures and transmits the template's registered language (e.g. `en_US`, `pt_BR`) and extracted dynamic parameters.
