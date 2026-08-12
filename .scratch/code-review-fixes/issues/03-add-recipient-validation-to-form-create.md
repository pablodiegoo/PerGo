# 03 — Add server-side recipient validation to form-based campaign Create

**What to build:** The form-based `Create` handler currently allows creating a campaign with zero recipients (no tags selected, no CSV uploaded) because the `required` attribute was removed from the CSV file input. The REST `APICreate` handler already validates this. Add the same server-side check to `Create`: after resolving tags + CSV recipients via the shared helper from ticket #02, if `len(recipientRecords) == 0`, return an error to the user. The error should be an HTMX-friendly response that the form can display.

**Blocked by:** 02 — Extract tag-recipient resolution helper (the validation sits after the shared resolution call).

**Status:** ready-for-agent

- [ ] `Create` returns an error (HTTP 400 or equivalent HTMX error fragment) when no recipients are resolved from tags or CSV
- [ ] The error message is user-facing and clear (e.g., "A campanha precisa de pelo menos um destinatário. Selecione uma tag ou envie um CSV.")
- [ ] A test case in `campaign_test.go` verifies that a form submission with no tag and no CSV returns an error
- [ ] Existing campaign creation tests (with tags, with CSV) continue to pass
