# 3. Directory Structure

Domain-oriented packages, not MVC layers. Each package is importable on
its own, depends only on `internal/platform` for infrastructure, and
exposes a small surface via constructor functions.

```
pergo/
├── cmd/
│   └── pergo/
│       └── main.go                 # wire deps, start HTTP + workers
├── internal/
│   ├── platform/                   # cross-cutting infra, no business logic
│   │   ├── postgres/
│   │   │   ├── pool.go             # *pgxpool.Pool constructor
│   │   │   ├── migrations/         # embed/*.sql, goose-driven
│   │   │   └── audit_repo.go       # batched audit insert (pgx.CopyFrom)
│   │   ├── nats/
│   │   │   ├── jetstream.go        # stream/consumer provisioning
│   │   │   ├── producer.go         # Publish with Trace-ID header
│   │   │   └── consumer.go         # Pull-subscribe helper
│   │   ├── crypto/
│   │   │   ├── aesgcm.go           # Seal/Open with per-row nonce
│   │   │   └── apikey.go           # SHA-256 hash + prefix split
│   │   ├── trace/
│   │   │   └── trace.go            # context key, generate, inject, extract
│   │   ├── backoff/
│   │   │   └── backoff.go          # exponential w/ jitter
│   │   ├── breaker/
│   │   │   └── breaker.go          # minimal 3-state circuit breaker
│   │   └── server/
│   │       └── echo.go             # Echo instance, middleware, pprof mount
│   │
│   ├── messaging/                  # core domain
│   │   ├── message.go              # MessagePayload, DispatchReceipt, Status
│   │   ├── ingest.go               # POST /messages handler
│   │   ├── routing.go              # fallback pipeline (RoutingEngine)
│   │   ├── queue.go                # JetStream producer (enqueue)
│   │   └── worker.go               # JetStream pull consumer loop
│   │
│   ├── channel/                    # provider adapters (>=2 impls -> interface earned)
│   │   ├── dispatcher.go           # type Dispatcher interface { Dispatch(ctx,*Message) error }
│   │   ├── registry.go             # map[string]Dispatcher, per-workspace build
│   │   ├── whatsappweb/
│   │   │   ├── adapter.go          # implements Dispatcher
│   │   │   └── client.go           # whatsmeow.NewClient wrapper
│   │   ├── whatsappcloud/
│   │   │   └── adapter.go          # WABA REST via net/http
│   │   └── telegram/
│   │       └── adapter.go          # Telegram Bot API via net/http
│   │
│   ├── session/                    # connection lifecycle
│   │   ├── registry.go             # RWMutex map[JID]*Session
│   │   ├── session.go              # one Session = one ws client + goroutine
│   │   ├── manager.go              # connect/disconnect/reconnect w/ backoff
│   │   └── store.go                # AES-GCM encrypt/decrypt of device row
│   │
│   ├── webhook/                    # outbound webhook delivery
│   │   ├── dispatcher.go           # POST to consumer URL w/ retries
│   │   └── worker.go               # JetStream consumer for webhook queue
│   │
│   ├── audit/                      # compliance logging engine
│   │   ├── event.go                # AuditEvent struct
│   │   ├── sink.go                 # type Sink interface { Record(context.Context, AuditEvent) error }
│   │   ├── buffer.go               # bounded chan + batch writer (Fan-in)
│   │   └── pg_sink.go              # implements Sink via postgres.audit_repo
│   │
│   ├── apikey/                     # auth
│   │   ├── auth.go                 # Echo middleware: parse, hash, lookup
│   │   └── repo.go                 # pgx-backed key store + in-mem cache
│   │
│   └── admin/                      # control panel (Templ + HTMX)
│       ├── server.go               # routes, HX-Request detection
│       ├── views/                  # *.templ -> generated *_templ.go
│       │   ├── layout.templ
│       │   ├── dashboard.templ
│       │   ├── sessions.templ
│       │   └── qr.templ
│       └── handlers.go             # workspace, session, audit handlers
│
├── migrations/                     # source of truth, symlinked into embed
│   └── *.sql
├── deploy/
│   ├── docker/
│   └── compose.yaml                # pergo + postgres + nats
├── go.mod
├── Makefile                        # run, test, lint, templ generate, migrate
└── AGENTS.md
```

## Package dependency rules (enforced by `goimports` + review)

```
platform  ──► (nothing internal)
audit     ──► platform
channel   ──► platform, messaging (types only)
session   ──► platform, channel (whatsappweb only)
messaging ──► platform, channel, audit, session
webhook   ──► platform, audit
admin     ──► platform, session, messaging (read-only)
cmd       ──► everything (composition root)
```

- No package imports `cmd`.
- `platform` imports nothing in `internal/`.
- `channel` does **not** import `messaging/worker` — the dependency is
  inverted via the consumer-side `Dispatcher` interface defined in
  `channel/dispatcher.go`.
- `session` depends on `channel/whatsappweb` concretely (WhatsApp Web is
  the only session-ful channel); other channels are stateless REST and
  do not need the session manager.

## Why this shape

- **Domain packages own their types.** `messaging.MessagePayload` is not
  a "model" in an `models/` bag; it lives next to the code that
  transforms it.
- **`platform/` is the only place that knows about pgx / nats / whatsmeow
  plumbing.** Swapping the broker or DB driver touches one subtree.
- **Adapters are siblings, not a hierarchy.** `whatsappweb`, `whatsappcloud`,
  `telegram` share an interface but no base struct — no `BaseAdapter`
  Java-ism.
- **`cmd/pergo` is the sole composition root.** No `internal/app`
  "god package"; `main.go` is allowed to be 150 lines of wiring.
