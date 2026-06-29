# Spike Wrap-Up Summary

**Date:** 2026-06-29
**Spikes processed:** 3
**Feature areas:** multi-instance-routing
**Skill output:** `./.agents/skills/spike-findings-pergo/`

## Processed Spikes
| # | Name | Type | Verdict | Feature Area |
|---|------|------|---------|--------------|
| 001 | multi-instance-schema | standard | VALIDATED | db, schema |
| 002 | api-routing-payload | standard | VALIDATED | api, routing |
| 003 | dynamic-adapter-registry | standard | VALIDATED | concurrency, registry |

## Key Findings
- **Consolidated Connections Schema:** We can safely merge the single-instance `channel_credentials` and `devices` tables into a unified `connections` table, mapping all active channels (Telegram bots, WABAs, whatsmeow Web sessions) under a single structure.
- **API `from` Field Routing:** Requests can now specify `from` representing a sender identity (phone number or bot username). If empty, the workspace's default connection of the requested type is used.
- **Static Adapters with Dynamic Routing (High Concurrency Safety):** By routing credentials on-the-fly inside the single global adapter implementations (by connection ID or JID), we completely avoid instantiating/destroying adapters at runtime, eliminating potential memory leaks and concurrency synchronization bugs.
- **Whatsmeow Multi-Device Compatibility:** The `whatsmeow` library's underlying PostgreSQL storage handles multiple devices out-of-the-box using the JID string as primary key. We just need to load devices using `container.GetDevice(jid)` instead of `container.GetFirstDevice()`.
