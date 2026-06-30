---
name: sketch-findings-pergo
description: Validated design decisions, CSS patterns, and visual direction from sketch experiments. Auto-loaded during UI implementation on PerGo.
---

<context>
## Project: PerGo

A lightweight, clean, and essentially monochromatic Notion-inspired user interface for developers/makers and operators. Focuses on clarity, high typography readability (Inter), and functional colors (green/blue for status/events) over decorative elements. Includes dynamic layout switching to guide first-time users before presenting complex dashboards.

Reference points: Notion, MessageBird (Bird).

Sketch sessions wrapped: 2026-06-29
</context>

<design_direction>
## Overall Direction

- **Palette:** Grayscale/Monochromatic (whites, zinc grays, black primaries). Colors are reserved strictly for functional states (success, errors, updates).
- **Typography:** Sans-serif (Inter/system-ui) with clean weights and compact sizes. Monospace (SFMono-Regular/JetBrains Mono) for keys and payloads.
- **Layout:** Standard left navigation sidebar (Notion-style, collapsible) combined with a flexible top header and a centered fluid content panel.
- **State Logic:** Dynamic dashboard layout switching. Renders an interactive 4-step quickstart checklist on first-login, and transitions to operational metrics once the workspace has configured connections and API keys.
</design_direction>

<findings_index>
## Design Areas

| Area | Reference | Key Decision |
|------|-----------|--------------|
| Admin Dashboard UI & UX | [admin-dashboard-ui.md](file:///.agents/skills/sketch-findings-pergo/references/admin-dashboard-ui.md) | Collapsible Notion-style left sidebar, dynamic onboarding wizard, and monochromatic daisyUI styling. |

## Theme

The winning theme file is at [sources/themes/default.css](file:///.agents/skills/sketch-findings-pergo/sources/themes/default.css).

## Source Files

Original sketch HTML files are preserved in `sources/` for complete reference:
- [sources/001-dashboard-layout/](file:///.agents/skills/sketch-findings-pergo/sources/001-dashboard-layout/)
- [sources/002-onboarding-vs-operational/](file:///.agents/skills/sketch-findings-pergo/sources/002-onboarding-vs-operational/)
</findings_index>

<metadata>
## Processed Sketches

- 001-dashboard-layout
- 002-onboarding-vs-operational
</metadata>
