---
spike: 20
name: campaign-engine
type: standard
validates: "Given a mailing list and throttling parameters, when configured in a UI, then we can clean the list, map variables, estimate duration, and simulate batch dispatching with logging comparison."
verdict: PENDING
related: []
tags: [ui, campaigns, logs]
---

# Spike 020: Campaign Engine

## What This Validates
Given a mailing list (CSV), when uploaded and mapped to a template with variables:
- We can parse the CSV and map column indices to dynamic variables (e.g. `{{1}}` to name, `{{2}}` to tracking code).
- We can scrub and clean the mailing list, showing metrics on duplicates and formatting invalidations.
- We can compute the estimated dispatch duration based on batch size and delay parameters.
- We can simulate a campaign dispatch in batches using delayed execution.
- We can compare two logging architectures (Enriched Outbound Logs vs Separate Campaign Tables) to make a sound architectural decision.

## How to Run
To run and view this interactive spike, open the `index.html` file in your web browser:

```bash
# On Linux/macOS
open .planning/spikes/020-campaign-engine/index.html
```

Or open it directly by navigation to the file in your browser.

## What to Expect
- A complete, high-fidelity Tailwind CSS + DaisyUI dashboard representing the Campaign Manager.
- Inputs for CSV raw text, WABA Template, dynamic variables mapper, scheduling time, batch size, and inter-batch delay.
- An instant estimation calculator explaining how long the campaign will take to run.
- A "Mailing Clean & Scrub" pipeline that removes duplicates and incorrectly formatted numbers.
- A live progress-bar simulator that executes the campaign batch-by-batch.
- A comparative database log output showing exactly what rows are generated in the DB for both "Enriched Outbound" and "Separate Campaign Logs" architectures, complete with JSON/CSV exporter.

## Investigation Trail
- **Initial Idea**: Build a backend-only pipeline with queueing. But a campaign tool is heavily visual — the key unknowns are how users map variables, how they calculate throughput limits to avoid suspension, and how we structures logs for both transactional auditing and campaign reports.
- **UI Decoupling**: Building it in an interactive single-page app (SPA) lets us mock the Go Echo backend + NATS queue state machines inside JS, validating the exact frontend interface contracts.
- **Log Design Trade-off**: High-throughput systems suffer under heavy index lookups when analytical queries (e.g. "which campaigns performed best at 9 AM?") hit transactional messaging tables. We compare the single-table versus two-table approach here.

## Results
Pending interactive simulation and architectural review.
