## 2026-08-12T09:58:51-03:00

Investigate Requirement R2 (Extract tag-recipient resolution into a shared domain helper) and Requirement R3 (Add server-side recipient validation to form-based campaign Create).

Tasks:
1. Investigate Requirement R2:
   - Locate form-based `Create` and REST `APICreate` campaign handlers.
   - Examine tag -> contact -> phone -> dedup logic in both handlers.
   - Identify where inline `already` deduplication loops exist and where `SanitizePhone(contact.Name)` fallback is used.
   - Propose the package, signature, and design for a single shared helper function returning deduplicated records and seen phones.
2. Investigate Requirement R3:
   - Examine form-based `Create` handler after tag + CSV recipient resolution.
   - Determine how to handle `len(recipientRecords) == 0` (returning HTTP 400 or HTMX error fragment).
   - Locate `campaign_test.go` and outline how to add a test case verifying this error behavior.

Write full analysis report to `.agents/explorer_survey_2/handoff.md` and send message to parent agent.
