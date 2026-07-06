# Testing Map

This document describes the testing framework, test structures, mock strategies, and execution commands in PerGo.

## 1. Testing Framework

- **Standard Library**: PerGo uses Go's built-in `testing` package.
- **Assertions**: Assertions are handled using native Go comparison structures rather than external assertion packages. When checks fail, `t.Errorf` or `t.Fatalf` are called:
  ```go
  if got := conn.Channel; got != "whatsapp" {
      t.Errorf("expected channel 'whatsapp', got %q", got)
  }
  ```

## 2. Test Execution Commands

- **Run all unit/integration tests**:
  ```bash
  go test ./...
  ```
- **Run test suite with race detection**:
  ```bash
  go test -race -count=1 ./...
  ```
- **Run tests in a specific package**:
  ```bash
  go test -v ./internal/api/handler/admin/...
  ```
- **Clean test execution (disable caching)**:
  ```bash
  go test -count=1 ./...
  ```

## 3. Web Layer Testing (Echo Handlers)

- **Mock Context**: Handlers are tested by setting up a local Echo instance, constructing a custom request, and recording the response using `net/http/httptest.NewRecorder()`.
- **Authentication Bypass**: In tests, tenant and auth contexts are injected directly into the HTTP request context.
- **Example Pattern (`internal/api/handler/admin/inbox_test.go`)**:
  ```go
  e := echo.New()
  req := httptest.NewRequest(http.MethodPost, "/admin/inbox/send", strings.NewReader("body=Hello"))
  req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
  rec := httptest.NewRecorder()
  c := e.NewContext(req, rec)
  
  // Inject mock workspace
  ctx := context.WithValue(req.Context(), tenant.WorkspaceKey, mockWorkspaceID)
  c.SetRequest(req.WithContext(ctx))
  ```

## 4. Database Integration Testing

- **Local Integration Test DB**: Database repository tests require a live PostgreSQL test instance.
- **Transaction Cleanups**: Repositories are tested by inserting mock data, running database commands, asserting outcomes, and rolling back / cleaning up rows using defer statements or transaction structures to prevent test pollutions.
- **Schema Migrations**: Database tests run Goose migrations programmatically against the test DB context on initialization.

## 5. Mocking Strategies

- **Interface-Based Mocks**:
  - JetStream publish events are mocked by creating a struct that implements `Publisher`.
  - Database entities are mocked by implementing repository-specific interfaces (such as `ConnectionFinder` or `AuditRepository`).
- **AWS S3 Storage Mocks**: Replaced at compile-time using mock packages in `./internal/mocks/aws-sdk-go-v2` defined in `go.mod` replace directives.
