---
title: WABA Voice Calling — Programmable Voice via WhatsApp
trigger_condition: After core WABA features are stable (Template CRUD, Reactions, Catalog) and there's demand for voice capabilities
planted_date: 2026-07-25
context: gsd-explore session — "Twilio lightweight" vision requires voice. Brazil has 94% WhatsApp penetration — WhatsApp voice IS the phone line.
scope: Separate milestone track (not part of current v1.x messaging milestones)
---

# WABA Voice Calling — Programmable Voice via WhatsApp

## Vision

Transform PerGo from a messaging-only CPaaS into a messaging + voice CPaaS by leveraging Meta's WABA Voice Calling API. In Brazil (94% WhatsApp penetration), WhatsApp voice calling is functionally equivalent to having a SIP trunk — without PSTN costs.

## Meta API Capabilities (Confirmed via Research)

- **GA** for Cloud API accounts with ≥2,000 daily conversations
- **Two architectures**: WebRTC (Graph API signaling) and SIP Trunking (TLS/SRTP to PBX/SBC, OPUS codec)
- **Limits**: 5,000 concurrent calls, 10,000 outbound business-initiated/24h
- **What Meta provides**: Call signaling + raw media stream
- **What Meta does NOT provide**: Recording, transcription, conferencing, IVR, DTMF — all must be implemented on PerGo's infrastructure

## Suggested Milestone Sequence

### Milestone A: Call Signaling Primitives
- Initiate outbound voice call via API (`POST /messages` with `type: "voice_call"`)
- Receive inbound call webhooks (ringing, answered, ended, missed)
- Call status tracking in dispatches table
- Admin UI: call log with duration, status, contact
- WebRTC signaling relay for browser-based agent answering

### Milestone B: SIP Trunking Integration
- SIP endpoint configuration in connection settings
- Route WABA voice calls to external PBX/SBC via SIP
- OPUS codec negotiation
- Call transfer / hold primitives

### Milestone C: Call Recording
- Capture WebRTC/SIP media stream server-side
- Store recordings (local filesystem or S3-compatible)
- Recording playback in admin UI and via API
- Privacy controls: consent-based recording, retention policies
- Webhook event: `call.recording.available`

### Milestone D: AI Transcription
- Integrate with Whisper (self-hosted) or Deepgram/AssemblyAI (cloud)
- Real-time or post-call transcription
- Transcription stored alongside recording
- Searchable call transcripts in admin UI
- Webhook event: `call.transcription.available`
- Language detection (critical for Brazil: Portuguese + regional variations)

### Milestone E: IVR & Call Routing (Stretch)
- DTMF detection and routing
- Programmable call flows (TwiML-equivalent?)
- Queue management for inbound calls
- Transfer between agents/departments

## Architectural Considerations

- **Media server**: Need a media server component (e.g., Janus, Pion WebRTC in Go) for recording/bridging
- **Storage**: Recordings can be large — S3/MinIO integration needed
- **Latency**: Voice is real-time — different performance profile than messaging
- **Compliance**: Call recording laws vary by jurisdiction (Brazil: one-party consent in most states)
- **Cost**: Each WABA voice call opens a conversation (Meta billing applies)

## Chatwoot Reference Implementation

Chatwoot Enterprise built WhatsApp voice with:
- In-browser agent call acceptance
- Call metadata logging in conversation timeline
- Recording storage
- AI transcription integration

## Anti-patterns

- Don't try to build a full PBX — PerGo is a CPaaS, it exposes primitives
- Don't mix voice milestones into messaging milestones — different complexity profile
- Don't skip WebRTC-in-Go research spike before committing to architecture
