# Development & Contributing Guide

This guide outlines code standards, build processes, and repository guidelines for developing PerGo.

---

## 1. Development Toolchain

- **Language:** Go 1.25+ (Toolchain 1.26+)
- **Live Reload:** Air (`github.com/air-verse/air`)
- **Template Engine:** Templ (`github.com/a-h/templ`)
- **Linter:** `golangci-lint`

---

## 2. Local Workflow

### Running the Server
```bash
# Start background infrastructure (PostgreSQL & NATS)
make infra

# Start PerGo with live hot-reloading
make dev
```

### Modifying UI Components (`.templ`)
Whenever modifying HTML templates in `templates/`:
```bash
make generate
```
`make dev` runs `templ generate` automatically on file changes when using Air.

---

## 3. Code Standards & Static Analysis

Before submitting pull requests:
1. **Formatting:** All Go code must be formatted with `go fmt ./...`.
2. **Linting:** Run `golangci-lint`:
   ```bash
   make lint
   ```
3. **Tests:** All tests must pass with race detection:
   ```bash
   make test-race
   ```

For detailed testing guidelines, see the **[Testing Guide](testing.md)**.
