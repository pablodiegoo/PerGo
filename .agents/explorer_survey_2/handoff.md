# Survey Analysis Report: Requirements R2 & R3

## 1. Observation

Direct observations from codebase inspection of `/home/pablodiegoo/coding/PerGo`:

### Requirement R2 Findings:
- **Campaign Handler Location**: `/home/pablodiegoo/coding/PerGo/internal/api/handler/admin/campaign.go`
  - Form-based creation handler: `func (h *CampaignHandler) Create(c *echo.Context) error` (lines 257–476).
  - REST API creation handler: `func (h *CampaignHandler) APICreate(c *echo.Context) error` (lines 720–899).
- **Inline UUID Deduplication Loops**:
  - `Create` handler (lines 324–334):
    ```go
    already := false
    for _, existing := range formTagIDs {
        if existing == parsedID {
            already = true
            break
        }
    }
    if !already {
        formTagIDs = append(formTagIDs, parsedID)
    }
    ```
  - `APICreate` handler (lines 759–768):
    ```go
    already := false
    for _, existing := range targetTagIDs {
        if existing == tid {
            already = true
            break
        }
    }
    if !already {
        targetTagIDs = append(targetTagIDs, tid)
    }
    ```
- **Tag -> Contact -> Phone -> Dedup Duplication & Fallback**:
  - `Create` handler (lines 356–397) and `APICreate` handler (lines 782–824) duplicate ~50 lines of tag resolution logic.
  - Both handlers currently contain the fallback that uses `contact.Name` as a phone number if no sender identity matches:
    - `Create` (line 371): `if clean, valid := domain.SanitizePhone(contact.Name); valid { phone = clean }`
    - `APICreate` (line 799): `if clean, valid := domain.SanitizePhone(contact.Name); valid { phone = clean }`
- **Domain Package Location**:
  - `/home/pablodiegoo/coding/PerGo/internal/domain/campaign.go` contains `CampaignRecipientRecord`, `CampaignRecipient`, `SanitizePhone`.
  - `/home/pablodiegoo/coding/PerGo/internal/domain/contact.go` contains `Contact` and `ContactIdentity`.
  - `/home/pablodiegoo/coding/PerGo/internal/repository/tag.go` defines `ListContactsByTag(ctx context.Context, workspaceID, tagID uuid.UUID) ([]domain.Contact, error)` on `*TagRepository`.

### Requirement R3 Findings:
- **Server-Side Recipient Validation Difference**:
  - `APICreate` handler validates recipient count at lines 848–850:
    ```go
    if len(recipientRecords) == 0 {
        return c.JSON(http.StatusBadRequest, map[string]string{"error": "no valid E.164 recipients resolved for campaign"})
    }
    ```
  - Form-based `Create` handler currently has NO validation check for `len(recipientRecords) == 0`. If a user submits a form without selecting any tags and without uploading a valid CSV, `Create` builds `domain.Campaign` with `TotalRecipients: 0` and attempts to persist it to PostgreSQL (lines 446–472).
- **Test File Location**: `/home/pablodiegoo/coding/PerGo/internal/api/handler/admin/campaign_test.go`
  - Existing subtest `t.Run("Create Campaign", ...)` (lines 147–309) tests form campaign creation with CSV recipients.
  - Existing subtest `t.Run("REST API Campaign Endpoints", ...)` (lines 311–426) tests `APICreate` with pre-flight recipient validation failure (lines 344–358).
  - No test currently asserts that form-based `Create` returns an error when zero recipients are resolved.

---

## 2. Logic Chain

1. **R2 Code Deduplication & Helper Architecture**:
   - The logic resolving tag IDs into `[]domain.CampaignRecipientRecord` and `[]domain.CampaignRecipient` while populating `seenPhones` is duplicated line-for-line across `Create` and `APICreate`.
   - By creating an interface in `domain`:
     ```go
     type TagContactLister interface {
         ListContactsByTag(ctx context.Context, workspaceID, tagID uuid.UUID) ([]Contact, error)
     }
     ```
     `*repository.TagRepository` automatically satisfies this interface without introducing an import cycle (`domain` -> `repository`).
   - Extracting `ResolveTagRecipients` into `internal/domain/campaign.go` encapsulates contact identity phone scanning and deduplication within domain boundaries.
   - Removing the `contact.Name` fallback (`SanitizePhone(contact.Name)`) ensures that contacts without a valid phone identity in `contact.Identities` are safely skipped rather than treating numerical contact names as phone numbers.
   - Creating a generic UUID deduplication helper `DeduplicateUUIDs(ids []uuid.UUID) []uuid.UUID` in `domain` eliminates all instances of inline `already` boolean flag loops in `campaign.go`.

2. **R3 Recipient Validation Logic**:
   - Form submission allows optional CSV upload and optional tag filter selection. If neither tag contacts nor CSV rows yield valid E.164 phone numbers, `recipientRecords` remains an empty slice (`len(recipientRecords) == 0`).
   - Adding `if len(recipientRecords) == 0` immediately after tag and CSV resolution in `Create` prevents creating orphaned 0-recipient campaign records in PostgreSQL.
   - Returning `c.String(http.StatusBadRequest, "A campanha precisa de pelo menos um destinatário. Selecione uma tag ou envie um CSV.")` yields a clear, user-facing HTMX-friendly response consistent with error responses in `internal/api/handler/admin/campaign.go`.

3. **Test Integration**:
   - Adding a subtest `t.Run("Create Campaign Validation - No Recipients", ...)` in `campaign_test.go` verifies that submitting a form with no tag and no CSV returns HTTP 400 Bad Request with the expected Portuguese error message.

---

## 3. Caveats

- **CSV Pre-parsing**: `UploadCSV` (line 111 in `campaign.go`) validates raw CSV files and renders preview templates, but does not persist campaign recipient records. Final merging occurs inside `Create` and `APICreate`.
- **Environment PATH**: Executing `go test` requires setting PATH to include `/home/pablodiegoo/.local/go/bin` (e.g. `export PATH=$PATH:/home/pablodiegoo/.local/go/bin`).
- **Read-Only Scope**: This report provides explicit design specifications and code changes for implementation agents without modifying repository code.

---

## 4. Conclusion

### Proposed R2 Implementation Design:

1. **Define `TagContactLister` interface & `DeduplicateUUIDs` helper in `internal/domain/campaign.go`**:
   ```go
   type TagContactLister interface {
       ListContactsByTag(ctx context.Context, workspaceID, tagID uuid.UUID) ([]Contact, error)
   }

   func DeduplicateUUIDs(ids []uuid.UUID) []uuid.UUID {
       seen := make(map[uuid.UUID]bool, len(ids))
       result := make([]uuid.UUID, 0, len(ids))
       for _, id := range ids {
           if id != uuid.Nil && !seen[id] {
               seen[id] = true
               result = append(result, id)
           }
       }
       return result
   }
   ```

2. **Define `ResolveTagRecipients` in `internal/domain/campaign.go`**:
   ```go
   func ResolveTagRecipients(
       ctx context.Context,
       lister TagContactLister,
       workspaceID uuid.UUID,
       tagIDs []uuid.UUID,
   ) ([]CampaignRecipientRecord, []CampaignRecipient, map[string]bool, error) {
       seenPhones := make(map[string]bool)
       var records []CampaignRecipientRecord
       var recipients []CampaignRecipient

       uniqueIDs := DeduplicateUUIDs(tagIDs)
       if lister == nil || len(uniqueIDs) == 0 {
           return records, recipients, seenPhones, nil
       }

       for _, tagID := range uniqueIDs {
           contacts, err := lister.ListContactsByTag(ctx, workspaceID, tagID)
           if err != nil {
               return nil, nil, nil, err
           }
           for _, contact := range contacts {
               phone := ""
               for _, ident := range contact.Identities {
                   if ident.SenderIdentity != "" {
                       if clean, valid := SanitizePhone(ident.SenderIdentity); valid {
                           phone = clean
                           break
                       }
                   }
               }
               if phone == "" {
                   continue // Removed SanitizePhone(contact.Name) fallback
               }
               if seenPhones[phone] {
                   continue
               }
               seenPhones[phone] = true

               contactID := contact.ID
               records = append(records, CampaignRecipientRecord{
                   ContactID: &contactID,
                   Phone:     phone,
                   Status:    RecipientStatusPending,
                   Variables: map[string]string{"name": contact.Name},
               })
               recipients = append(recipients, CampaignRecipient{
                   To:        phone,
                   Variables: map[string]string{"name": contact.Name},
               })
           }
       }

       return records, recipients, seenPhones, nil
   }
   ```

3. **Update `Create` and `APICreate` in `internal/api/handler/admin/campaign.go`**:
   - Replace tag ID collecting loops using `domain.DeduplicateUUIDs`.
   - Call `domain.ResolveTagRecipients` in both handlers.
   - Remove inline ~50-line tag resolution and `SanitizePhone(contact.Name)` fallback lines (lines 371 & 799).

### Proposed R3 Implementation Design:

1. **Add check to `Create` in `internal/api/handler/admin/campaign.go`**:
   Immediately after resolving tag contacts and CSV recipients:
   ```go
   if len(recipientRecords) == 0 {
       return c.String(http.StatusBadRequest, "A campanha precisa de pelo menos um destinatário. Selecione uma tag ou envie um CSV.")
   }
   ```

2. **Add unit test in `internal/api/handler/admin/campaign_test.go`**:
   ```go
   t.Run("Create Campaign Validation - No Recipients", func(t *testing.T) {
       form := url.Values{}
       form.Set("name", "Empty Recipients Campaign")
       form.Set("channel", "whatsapp")
       form.Set("batch_size", "50")
       form.Set("delay_seconds", "3")
       form.Set("body_template", "Test")

       req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/admin/workspaces/%s/campaigns", ws.ID), strings.NewReader(form.Encode()))
       req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
       rec := httptest.NewRecorder()
       c := e.NewContext(req, rec)
       c.SetPath("/admin/workspaces/:workspace_id/campaigns")
       c.SetPathValues(echo.PathValues{
           {Name: "workspace_id", Value: ws.ID.String()},
       })

       if err := h.Create(c); err != nil {
           t.Fatalf("Create failed: %v", err)
       }
       if rec.Code != http.StatusBadRequest {
           t.Errorf("expected status 400, got %d", rec.Code)
       }
       if !strings.Contains(rec.Body.String(), "A campanha precisa de pelo menos um destinatário") {
           t.Errorf("expected validation error, got: %s", rec.Body.String())
       }
   })
   ```

---

## 5. Verification Method

To verify the implementation once applied:

1. **Run Unit & Integration Tests**:
   ```bash
   export PATH=$PATH:/home/pablodiegoo/.local/go/bin
   go test -v ./internal/domain/... ./internal/api/handler/admin/...
   ```
2. **Code Inspection Invalidation Checks**:
   - Check `internal/api/handler/admin/campaign.go` for any remaining `already := false` loops (should be 0).
   - Check `internal/api/handler/admin/campaign.go` for any `SanitizePhone(contact.Name)` calls (should be 0).
   - Confirm `Create` handler contains the `len(recipientRecords) == 0` validation returning HTTP 400.
