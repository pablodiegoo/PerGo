# Research Question: Meta Template Validation Rules

**Date**: 2026-07-25
**Context**: Exploration session — WABA Template CRUD features
**Priority**: HIGH — blocks Phase 30 planning

## Question

What are all the validation rules Meta enforces when creating/editing WABA message templates, so PerGo can replicate them locally for instant feedback?

## Known Constraints (to verify & expand)

- Body text: max 1024 characters
- Footer: max 60 characters
- Variables must be sequential `{{1}}`, `{{2}}`, ... — no gaps
- Header types: TEXT (max 60 chars, 1 variable max), IMAGE, VIDEO, DOCUMENT
- Media headers require sample URLs on creation
- Button types: QUICK_REPLY (max 3), URL (max 2), PHONE_NUMBER (max 1), COPY_CODE
- URL buttons: max 2000 chars, dynamic suffix variable allowed
- Category-specific rules:
  - AUTHENTICATION: must include OTP button, restricted body format
  - MARKETING: requires opt-out footer for certain regions?
  - UTILITY: stricter approval criteria?
- Language codes: BCP-47 subset supported by Meta (need full list)
- Template name: lowercase alphanumeric + underscores only

## What We Need

1. **Complete validation rule matrix** by component type (header, body, footer, buttons) and category
2. **Meta error codes** for each validation failure (to map to our local errors)
3. **Regional differences** — do validation rules vary by WABA region or country?
4. **Rate limits** on template creation/editing API calls
5. **Undocumented gotchas** — common rejection reasons not in Meta's docs

## Sources to Investigate

- [Meta Business Platform docs — Message Templates](https://developers.facebook.com/docs/whatsapp/business-management-api/message-templates)
- [Meta Graph API — Template Components reference](https://developers.facebook.com/docs/whatsapp/business-management-api/message-templates/components)
- Evolution-API template validation source code
- Community forums / Stack Overflow for undocumented rejections
