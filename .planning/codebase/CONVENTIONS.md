# Conventions Map

This document establishes the coding standards, patterns, error handling conventions, database access style, and logging standards in PerGo.

## 1. Code Style and Idiomatic Go

- **Style Guide**: Conforms to standard Go formatting (`gofmt`, `goimports`).
- **Naming Rules**:
  - Go symbols: camelCase for variables and private functions, PascalCase for structs, public functions, and exported methods.
  - DB schema: snake_case for tables, columns, and indexes.
- **Package Integrity**: Dependencies are kept clean. No ORMs, and minimal runtime dependencies outside standard library, `pgx`, `nats.go`, and `whatsmeow`.

## 2. Error Handling & Propagation

- **Explicit Checks**: Errors are handled immediately using standard `if err != nil` loops.
- **Error Wrapping**: Wrap errors to add context when bubbling up through service layers:
  ```go
  if err != nil {
      return fmt.Errorf("failed to load session credentials: %w", err)
  }
  ```
- **API Error Responses**: Standard API controllers map errors to `domain.ErrorResponse` returning structured JSON fields:
  ```go
  return c.JSON(http.StatusTooManyRequests, domain.ErrorResponse{
      Code:    "queue_full",
      Message: "per-session message queue limit exceeded",
  })
  ```

## 3. Structured Logging (slog)

- **Library**: Uses Go standard library `log/slog`.
- **Key-Value Context**: Avoid formatting log messages. Pass structured attributes instead:
  ```go
  slog.Error("failed to publish outbound message", "error", err, "trace_id", traceID, "connection_id", connID)
  ```
- **Trace Propagation**: Every log in the request-handling pathway must include `"trace_id"` for tracking and debugging correlation.

## 4. Database Access Style

- **No ORM**: Raw SQL queries are written inline directly inside `internal/repository/`.
- **Parameterization**: All queries must use parameterized placeholders (`$1`, `$2`) to prevent SQL injection.
- **PGX Queries**:
  - Single rows are scanned using `pool.QueryRow`.
  - Collections are queried using `pool.Query` and closed via `defer rows.Close()`:
  ```go
  rows, err := r.pool.Query(ctx, query, workspaceID)
  if err != nil {
      return nil, err
  }
  defer rows.Close()
  ```

## 5. Templ & HTMX UI Conventions

- **Server-Driven UI**: Echo handlers return HTML fragments rendered by compiled `templ` layouts rather than JSON.
- **Out-of-Band (OOB) Updates**: Use `hx-swap-oob="true"` to update widgets (like unread counters) outside the main target pane:
  ```html
  <span id="inbox-unread-badge" hx-swap-oob="true" class="...">3</span>
  ```
- **HTMX Triggers**: Fire custom JavaScript events from HTTP responses using `HX-Trigger` headers:
  ```go
  c.Response().Header().Set("HX-Trigger", `{"showToast":{"text":"Nova mensagem"}}`)
  ```
- **Separation of Concerns**: Avoid writing complex client-side JS. Keep state in the database and let HTMX poll/render state changes.
