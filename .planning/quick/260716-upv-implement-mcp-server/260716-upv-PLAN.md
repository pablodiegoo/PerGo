# Plan: Implement MCP Server Support (260716-upv)

Implement a Model Context Protocol (MCP) server within PerGo using `github.com/mark3labs/mcp-go`. The server will support both stdio transport (as a CLI subcommand `pergo mcp`) and SSE transport (via `GET /api/mcp/sse` on the Echo server). It will expose tools for listing workspaces, listing connections, searching contacts, sending messages, and querying audit logs.

## Must-Haves
- `github.com/mark3labs/mcp-go` dependency added to `go.mod`.
- MCP server package `internal/api/mcp` created with tools:
  - `list_workspaces`
  - `list_connections`
  - `search_contacts`
  - `send_message`
  - `get_audit_logs`
- CLI command `pergo mcp` running the MCP server over Stdio.
- HTTP route `GET /api/mcp/sse` and HTTP POST endpoint for SSE client registration and message routing.
- Integration tests verifying the MCP tool executions.

## Tasks

### Task 1: Add Dependency and Implement internal/api/mcp/server.go
- File: [internal/api/mcp/server.go](file:///home/pablo/Coding/PerGo/internal/api/mcp/server.go)
- Action:
  - Fetch `github.com/mark3labs/mcp-go` using `go get`.
  - Create the `internal/api/mcp` package.
  - Define the `PerGoMCPServer` struct holding core repositories (`WorkspaceRepository`, `ConnectionRepository`, `ContactRepository`, `AuditRepository`) and the `OutboundProcessor` for message ingestion.
  - Register the MCP tools with correct JSON schemas.
  - Implement tool call handlers mapping arguments and invoking internal services.
  - Implement SSE transport handler adapters.
- Verify: Compile successfully and run mock tool calls.

### Task 2: Wire CLI command and Echo route
- Files:
  - [cmd/pergo/main.go](file:///home/pablo/Coding/PerGo/cmd/pergo/main.go)
- Action:
  - Update `main()` in `cmd/pergo/main.go` to check if `os.Args[1] == "mcp"`.
  - If `mcp` is passed, initialize database/NATS/services and start the MCP server in Stdio mode (`server.ServeStdio(mcpServer)`).
  - Add routes `GET /api/mcp/sse` and the companion POST routes to the Echo router to handle SSE-based MCP clients.
- Verify: Run `go build` and verify CLI subcommand launches without errors.

### Task 3: Write Integration Tests
- File: [internal/api/mcp/server_test.go](file:///home/pablo/Coding/PerGo/internal/api/mcp/server_test.go)
- Action:
  - Create test suite initializing the database pool and NATS connection (using local test infrastructure).
  - Invoke the registered MCP tools directly and verify results (e.g. `list_workspaces`, `send_message` routing).
- Verify: Run `go test ./internal/api/mcp/...` and ensure all tests pass.
