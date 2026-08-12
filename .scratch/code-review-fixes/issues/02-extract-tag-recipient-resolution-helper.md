# 02 — Extract tag-recipient resolution into a shared domain helper

**What to build:** The ~50-line tag→contact→phone→dedup logic is copy-pasted between the form-based `Create` handler and the REST `APICreate` handler in the campaign admin code. Extract this into a single shared function (e.g., `resolveTagRecipients`) that both handlers call. Also extract the tag-ID deduplication loop (`already := false; for _, existing := range ...`) — which appears three times — into a small `uniqueAppend` or set-based helper. Remove the fallback that treats `contact.Name` as a phone number via `SanitizePhone(contact.Name)` — this is undocumented behaviour that risks false positives when a contact name happens to be numeric.

**Blocked by:** None — can start immediately.

**Status:** ready-for-agent

- [ ] A single shared function resolves tag IDs → deduplicated `CampaignRecipientRecord` + `CampaignRecipient` slices + `seenPhones` map
- [ ] Both `Create` and `APICreate` call the shared function instead of inline logic
- [ ] Tag-ID deduplication uses a helper or set — no more inline `already` loops
- [ ] The `SanitizePhone(contact.Name)` fallback is removed; contacts without a valid phone in their identities are skipped
- [ ] All existing campaign tests pass (`campaign_test.go`)
