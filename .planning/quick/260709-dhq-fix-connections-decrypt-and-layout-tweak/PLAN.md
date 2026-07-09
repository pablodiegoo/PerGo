---
quick_id: "260709-dhq"
slug: "fix-connections-decrypt-and-layout-tweak"
type: quick
status: executing
---

# Plan: Fix Connections Decrypt & Layout Tweaks

## Objective
Fix the decryption failure when loading the connection list, enforce the light theme for DaisyUI, improve input field aesthetics, and refactor the audit filters into a responsive, horizontal desktop grid.

## Tasks
1. **Task 1: Graceful Decryption in Connection Listing**
   - Modify `scanAndDecrypt` and `scanRowAndDecrypt` in `internal/repository/connection.go` to log decryption errors instead of failing the database scan. This prevents visual 500 crashes if credentials cannot be decrypted (e.g. KEK changed/corrupted), while preserving strict decryption failures for `GetCredentials`.
2. **Task 2: Force Light Theme**
   - Add `data-theme="light"` to the `html` tags in `templates/layout/base.templ` (Base and LoginBase) to prevent DaisyUI's automatic dark theme activation.
3. **Task 3: Consolidate Input Field Styles**
   - Define a clean, uniform style for `.form-input` and `.form-group input/select` in `static/css/admin.css` with a white background and subtle zinc focus rings.
4. **Task 4: Responsive Audit Filter UI Grid**
   - Refactor `AuditFiltersUI` in `templates/pages/audit.templ` to layout fields in a clean 5-column grid on desktop, aligning all fields (Workspace, Trace ID, Start Date, End Date) and search/export buttons horizontally.
5. **Task 5: Update Tests & Verification**
   - Add a database cleanup step to `TestWABA_MediaExternalURL` in `waba_test.go`.
   - Regenerate templates and run all tests to verify passing status.
