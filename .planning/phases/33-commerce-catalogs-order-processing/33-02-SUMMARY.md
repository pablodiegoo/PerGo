---
phase: 33
plan_id: "33-02"
title: "Ingestion Handler & Pre-flight Validation"
subsystem: "outbound & api handler"
tags:
  - "commerce"
  - "catalog"
  - "validation"
  - "pre-flight"
key-files:
  - "internal/outbound/processor.go"
  - "internal/outbound/errors.go"
  - "internal/api/handler/message.go"
  - "internal/outbound/processor_test.go"
  - "internal/api/handler/message_test.go"
---

# Plan 33-02 Summary

## Executive Summary
Plan 33-02 implemented synchronous pre-flight catalog and SKU validation for product messages during `POST /messages` ingestion. Catalog ID resolution follows strict precedence (request payload > connection default > HTTP 422 `missing_catalog_id`), rejecting missing catalog IDs or invalid product payload bounds prior to NATS JetStream queuing.

## Task Summary Table

| Task ID | Task Description | Commit Hash | Key Files / Changes |
|---------|------------------|-------------|---------------------|
| T1 | Catalog ID Resolution & Product Validation in Outbound Processor | `912e842` | `internal/outbound/processor.go`, `internal/outbound/errors.go`, `internal/outbound/processor_test.go` |
| T2 & T3 | Map Ingestion Errors to HTTP 422 Responses & Handler Integration Tests | `9c6e90e` | `internal/api/handler/message.go`, `internal/api/handler/message_test.go` |

## Verification Results
- `go test -v -race ./internal/outbound/... ./internal/api/handler/...`: PASSED cleanly.
- Structural bounds checks reject invalid section titles (>24 runes) and item bounds (>30 total items) synchronously before queue publication.
- Missing catalog IDs return HTTP 422 with `code: missing_catalog_id`.
- Product bounds violations return HTTP 422 with `code: invalid_product_payload` and field-level details.

## Deviations
None.

## Self-Check: PASSED
