# Phase 29: Connection Slugs & API Channel Routing - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-07-25
**Phase:** 29-connection-slugs-and-api-channel-routing
**Areas discussed:** API Payload Routing, Slug Generation Format, Database Migration, Ingestion Cache

---

## 1. API Payload Routing (`POST /api/v1/messages`)

| Option | Description | Selected |
|--------|-------------|----------|
| Option A | `channel` field accepts both connection slug and legacy channel type with fallback | ✓ |
| Option B | Introduce a separate `connection` field in JSON request payload | |

**User's choice:** Option A
**Notes:** Allows `"channel": "vendas-sp"` directly while maintaining full backward compatibility for existing integrations.

---

## 2. Slug Auto-Generation Format

| Option | Description | Selected |
|--------|-------------|----------|
| Option A | Generated from connection name (e.g. `vendas-sp`), with numeric suffix on collision | ✓ |
| Option B | Generated from channel type + random hash (e.g. `waba-a1b2c3`) | |

**User's choice:** Option A
**Notes:** Maximizes readability for human operators and developers.

---

## 3. Database Migration Strategy

| Option | Description | Selected |
|--------|-------------|----------|
| Option A | Populate `slug` column for existing connections by sanitizing name + channel | ✓ |
| Option B | Leave legacy connections NULL (not selected) | |

**User's choice:** Option A
**Notes:** Ensures all connections have a valid non-NULL slug immediately.

---

## 4. Ingestion Cache

| Option | Description | Selected |
|--------|-------------|----------|
| Option A | Maintain in-memory map `map[string]*Connection` indexed by `workspace_id:slug` | ✓ |
| Option B | Query PostgreSQL index on every request | |

**User's choice:** Option A
**Notes:** Guarantees sub-millisecond route resolution under high ingestion load.

---

## Agent Discretion

None — all core design choices explicitly confirmed by user.

## Deferred Ideas

None — discussion stayed within phase scope.
