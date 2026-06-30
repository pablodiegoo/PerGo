# Admin Dashboard UI & UX

## Design Decisions
- **Notion-Style Aesthetic:** Clean, spacious, light-themed, essentially monochromatic layout. Gray borders (`#e4e4e7` or `border-zinc-200`) and backgrounds (`#f4f4f5` or `bg-zinc-100`) define structure.
- **Accent and Functional Colors:** Black (`#000000`) is the primary button color. Colors are reserved strictly for functional states (e.g. green status dot for connected whatsmeow JID, blue status dot/alert for webhook received).
- **Navigation:** Wide, left-hand sidebar layout (Notion-style, collapsible) containing the workspace logo/dropdown, primary hubs (Visão Geral, Conexões, Playground, Logs, Configurações), and developer documentation links at the bottom.
- **Dynamic Onboarding Checklist:** When a workspace has no API keys or connections configured, the dashboard overview hides charts/metrics and renders a 4-step progressive onboarding checklist.
- **Returning User Dashboard:** Displays a multi-instance connections status grid, high-level telemetry widgets (message counts, latency in ms, memory usage), and a live audit logs table.

## CSS Patterns
Shared monochromatic theme custom properties from `themes/default.css`:
```css
:root {
  --color-bg: #fcfcfc;
  --color-surface: #ffffff;
  --color-border: #e9e9eb;
  --color-text: #1e1e1e;
  --color-text-muted: #8a8a8e;
  --color-primary: #000000;
  --color-primary-hover: #2e2e2e;
  --color-accent: #374151;
  --color-danger: #dc2626;
  --color-success: #16a34a;

  --font-sans: 'Inter', system-ui, -apple-system, sans-serif;
  --radius-md: 6px;
}
```

## HTML Structures
daisyUI class patterns for main visual elements:

### 1. Left Navigation Sidebar
```html
<aside class="w-64 bg-zinc-50 border-r border-zinc-200 flex flex-col justify-between shrink-0 h-screen sticky top-0">
  <div>
    <!-- Workspace Selector -->
    <div class="p-4 border-b border-zinc-200">
      <div class="flex items-center gap-2 p-2 hover:bg-zinc-200/50 rounded cursor-pointer transition-all">
        <div class="w-6 h-6 bg-black text-white rounded flex items-center justify-center font-bold text-sm">P</div>
        <div class="text-sm font-semibold">Workspace Name</div>
      </div>
    </div>
    <!-- Menu -->
    <nav class="p-3 space-y-1">
      <a href="#" class="flex items-center gap-3 px-3 py-2 text-sm font-medium rounded-md bg-zinc-200/70 text-black">
        <i data-lucide="layout-dashboard" class="w-4 h-4"></i> Visão Geral
      </a>
      <a href="#" class="flex items-center gap-3 px-3 py-2 text-sm font-medium rounded-md text-zinc-500 hover:bg-zinc-200/50 hover:text-black">
        <i data-lucide="link" class="w-4 h-4"></i> Conexões
      </a>
    </nav>
  </div>
</aside>
```

### 2. Onboarding Step Card
```html
<div class="border border-zinc-200 rounded-lg p-5 bg-white space-y-3 shadow-sm">
  <div class="flex items-start gap-4">
    <!-- Active Step Badge -->
    <div class="w-6 h-6 rounded-full bg-black text-white flex items-center justify-center font-bold text-xs">1</div>
    <!-- Completed Step Badge -->
    <!-- <div class="w-6 h-6 rounded-full bg-green-500 text-white flex items-center justify-center font-bold text-xs">✓</div> -->
    <div class="flex-1">
      <h3 class="text-sm font-semibold text-black mb-1">Criar sua primeira Chave de API</h3>
      <p class="text-xs text-zinc-500">Descrição curta do passo.</p>
    </div>
  </div>
</div>
```

## What to Avoid
- **Avoid complex colors:** Do not use Tailwind's default primary colors (blue-600, indigo-600) for standard layout styling. Stick to neutral zinc/gray.
- **Avoid empty state graphs:** Never display empty charts when a workspace has no data. Instead, leverage the dynamic onboarding view.
- **Avoid Slim Sidebar as default:** Although Variant B (slim sidebar) saves screen space, user testing showed it reduces readability and ease of use for non-technical operators. Keep the wide sidebar as default.

## Origin
Synthesized from sketches: 001, 002
Source files available in:
- `sources/001-dashboard-layout/`
- `sources/002-onboarding-vs-operational/`
