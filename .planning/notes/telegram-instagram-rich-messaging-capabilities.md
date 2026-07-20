---
title: Telegram & Instagram Rich Messaging Capabilities
date: 2026-07-20
context: Explored deeper channel integrations based on Chatwoot and omnichannel platform references.
---

# Telegram & Instagram Rich Messaging Capabilities

## Context
Following the implementation of WABA rich messaging, we explored expanding these capabilities to Telegram and Instagram to build a competitive edge against other omnichannel platforms.

## Research Findings
- **Telegram (Rich & Threaded)**: Standard inputs map cleanly into Telegram's `inline_keyboard`. It also has strong support for conversational threading via `reply_to_message_id` and forum topics.
- **Instagram (Stories vs. Interactivity)**: Other platforms support fetching Instagram Stories via the Graph API when users reply to them. However, they largely neglect outbound rich features like Quick Replies, Generic Templates, and Ice Breakers.

## Decision
We will build a comprehensive integration for BOTH platforms:
1. **Telegram**: Support inline keyboards, force reply, and threaded forum topics.
2. **Instagram**: Build story ingestion handling AND fill the industry gap by actively supporting outbound Quick Replies and Generic Templates.
