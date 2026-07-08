---
spike: 010
name: settings-nested-sidebar
type: standard
validates: "Given a left navigation sidebar, when the 'Configurações' option is clicked, then it toggles a sub-navigation section inline with sub-options (Logs, Conexões, Workspace, Webhooks, Telemetry), and when a settings page is active, the sub-navigation remains open/active."
verdict: VALIDATED
related: []
tags: [ui, sidebar, settings]
---

# Spike 010: Settings Nested Sidebar

## What This Validates
This spike validates the behavior, HTML/CSS structure, and active state memory of a collapsible nested navigation section for Configurações inside the PerGo left sidebar.

## Research
### Competing Approaches

| Approach | Tool/Library | Pros | Cons | Status |
|----------|-------------|------|------|--------|
| HTML `<details>` element | Native HTML | No JS required, simple markup. | Hard to animate height smoothly, limited style customization across browsers. | Rejected |
| Tailwind transition heights + JS | CSS transitions + short vanilla JS | Extremely customizable, smooth height expansion animation, robust control over active state. | Requires small JS snippet for toggle. | Chosen |

## How to Run
Open the prototype file `.planning/spikes/010-settings-nested-sidebar/index.html` in your web browser.

## What to Expect
- The sidebar displays exactly three top-level items: **Visão Geral**, **Inbox**, and **Configurações**.
- Clicking **Configurações** expands the submenu inline with the options: **Conexões**, **Logs**, **Workspace**, **Webhooks**, and **Telemetry**.
- Clicking any of these sub-options maintains the expanded state of **Configurações** and highlights the active sub-option.
- Clicking **Visão Geral** or **Inbox** collapses the configurations submenu.

## Investigation Trail
- **Iteration 1**: Created the basic layout using Tailwind CSS. Built a nested `<ul>` with a JS click handler.
- **Iteration 2**: Added dynamic height calculation transition using class manipulation (`max-h-0` / `max-h-[240px]` and `opacity-0` / `opacity-100`) combined with rotating chevrons to offer a premium UI experience.

## Results
- **Verdict**: **VALIDATED**
- **Evidence**: Verified that the submenu expands smoothly, correctly updates active indicators for both the parent item and children, and collapses when moving to non-settings pages.
