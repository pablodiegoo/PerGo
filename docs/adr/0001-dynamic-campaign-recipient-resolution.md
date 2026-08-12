# Dynamic Execution-Time Campaign Recipient Resolution

We have decided to move Campaign Tag resolution from Campaign Creation time (API layer) to Campaign Execution time (Broadcaster Engine). 

Previously, when a Campaign was created with a Tag (e.g. "VIP"), the contacts were queried immediately and hardcoded into `campaign_recipients`.
Now, the `Create` API simply stores the `tag_ids` array on the `campaigns` table. At execution time, the Broadcaster Engine evaluates the tags (using an OR/Union approach), merges them with any static CSV uploads provided at creation time, and deduplicates by the channel-specific Identity (e.g. phone number). Empty resolutions at execution time are logged but considered a valid state.

This guarantees that scheduled campaigns will broadcast to the exact state of the segment at the moment of execution, rather than an outdated snapshot from when the campaign was created. The trade-off is that we can no longer provide a perfectly accurate recipient count on the Campaign creation success screen.
