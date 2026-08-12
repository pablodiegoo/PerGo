# Handoff Report — Codebase Survey for Requirements R1 and R5

## 1. Observation

### Requirement R1: Circuit Breaker Half-Open State Machine
- **Files Inspected**:
  - `internal/platform/breaker/breaker.go` (lines 1-88)
  - `internal/platform/breaker/breaker_test.go` (lines 1-88)
  - `.scratch/code-review-fixes/issues/01-fix-circuit-breaker-half-open-state.md`
- **Verbatim Code (`internal/platform/breaker/breaker.go:72-87`)**:
  ```go
  func (cb *CircuitBreaker) RecordFailure(endpoint string) {
  	cb.mu.Lock()
  	defer cb.mu.Unlock()

  	ep, ok := cb.endpoints[endpoint]
  	if !ok {
  		ep = &EndpointBreaker{}
  		cb.endpoints[endpoint] = ep
  	}

  	ep.consecutiveFailures++
  	if ep.state == StateHalfOpen || ep.consecutiveFailures >= cb.maxFailures {
  		ep.state = StateOpen
  		ep.openUntil = time.Now().Add(cb.resetTimeout)
  	}
  }
  ```
- **Verbatim Code (`internal/platform/breaker/breaker.go:44-59`)**:
  ```go
  func (cb *CircuitBreaker) Allow(endpoint string) error {
  	cb.mu.Lock()
  	defer cb.mu.Unlock()

  	ep, ok := cb.endpoints[endpoint]
  	if !ok || ep.state == StateClosed {
  		return nil
  	}

  	if ep.state == StateHalfOpen || time.Now().Before(ep.openUntil) {
  		return ErrCircuitOpen
  	}

  	ep.state = StateHalfOpen
  	return nil
  }
  ```
- **Observed Behavior**:
  - When `ep.state` transitions from `StateOpen` to `StateHalfOpen` inside `Allow()` (line 57), `ep.consecutiveFailures` is NOT modified (retaining its value from when it reached `cb.maxFailures`).
  - When a probe fails in `StateHalfOpen`, `RecordFailure` is invoked. Line 82 (`ep.consecutiveFailures++`) executes first, incrementing `consecutiveFailures` (e.g. from 5 to 6).
  - `ep.state` is then set to `StateOpen` (line 84), but `ep.consecutiveFailures` remains at 6.
  - On subsequent open → half-open → open probe failure cycles, `ep.consecutiveFailures` increments to 7, 8, 9, etc., growing unboundedly across cycles even though each cycle only experienced a single probe failure.
- **`RecordSuccess` Behavior (`internal/platform/breaker/breaker.go:61-70`)**:
  - Sets `ep.state = StateClosed`, `ep.consecutiveFailures = 0`, `ep.openUntil = time.Time{}`. This correctly resets all counters upon success.

### Requirement R5: Telegram Adapter Error Wrapping
- **Files Inspected**:
  - `internal/channel/telegram/telegram.go` (line 119)
  - `internal/channel/telegram/telegram_challenge_test.go` (lines 10-28)
  - `internal/channel/telegram/telegram_test.go` (lines 386-412)
  - `.scratch/code-review-fixes/issues/05-fix-double-error-wrap-telegram.md`
- **Verbatim Code (`internal/channel/telegram/telegram.go:118-120`)**:
  ```go
  bodyRC, _, err := a.s3Client.Download(ctx, key)
  if err != nil {
  	return "", fmt.Errorf("%w: telegram media download from S3 failed: %w", ErrTelegramMediaRetryable, err)
  }
  ```
- **Verbatim Code (`internal/channel/telegram/telegram_challenge_test.go:10-28`)**:
  ```go
  s3Err := errors.New("s3 connection reset by peer")
  err := fmt.Errorf("%w: telegram media download from S3 failed: %w", ErrTelegramMediaRetryable, s3Err)
  ...
  singleUnwrap := errors.Unwrap(err)
  t.Logf("errors.Unwrap(err) returned: %v", singleUnwrap)
  ```
- **Execution Tool Command & Output**:
  - Command: `/home/pablodiegoo/.local/go/bin/go test -v ./internal/channel/telegram/telegram_challenge_test.go ./internal/channel/telegram/telegram.go ./internal/channel/telegram/inbound.go`
  - Output snippet: `telegram_challenge_test.go:25: errors.Unwrap(err) returned: <nil>`
- **Observed Behavior**:
  - The format string `"%w: telegram media download from S3 failed: %w"` uses two `%w` verbs.
  - In Go 1.20+, multi-`%w` creates a joined multi-error. Standard single `errors.Unwrap(err)` returns `nil` for multi-wrapped errors.
  - The project architecture requires single `%w` wrapping per layer to maintain standard Go error unwrapping semantics.

---

## 2. Logic Chain

### Logic Chain for R1 (Circuit Breaker State Machine Fix):
1. **Observation**: `RecordFailure` increments `ep.consecutiveFailures` on line 82 before checking `ep.state == StateHalfOpen`.
2. **Observation**: When state is `StateHalfOpen`, the failure is a single probe failure. Transitioning back to `StateOpen` should reset `ep.consecutiveFailures` to the threshold `cb.maxFailures` (or set `ep.consecutiveFailures = cb.maxFailures`).
3. **Deduction**: Because line 82 increments `ep.consecutiveFailures` without resetting it when `ep.state == StateHalfOpen`, repeated cycles cause `ep.consecutiveFailures` to accumulate endlessly (`maxFailures + 1`, `maxFailures + 2`, etc.).
4. **Resolution**:
   - In `RecordFailure`:
     Check if `ep.state == StateHalfOpen`. If so, transition to `StateOpen`, set `ep.openUntil = time.Now().Add(cb.resetTimeout)`, and reset `ep.consecutiveFailures = cb.maxFailures`.
   - In `breaker.go` (or `EndpointBreaker` struct):
     Provide an exported method `ConsecutiveFailures(endpoint string) int` on `CircuitBreaker` (or helper) so tests and callers can query the counter state.
   - In `breaker_test.go`:
     Add a unit test `TestCircuitBreaker_MultiCycleAccumulation` simulating 3+ open → half-open → open cycles with probe failures. Assert that `ConsecutiveFailures(endpoint)` equals `maxFailures` after every re-open, proving it does not grow unboundedly.

### Logic Chain for R5 (Telegram Adapter Double `%w` Fix):
1. **Observation**: `fmt.Errorf("%w: telegram media download from S3 failed: %w", ErrTelegramMediaRetryable, err)` wraps two errors using `%w`.
2. **Observation**: Empirical test log proves `errors.Unwrap(err)` returns `<nil>` for multi-`%w` wrapped errors.
3. **Deduction**: The outer sentinel error `ErrTelegramMediaRetryable` must be wrapped with `%w` so that `errors.Is(err, ErrTelegramMediaRetryable)` succeeds and `errors.Unwrap(err)` returns `ErrTelegramMediaRetryable`. The inner S3 `err` detail must be formatted using `%v` instead of `%w`.
4. **Resolution**:
   - In `internal/channel/telegram/telegram.go:119`:
     Change:
     `return "", fmt.Errorf("%w: telegram media download from S3 failed: %w", ErrTelegramMediaRetryable, err)`
     To:
     `return "", fmt.Errorf("%w: telegram media download from S3 failed: %v", ErrTelegramMediaRetryable, err)`
   - In `internal/channel/telegram/telegram_challenge_test.go`:
     Update test error construction to match the single `%w` format:
     `err := fmt.Errorf("%w: telegram media download from S3 failed: %v", ErrTelegramMediaRetryable, s3Err)`
     Update assertion: `errors.Is(err, ErrTelegramMediaRetryable)` must be `true`, `errors.Unwrap(err)` returns `ErrTelegramMediaRetryable`, and `errors.Is(err, s3Err)` returns `false` (with message containing `s3Err.Error()`).
   - In `internal/channel/telegram/telegram_test.go`:
     Confirm existing `Telegram S3 Media Download Failure Error Wrapping` test passes (`errors.Is(err, ErrTelegramMediaRetryable)`).

---

## 3. Caveats

- **No Caveats**: The issues, current codebase behaviors, root causes, exact code fixes, and test strategies for both R1 and R5 have been fully investigated and verified.

---

## 4. Conclusion

1. **R1 Assessment & Plan**:
   - Root Cause: `RecordFailure` in `internal/platform/breaker/breaker.go` increments `consecutiveFailures` without resetting it when returning from `StateHalfOpen` to `StateOpen`.
   - Proposed Fix:
     - Update `RecordFailure`:
       ```go
       if ep.state == StateHalfOpen {
           ep.state = StateOpen
           ep.consecutiveFailures = cb.maxFailures
           ep.openUntil = time.Now().Add(cb.resetTimeout)
           return
       }
       ```
     - Add getter method `ConsecutiveFailures(endpoint string) int` to `CircuitBreaker`.
     - Add test `TestCircuitBreaker_MultiCycleAccumulation` in `breaker_test.go` asserting `ConsecutiveFailures` remains capped at `maxFailures` over 3+ open→half-open→open cycles.

2. **R5 Assessment & Plan**:
   - Root Cause: Line 119 of `internal/channel/telegram/telegram.go` uses two `%w` verbs in `fmt.Errorf`, violating the single wrap per layer design rule and breaking `errors.Unwrap()`.
   - Proposed Fix:
     - Replace `%w` for inner S3 error with `%v`:
       `fmt.Errorf("%w: telegram media download from S3 failed: %v", ErrTelegramMediaRetryable, err)`
     - Update `telegram_challenge_test.go` and ensure `telegram_test.go` passes.

---

## 5. Verification Method

### Step 1: Run Circuit Breaker Tests
Execute:
```bash
/home/pablodiegoo/.local/go/bin/go test -v ./internal/platform/breaker/...
```
- **Success Condition**: All tests pass, including the new multi-cycle accumulation test.
- **Invalidation Condition**: Failure of any test or `consecutiveFailures` exceeding `maxFailures` after re-open.

### Step 2: Run Telegram Channel Tests
Execute:
```bash
/home/pablodiegoo/.local/go/bin/go test -v ./internal/channel/telegram/...
```
- **Success Condition**: All telegram unit tests pass, `errors.Is(err, ErrTelegramMediaRetryable)` evaluates to `true`, and single `errors.Unwrap(err)` returns `ErrTelegramMediaRetryable` without returning `nil`.
