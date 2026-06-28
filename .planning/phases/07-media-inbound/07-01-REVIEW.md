## ISSUES FOUND

**Phase:** Media & Inbound
**Plans checked:** 1
**Issues:** 3 blocker(s), 3 warning(s), 0 info

### Blockers (must fix)

**1. [scope_sanity] Plan 01 has 11 tasks and 25 files modified - exceeds context budget**
- Plan: "07-01"
- Fix: Split the phase into multiple plans (07-01, 07-02, 07-03, 07-04) aligned with the waves in [07-VALIDATION.md](file:///home/pablo/Coding/PerGo/.planning/phases/07-media-inbound/07-VALIDATION.md) (Wave 1: media validation and storage, Wave 2: outbound adapters, Wave 3: inbound webhooks and dedup, Wave 4: webhook worker and auditing).

**2. [verification_derivation] Missing 'Artifacts this phase produces' section**
- Plan: "07-01"
- Fix: Add the missing "Artifacts this phase produces" section listing created/modified symbols (e.g. structs, interfaces, methods, endpoints) to PLAN.md.

**3. [requirement_coverage] Discrepancy between validation waves and plan tasks**
- Plan: "07-01"
- Fix: Group tasks into plans matching the distinct waves from the validation strategy in [07-VALIDATION.md](file:///home/pablo/Coding/PerGo/.planning/phases/07-media-inbound/07-VALIDATION.md) instead of cramming everything into wave 1.

### Warnings (should fix)

**1. [verification_derivation] Missing SPEC edge cases in must_haves.truths**
- Plan: "07-01"
- Fix: Add truths for covered SPEC edge cases from [07-SPEC.md](file:///home/pablo/Coding/PerGo/.planning/phases/07-media-inbound/07-SPEC.md): WABA status objects ignored (R4), empty text with media/location/contacts forwarded normally (R4), audit logs for non-empty events only (R6), dedup key provider-specific (R7), and media without caption delivers no caption text (R8).

**2. [task_completeness] Discrepancy in files_modified frontmatter metadata**
- Plan: "07-01"
- Fix: Add `internal/api/handler/message.go` and `internal/api/handler/message_test.go` to the `files_modified` frontmatter array, since they are modified in Task 4.

**3. [security_compliance] Missing dependency legitimacy threat T-07-SC in threat register**
- Plan: "07-01"
- Fix: Add dependency legitimacy / supply chain threat `T-07-SC` to the `<threat_model>` block because new external packages (`aws-sdk-go-v2`) are fetched in Task 2.

### Structured Issues

```yaml
issues:
  - plan: "07-01"
    dimension: "scope_sanity"
    severity: "blocker"
    description: "Plan 01 has 11 tasks and 25 files modified - exceeds context budget (target 2-3 tasks, 5-8 files)"
    fix_hint: "Split the phase into multiple plans (07-01, 07-02, 07-03, 07-04) aligned with the waves in 07-VALIDATION.md"
  - plan: "07-01"
    dimension: "verification_derivation"
    severity: "blocker"
    description: "Missing 'Artifacts this phase produces' section in the plan"
    fix_hint: "Add the missing 'Artifacts this phase produces' section listing created/modified symbols to PLAN.md"
  - plan: "07-01"
    dimension: "requirement_coverage"
    severity: "blocker"
    description: "Discrepancy between validation waves and plan tasks; all tasks assigned to Wave 1"
    fix_hint: "Split the tasks into sequential plans (07-01 to 07-04) matching the validation waves"
  - plan: "07-01"
    dimension: "verification_derivation"
    severity: "warning"
    description: "Missing SPEC edge cases in must_haves.truths (R4 status ignored, R4 empty text, R6 non-empty audit, R7 dedup key details, R8 media caption details)"
    fix_hint: "Add must_haves.truths statements for each covered SPEC edge case"
  - plan: "07-01"
    dimension: "task_completeness"
    severity: "warning"
    description: "Files internal/api/handler/message.go and message_test.go are modified in Task 4 but missing from files_modified in frontmatter"
    fix_hint: "Add missing files to the frontmatter files_modified list"
  - plan: "07-01"
    dimension: "security_compliance"
    severity: "warning"
    description: "Missing supply chain / package legitimacy threat T-07-SC in threat register"
    fix_hint: "Add T-07-SC for aws-sdk-go-v2 package installation with a validation/audit mitigation plan"
```

### Recommendation

3 blocker(s) require revision. Returning to planner with feedback.
