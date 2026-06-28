---
status: complete
date: 2026-06-28
description: Auto-load WABA templates from Meta when WABA credentials are saved with validation feedback
---

# Quick Task: auto-carregar-templates-meta - Summary

## Work Done
1. **Synchronous Validation Feedback**:
   - Refactored `SaveCredentials` in `workspace.go` to invoke `syncTemplatesFromMeta` **synchronously** when saving WABA credentials.
   - If the Meta API call fails (such as an expired token or invalid account IDs), the credentials are NOT saved to the database.
   - Instead, the handler returns the form back to the UI, displaying the specific error message returned by the Meta Graph API.
2. **Form Value Preservation**:
   - Configured `WABACredentialsCard` in `workspaces.templ` to accept an optional error string.
   - Preserved `phone_number_id` and `waba_account_id` input values in the form during validation errors so that the user does not have to retype them.
3. **Meta API Error Decoding**:
   - Configured `syncTemplatesFromMeta` to decode Meta's error JSON (`{"error":{"message":...}}`) on non-200 responses. This extracts the exact reason for the failure (e.g., expired token, invalidated session).
4. **Successful Compilation and Run**:
   - Ran `templ generate`, built Go code, and passed 100% of integration tests.
