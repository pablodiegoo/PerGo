---
title: Audio Speech-to-Text Transcription
trigger_condition: After S3 media storage is implemented and there's demand for AI-powered features
planted_date: 2026-07-25
context: gsd-explore session — WABA features gap analysis. Differentiator feature for PerGo as a CPaaS.
spike_needed: true
---

# Audio Speech-to-Text Transcription

## Idea

Automatically transcribe incoming audio/voice messages and include the transcription text in the webhook payload and conversation history. In Brazil, where WhatsApp audio messages dominate business communication, this is a genuine differentiator.

## How It Would Work

1. Inbound audio message received from Meta webhook
2. PerGo downloads the audio file (via media.Store)
3. Audio is sent to transcription service (async, non-blocking)
4. Transcription result is:
   - Stored alongside the message in conversation history
   - Emitted as enriched field in the webhook event: `"transcription": { "text": "...", "language": "pt-BR", "confidence": 0.95 }`
   - Displayed in admin UI chat view below the audio player

## Transcription Options

| Option | Pros | Cons |
|--------|------|------|
| **Whisper (self-hosted)** | No API cost, data sovereignty, LGPD compliant | Requires GPU or high CPU, adds infrastructure complexity |
| **Deepgram API** | Low latency, high accuracy, easy integration | External dependency, per-minute cost, data leaves infra |
| **AssemblyAI API** | Good Portuguese support, speaker diarization | External dependency, cost |
| **Google Speech-to-Text** | High accuracy, many languages | GCP lock-in, cost |

## Spike Scope

Before implementation, spike should validate:
1. Whisper self-hosted performance: latency for a 60s audio on 2 vCPU (PerGo's target footprint)
2. Audio format handling: WhatsApp sends OGG/OPUS — does Whisper accept it natively or need conversion?
3. Async pipeline: how to handle transcription latency (5-30s) without blocking the webhook response
4. Cost analysis: Deepgram/AssemblyAI per-minute cost vs self-hosted Whisper infrastructure cost
5. Language detection: auto-detect Portuguese vs Spanish vs English

## Architectural Considerations

- Transcription must be **opt-in per workspace** (not all businesses want/need it)
- Must be **async** — don't block webhook processing on transcription
- Consider a `transcription.available` webhook event for late-arriving transcriptions
- Storage: transcription text stored in conversations table, linked to original audio message

## Dependencies

- REQ-MEDIA-OBJECT-STORAGE (S3 seed) — audio files need to be accessible to the transcription worker
- Background worker infrastructure (NATS JetStream consumer)

## Inspiration

- Evolution API: OpenAI Whisper integration for automatic audio transcription in webhook payloads
