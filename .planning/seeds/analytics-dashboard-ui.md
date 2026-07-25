---
title: Consolidated Analytics Dashboard UI
trigger_condition: When REQ-ANALYTICS-CONSOLIDATED API is implemented and returning data
planted_date: 2026-07-25
context: gsd-explore session — "mini Twilio" vision needs a unified analytics view like Twilio Console
---

# Consolidated Analytics Dashboard UI

## Idea

Build a visual analytics dashboard in the PerGo admin console (HTMX + templ) that shows cross-channel messaging performance at a glance — the equivalent of Twilio's Usage Dashboard but for a self-hosted CPaaS.

## Key Design Considerations

- **Overview cards** — total messages sent, delivery rate, active conversations, cost estimate (WABA)
- **Channel comparison chart** — bar/line chart comparing delivery rates across WABA, Telegram, WhatsApp Web, Email
- **WABA conversation breakdown** — pie chart by category (Marketing, Utility, Authentication, Service)
- **Quality rating indicator** — prominent display of Meta's quality rating with trend (improving/declining)
- **Time range picker** — today, 7d, 30d, 90d, custom range
- **Campaign drill-down** — click a campaign to see its specific metrics across channels
- **Template performance** — which templates have highest delivery/read rates
- **Failure analysis** — top error reasons with counts, grouped by channel
- **Export** — CSV/PDF export of any view for reporting

## Data Sources

- `dispatches` table — delivery status, latency, channel, campaign_id
- `analytics_snapshots` table — Meta conversation counts, costs, quality rating
- `audit_logs` — API usage patterns

## Dependencies

- REQ-ANALYTICS-SYNC (Meta data sync job)
- REQ-ANALYTICS-CONSOLIDATED (unified API)
- Sketch session recommended — dashboard UX is critical to get right

## Inspiration

- Twilio Usage Dashboard
- Chatwoot Reports panel
- Novu Activity Feed

## Anti-patterns

- Don't build real-time dashboards — 5-minute cache is fine for analytics
- Don't use heavy JS charting libraries — HTMX + server-rendered SVG charts or lightweight lib (Chart.js via CDN)
- Don't show raw Meta API data — translate everything to PerGo's domain language
