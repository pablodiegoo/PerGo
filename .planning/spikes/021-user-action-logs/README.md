---
spike: 21
name: user-action-logs
type: standard
validates: "Given requests to PerGo from API keys or dashboard users, when recorded in a unified action log table, then we can track actors, actions, metadata, sources, and access times in the UI."
verdict: VALIDATED
related: []
tags: [db, schema, logs, audit]
---

# Spike 021: User & API Action Logs

## What This Validates
This spike validates the schema, capture pattern, and user interface for recording developer and operator actions in PerGo. It addresses:
- **Polymorphic Actor Schema**: Supporting both API Keys and future multi-user accounts under a unified audit trail table.
- **Log Source Classification**: Grouping and highlighting requests based on their origin (`api` vs `dashboard`).
- **Enriched Access Metadata**: Capturing timestamps, IP addresses, User-Agents, and action-specific payloads (JSONB).
- **UI Presentation**: Rendering a high-fidelity logs dashboard tab featuring badge indicators and interactive JSON metadata inspector modals.

## How to Run
To view the interactive UI prototype for this spike, open the `index.html` file in your web browser:

```bash
# On Linux/macOS
xdg-open .planning/spikes/021-user-action-logs/index.html
```

To run the unit tests verifying the schema and query performance, run:
```bash
go test -v .planning/spikes/021-user-action-logs/logs_test.go
```

## What to Expect
- **Unified Log Dashboard**: A modern, responsive user log terminal styled with Tailwind CSS and DaisyUI.
- **Dynamic Filter Tabs**: Filtering logs by actor type (`All`, `Console Users`, `API Keys`) or source (`All`, `Console`, `API`).
- **Action-Specific Payloads**: Clicking "View Details" on any log row opens a modal displaying the exact raw JSON metadata recorded (e.g., parameters sent to `/api/v1/messages` or configurations modified in settings).
- **Rich Metadata**: Informative tooltips and indicators showing exact IP addresses and User-Agent details.

## Investigation Trail
- **Polymorphic Actors**: Since PerGo is currently a single-operator deployment using a shared admin password, there is no `users` table. However, we must design the schema to support a future multi-user environment. By using `actor_type` (`'user'`, `'api_key'`, `'system'`) and a text `actor_id` (storing the user's email or the API key's UUID), we keep the table clean while ensuring seamless integration of a future `users` table.
- **API vs Dashboard segregation**: The user proposed: *"não sei se o ideal já é englobar tudo no mesmo chapéu ou se dividir solicitações feitas por api de solicitações feitas no dashboard... acho que manter todas juntas e só sinalizar no registro as diferenças de ambas"* (I don't know if the ideal is to group everything under the same hat or split requests made by API from requests made in the dashboard... I think keeping them all together and just signaling the differences between both is best). We validated this by keeping them in a single table with a `source` column (`'api'` or `'dashboard'`), which simplifies logging operations and unified queries, while using UI badges to separate them visually.

## Results
- **Schema Selection**: Validated a robust `user_action_logs` schema that uses `JSONB` for payload metadata.
- **Index Optimization**: Identified that `(workspace_id, created_at DESC)` is the optimal index strategy to serve administrative log views with sub-millisecond query latency.
- **Audit Isolation**: Transactional message logs (already in `audit_logs`) will remain separate from administrative action logs to prevent high-throughput messaging from polluting operator activity audits.
