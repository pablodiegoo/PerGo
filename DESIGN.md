---
version: 1.0.0
name: PerGo Design System
description: Authoritative paper-calm design system specification for PerGo CPaaS data plane and developer console. Features electric cyan accents, Outfit headlines, Inter system copy, and high-contrast accessible controls.

colors:
  primary: "#0284c7"
  primary-active: "#0369a1"
  glow: "#38bdf8"
  canvas: "#ffffff"
  canvas-soft: "#fbfbfa"
  surface: "#ffffff"
  hairline: "#e9e9e7"
  dark-canvas: "#191919"
  dark-surface: "#202020"
  dark-hairline: "#2f2f2f"
  ink: "#37352f"
  ink-secondary: "#787774"
  dark-ink: "#e3e3e3"
  status-success: "#10b981"
  status-warning: "#f59e0b"
  status-error: "#ef4444"
  on-primary: "#ffffff"

typography:
  display-1:
    fontFamily: Outfit, sans-serif
    fontSize: 36px
    fontWeight: 700
    lineHeight: 1.15
    letterSpacing: -0.75px
  heading-1:
    fontFamily: Outfit, sans-serif
    fontSize: 24px
    fontWeight: 700
    lineHeight: 1.25
    letterSpacing: -0.5px
  heading-2:
    fontFamily: Outfit, sans-serif
    fontSize: 18px
    fontWeight: 600
    lineHeight: 1.3
    letterSpacing: -0.25px
  heading-3:
    fontFamily: Inter, sans-serif
    fontSize: 14px
    fontWeight: 600
    lineHeight: 1.4
    letterSpacing: 0px
  body-md:
    fontFamily: Inter, sans-serif
    fontSize: 14px
    fontWeight: 400
    lineHeight: 1.5
    letterSpacing: 0px
  body-sm:
    fontFamily: Inter, sans-serif
    fontSize: 13px
    fontWeight: 400
    lineHeight: 1.4
    letterSpacing: 0px
  button:
    fontFamily: Inter, sans-serif
    fontSize: 13px
    fontWeight: 600
    lineHeight: 1.4
    letterSpacing: 0px
  caption:
    fontFamily: Inter, sans-serif
    fontSize: 12px
    fontWeight: 500
    lineHeight: 1.4
    letterSpacing: 0px
  eyebrow:
    fontFamily: Inter, sans-serif
    fontSize: 11px
    fontWeight: 600
    lineHeight: 1.3
    letterSpacing: 0.5px

rounded:
  xs: 4px
  sm: 6px
  md: 8px
  lg: 12px
  xl: 16px
  full: 9999px

spacing:
  xxs: 4px
  xs: 8px
  sm: 12px
  md: 16px
  lg: 24px
  xl: 32px

components:
  button-primary:
    backgroundColor: "{colors.primary-active}"
    textColor: "{colors.on-primary}"
    typography: "{typography.button}"
    rounded: "{rounded.lg}"
    padding: "{spacing.sm}"
  button-secondary:
    backgroundColor: "{colors.canvas-soft}"
    textColor: "{colors.ink}"
    typography: "{typography.button}"
    rounded: "{rounded.lg}"
    padding: "{spacing.sm}"
  card:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.ink}"
    typography: "{typography.body-md}"
    rounded: "{rounded.xl}"
    padding: "{spacing.md}"
  input:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.ink}"
    typography: "{typography.body-sm}"
    rounded: "{rounded.lg}"
    padding: "{spacing.xs}"
    height: 38px
  badge-warning:
    backgroundColor: "{colors.status-warning}"
    textColor: "{colors.ink}"
    typography: "{typography.caption}"
    rounded: "{rounded.full}"
    padding: "{spacing.xxs}"
  status-dot-success:
    backgroundColor: "{colors.status-success}"
    rounded: "{rounded.full}"
    size: 8px
  status-dot-error:
    backgroundColor: "{colors.status-error}"
    rounded: "{rounded.full}"
    size: 8px
  app-shell:
    backgroundColor: "{colors.canvas-soft}"
    textColor: "{colors.ink}"
  app-shell-dark:
    backgroundColor: "{colors.dark-canvas}"
    textColor: "{colors.dark-ink}"
  sidebar-dark:
    backgroundColor: "{colors.dark-surface}"
    textColor: "{colors.dark-ink}"
  divider:
    backgroundColor: "{colors.hairline}"
    height: 1px
  divider-dark:
    backgroundColor: "{colors.dark-hairline}"
    height: 1px
  interactive-glow:
    textColor: "{colors.glow}"
    size: 20px
  caption-secondary:
    textColor: "{colors.ink-secondary}"
    typography: "{typography.caption}"
  brand-indicator:
    backgroundColor: "{colors.primary}"
    size: 24px
    rounded: "{rounded.md}"
  main-canvas:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.ink}"
---

# PerGo — Design System & Aesthetic Specification (DESIGN.md)

This document is the **Single Source of Truth** for UI aesthetics, component contracts, typography hierarchy, layout rules, and navigation state behaviors across the PerGo CPaaS platform. All UI implementations, Templ templates, and AI agents must strictly adhere to the guidelines codified below.

---

## Overview

PerGo adopts a **Paper-Calm** design aesthetic tailored for high-reliability CPaaS infrastructure:
- **Quiet Monochrome Chrome**: Sidebars, top navigation bars, and structural panels remain muted neutral gray (`{colors.canvas-soft}` / `#fbfbfa` in light mode; `{colors.dark-canvas}` / `#191919` in dark mode). Structural chrome avoids heavy saturated fills.
- **Electric CPaaS Cyan Accent**: The primary brand accent is Electric Cyan (`#0284c7`), with deep cyan (`#0369a1`) utilized on interactive buttons to guarantee accessible WCAG AA contrast against white text (`4.58:1 >= 4.5:1`), and cyan glow (`#38bdf8`) used for reactive highlight halos.
- **State-Reactive Icon Illumination**: Navigation icons and interactive triggers remain muted gray (`{colors.ink-secondary}`) in idle state, illuminating in brand cyan (`#0284c7` / `#38bdf8`) strictly on `:hover` and `:active` states.
- **Strict Zero-Emoji Policy**: Raw Unicode emojis (e.g. `📊`, `🚀`, `📱`, `💬`, `⚡`, `⚠️`) are strictly forbidden in UI chrome, action buttons, headers, and navigation menus. All iconography must use vectorized SVG symbols with uniform stroke weight (`stroke-width="2"`).

---

## Colors

The PerGo color system balances paper-like calm backgrounds with high-visibility dispatch indicators:

| Token | Light Mode | Dark Mode | Role & Semantic Purpose |
| :--- | :--- | :--- | :--- |
| **`primary`** | `#0284c7` | `#38bdf8` | Brand accent, reactive icon illumination, links, active tab indicator |
| **`primary-active`** | `#0369a1` | `#0284c7` | High-contrast button background (WCAG AA >= 4.5:1 on white text) |
| **`glow`** | `#38bdf8` | `#0284c7` | Hover halo, focus rings, interactive glow effects |
| **`canvas`** | `#ffffff` | `#191919` | Main base canvas layer |
| **`canvas-soft`** | `#fbfbfa` | `#191919` | Soft warm neutral canvas and sidebar fill |
| **`surface`** | `#ffffff` | `#202020` | Content cards, data tables, dialog panels, form inputs |
| **`hairline`** | `#e9e9e7` | `#2f2f2f` | 1px structural borders, dividers, subtle table rules |
| **`dark-canvas`** | `#191919` | `#191919` | Dark mode page canvas root |
| **`dark-surface`** | `#202020` | `#202020` | Dark mode surface cards and dialogs |
| **`dark-hairline`** | `#2f2f2f` | `#2f2f2f` | Dark mode 1px hairline borders |
| **`ink`** | `#37352f` | `#e3e3e3` | Primary body text, headings, dark icons |
| **`ink-secondary`** | `#787774` | `#9b9b9b` | Subtitles, metadata, idle icons, input placeholders |
| **`dark-ink`** | `#e3e3e3` | `#e3e3e3` | Primary text in dark mode environments |
| **`status-success`** | `#10b981` | `#34d399` | Delivery confirmation, active webhooks, operational channels |
| **`status-warning`** | `#f59e0b` | `#fbbf24` | Rate limits approaching, quota warnings, degraded latency |
| **`status-error`** | `#ef4444` | `#f87171` | Delivery failures, dead letter queue alerts, channel disconnections |
| **`on-primary`** | `#ffffff` | `#ffffff` | Text and icons rendered atop primary colored backgrounds |

### Navigation & Menu Hover Standard (Editor Menu Pattern)
All navigation items in the PerGo sidebar and dropdowns follow the reactive illumination contract:
- **Idle State**: Neutral background (`bg-transparent`), muted text (`text-slate-600` / `text-base-content/70`), and subdued SVG vector icon (`text-slate-400` / `text-base-content/50`).
- **Hover State**: Subtle warm gray background tint (`hover:bg-slate-100` / `hover:bg-base-200`), text sharpens (`hover:text-slate-900`), and SVG icon lights up in Electric Cyan (`group-hover:text-sky-600` / `#0284c7`).
- **Active State**: Soft cyan tinted background (`bg-sky-50` / `bg-primary/10`), bold cyan text (`text-sky-700 font-semibold`), and vibrant cyan SVG icon (`text-sky-600`).

---

## Typography

PerGo combines **Outfit** for editorial titles and headings with **Inter** for high-density tabular data, form controls, and console logs.

| Scale Token | Family | Size | Weight | Line Height | Tracking & Usage |
| :--- | :--- | :--- | :--- | :--- | :--- |
| **`display-1`** | `Outfit` | `36px` | 700 | 1.15 | `-0.75px` • Landing hero displays, major milestone headers |
| **`heading-1`** | `Outfit` | `24px` | 700 | 1.25 | `-0.5px` • Page headers (`PageHeader`), dashboard titles |
| **`heading-2`** | `Outfit` | `18px` | 600 | 1.3 | `-0.25px` • Section cards, modal headers, panel titles |
| **`heading-3`** | `Inter` | `14px` | 600 | 1.4 | `0px` • Table headers, metric card labels, form fieldsets |
| **`body-md`** | `Inter` | `14px` | 400 | 1.5 | `0px` • Long-form copy, description blocks, message previews |
| **`body-sm`** | `Inter` | `13px` | 400 | 1.4 | `0px` • Form inputs, table cells, campaign metadata |
| **`button`** | `Inter` | `13px` | 600 | 1.4 | `0px` • Interactive buttons, pill action triggers |
| **`caption`** | `Inter` | `12px` | 500 | 1.4 | `0px` • Timestamps, secondary subtitles, helper text |
| **`eyebrow`** | `Inter` | `11px` | 600 | 1.3 | `0.5px` uppercase • Metric category tags, status tags |

---

## Layout

PerGo uses a strict **8px base grid** with a **4px half-step** for micro-alignments.

### Spacing Scale
- `xxs` (4px): Micro gaps, badge inner padding, icon-to-label separation.
- `xs` (8px): Form input vertical padding, compact button spacing, tight list gaps.
- `sm` (12px): Standard button padding, table cell vertical padding, card chip margins.
- `md` (16px): Card internal padding, grid gutters, section separations.
- `lg` (24px): Panel headers, major card paddings, modal interior spacing.
- `xl` (32px): Page section gutters, hero spacing, dashboard widget gaps.

### Page Alignment & Elimination of Page Jumps
- **Uniform Canvas Width**: All page root views must mount to a unified container contract: `w-full space-y-6`.
- **No Arbitrary Max-Width Shifting**: Do not constrain inner pages (like Settings, Webhooks, or Logs) to random `max-w-4xl` or `max-w-2xl` while dashboards span full width. Keep consistent left-margin alignment across all page transitions.
- **Breadcrumb Trail**: Mandatory on all nested views at route depth $\ge 2$ (e.g. `/templates/[id]`, `/webhooks/[id]`, `/workspaces/[id]`).

---

## Elevation & Depth

PerGo adheres to the **Paper-Calm philosophy**, relying on structural borders and tonal layering rather than heavy drop shadows:
- **Level 0 (Flat Cards & Panels)**: Thin 1px hairline border (`1px solid #e9e9e7` in light mode; `1px solid #2f2f2f` in dark mode). No drop shadow (`shadow-none`). Used for feature cards, table containers, message preview boxes.
- **Level 1 (Soft Popover / Floating Menus)**: Subtle ambient blur `0 10px 25px -5px rgba(0, 0, 0, 0.08)` (in dark mode: `0 10px 25px -5px rgba(0, 0, 0, 0.5)`). Used for command palettes, autocomplete menus, active dialog backdrops.
- **Tonal Contrast**: Distinct surfaces are separated by tone (`#fbfbfa` canvas vs `#ffffff` card surface) rather than artificial elevation.

---

## Shapes

The geometric language of PerGo communicates engineered precision and soft tactile ergonomics:

- **`xs` (4px)**: Code tokens, channel badge pills, table tag labels.
- **`sm` (6px)**: Dropdown menu rows, subtle hover highlights, inline chips.
- **`md` (8px)**: Secondary utility buttons, small dialog cards, status tags.
- **`lg` (12px)**: Default buttons, form text inputs, select dropdown triggers.
- **`xl` (16px)**: Primary cards, analytics widgets, message bubble clusters.
- **`full` (9999px)**: Status dot indicators, avatar icons, circular action buttons.

---

## Components

### 1. Button Specifications
- **Primary Button**: High contrast `#0369a1` background with white text (`#ffffff`), satisfying WCAG AA 4.58:1 contrast. Corner radius `rounded-lg` (12px), vertical padding 8px, horizontal padding 16px.
- **Secondary Button**: Neutral soft background (`#fbfbfa`), hairline border (`1px solid #e9e9e7`), ink text (`#37352f`).
- **Ghost / Action Button**: Transparent background, ink-secondary text, hover background `rgba(0,0,0,0.04)`.

### 2. Form Controls & Inputs
- **Height & Spacing**: Comfortable 38px minimum touch height with `rounded-lg` (12px) corners and 1px hairline border.
- **Focus Rings**: Electric Cyan glow ring (`focus:ring-2 focus:ring-sky-400 focus:border-sky-500`).

### 3. Navigation Sidebar & Popovers
- Muted sidebar chassis (`#fbfbfa` / dark `#202020`) with 1px right border (`#e9e9e7` / dark `#2f2f2f`).
- Navigation links wrapped with `.group` containing vectorized SVG icons that illuminate in Electric Cyan (`#0284c7`) strictly on hover.

### 4. Popover Surface Specification (Level 1 Elevation)
Popovers provide a tactile, premium micro-interaction surface powered by Alpine.js:
- **Visual Token**: `shadow-notion-popover` (`0 10px 25px -5px rgba(0, 0, 0, 0.08), 0 8px 10px -6px rgba(0, 0, 0, 0.04)`), `border border-[#e9e9e7] dark:border-[#2f2f2f]`, `rounded-xl`, `bg-white dark:bg-[#202020]`.
- **Transitions (100ms)**:
  - Enter: `transition ease-out duration-100 transform opacity-0 scale-95 -> opacity-100 scale-100`
  - Leave: `transition ease-in duration-75 transform opacity-100 scale-100 -> opacity-0 scale-95`
- **Dismissal**: Handled cleanly by `@click.outside="open = false"` and `@keydown.escape.window="open = false"`.

### 5. Canonical Popovers
- **Workspace Selector Popover**: Replaces standard HTML `<select>` with a rich tactile trigger in the sidebar header displaying the active workspace avatar, name, and chevron, opening an overlay with search, active checkmark, and quick management links.
- **Context Action Menu (`...`)**: Secondary actions on table rows (campaigns, templates, devices) use fixed-coordinate floating popovers to eliminate clipping from horizontal scroll wrappers (`overflow-x-auto`).
- **User Profile Popover**: Located in the sidebar footer, combining user avatar, session info, language switcher (`EN` / `PT`), theme toggle, and secure logout.

### 6. Status Dots & Badge Indicators
- Operational status conveyed via 8px circular status dots (`status-dot-success`, `status-dot-error`) alongside explicit text labels (never relying solely on color).

### 7. Iconography Standard — Strict Zero-Emoji Rule
- **Vectorized SVG Only**: Emojis are strictly banned from production UI controls, headers, buttons, tables, and notifications.
- All icons must be rendered as inline SVG components with `viewBox="0 0 24 24"`, `stroke-width="2"`, and reactive hover classes (`group-hover:text-sky-600 transition-colors`).

---

## Do's and Don'ts

| Category | 🚫 Don't | ✅ Do |
| :--- | :--- | :--- |
| **Iconography** | Use raw Unicode emojis (`📊`, `🚀`, `💬`, `⚡`, `⚠️`) in buttons or sidebars | Use clean vectorized SVG icons with `stroke-width="2"` and reactive hover states |
| **Contrast** | Use light cyan `#0284c7` on white text buttons (fails WCAG AA at 3.5:1) | Use deep cyan `#0369a1` on white text buttons (passes WCAG AA at 4.58:1) |
| **Chrome** | Fill sidebars or top headers with vibrant, high-saturation color blocks | Keep chrome neutral paper-calm (`#fbfbfa` / `#191919`) with reactive cyan illumination |
| **Elevation** | Add heavy blurred dark drop-shadows to standard cards | Use clean 1px hairline borders (`#e9e9e7`) and reserve soft shadows for popovers |
| **Layout** | Use erratic container max-widths across different pages | Maintain unified `w-full space-y-6` canvas alignment across all views |
| **Navigation** | Omit breadcrumb trails on nested detail routes | Include `<BreadcrumbTrail>` on all paths with depth $\ge 2$ |
| **Inputs** | Squeeze form fields into tiny 24px text inputs | Provide comfortable 38px height inputs with `rounded-lg` corners |
