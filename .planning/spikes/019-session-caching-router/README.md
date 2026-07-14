---
spike: 019
name: session-caching-router
type: standard
validates: "Given multiple active WhatsMeow connections, when a message is sent, then the gateway resolves and routes the request using an in-memory session cache instead of querying the database on every dispatch."
verdict: VALIDATED
related: [002, 003]
tags: [api, cache, concurrency]
---

# Spike 019: Session Caching Router

## What This Validates
This spike validates the performance and scalability of an in-memory, thread-safe active session cache. It verifies that we can bypass database reads for active connection sessions on the hot path (POST /messages dispatch), falling back to SQL queries only on cache misses.

## Research
### Competing Approaches

| Approach | Tool/Library | Pros | Cons | Status |
|----------|-------------|------|------|--------|
| Direct SQL Queries | pgxpool.Pool | Clean, transaction-safe, no sync issues. | 500+ messages/sec creates excessive database connection reads and thread contention. | Rejected |
| Distributed Cache Daemon | Redis | Multi-node safe, TTL eviction. | Adds operational infrastructure complexity. | Rejected (deferred) |
| Local In-Memory Cache | Mutex-protected Map | In-memory lookup, zero latency, thread-safe. | Invalidation required on connection deletion or status change. | Chosen |

## How to Run
Run the unit tests verifying cache hits and concurrency:
```bash
go test .planning/spikes/019-session-caching-router/cache_test.go -v
```

## What to Expect
- The first lookup triggers the database fallback query and caches the resolved session object.
- Subsequent calls read directly from the in-memory cache, bypassing database lookups entirely.
- evictions (`Delete`) remove identity and connection mappings cleanly.
- RWMutex guards against race conditions in concurrent write/read workflows.

## Investigation Trail
- **Iteration 1**: Designed the `ActiveSession` state tracking struct.
- **Iteration 2**: Added the double map indexing (`byID` and `byIdentity`) for quick lookup in either format.
- **Iteration 3**: Validated concurrency locks and thread safety with concurrent workers.

## Results
- **Verdict**: **VALIDATED**
- **Evidence**: Verified that the session router hits the database exactly once on startup, serves subsequent requests with sub-millisecond local reads, and supports concurrent access without data races.
