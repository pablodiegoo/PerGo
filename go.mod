module github.com/pablojhp.omnigo

go 1.26.4

require (
	github.com/a-h/templ v0.3.1020
	github.com/google/uuid v1.6.0
	github.com/jackc/pgx/v5 v5.10.0
	github.com/labstack/echo/v5 v5.2.1
	github.com/nats-io/nats.go v1.52.0
	github.com/pressly/goose/v3 v3.27.1
	go.mau.fi/whatsmeow v0.0.0-20260622185415-5f04eac6dbbb
	golang.org/x/time v0.14.0
)

require (
	filippo.io/edwards25519 v1.2.0 // indirect
	github.com/beeper/argo-go v1.1.2 // indirect
	github.com/coder/websocket v1.8.15 // indirect
	github.com/elliotchance/orderedmap/v3 v3.1.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/klauspost/compress v1.18.5 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.21 // indirect
	github.com/mfridman/interpolate v0.0.2 // indirect
	github.com/nats-io/nkeys v0.4.15 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	github.com/petermattis/goid v0.0.0-20260330135022-df67b199bc81 // indirect
	github.com/rs/zerolog v1.35.1 // indirect
	github.com/sethvargo/go-retry v0.3.0 // indirect
	github.com/vektah/gqlparser/v2 v2.5.27 // indirect
	go.mau.fi/libsignal v0.2.2 // indirect
	go.mau.fi/util v0.9.10 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/crypto v0.53.0 // indirect
	golang.org/x/exp v0.0.0-20260611194520-c48552f49976 // indirect
	golang.org/x/net v0.56.0 // indirect
	golang.org/x/sync v0.21.0 // indirect
	golang.org/x/sys v0.46.0 // indirect
	golang.org/x/text v0.38.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
	github.com/aws/aws-sdk-go-v2 v0.0.0 // indirect
	github.com/aws/aws-sdk-go-v2/service/s3 v0.0.0 // indirect
)

replace github.com/aws/aws-sdk-go-v2 => ./internal/mocks/aws-sdk-go-v2

replace github.com/aws/aws-sdk-go-v2/service/s3 => ./internal/mocks/aws-sdk-go-v2-s3
