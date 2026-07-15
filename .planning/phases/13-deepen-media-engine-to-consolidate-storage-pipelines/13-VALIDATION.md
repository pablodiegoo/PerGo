# Validation Strategy: Phase 13 - Deepen Media Engine

This document defines the validation gates, verification commands, and must-have scenarios for verifying the Media Engine refactoring.

## Automated Verification Gates

We will run the full Go test suite to ensure all unit tests pass:
```bash
go test -v ./internal/media/...
go test -v ./internal/inbound/...
go test -v ./internal/outbound/...
go test -v ./internal/api/handler/...
go test ./...
```

## Must-Have Scenarios (Automated & Manual)

| Must-Have Scenario | Verification Code / Command | Expected Behavior |
|--------------------|----------------------------|-------------------|
| Media Engine Unit Tests | `go test -v ./internal/media/...` | Passes, verifying `ProcessInbound` and `ProcessOutbound` execute downloads, hashes, size checks, and S3 uploads correctly. |
| Inbound Media Processing | `go test -v ./internal/inbound/...` | Passes, verifying inbound processor utilizes `media.Engine` mock to store and translate media properties. |
| Outbound Media Ingestion | `go test -v ./internal/outbound/...` | Passes, verifying outbound processor maps download failures and size limits correctly, returning the internal media proxy URL. |
| API Webhook Integration | `go test -v ./internal/api/handler/...` | Passes, verifying Telegram and WABA webhook handlers utilize the refactored media engine without compilation issues. |
