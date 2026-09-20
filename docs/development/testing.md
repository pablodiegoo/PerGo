# Testing Strategy & Guidelines

This guide documents the testing strategy for PerGo, explaining how to write, execute, and validate unit and integration test suites.

---

## 1. Test Categories

### Unit Tests
Validate isolated functions, encryption algorithms, slug generators, and payload parsers without external dependencies.
```bash
make test
```

### Concurrency & Race Detection
Validates concurrency safety across WebSocket sessions, NATS consumer workers, and message dispatch pipelines using Go's `-race` detector:
```bash
make test-race
```

### Integration Tests
Execute end-to-end flows against real PostgreSQL and NATS JetStream instances (using testcontainers or local docker infrastructure):
```bash
make test-integration
```

---

## 2. Best Practices

- **Test at Public Seams:** Test only at pre-agreed module interfaces (e.g. `channel.Dispatcher`, `outbound.DispatchOrchestrator`) rather than asserting on internal unexported functions.
- **Table-Driven Tests:** Structure test cases into table slices with clear test scenario names and expected outcomes.
- **No External Test Frameworks:** Use standard library `testing` package with assertions from `github.com/stretchr/testify/assert` and `require`.
- **Database Cleanup:** Tests utilizing database tables should execute within transactions or perform cleanup to avoid cross-test interference.
