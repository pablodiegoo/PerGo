# Research Findings: Rich Messaging Capabilities

Based on an analysis of Chatwoot, Omni, and Evolution-API in the `context/inspiration` directory, here are the key findings regarding mapped API capabilities for Telegram and Instagram:

1. **Telegram Interactive Keyboards**: Both platforms natively map interactive elements to Telegram. Chatwoot translates its internal `input_select` format into Telegram's `inline_keyboard` (utilizing `callback_data`), while Omni exposes this via its `canSendButtons` capability. 
2. **Telegram Threads & Replies**: Threading context is well-supported. Chatwoot directly maps `reply_to_message_id` to maintain conversation flow. Omni takes this further by supporting `canReplyToMessage` and natively handling Telegram's forum topics through its `canHandleThreads` flag.
3. **Instagram Story Handling**: Chatwoot provides dedicated support for Instagram Stories via the Meta Graph API. It actively listens for story replies, fetches the story object (`get_story_object_from_source_id`), and maps it as an `ig_story` attachment within the conversation. It also handles deleted or expired stories.
4. **Limited Instagram Rich Messaging**: Advanced Instagram capabilities like Quick Replies, Generic Templates, and Ice Breakers are not explicitly mapped across the analyzed platforms. Chatwoot's Instagram integration (unlike its Facebook integration) limits outbound messaging strictly to standard text and media attachments.

These findings show that while Telegram's rich features are broadly adopted, Instagram's interactive capabilities remain underutilized.
