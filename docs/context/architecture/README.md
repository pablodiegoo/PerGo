# PerGo — Architectural Implementation Plan (Golang)

This directory contains the architectural design derived from
`docs/PRD PerGo.md`. It is written from the perspective of a senior
distributed-systems engineer reviewing the PRD during a design-doc review.

The plan favours the Go standard library, idiomatic concurrency, and the
minimum viable complexity required to satisfy the non-functional targets
(>=500 req/s, <=50ms p99 ingestion, <512MB RAM, 99.5% delivery, 100%
trace-correlated audit).

## Sections

| # | File | Topic |
|---|------|-------|
| 1 | [01-architectural-summary.md](01-architectural-summary.md) | PRD analysis & distributed-systems challenges |
| 2 | [02-technical-decisions.md](02-technical-decisions.md) | Libraries, transport, persistence |
| 3 | [03-directory-structure.md](03-directory-structure.md) | Domain-oriented package layout |
| 4 | [04-concurrency-performance.md](04-concurrency-performance.md) | Goroutines, channels, worker pools |
| 5 | [05-resilience-error-handling.md](05-resilience-error-handling.md) | Timeouts, retries, circuit breakers, error wrapping |
| 6 | [06-core-code-example.md](06-core-code-example.md) | Core snippets (ingest, routing, audit, session worker) |

## Guiding principles

1. **No premature abstraction.** Interfaces are introduced only where
   multiple real implementations already exist (channel adapters,
   audit sinks). Small, consumer-side interfaces.
2. **No magic DI.** Manual constructor injection: `func NewX(deps...) *X`.
3. **No Java-fication.** Plain exported struct fields where encapsulation
   adds no value; no getter/setter ceremony; no deep type hierarchies.
4. **No over-engineering.** A `sync.RWMutex` map or a buffered channel
   beats a distributed event bus when the problem is local.
5. **Idiomatic Go only.** `gofmt`, `golangci-lint`, errors returned last,
   short names, `context.Context` as first argument.
