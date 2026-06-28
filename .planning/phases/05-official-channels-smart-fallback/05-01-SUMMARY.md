---
phase: 05-official-channels-smart-fallback
plan: 01
subsystem: channels
tags: [waba, telegram, credentials, encryption, registry]

requires:
  - phase: none
    provides: credentials and official channels adapters
provides:
  - "Database schema for storing encrypted channel credentials"
  - "CredentialsRepository supporting Save/Get/Delete with KEK/DEK AES-256-GCM envelope encryption"
  - "WABA REST Adapter supporting text/template message payloads and comprehensive error classification"
  - "Telegram Bot REST Adapter supporting sendMessage payloads and comprehensive error classification"
  - "Composition root wiring of credentials repository and official channel adapters registered in the channel dispatcher registry"
affects: [05-02]

tech-stack:
  added: []
  patterns: [envelope-encryption, rest-adapter, terminal-error-handling]

key-files:
  created:
    - internal/platform/postgres/migrations/004_create_channel_credentials.sql
    - internal/repository/credentials.go
    - internal/repository/credentials_test.go
    - internal/channel/whatsapp/waba.go
    - internal/channel/whatsapp/waba_test.go
    - internal/channel/telegram/telegram.go
    - internal/channel/telegram/telegram_test.go
  modified:
    - cmd/pergo/main.go
    - .gitignore

key-decisions:
  - "Utilized pgx / ON CONFLICT DO UPDATE (UPSERT) for credentials repository to prevent duplicate channel configurations per workspace"
  - "Isolated KEK handling in main.go, falling back to a development-only key for local development ease but emitting a security warning"
  - "Classified Meta Graph API error codes (e.g., 131030, 131047) and Telegram API error codes (e.g., 400, 403) explicitly into terminal or transient errors to drive the smart fallback engine"
  - "Designed REST adapters with customizable base URLs and HTTP clients to enable lightweight HTTP mocking in unit tests"

patterns-established:
  - "Envelope-encrypted credentials storage matching requirements of data privacy (AES-256-GCM KEK/DEK flow)"
  - "Error classification for third-party HTTP/REST gateways to allow retry or smart fallback redirection"

requirements-completed: [WABA-01, TGRAM-01]

coverage:
  - id: WABA-01-01
    description: "WABA Adapter implements channel.Dispatcher and sends POST to Meta Messages endpoint"
    requirement: WABA-01
    verification:
      - kind: unit
        ref: "internal/channel/whatsapp/waba_test.go#TestWABADispatch/Success_Freeform_Text_Message"
        status: pass
      - kind: unit
        ref: "internal/channel/whatsapp/waba_test.go#TestWABADispatch/Success_Template_Message"
        status: pass
  - id: WABA-01-02
    description: "Meta API rate limits and terminal errors are parsed and correctly classified"
    requirement: WABA-01
    verification:
      - kind: unit
        ref: "internal/channel/whatsapp/waba_test.go#TestWABADispatch/Terminal_Error_-_Number_Not_on_WhatsApp_(131030)"
        status: pass
      - kind: unit
        ref: "internal/channel/whatsapp/waba_test.go#TestWABADispatch/Terminal_Error_-_Outside_24h_Window_(131047)"
        status: pass
      - kind: unit
        ref: "internal/channel/whatsapp/waba_test.go#TestWABADispatch/Transient_Error_-_Rate_Limit_(130429)"
        status: pass
  - id: TGRAM-01-01
    description: "Telegram Adapter implements channel.Dispatcher and sends POST to sendMessage endpoint"
    requirement: TGRAM-01
    verification:
      - kind: unit
        ref: "internal/channel/telegram/telegram_test.go#TestTelegramDispatch/Success_Send_Message"
        status: pass
  - id: TGRAM-01-02
    description: "Telegram API rate limits and terminal errors are parsed and correctly classified"
    requirement: TGRAM-01
    verification:
      - kind: unit
        ref: "internal/channel/telegram/telegram_test.go#TestTelegramDispatch/Terminal_Error_-_Chat_Not_Found_(400)"
        status: pass
      - kind: unit
        ref: "internal/channel/telegram/telegram_test.go#TestTelegramDispatch/Terminal_Error_-_Bot_Blocked_(403)"
        status: pass
      - kind: unit
        ref: "internal/channel/telegram/telegram_test.go#TestTelegramDispatch/Transient_Error_-_Too_Many_Requests_(429)"
        status: pass
  - id: CRED-01-01
    description: "Credentials are saved, encrypted via KEK/DEK envelope, and correctly decrypted on retrieve"
    requirement: WABA-01
    verification:
      - kind: integration
        ref: "internal/repository/credentials_test.go#TestCredentialsRepository"
        status: pass
