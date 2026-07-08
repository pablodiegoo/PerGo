# Settings UI

## Requirements

- **Nested Configurations Sidebar**: The left sidebar must display a "Configurações" option that expands inline to reveal five sub-options: **Conexões**, **Logs**, **Workspace**, **Webhooks**, and **Telemetry**.
- **Active State Persistence**: The submenu must automatically remain open and active on load if any of the configurations sub-pages are currently selected.
- **Unified Settings Layout**: Remove redundant top tab switchers (e.g., from Workspace, Webhooks, and Telemetry pages) to clean up UI clutter and align navigation purely with the left sidebar.
- **Smooth Chevron & Collapsible Animation**: Toggling the Configurations submenu must trigger a smooth accordion height transition and rotate the chevron icon.

## How to Build It

### 1. Sidebar HTML & Tailwind structure (`templates/layout/sidebar.class` / `sidebar.templ`)
Ensure the left sidebar uses Tailwind CSS transition heights combined with a small JS snippet or pure state binding if rendering server-side:

```html
<!-- Configurações Parent Item -->
<li>
  <button type="button" onclick="toggleSettingsSubmenu(true)" class="nav-btn w-full flex items-center justify-between px-3 py-2.5 text-sm font-medium rounded-md text-zinc-600 hover:bg-zinc-200/50 hover:text-zinc-900 transition-all">
    <span class="flex items-center gap-3">
      <!-- Settings icon -->
      <svg class="h-4.5 w-4.5 stroke-[2]" fill="none" viewBox="0 0 24 24" stroke="currentColor">...</svg>
      <span>Configurações</span>
    </span>
    <!-- Chevron -->
    <svg id="chevron-icon" class="h-4 w-4 transform transition-transform duration-200 rotate-0 text-zinc-400" fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7" />
    </svg>
  </button>
  
  <!-- Collapsible Submenu Container -->
  <ul id="settings-submenu" class="transition-all duration-200 pl-9 mt-1 space-y-1 overflow-hidden max-h-0 opacity-0">
    <li><a href="/admin/connections" class="sub-nav-btn ...">Conexões</a></li>
    <li><a href="/admin/logs" class="sub-nav-btn ...">Logs</a></li>
    <li><a href="/admin/workspace" class="sub-nav-btn ...">Workspace</a></li>
    <li><a href="/admin/webhooks" class="sub-nav-btn ...">Webhooks</a></li>
    <li><a href="/admin/telemetry" class="sub-nav-btn ...">Telemetry</a></li>
  </ul>
</li>
```

### 2. Smooth Height Transition JS helper
Use a standard vanilla JavaScript function to toggle class styles smoothly:

```javascript
let isSettingsExpanded = false;

function toggleSettingsSubmenu(userClicked = false) {
  const submenu = document.getElementById('settings-submenu');
  const chevron = document.getElementById('chevron-icon');
  
  if (userClicked) {
    isSettingsExpanded = !isSettingsExpanded;
  }
  
  if (isSettingsExpanded) {
    submenu.style.maxHeight = '240px'; // large enough to contain all options
    submenu.style.opacity = '1';
    chevron.classList.add('rotate-180');
  } else {
    submenu.style.maxHeight = '0px';
    submenu.style.opacity = '0';
    chevron.classList.remove('rotate-180');
  }
}
```

### 3. Server-side State Retention on Load
When rendering templates in `Go` / `templ`, detect the current active route:
- If the current route matches `/admin/connections`, `/admin/logs`, `/admin/workspace`, `/admin/webhooks`, or `/admin/telemetry`:
  - Set the parent "Configurações" button to active (`bg-zinc-200/60`, `text-zinc-900`, `font-semibold`).
  - Render the submenu `<ul>` as expanded by default (`max-h-[240px]`, `opacity-100`) and the chevron rotated (`rotate-180`).
  - Highlight the corresponding active sub-item.

### 4. Layout Optimization & Tab Removal
- Clean up `templates/workspace.templ`, `templates/webhooks.templ`, and `templates/telemetry.templ`.
- Remove any `<div class="tabs">` block located at the top-right of those screens.
- Implement standard title headers aligned with DaisyUI conventions:
  ```html
  <div class="border-b border-slate-200 pb-5 mb-8 flex justify-between items-end">
    <div>
      <h1 class="text-3xl font-bold tracking-tight text-slate-900">Workspace</h1>
      <p class="text-slate-500 text-sm mt-1">Gerencie workspaces, credenciais de integração e chaves de API.</p>
    </div>
  </div>
  ```

## What to Avoid

- **HTML `<details>` Element**: Do not use `<details>` for sidebar collapse. Animating height expansion smoothly across browsers requires complex workarounds compared to a simple utility/inline JS toggle.
- **Diverging Header Styling**: Ensure all configurations sub-pages use the identical top header border and padding spacing.

## Constraints

- **Tailwind CSS classes**: transition-all, duration-200, rotate-180, max-h-0, opacity-0.
- **Sidebar width**: Constrained to 60 (240px) to keep standard visual consistency.

## Origin

Synthesized from spikes: 010, 011
Source files available in: sources/010-settings-nested-sidebar/, sources/011-settings-layout-optimization/
