# 01 — Fix circuit breaker half-open state machine

**What to build:** The circuit breaker in `internal/platform/breaker` must correctly reset `consecutiveFailures` when transitioning from open → half-open. Currently, `RecordFailure` in half-open resets `openUntil` but does NOT reset `consecutiveFailures`, so the counter grows unboundedly across open→half-open→open cycles even though each cycle only had one actual probe failure. After this fix, the state machine matches the architecture doc: closed → open (at threshold) → half-open (after timeout) → one probe allowed → success closes, failure re-opens with a fresh failure count.

**Blocked by:** None — can start immediately.

**Status:** ready-for-agent

- [ ] `RecordFailure` resets `consecutiveFailures` appropriately when transitioning back to open from half-open (only 1 failure caused the re-open, counter reflects that)
- [ ] `RecordSuccess` in half-open correctly transitions to closed with zeroed counters (already works, verify)
- [ ] New test case in `breaker_test.go`: multi-cycle accumulation — run through 3+ open→half-open→open cycles and assert `consecutiveFailures` doesn't grow unboundedly
- [ ] All existing tests in `breaker_test.go` continue to pass
