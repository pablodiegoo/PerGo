# Plan: Fix Test Connection Modal visibility and WhatsApp Web pairing closure issue

## Context
1. **Test Connection Modal Hidden**: DaisyUI's `.modal` class overrides custom styles setting `opacity: 0` and `pointer-events: none`. Changing it to `.custom-modal` with inline `style="max-width: 56rem;"` fixes visibility while preserving the two-column grid layout width.
2. **WhatsApp Web Pairing Modal closes immediately**: The `after-request` event handler closes the modal on any successful POST, but for WhatsApp Web pairing, the success response contains the QR code which needs to be displayed rather than closing the modal. Adding a select value check prevents early dismissal.

## Tasks

### Task 1: Update TestConnectionModal class and max-width style
- File: `templates/pages/devices.templ`
  - Change `.modal` in `TestConnectionModal` to `.custom-modal` and add inline `style="max-width: 56rem;"`.

### Task 2: Modify after-request form check to ignore whatsapp channel
- File: `templates/pages/devices.templ`
  - In `PairForm`, update `hx-on::after-request` condition to ensure the backdrop is only removed when selected channel is NOT `'whatsapp'`.
