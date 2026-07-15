---
phase: 13-deepen-media-engine-to-consolidate-storage-pipelines
plan: 01
subsystem: media
tags: [go, media, refactor]

# Dependency graph
requires: []
provides:
  - "Deep Media Engine interface ProcessInbound and ProcessOutbound"
affects: [inbound, outbound, cmd, api]

# Tech tracking
tech-stack:
  added: []
  patterns: [Deep Module consolidation pattern]

key-files:
  created: []
  modified:
    - internal/media/engine.go
    - internal/media/engine_test.go
    - internal/inbound/processor.go
    - internal/inbound/processor_test.go
    - internal/outbound/processor.go
    - internal/outbound/processor_test.go
    - cmd/pergo/main.go
    - internal/api/handler/telegram_webhook.go
    - internal/api/handler/telegram_webhook_test.go
    - internal/api/handler/waba_webhook.go
    - internal/api/handler/waba_webhook_test.go

key-decisions:
  - "Unified media download/upload/mimetype orchestration inside ProcessInbound and ProcessOutbound"

patterns-established:
  - "Deep module pattern for resource storage pipelines"

requirements-completed: []

coverage:
  - id: D1
    description: "Inbound media calculation and S3 upload consolidation"
    verification:
      - kind: unit
        ref: "internal/media/engine_test.go"
        status: pass
      - kind: unit
        ref: "internal/inbound/processor_test.go"
        status: pass
  - id: D2
    description: "Outbound media remote download and S3 upload consolidation"
    verification:
      - kind: unit
        ref: "internal/media/engine_test.go"
        status: pass
      - kind: unit
        ref: "internal/outbound/processor_test.go"
        status: pass

# Metrics
duration: 15min
completed: 2026-07-15
status: complete
---

# Phase 13 Summary

Consolidated and deepened the Media Engine. Download, validation, hashing, MIME detection, S3 key construction, and internal proxy URL formatting are now centralized in the Media Engine.

## Accomplishments
- Refactored `media.Engine` interface to expose `ProcessInbound` and `ProcessOutbound`.
- Refactored inbound and outbound processors to use `media.Engine` interface instead of raw storage uploader/downloader interfaces.
- Decoupled `inbound.InboundProcessor` and `outbound.Processor` from direct S3 layout logic.
- Verified compilation and test suite correctness project-wide.
