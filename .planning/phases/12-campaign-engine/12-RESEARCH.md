# Phase 12: Campaign Engine - Research and Technical Approach

## 1. Executive Summary & Findings

The goal of Phase 12 is to implement a robust, scalable, and throttled **Campaign Engine** inside PerGo. This engine will allow operators to upload bulk mailing lists via CSV, dynamically map template placeholders using advanced regex-based variable interpolation, schedule campaigns, and execute them in the background using NATS JetStream without risking WhatsApp account bans or API rate limits.

### Key Discoveries & Recommendations:
- **Sanitization Pipeline**: Standardizing phone number cleaning to strip all non-numeric characters and enforce the E.164 range constraints (10-15 digits) ensures maximum compatibility and prevents invalid sends.
- **Dynamic Interpolation**: Instead of using rigid column-to-parameter dropdown mappings, a regex-based text approach (`\{\{(.+?)\}\}`) will be adopted. This allows combining static text and multiple variables (e.g. `Prezado {{nome}} de {{cidade}}`) while automatically falling back to index-based placeholders (`{{0}}`, `{{1}}`) if columns are unnamed or headerless.
- **Serialized Batch Throttling**: Rather than enqueuing individual messages, campaigns are sliced into discrete batch tasks enqueued to NATS JetStream. Serially processing these batches is enforced at the NATS consumer level by specifying `MaxAckPending: 1`. This native NATS constraint guarantees that batches are executed in sequence with configured inter-batch delays and random jitter, even when scaled across multiple server replicas, without requiring heavy distributed lock systems.
- **Logging Decision (Option A)**: We recommend **Option A (Enriched Outbound Logs)** as outlined in the implementation decisions. By directly adding `campaign_id`, `template_name`, and `variables_json` to `message_dispatches`, we avoid query-intensive table joins or `UNION` queries, keeping transactional auditing and campaign reports unified.
- **Trace-ID Idempotency**: Campaign trace IDs will follow a strict format: `campaign_${campaign_id}_${recipient}`. Since `trace_id` is unique in `message_dispatches`, this prevents double-sending to the same recipient in the same campaign run, even in the event of NATS message redeliveries or node crashes.

---

## 2. CSV Parsing, Phone Validation, and Deduplication

### Delimiter Sniffing & Parsing
To prevent ingestion issues due to different locale configurations (e.g. European semicolons versus American commas), the engine will sniff the CSV delimiter dynamically by analyzing the file's first line:

```go
func SniffDelimiter(firstLine string) rune {
    candidates := []rune{',', ';', '\t'}
    counts := make(map[rune]int)
    for _, char := range firstLine {
        for _, cand := range candidates {
            if char == cand {
                counts[cand]++
            }
        }
    }
    best := ','
    maxCount := 0
    for cand, count := range counts {
        if count > maxCount {
            maxCount = count
            best = cand
        }
    }
    return best
}
```

The CSV will be parsed line-by-line using Go's `encoding/csv` reader configured with the detected delimiter. The first line is treated as headers, lowercased and trimmed. If header columns are empty, the parser falls back to using index positions.

### Phone Validation (E.164-like)
WhatsApp and other outbound providers require digits only. The sanitation process is:
1. Strip all non-numeric characters: `re := regexp.MustCompile("[^0-9]")`
2. Check length constraints: must be between **10 and 15 digits** (inclusive).
3. Numbers failing this check are flagged as `invalid_phone` and skipped.

### Deduplication Strategy
To avoid sending spam or wasting WABA limits, duplicates must be filtered out:
- During parsing, a hash set of cleaned phone numbers (`map[string]bool`) is maintained.
- If a phone number is already present, the record is flagged as `duplicate` and skipped.

### Skipped Rows CSV Report
When upload is completed, the API returns a response containing:
- Summary counts: `total_rows`, `valid_rows`, `duplicate_rows`, `invalid_rows`.
- A temporary file download URL containing a CSV of skipped records. This CSV includes:
  - `Line Number`: Original line in the uploaded file.
  - `Raw Input`: The raw text line.
  - `Reason`: Why the row was rejected (e.g. `duplicate`, `invalid_phone: length 8`).

---

## 3. Dynamic Variable Mapping & Interpolation

WABA templates require mapped parameters. Rather than forcing users to pair template variables to single columns, the system supports a flexible parser that resolves template parameter strings containing multiple placeholders:

```go
// ResolveVariables matches {{column_name}} placeholders inside a template input
// and replaces them with corresponding values from the parsed CSV row.
func ResolveVariables(inputVal string, row map[string]string) string {
    re := regexp.MustCompile(`\{\{(.+?)\}\}`)
    return re.ReplaceAllStringFunc(inputVal, func(match string) string {
        colName := strings.TrimSpace(match[2 : len(match)-2])
        // Case-insensitive matching
        colKey := strings.ToLower(colName)
        if val, exists := row[colKey]; exists {
            return val
        }
        return match // Keep raw placeholder if column is missing
    })
}
```

### Scenario Mapping:
- **Standard**: User enters `Prezado {{Nome}}` -> resolved using column `"nome"`.
- **Composite**: User enters `Olá {{First Name}} {{Last Name}}` -> resolved using two columns.
- **Headerless**: User enters `{{0}} {{1}}` -> falls back to index-based keys if no headers are matched.
- **Static**: If the string lacks any braces, it is treated as a static text parameter.

---

## 4. Throttling and Scheduler Implementation using NATS JetStream

To protect WhatsApp numbers from suspension due to sudden burst patterns, campaign dispatches are throttled using batch-by-batch delivery backed by NATS JetStream.

```
+------------------+     Create Batches     +-------------------+
|  Campaign UI/API  | --------------------> | NATS JetStream    |
|  Create Campaign  |                       | Stream: CAMPAIGNS |
+------------------+                       +-------------------+
                                                      |
                                                      | MaxAckPending: 1
                                                      v
                                            +-------------------+
                                            |  Campaign Worker  |
                                            |  Processes Batch  |
                                            +-------------------+
                                                      |
                                           (Delay + Jitter Sleep)
                                                      |
                                                      v
                                            +-------------------+
                                            |  ACK Batch        |
                                            +-------------------+
```

### 1. NATS JetStream Stream Configuration
We will create a stream dedicated to campaigns:
- **Name**: `CAMPAIGNS`
- **Subjects**: `campaigns.>`
- **Retention**: `WorkQueuePolicy` (guarantees messages are deleted from the queue on ACK).

### 2. Campaign Creation & Batch Slicing
When a campaign starts (or when scheduled execution triggers):
1. The scheduler updates the campaign's database status to `sending`.
2. The scheduler slices the list of valid recipients into batches of size `batch_size`.
3. For each batch, a JSON task payload is published to `campaigns.batches`:
   ```json
   {
     "campaign_id": "893c5d6e-...",
     "workspace_id": "01b2a3c4-...",
     "batch_index": 1,
     "recipients": [
       {"to": "5511999998888", "variables": {"nome": "João", "cidade": "São Paulo"}},
       {"to": "5511999997777", "variables": {"nome": "Maria", "cidade": "Campinas"}}
     ],
     "delay_seconds": 5
   }
   ```

### 3. Serialization via NATS Consumer Config
To enforce sequential, throttled processing across multiple application replicas:
- A consumer named `campaign-batch-consumer` is registered on the `CAMPAIGNS` stream.
- **Crucial parameter**: `MaxAckPending` must be set to `1`.
- When set to `1`, NATS will not dispatch any further messages (batches) to *any* worker instance until the currently leased message is explicitly ACKed.

### 4. Worker Processing Loop & Jitter Calculation
The campaign worker loop performs the following operations:
1. Pulls a batch task.
2. Checks campaign status in the DB:
   - If the campaign status is `cancelled`, the worker immediately ACKs the NATS message and halts processing.
3. Iterates over the batch recipients, formats the message text using interpolation, generates the trace ID, and enqueues/dispatches the message.
4. Performs a throttled sleep before ACK-ing the message:
   ```go
   // Calculate random jitter between -0.5s and +0.5s
   jitter := (rand.Float64() - 0.5) * 1.0 // float value between -0.5 and +0.5
   totalSleep := time.Duration(float64(task.DelaySeconds)+jitter) * time.Second
   if totalSleep < 0 {
       totalSleep = 0
   }
   time.Sleep(totalSleep)
   ```
5. ACKs the batch task. NATS then releases the next batch task to the consumer cluster.

### 5. Duration Estimation Formula
The estimated campaign dispatch duration is calculated dynamically in the UI and backend validator using the following parameters:
- $N_{valid}$ = Total number of valid recipients.
- $S_{batch}$ = Configured batch size.
- $D_{delay}$ = Inter-batch delay (in seconds).
- $J_{mean}$ = Mean value of the random jitter. Since jitter is uniformly distributed on $[-0.5, 0.5]$, $J_{mean} = 0$.

$$\text{Estimated Duration} = \left\lceil \frac{N_{valid}}{S_{batch}} \right\rceil \times (D_{delay} + J_{mean}) = \left\lceil \frac{N_{valid}}{S_{batch}} \right\rceil \times D_{delay}$$

---

## 5. DB Migration Plan

We will create migration `016_create_campaigns.sql` in `internal/platform/postgres/migrations/`.

```sql
-- +goose Up

-- 1. Create campaigns table
CREATE TABLE campaigns (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('draft', 'scheduled', 'sending', 'completed', 'cancelled')),
    scheduled_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Index for searching campaigns by workspace and status
CREATE INDEX idx_campaigns_workspace_status ON campaigns(workspace_id, status);
-- Partial index for active scheduled tasks
CREATE INDEX idx_campaigns_scheduled_at ON campaigns(scheduled_at) WHERE status = 'scheduled';

-- 2. Enrich message_dispatches table
ALTER TABLE message_dispatches
    ADD COLUMN campaign_id UUID REFERENCES campaigns(id) ON DELETE SET NULL,
    ADD COLUMN template_name VARCHAR(100),
    ADD COLUMN variables_json JSONB;

-- 3. Create composite partial index for high-performance campaign audit logs
CREATE INDEX idx_message_dispatches_campaign ON message_dispatches(workspace_id, campaign_id) 
    WHERE campaign_id IS NOT NULL;


-- +goose Down
DROP INDEX IF EXISTS idx_message_dispatches_campaign;

ALTER TABLE message_dispatches
    DROP COLUMN IF EXISTS campaign_id,
    DROP COLUMN IF EXISTS template_name,
    DROP COLUMN IF EXISTS variables_json;

DROP INDEX IF EXISTS idx_campaigns_scheduled_at;
DROP INDEX IF EXISTS idx_campaigns_workspace_status;
DROP TABLE IF EXISTS campaigns;
```

---

## 6. Enriched Logs Architecture & Indexing

### Option A (Enriched Outbound Logs) vs Option B (Separate Logs Table)
We selected **Option A**. The reasons are:
1. **Simplified State Machine**: Outbound routing, fallback loops, and real-time webhook status deliveries (e.g. `delivered`, `read`, `failed`) are already fully implemented for `message_dispatches`. Re-implementing this workflow for a separate campaigns log table would violate DRY principles and double our maintenance footprint.
2. **Unified Auditing**: Operators can query one single place to trace all outbound traffic, whether transactional or campaign-related.

### Composite Partial Index
Since the `message_dispatches` table is high-volume, regular queries would scan millions of rows. We mitigate this using a partial composite index:
```sql
CREATE INDEX idx_message_dispatches_campaign ON message_dispatches(workspace_id, campaign_id) 
    WHERE campaign_id IS NOT NULL;
```
Because transactional sends (which constitute the majority of volume) have a `NULL` `campaign_id`, they are completely omitted from this index. This keeps the index extremely compact, maintaining memory-resident lookups for dashboard analytics queries.

### Variables JSON Format
Variables are stored as a flat JSONB object mapping placeholder names directly to strings:
```json
{
  "nome": "João",
  "cidade": "São Paulo",
  "pedido_id": "99482"
}
```
This is queryable in PostgreSQL using jsonb operators: `variables_json->>'nome'`.

### Idempotency via Trace-ID Formatting
Trace IDs are critical to guarantee exactly-once delivery. For campaigns, they are generated using:
`campaign_${campaign_id}_${recipient}` (e.g., `campaign_893c5d6e_5511999998888`).
Because `trace_id` is defined as `UNIQUE` on `message_dispatches`:
- If a worker crashes mid-batch and NATS redeliveries trigger, the SQL upsert `ON CONFLICT (trace_id) DO UPDATE` will match the trace, preventing duplicate sends to the recipient.
- This represents an incredibly robust distributed idempotency boundary.

---

## 7. Validation Architecture

To ensure correctness and prevent regression, the following testing matrix is proposed:

```
+--------------------------------------------------------------+
|                    Validation Pipeline                       |
+--------------------------------------------------------------+
                               |
       +-----------------------+-----------------------+
       |                       |                       |
       v                       v                       v
+--------------+       +---------------+       +---------------+
|  Unit Tests  |       |  Integration  |       |    UI / E2E   |
+--------------+       +---------------+       +---------------+
 - Sniffing     - Migration tests       - Drag-n-drop file
 - Phone Validation     - JetStream workers     - Variable check
 - Regex Mapper         - Cancellation check    - Live cancel
 - Duration Calc        - Trace constraints     - Download error
```

### 1. Unit Tests
- **Delimiter Detection**: Write tests presenting strings separated by `,`, `;`, and `\t`, verifying that the correct delimiter is detected and that fallback defaults to a comma.
- **Phone Sanitizer**: Write test cases for:
  - Valid numbers: `+55 (11) 99999-8888` -> `5511999998888` (length 13, valid).
  - Short numbers: `99999-8888` -> `999998888` (length 9, invalid).
  - Long numbers: `5511999998888777` (length 16, invalid).
  - Alphabetic noise: `551199abc998888` -> `551199998888` (valid).
- **Interpolation Resolver**: Verify that:
  - Correct values replace matching variables (e.g. `{{nome}}` -> `João`).
  - Regex mapping is case-insensitive.
  - Plain text fields map variables sequentially if index-based keys are present.
  - Missing keys remain unreplaced or handle graceful fallback.
- **Estimated Duration**: Verify that the duration calculation function correctly computes batch counts and handles division remainders safely.

### 2. Integration Tests
- **Database Schema Validation**: Verify Goose migrations execute successfully and structural constraints (foreign keys, check constraints) behave as expected.
- **NATS Consumer Constraints**: Set up a test suite using `testcontainers-go/modules/nats` to launch a transient NATS instance:
  - Enqueue 3 batch messages.
  - Pull messages using `MaxAckPending: 1`.
  - Assert that Batch 2 is not delivered until Batch 1 has been explicitly ACKed.
- **Cancellation Flow**: 
  - Schedule a campaign.
  - While worker is processing batches, update database status to `cancelled`.
  - Verify that the worker immediately skips processing subsequent batches, returns them with an ACK, and halts operations.
- **Idempotency & Enriched Logs**:
  - Insert message dispatch using the prefix format `campaign_${campaign_id}_${recipient}`.
  - Attempt to insert it again; verify that the insert results in a conflict error or is handled idempotently.
  - Assert that `variables_json`, `campaign_id`, and `template_name` fields are written correctly to the DB and can be queried.

### 3. UI/E2E Verification Checklist
- [ ] Uploading a CSV triggers delimiter detection and updates summary cards immediately.
- [ ] Clicking "Download skipped rows" retrieves the rejected records CSV.
- [ ] Variable input slots dynamically highlight detected columns in real time.
- [ ] Estimated duration display is refreshed immediately when batch size or delay input fields are changed.
- [ ] Clicking "Cancel Campaign" updates the dashboard status badge using HTMX without reloading the entire page.
