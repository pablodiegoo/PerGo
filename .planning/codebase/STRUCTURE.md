# Structure Map

This document describes the directory layout, folder purposes, key files, and naming conventions of the PerGo codebase.

## 1. Directory Layout

The project follows a standard Go project layout with a clean separation of concerns between code modules and frontend assets:

```
├── cmd/
│   └── pergo/                 # Application Entry Point
│       └── main.go            # dependency injection, server boot
├── internal/                  # Private application code
│   ├── api/                   # API Routing & Web Layer
│   │   ├── handler/           # API handlers (Public & Admin Console)
│   │   └── middleware/        # Middlewares (tenant, auth, trace, telemetry)
│   ├── channel/               # Outbound Channel Drivers
│   │   ├── telegram/          # Telegram Bot channel adapter
│   │   └── whatsapp/          # WABA & WhatsApp Web adapters
│   ├── config/                # Environment variables parsing
│   ├── domain/                # Core business entities & validations
│   ├── platform/              # Shared infrastructure clients
│   │   ├── crypto/            # AES-256-GCM encryption utilities
│   │   ├── postgres/          # DB connection & migrations
│   │   ├── queue/             # NATS JetStream client & queue workers
│   │   └── storage/           # S3 compatible storage client
│   ├── repository/            # DB Access Layer (pure SQL queries)
│   └── session/               # whatsmeow client daemon management
├── static/                    # Static UI assets (CSS, JS, logo)
├── templates/                 # Compiled type-safe Templ templates
│   ├── components/            # Reusable HTML snippets (chat, item, list, modals)
│   ├── layout/                # Global layout wrapper & sidebars
│   └── pages/                 # Full panel views (dashboard, inbox, playground)
└── .planning/                 # Project documentation and GSD progress maps
```

## 2. Key Package Responsibilities

### `cmd/pergo/`
- **`main.go`**: The application bootstrapping routine. Responsible for reading configurations, instantiating database connection pools (`pgxpool`), initializing NATS connections, setting up S3 clients, running goose migrations, wiring repositories to handlers, and starting the Echo server.

### `internal/api/handler/`
- **`message.go`**: Handles public outbound message ingestion at `POST /api/v1/messages`.
- **`telegram_webhook.go` & `waba_webhook.go`**: Ingestion webhook handlers for third-party events.
- **`admin/`**: Houses all controllers for the operator dashboard console:
  - **`inbox.go`**: Serves `/admin/inbox` endpoints, thread list loading, active chat retrieval, and real-time polling updates.
  - **`workspace.go`**: Manages workspaces, tenant configurations, and credential setups.

### `internal/session/`
- **`manager.go`**: Manages the lifecycles of whatsmeow client sessions for multiple WhatsApp Web numbers within a workspace.
- **`inbound_processor.go`**: Orchestrates duplicate checks, S3 uploads for media events, and session updates when a new inbound WhatsApp message is captured.

### `internal/platform/queue/`
- **`jetstream.go`**: Defines the streams for `MESSAGES` and `WEBHOOKS` and manages durable pull consumers.
- **`worker.go`**: The dispatcher worker loop that reads messages from JetStream and sends them down the channel adapters.
- **`webhook_worker.go`**: Delivers events to registered client endpoints, pushing failing payloads to the DLQ.

### `templates/`
- Renders server-side templates type-compiled into Go by `a-h/templ`.
- **`components/chat_panel.templ`**: UI structures for the message display scroll area and input box.
- **`components/conv_list.templ`**: Conversational sidebar selector listing active contacts.

## 3. Naming & Coding Conventions

- **File Naming**: Lowercase with underscores (`inbound_processor.go`, `recipient_session.go`, `014_inbox_read_status.sql`).
- **Package Naming**: Single-word, lowercase packages (`domain`, `repository`, `session`).
- **Interfaces**: Postfixed with their behavior where applicable (`Publisher`, `ConnectionFinder`, `Writer`).
- **Test files**: Co-located within the package, postfixed with `_test.go`.
- **Generated Templates**: Postfixed with `_templ.go` (created automatically by running `templ generate`).
