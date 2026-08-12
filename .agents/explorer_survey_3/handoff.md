# Handoff Report: Survey of Requirements R4 & R6

## 1. Observation

Direct observations from examining the codebase:

### Requirement R4: Idempotency & Audit Errors
1. **`internal/api/handler/message.go`**
   - Lines 303-311 (`checkAndRecordIdempotency`):
     ```go
     _, _ = h.IdempotencyRepo.CheckAndStore(ctx, workspaceID, keyHash, traceID, 24*time.Hour)
     _ = h.IdempotencyRepo.RecordLedger(ctx, &repository.IngressLedgerEntry{
         WorkspaceID:    workspaceID,
         TraceID:        traceID,
         IdempotencyKey: idempotencyKey,
         Channel:        req.Channel,
         Recipient:      req.To,
         Status:         "accepted",
     })
     ```
     Errors returned from `CheckAndStore` and `RecordLedger` are discarded using blank identifiers (`_, _ =` and `_ =`).
   - Lines 318-319 (`recordIdempotencyCompletion`):
     ```go
     _ = h.IdempotencyRepo.UpdateLedgerStatus(ctx, workspaceID, traceID, "enqueued", nil)
     _ = h.IdempotencyRepo.UpdateResponse(ctx, workspaceID, keyHash, http.StatusAccepted, respBytes, nil)
     ```
     Errors returned from `UpdateLedgerStatus` and `UpdateResponse` are discarded using blank identifiers (`_ =`).

2. **`internal/platform/queue/campaign_worker.go`**
   - Lines 111-129 (`EmitAuditLog`):
     ```go
     // EmitAuditLog writes an audit log event for a campaign dispatch state change.
     func (w *CampaignWorker) EmitAuditLog(workspaceID uuid.UUID, traceID, eventType, status, recipient string, campaignID uuid.UUID, channel, errStr string) error {
     ```
     `EmitAuditLog` is exported and takes 8 positional parameters (`workspaceID`, `traceID`, `eventType`, `status`, `recipient`, `campaignID`, `channel`, `errStr`).
   - Call sites in `processBatch` (lines 268, 274, 280, 289, 297, 303):
     ```go
     _ = w.EmitAuditLog(task.WorkspaceID, traceID, "campaign_dispatch", "failed", recipient.To, task.CampaignID, channel, err.Error())
     _ = w.EmitAuditLog(task.WorkspaceID, traceID, "campaign_dispatch", "delivered", recipient.To, task.CampaignID, channel, "")
     _ = w.EmitAuditLog(task.WorkspaceID, traceID, "campaign_dispatch", "sent", recipient.To, task.CampaignID, channel, "")
     _ = w.EmitAuditLog(task.WorkspaceID, traceID, "campaign_dispatch", "failed", recipient.To, task.CampaignID, channel, err.Error())
     _ = w.EmitAuditLog(task.WorkspaceID, traceID, "campaign_dispatch", "failed", recipient.To, task.CampaignID, channel, err.Error())
     _ = w.EmitAuditLog(task.WorkspaceID, traceID, "campaign_dispatch", "sent", recipient.To, task.CampaignID, channel, "")
     ```
     Every invocation swallows the returned `error`.
   - Test call site in `internal/platform/queue/campaign_worker_test.go` (line 530):
     ```go
     err := worker.EmitAuditLog(wsID, traceID, "campaign_dispatch", "failed", "5511999993333", campID, "whatsapp", "publish error")
     ```

### Requirement R6: Required `wsRepo` in `NewTagAdminHandler`
1. **`internal/api/handler/admin/tag.go`**
   - Lines 27-36 (`NewTagAdminHandler`):
     ```go
     func NewTagAdminHandler(tagRepo *repository.TagRepository, contactRepo *repository.ContactRepository, wsRepo ...*repository.WorkspaceRepository) *TagAdminHandler {
         h := &TagAdminHandler{
             tagRepo:     tagRepo,
             contactRepo: contactRepo,
         }
         if len(wsRepo) > 0 {
             h.wsRepo = wsRepo[0]
         }
         return h
     }
     ```
     `wsRepo` is currently a variadic parameter `...*repository.WorkspaceRepository`.
   - Lines 46-51 (`RedirectToWorkspaceTags`):
     ```go
     if wsID == uuid.Nil && h.wsRepo != nil {
         list, err := h.wsRepo.List(ctx, 1)
         if err == nil && len(list) > 0 {
             wsID = list[0].ID
         }
     }
     ```
     Defensive `h.wsRepo != nil` check exists because `wsRepo` was optional.

2. **Call sites**
   - `cmd/pergo/main.go:662`:
     ```go
     tagAdminHandler := admin.NewTagAdminHandler(tagRepo, contactRepo, wsRepo)
     ```
     Already passes `wsRepo` as 3rd positional argument.
   - `internal/api/handler/admin/tag_test.go:40`:
     ```go
     handler := admin.NewTagAdminHandler(tagRepo, contactRepo)
     ```
     Passes only 2 arguments and fails compilation if variadic is converted to non-variadic.
   - `internal/api/handler/admin/tag_test.go:225`:
     ```go
     handlerWithWS := admin.NewTagAdminHandler(tagRepo, contactRepo, wsRepo)
     ```
     Already passes `wsRepo` as 3rd positional argument.

---

## 2. Logic Chain

1. **R4 Idempotency Error Handling**:
   - In `message.go`, `CheckAndStore`, `RecordLedger`, `UpdateLedgerStatus`, and `UpdateResponse` perform database transactions for message deduplication and ingress tracking.
   - If a database error occurs in these calls, discarding the error with `_ =` masks database outages or constraint failures.
   - The requirement mandates surfacing these non-fatal errors with `slog.Error` including `trace_id` (and `workspace_id`) context.

2. **R4 Audit Log Emissions in Campaign Worker**:
   - `EmitAuditLog` in `campaign_worker.go` is only called within the `queue` package (`campaign_worker.go` and `campaign_worker_test.go`). Exporting it (`EmitAuditLog`) exposes internal implementation detail unnecessarily.
   - 8 positional parameters create a data-clump smell and make call sites verbose and error-prone.
   - Bundling parameters into a single struct (e.g., `auditDispatchEvent`) simplifies call sites and parameter extension.
   - Silently ignoring returned errors from `emitAuditLog` in `processBatch` violates tracing and audit durability guarantees. Log errors with `slog.Error`.

3. **R6 Tag Admin Handler Signature**:
   - `TagAdminHandler.RedirectToWorkspaceTags` requires `h.wsRepo` to query workspace IDs when missing from cookies.
   - Making `wsRepo` optional via variadic slice disguised a mandatory dependency.
   - Changing `NewTagAdminHandler` to `func NewTagAdminHandler(tagRepo *repository.TagRepository, contactRepo *repository.ContactRepository, wsRepo *repository.WorkspaceRepository) *TagAdminHandler` enforces contract at compile time.
   - Once `wsRepo` is non-variadic and guaranteed non-nil, `if wsID == uuid.Nil && h.wsRepo != nil` in `RedirectToWorkspaceTags` should be simplified to `if wsID == uuid.Nil`.
   - Update `tag_test.go:40` to pass `wsRepo`.

---

## 3. Caveats

- **No caveats**: All file locations, signatures, call sites, and test cases were completely surveyed and verified against the workspace code.

---

## 4. Conclusion & Proposed Implementation Plan

### Requirement R4 Changes

#### 1. Edit `internal/api/handler/message.go`
- In `checkAndRecordIdempotency`:
  ```go
  if _, err := h.IdempotencyRepo.CheckAndStore(ctx, workspaceID, keyHash, traceID, 24*time.Hour); err != nil {
      slog.Error("failed to store idempotency key", "trace_id", traceID, "workspace_id", workspaceID.String(), "error", err)
  }
  if err := h.IdempotencyRepo.RecordLedger(ctx, &repository.IngressLedgerEntry{
      WorkspaceID:    workspaceID,
      TraceID:        traceID,
      IdempotencyKey: idempotencyKey,
      Channel:        req.Channel,
      Recipient:      req.To,
      Status:         "accepted",
  }); err != nil {
      slog.Error("failed to record idempotency ledger", "trace_id", traceID, "workspace_id", workspaceID.String(), "error", err)
  }
  ```
- In `recordIdempotencyCompletion`:
  ```go
  if err := h.IdempotencyRepo.UpdateLedgerStatus(ctx, workspaceID, traceID, "enqueued", nil); err != nil {
      slog.Error("failed to update idempotency ledger status", "trace_id", traceID, "workspace_id", workspaceID.String(), "error", err)
  }
  if err := h.IdempotencyRepo.UpdateResponse(ctx, workspaceID, keyHash, http.StatusAccepted, respBytes, nil); err != nil {
      slog.Error("failed to update idempotency response", "trace_id", traceID, "workspace_id", workspaceID.String(), "error", err)
  }
  ```

#### 2. Edit `internal/platform/queue/campaign_worker.go`
- Define struct:
  ```go
  type auditDispatchEvent struct {
      WorkspaceID uuid.UUID
      TraceID     string
      EventType   string
      Status      string
      Recipient   string
      CampaignID  uuid.UUID
      Channel     string
      ErrStr      string
  }
  ```
- Rename `EmitAuditLog` -> `emitAuditLog(event auditDispatchEvent) error`:
  ```go
  func (w *CampaignWorker) emitAuditLog(event auditDispatchEvent) error {
      if w.auditWriter == nil {
          return nil
      }
      payload := map[string]any{
          "campaign_id": event.CampaignID,
          "recipient":   event.Recipient,
          "status":      event.Status,
          "channel":     event.Channel,
      }
      if event.ErrStr != "" {
          payload["error"] = event.ErrStr
      }
      payloadBytes, err := json.Marshal(payload)
      if err != nil {
          return err
      }
      return w.auditWriter.Write(audit.NewEvent(event.WorkspaceID, event.TraceID, event.EventType, payloadBytes))
  }
  ```
- In `processBatch`, replace `_ = w.EmitAuditLog(...)` calls with:
  ```go
  if err := w.emitAuditLog(auditDispatchEvent{...}); err != nil {
      slog.Error("campaign_worker: failed to emit audit log", "trace_id", traceID, "error", err)
  }
  ```
- In `internal/platform/queue/campaign_worker_test.go:530`, update call to:
  ```go
  err := worker.emitAuditLog(auditDispatchEvent{
      WorkspaceID: wsID,
      TraceID: traceID,
      EventType: "campaign_dispatch",
      Status: "failed",
      Recipient: "5511999993333",
      CampaignID: campID,
      Channel: "whatsapp",
      ErrStr: "publish error",
  })
  ```

---

### Requirement R6 Changes

#### 1. Edit `internal/api/handler/admin/tag.go`
- Change `NewTagAdminHandler`:
  ```go
  func NewTagAdminHandler(tagRepo *repository.TagRepository, contactRepo *repository.ContactRepository, wsRepo *repository.WorkspaceRepository) *TagAdminHandler {
      return &TagAdminHandler{
          tagRepo:     tagRepo,
          contactRepo: contactRepo,
          wsRepo:      wsRepo,
      }
  }
  ```
- Change `RedirectToWorkspaceTags`:
  ```go
  if wsID == uuid.Nil {
      list, err := h.wsRepo.List(ctx, 1)
      if err == nil && len(list) > 0 {
          wsID = list[0].ID
      }
  }
  ```

#### 2. Edit `internal/api/handler/admin/tag_test.go`
- Update line 40:
  ```go
  handler := admin.NewTagAdminHandler(tagRepo, contactRepo, wsRepo)
  ```

---

## 5. Verification Method

To verify the implementation once applied:

1. **Compile check**:
   ```bash
   go test ./... -exec true
   ```
2. **Run Handler and Queue Test Suites**:
   ```bash
   go test -v ./internal/api/handler/...
   go test -v ./internal/platform/queue/...
   go test -v ./internal/api/handler/admin/...
   ```
3. **Full Integration and Unit Test Suite**:
   ```bash
   go test -race ./...
   ```

**Invalidation conditions**:
- Any swallowed error (`_ =`) remaining in `checkAndRecordIdempotency`, `recordIdempotencyCompletion`, or `processBatch` audit emissions.
- Compilation failure in `main.go` or `tag_test.go` due to constructor parameter mismatch.
- Failure of `TestTagAdminHandler` or `TestCampaignWorker_*` tests.
