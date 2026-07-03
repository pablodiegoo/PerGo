# ADR-0004: Extract CredentialProvider port from crypto.Encryptor

**Status:** Proposed  
**Date:** 2026-07-03

## Context

Both `ConnectionRepository` and `CredentialsRepository` inject `*crypto.Encryptor` directly. Every repository test must set up a real AES-256-GCM encryptor with a valid 32-byte key. The encryption logic (DEK generation, GCM wrapping/unwrapping) has real complexity that shouldn't be exercised in every repository test.

## Decision

Introduce a `CredentialProvider` port owned by the repository package. The concrete `crypto.Encryptor` becomes an adapter that satisfies it.

### Port (in `internal/repository/`)

```go
type CredentialProvider interface {
    Encrypt(plaintext []byte) (ciphertext, keyID string, keyVersion int, error)
    Decrypt(ciphertext []byte) (plaintext, error)
}
```

### Adapters

| Adapter | Location | Purpose |
|---------|----------|---------|
| `crypto.Encryptor` | `internal/platform/crypto/` | Production — AES-256-GCM envelope encryption |
| `noopProvider` (test-only) | test files | Returns plain bytes unchanged |

### Consumers

- `ConnectionRepository` — `scanAndDecrypt`, `scanRowAndDecrypt`, `SaveCredentials`, `GetCredentials`
- `CredentialsRepository` — `Save`, `Get`

### Changed files

- `internal/repository/credential_provider.go` — new: port definition
- `internal/repository/connection.go` — `*crypto.Encryptor` → `CredentialProvider`
- `internal/repository/credentials.go` — `*crypto.Encryptor` → `CredentialProvider`
- `cmd/pergo/main.go` — pass `encryptor` as `CredentialProvider`

## Consequences

- **Testability:** repository tests use no-op provider — no key generation, no GCM operations
- **Seam:** swap encryption scheme (e.g., KMS-backed) without touching repositories
- **Two adapters justify the seam:** production Encryptor + test no-op provider
