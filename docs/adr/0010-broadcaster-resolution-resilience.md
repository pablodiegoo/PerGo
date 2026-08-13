# Broadcaster Engine Resolution Resilience & Edge Cases

We have defined the resilience and edge-case behavior for the dynamic execution-time campaign resolution (introduced in ADR-0001). 

Specifically, we decided to:
1. Handle worker crashes during resolution by relying on `ON CONFLICT DO NOTHING` when inserting into `campaign_recipients`, and using JetStream's native `Nats-Msg-Id` header to deduplicate batch publishes. This avoids complex Outbox patterns while guaranteeing exactly-once semantics downstream.
2. Record contacts lacking a valid channel identity as `skipped` in the database and generate discrete `campaign.dispatch.skipped` audit logs for each, rather than silently ignoring them. They are deliberately excluded from NATS batch payloads to conserve bandwidth.
3. Prioritize the Database (Tag) contact over a static CSV contact during deduplication conflicts.

**Why:** The `skipped` audit logs are critical for compliance, as the system targets public institutions and electoral campaigns requiring rigorous traceability. Using NATS deduplication combined with database `ON CONFLICT` offers the best balance of robust fault-tolerance without over-engineering the persistence layer.
