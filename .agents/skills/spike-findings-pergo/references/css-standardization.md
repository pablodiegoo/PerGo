# CSS Standardization & Visual Tokens

## Requirements
- All administrative console pages must maintain styling coherence (typography, colors, tables, forms, spacing).
- Page-level tabs and navigation must be unified (using shared header components).
- Status badges and feedback messages must use light background tints to avoid visual clutter.

## How to Build It

### 1. Page Header & Tabs
Use the unified header component to display the page title, subheader, and active logs tab:

```go
// LogsHeaderTabs renders the unified logs tabs navigation.
templ LogsHeaderTabs(activeTab string) {
	<div class="border-b border-zinc-200 pb-5 mb-8 flex justify-between items-end">
		<div>
			<h1 class="text-2xl font-bold tracking-tight text-zinc-900">Logs de Auditoria</h1>
			<p class="text-zinc-500 text-sm mt-1">Monitore tráfego e ações administrativas.</p>
		</div>
		<div class="tabs tabs-boxed bg-zinc-100 p-1 rounded-lg inline-flex">
			<a href="/admin/logs/outbound" class={ "tab px-4 py-1.5 text-sm font-semibold rounded-md transition-all", templ.KV("tab-active bg-white text-zinc-900 shadow-sm", activeTab == "outbound") }>Outbound</a>
			<a href="/admin/logs/inbound" class={ "tab px-4 py-1.5 text-sm font-semibold rounded-md transition-all", templ.KV("tab-active bg-white text-zinc-900 shadow-sm", activeTab == "inbound") }>Inbound</a>
			<a href="/admin/logs/actions" class={ "tab px-4 py-1.5 text-sm font-semibold rounded-md transition-all", templ.KV("tab-active bg-white text-zinc-900 shadow-sm", activeTab == "actions") }>Actions</a>
		</div>
	</div>
}
```

### 2. Standard Container Cards
Wrap sections in white, bordered containers with subtle shadows:
```html
<div class="bg-white border border-zinc-200 rounded-lg p-6 shadow-sm">
    <!-- Component Content -->
</div>
```

### 3. Forms & Inputs
Labels must use upper-case, tracking-wider style, and inputs should have clean focus borders:
```html
<div class="flex flex-col gap-1">
    <label class="text-xs font-semibold text-zinc-500 uppercase tracking-wider mb-1">Rótulo do Campo</label>
    <input type="text" class="form-input border border-zinc-300 rounded-md px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-zinc-950 focus:border-transparent transition-all" />
</div>
```

### 4. Data Tables
Tables must use standard spacing, headers in light backgrounds, and alternate/hover states:
```html
<div class="overflow-x-auto border border-zinc-200 rounded-lg shadow-sm">
    <table class="table min-w-full divide-y divide-zinc-200">
        <thead class="bg-zinc-50 text-zinc-500 text-xs font-semibold uppercase tracking-wider text-left border-b border-zinc-200">
            <tr>
                <th class="px-6 py-3">Nome</th>
                <th class="px-6 py-3 text-right">Ação</th>
            </tr>
        </thead>
        <tbody class="divide-y divide-zinc-200 text-sm text-zinc-700 bg-white">
            <tr class="hover:bg-zinc-50/50 transition-colors">
                <td class="px-6 py-4 font-semibold text-zinc-900">Exemplo</td>
                <td class="px-6 py-4 text-right">
                    <button class="btn btn-ghost hover:bg-zinc-100 text-blue-600 btn-xs font-semibold">Ver</button>
                </td>
            </tr>
        </tbody>
    </table>
</div>
```

### 5. Status Badges
Status indicators must use soft tints with saturated texts:
* **Success/Active**: `bg-emerald-50 text-emerald-700 border-emerald-200`
* **Warning/Pending**: `bg-amber-50 text-amber-700 border-amber-200`
* **Error/Failed**: `bg-rose-50 text-rose-700 border-rose-200`
* **Neutral/Info**: `bg-blue-50 text-blue-700 border-blue-200`

## What to Avoid
- Avoid deep colored buttons (`btn-success` or `btn-info`) which clash with the design system. Use `btn-black` (`bg-zinc-950`) or simple outlines (`border-zinc-300`).
- Avoid inline layout styling. Leverage grid system classes (`grid grid-cols-1 md:grid-cols-3 gap-6`).

## Constraints
- Must remain compatible with DaisyUI classes (`badge`, `table`, `join`) and Tailwind CSS directives.

## Origin
Synthesized from spikes: 022
Source files available in: sources/022-css-standardization/
