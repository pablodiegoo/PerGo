## 2026-08-12T13:00:37Z
You are Worker M4 assigned to implement Requirement R5 (Fix double `%w` error wrap in Telegram adapter).
Your working directory is `/home/pablodiegoo/coding/PerGo/.agents/worker_m4`. Create your directory if needed and write all metadata files (progress.md, handoff.md) there.

Authoritative Request: Read `/home/pablodiegoo/coding/PerGo/.agents/ORIGINAL_REQUEST.md` and `/home/pablodiegoo/coding/PerGo/.agents/explorer_survey_1/handoff.md` first.

File Ownership: You exclusively own `internal/channel/telegram/telegram.go`, `internal/channel/telegram/telegram_challenge_test.go`, and `internal/channel/telegram/telegram_test.go`.

Tasks:
1. In `internal/channel/telegram/telegram.go` line 119:
   - Restructure `fmt.Errorf` in the S3 download path to wrap only `ErrTelegramMediaRetryable` with `%w` and format inner error with `%v`:
     `fmt.Errorf("%w: telegram media download from S3 failed: %v", ErrTelegramMediaRetryable, err)`
2. Update `internal/channel/telegram/telegram_challenge_test.go` and `internal/channel/telegram/telegram_test.go` to assert that `errors.Is(err, ErrTelegramMediaRetryable)` works correctly and `errors.Unwrap(err)` returns `ErrTelegramMediaRetryable`.
3. Run builds and tests (`go test -v ./internal/channel/telegram/...`).
4. Document commands and exact test outputs in `/home/pablodiegoo/coding/PerGo/.agents/worker_m4/handoff.md`.

MANDATORY INTEGRITY WARNING: DO NOT CHEAT. All implementations must be genuine. DO NOT hardcode test results, create dummy/facade implementations, or circumvent the intended task. A teamwork_preview_auditor will independently verify your work. Integrity violations WILL be detected and your work WILL be rejected.
