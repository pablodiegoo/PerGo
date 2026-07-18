---
last_mapped_commit: 99448836f14aa64e923a366b95721858185d878b
last_mapped_date: 2026-07-18
---

# Technical Concerns Map

This document outlines key technical debt, security, performance, reliability, and operational risks identified in the PerGo codebase.

## 1. Unofficial WhatsApp Web Client Risks (whatsmeow)

- **Reliability Concern: Unofficial Web Protocol**  
  *Severity: High*  
  *Description*: whatsmeow emulates a WhatsApp Web multi-device connection. WhatsApp routinely updates its internal Web WebSocket protocols and message structures. A sudden protocol change can break whatsmeow connection handlers, requiring immediate library updates.
- **Account Suspension Risk (Soft/Hard Ban)**  
  *Severity: High*  
  *Description*: Automation of outbound messages on WhatsApp Web connections violates WhatsApp's Terms of Service. Unnatural dispatch intervals trigger Meta's automated spam-detection algorithms.
  *Mitigation*: Outbound workers enforce a random staggered dispatch delay of 1-3 seconds between messages on unofficial channels to simulate human typing cadence.

## 2. Security Concerns

- **Key Sovereignty & Secret Handoffs**  
  *Severity: Medium*  
  *Description*: WhatsApp session credentials (noise, identity, and pre-keys) are written to the database. If the database is compromised, an attacker can hijack active WhatsApp sessions.
  *Mitigation*: Credentials are encrypted at rest using AES-256-GCM before writing to the `connections` table, and whatsmeow credentials in `whatsmeow_device` are similarly encrypted using custom repository hooks.
- **API Key Storage**  
  *Severity: Medium*  
  *Description*: API keys used by integrations must be stored securely. Keys are hashed using SHA-256 in the `api_keys` table.

## 3. Performance & Scaling Risks

- **Real-Time HTMX Polling Overhead**  
  *Severity: Medium*  
  *Description*: The operator dashboard employs continuous 3-second polling for active chats and 5-second polling for conversation lists. At scale (dozens of concurrent operators), this creates a continuous stream of SQL read requests.
  *Mitigation*: Message queries utilize a strict indexed UUID cursor (`after` parameter) to query only delta entries. 
- **Audit Logs Query Degradation**  
  *Severity: Medium*  
  *Description*: The conversation list dynamically groups audit logs by `(payload->>'from', payload->>'channel', payload->>'to')`. Grouping queries on JSONB columns are CPU-intensive.
  *Mitigation*: Compound indexes are created on `(workspace_id, (payload->>'channel'), (payload->>'from'), (payload->>'to'))` to optimize index scans.

## 4. Operational & Database Risks

- **Audit Log Partitioning Management**  
  *Severity: High*  
  *Description*: The `audit_logs` table is partitioned by date. If future date partitions (e.g. for the next month) are not pre-created automatically, inserts for those dates will fail.
  *Mitigation*: PerGo implements an automatic partitioning scheduler or pre-seeding migration step to preemptively create partition tables.
- **Single-Host Self-Hosted Topology**  
  *Severity: Low*  
  *Description*: Designed for simple self-hosted single-node deployments. If the hosting node's memory is exhausted or services crash, messages will back up in the queue, but they will be persistent in NATS JetStream once NATS is back online.
