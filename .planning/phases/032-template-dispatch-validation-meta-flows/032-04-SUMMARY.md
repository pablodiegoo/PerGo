<Plan 032-04 Summary: RSA/AES Data Exchange middleware endpoint>
## What was built
- Implemented Flow Cryptography Utilities (`DecryptRSA`, `DecryptAES128GCM`, `EncryptAES128GCM`, `InvertIV`) in `internal/platform/crypto/encrypt.go`.
- Created RSA key loading function `LoadRSAPrivateKey` in `internal/platform/crypto/rsa.go` that decrypts from AES-256-GCM encrypted connection credentials and falls back to environment variable `WABA_FLOWS_PRIVATE_KEY`.
- Implemented `HandleFlowDataExchange` Echo handler mapped to `POST /api/v1/waba/flows/data-exchange` to process Meta's data exchange requests securely.

## Files changed
- `internal/platform/crypto/encrypt.go` — Added RSA and AES cryptography utility functions.
- `internal/platform/crypto/flow_test.go` — Added unit tests for cryptographic operations.
- `internal/platform/crypto/rsa.go` — Added RSA key loader from credentials JSON.
- `internal/platform/crypto/rsa_test.go` — Added tests for RSA key loading.
- `internal/api/handler/flow_data_exchange.go` — Added Data Exchange endpoint Echo handler.
- `internal/api/handler/flow_data_exchange_test.go` — Added test scaffold for the endpoint.

## Tests
- `TestFlowCrypto` (AES-128-GCM and RSA decryption, IV inversion) pass successfully.
- `TestLoadRSAPrivateKey` (valid/invalid key loading and fallbacks) pass successfully.

## Decisions made
- Defined `FlowDataExchangeHandler` to isolate the data exchange webhook logic from the standard inbound webhook logic.
- Mocked DB operations in handler test to skip it, due to tight coupling with `pgxpool`.
</Plan 032-04 Summary: RSA/AES Data Exchange middleware endpoint>
