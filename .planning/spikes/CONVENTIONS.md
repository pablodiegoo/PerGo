# Spike Conventions

Patterns and stack choices established across spike sessions. New spikes follow these unless the question requires otherwise.

## Stack
- **Frontend Framework**: DaisyUI + Tailwind CSS (via CDN in dev/spikes)
- **Backend/HTTP**: Go 1.25+ with Echo v5
- **Persistence**: PostgreSQL via pgx/v5
- **Interactivity**: HTMX v2.x (CDN `htmx.org@2.0.10`) for single-page style swapping

## Structure
- Each spike lives under `.planning/spikes/NNN-descriptive-name/`
- Every spike includes:
  - `README.md` detailing Given/When/Then, trail, and results
  - Code scripts or interactive HTML files
- Verified configurations are archived to `.agents/skills/spike-findings-pergo/`

## Patterns
- **Layouts**: Left-side vertical navigation menu (zinc-900/slate-950) with main panel content container using left-margin.
- **Theming**: light theme as default (`data-theme="light"`).
- **Design Tokens**: Standardized card container margins (`p-6 shadow-sm border border-zinc-200 rounded-lg`), tables, labels, and badges.
- **Throttling**: Staggered dispatching (1-3s random delay) for WhatsApp Web instances.
- **Credential Migration**: When consolidating credential sources, always migrate handler dependencies from legacy repo to connections repo — never maintain two credential access paths simultaneously.
- **Gap Auditing**: Cross-reference context/ PRDs against spike MANIFEST → codebase files using parallel subagents for exhaustive coverage.
