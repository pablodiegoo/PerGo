# 05 — Fix double %w error wrap in Telegram adapter

**What to build:** The Telegram adapter's S3 media download error uses `fmt.Errorf("%w: ... %w", ErrTelegramMediaRetryable, err)` — two `%w` verbs in a single wrap. The architecture doc mandates "one wrap per layer". While Go 1.20+ supports multi-`%w` via `Unwrap() []error`, single `errors.Unwrap()` returns `nil` for these, which is fragile and semantically ambiguous. Restructure to wrap only the sentinel error with `%w` and format the inner S3 error with `%v`, or use a custom error type that properly chains both.

**Blocked by:** None — can start immediately.

**Status:** ready-for-agent

- [ ] The `fmt.Errorf` in the Telegram adapter's S3 download path uses a single `%w` for `ErrTelegramMediaRetryable`
- [ ] The inner S3 error detail is still included in the error message (via `%v` or nested wrap)
- [ ] `errors.Is(err, ErrTelegramMediaRetryable)` still works correctly
- [ ] Update `telegram_challenge_test.go` to reflect the new wrapping structure (remove assertion on `errors.Is(err, s3Err)` if no longer applicable, or adjust)
- [ ] `telegram_test.go` S3 media download failure test continues to pass
