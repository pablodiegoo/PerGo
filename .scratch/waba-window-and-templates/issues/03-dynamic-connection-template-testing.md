# 03 — Dynamic Template Parameter Resolution in Connection Testing

**What to build:** Enable dynamic parameter extraction in the Connection test dialog on `/admin/connections`. The modal parses the selected template's component JSON in real time, rendering zero inputs for templates with no variables and exact inputs for parameterized templates, and dispatches the test message with the template's designated language code.

**Blocked by:** None — can start immediately

**Status:** ready-for-agent

- [ ] Selecting a template in `TestConnectionModal` dynamically calculates variable placeholders from `components` JSON.
- [ ] Templates with zero body parameters display zero parameter input boxes.
- [ ] Templates with N parameters display exactly N labeled parameter inputs.
- [ ] Dispatched test messages via `RunTest` serialize the actual template language code and all non-empty parameter values into the outbound queue message.
