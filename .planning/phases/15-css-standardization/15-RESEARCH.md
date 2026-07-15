# Phase 15 Research: CSS Standardization Refactoring

## 1. Objective
Refactor all administrative console layout files, templates, tables, form components, and badges to align with the unified design tokens and Style Guide established in Spike 022. This eliminates page-to-page visual inconsistencies.

## 2. Inconsistent Components & Remediation Strategy

### A. Page Headers & Layout
- **Current state**: Inconsistent margins, font-sizes, and custom HTML headers.
- **Target**:
  - Main headers: Use `text-2xl font-bold tracking-tight text-zinc-900` for titles, and `text-zinc-500 text-sm mt-1` for help text.
  - Tab headers: Re-use `@LogsHeaderTabs("outbound"|"inbound"|"actions")` for all log/audit views.

### B. Cards & Panels
- **Current state**: Ad-hoc classes, varying background whites, hardcoded borders.
- **Target**: Wrap main areas in:
  ```html
  <div class="bg-white border border-zinc-200 rounded-lg p-6 shadow-sm">
  ```

### C. Forms, Selects & Labels
- **Current state**: Inputs vary in size, background colors, and label structures.
- **Target**:
  - Labels: `text-xs font-semibold text-zinc-500 uppercase tracking-wider mb-1`.
  - Inputs & Selects: `form-input border border-zinc-300 rounded-md px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-zinc-950 focus:border-transparent bg-white`.
  - Primary button: `btn btn-black bg-zinc-950 hover:bg-zinc-900 text-white border-none font-semibold rounded-md`.
  - Secondary button: `btn btn-outline border-zinc-300 hover:bg-zinc-100 text-zinc-700 font-semibold rounded-md bg-white`.

### D. Data Tables
- **Current state**: Different columns padding, border settings, and row highlight.
- **Target**:
  - Wrapper: `overflow-x-auto border border-zinc-200 rounded-lg shadow-sm`.
  - Table: `table min-w-full divide-y divide-zinc-200`.
  - Head: `bg-zinc-50 text-zinc-500 text-xs font-semibold uppercase tracking-wider text-left border-b border-zinc-200`.
  - Row Hover: `hover:bg-zinc-50/50 transition-colors`.
  - Padding: `px-6 py-4` (both table header and cells).

### E. Badges & Indicators
- **Current state**: Solid primary background colors, inconsistent padding.
- **Target**:
  - Connected/Active: `bg-emerald-50 text-emerald-700 border-emerald-200`
  - Pending/Warning: `bg-amber-50 text-amber-700 border-amber-200`
  - Error/Disconnected: `bg-rose-50 text-rose-700 border-rose-200`
  - Neutral/Info: `bg-blue-50 text-blue-700 border-blue-200`

## 3. Template Target Audit List
The following templates must be modified to apply these standards:
1. `templates/pages/devices.templ` (Connections list, pairing modals)
2. `templates/pages/campaigns.templ` (Campaign tables, form inputs, step controls)
3. `templates/pages/webhooks.templ` (Subscription config fields, tables)
4. `templates/pages/audit.templ` (Audit filter inputs, tables)
5. `templates/pages/telemetry.templ` (System health stats, details)
6. `templates/pages/onboarding.templ` (Initial credentials forms)
7. `templates/pages/workspace.templ` (API keys lists, forms)
