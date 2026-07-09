---
quick_id: "260709-dhq"
slug: "fix-connections-decrypt-and-layout-tweak"
status: complete
completed: 2026-07-09
---

# Summary: Fix Connections Decrypt & Layout Tweaks

## Accomplishments
- **Decryption Resilience**: Modified `scanAndDecrypt` and `scanRowAndDecrypt` in `internal/repository/connection.go` to log decryption failures using `slog.Error` but continue loading the connection instead of failing the database scan. This keeps the "Conexões" page accessible even if some connections have stale or un-decryptable credentials.
- **DaisyUI Light Theme Enforcement**: Added `data-theme="light"` to `base.templ` html tags to override automatic dark mode selection, ensuring colors and widgets match our light design system.
- **Input Fields Styling**: Consolidated `.form-input` and `.form-group input/select` styles in `admin.css` to feature white backgrounds and zinc focus outline rings.
- **Minimalist Audit Filters Grid**: Refactored the audit search form in `audit.templ` into a responsive 5-column grid, aligning all filters and search/export buttons horizontally on desktop.
- **Robust Integration Tests**: Added pre-run cleanup of duplicate test records in `waba_test.go` to prevent unique constraint violations on dirty test databases.

## Commits
- Implementation changes: `dfe2baa`
- Planning and summary files: [Committing next]
