# Campaign Engine & Throttling

## Requirements
- Must support CSV mailing list upload, sanitization (duplicate removal, format validation), dynamic variable mapping, scheduling, batch throttling, duration estimation, and enriched outbound logs.
- Variable mapping must support text inputs with multi-variable interpolation (e.g. `{{nome}} de {{cidade}}`), falling back to static strings if no curly braces match.
- Campaigns must run in the background (via NATS JetStream) using throttled batches to prevent WhatsApp account suspension.
- Enriched campaign logs must be stored within the main `outbound_logs` table (Option A) for simplified reporting and traceability.

## How to Build It

### 1. CSV Parsing & Sanitization
When the mailing CSV is uploaded, parse it line-by-line using standard CSV parsing:
- **Phone Validation**: Cleanse phone numbers by stripping non-numeric characters. Validate that the length is between 10 and 15 digits (E.164-like).
- **Deduplication**: Keep a hash set of cleaned phone numbers per campaign batch. Discard any rows with duplicate phone numbers, logging them as "duplicates" for the campaign summary.

### 2. Flexible Variable Interpolation
Instead of using dropdown selectors, provide text input fields for WABA template variable placeholders (`{{1}}`, `{{2}}`, etc.).
- **Resolution Logic**: In Go/JS, use a regular expression (such as `\{\{(.+?)\}\}`) to locate column placeholder names inside the input text.
- If a match is found, replace the placeholder with the corresponding column value from the parsed CSV row. E.g., `Prezado {{nome}} de {{cidade}}` becomes `Prezado João de São Paulo`.
- If no column matches or it's plain text, it is treated as a static string and sent as-is.

```go
// Example Go dynamic interpolation
func ResolveVariables(inputVal string, row map[string]string) string {
    re := regexp.MustCompile(`\{\{(.+?)\}\}`)
    return re.ReplaceAllStringFunc(inputVal, func(match string) string {
        colName := strings.Trim(match[2:-2], " ")
        if val, exists := row[colName]; exists {
            return val
        }
        return match // keep raw if col doesn't exist
    })
}
```

### 3. Batch Throttling & Jitter Worker
To prevent API blocking and WhatsApp ban triggers:
- **Batching**: Slice the cleaned mailing list into small chunks of size `batch_size`.
- **NATS Queueing**: Enqueue the campaign job to NATS JetStream.
- **Staggered Dispatch (Jitter)**: A background worker pulls messages, sends the batch, and waits for `delay_seconds` before processing the next batch. Add a random stagger jitter of `[-0.5s, +0.5s]` per batch dispatch.

```go
// Jitter delay calculation
jitter := rand.Float64() - 0.5 // range -0.5 to +0.5
time.Sleep(time.Duration(float64(delaySeconds)+jitter) * time.Second)
```

### 4. Enriched Outbound Database Schema (Option A)
To simplify campaign audits, store campaign metadata directly in the `outbound_logs` table:
```sql
ALTER TABLE outbound_logs 
  ADD COLUMN campaign_id UUID NULL,
  ADD COLUMN template_name VARCHAR(100) NULL,
  ADD COLUMN variables_json JSONB NULL;

-- Create an index to keep analytics queries fast
CREATE INDEX idx_outbound_logs_campaign ON outbound_logs(workspace_id, campaign_id) 
  WHERE campaign_id IS NOT NULL;
```

## What to Avoid
- **Avoid rigid dropdown mapping**: Users prefer typing combinations of variables (e.g. `{{nome}} de {{cidade}}`) over a single column drop-down select.
- **Avoid blocking the HTTP thread**: Never process the campaign dispatch loops synchronously in the controller handler. Always dispatch to NATS JetStream and let the worker manage batch timeouts.
- **Avoid missing indexes on campaign_id**: The `outbound_logs` table is high-volume; scanning it for campaign analytics without a composite partial index will degrade DB performance.

## Constraints
- **Phone Constraints**: Numbers must contain country and area codes (e.g. `5511999998888`).
- **WABA Constraints**: Dynamic variables mapping must match the exact number of placeholders required by the selected approved Facebook WABA template.

## Origin
Synthesized from spike: 020
Source files available in: `sources/020-campaign-engine/`
