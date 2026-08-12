## 2026-08-12T10:00:37Z

Implement Requirements R2 and R3 (Tag-recipient resolution and recipient validation).

File Ownership:
- `internal/domain/campaign.go`
- `internal/api/handler/admin/campaign.go`
- `internal/api/handler/admin/campaign_test.go`

Tasks:
1. In `internal/domain/campaign.go`:
   - Add `TagContactLister` interface:
     `type TagContactLister interface { ListContactsByTag(ctx context.Context, workspaceID, tagID uuid.UUID) ([]Contact, error) }`
   - Add `DeduplicateUUIDs(ids []uuid.UUID) []uuid.UUID` helper function.
   - Add `ResolveTagRecipients(ctx context.Context, lister TagContactLister, workspaceID uuid.UUID, tagIDs []uuid.UUID)` returning `([]CampaignRecipientRecord, []CampaignRecipient, map[string]bool, error)`. Remove `SanitizePhone(contact.Name)` fallback (only check `contact.Identities`).
2. In `internal/api/handler/admin/campaign.go`:
   - Update form `Create` and REST `APICreate` handlers to use `domain.ResolveTagRecipients` and `domain.DeduplicateUUIDs`. Remove inline `already := false` deduplication loops. Remove inline `SanitizePhone(contact.Name)` fallbacks.
   - In form `Create`, add server-side validation: if `len(recipientRecords) == 0`, return HTTP 400 Bad Request with user-facing message `"A campanha precisa de pelo menos um destinatário. Selecione uma tag ou envie um CSV."`.
3. In `internal/api/handler/admin/campaign_test.go`:
   - Add subtest `Create Campaign Validation - No Recipients` verifying HTTP 400 response and error message when no recipients are resolved.
4. Run builds and tests (`go test -v ./internal/domain/... ./internal/api/handler/admin/...`).
5. Document commands and exact test outputs in `/home/pablodiegoo/coding/PerGo/.agents/worker_m2/handoff.md`.
