# ─────────────────────────────────────────────────────────────
#  PerGo — Makefile
#  Uso:
#    make dev        → hot-reload (carrega .env automaticamente)
#    make prod       → build + sobe tudo via docker compose
#    make infra      → sobe só postgres + nats (sem o app)
#    make infra-down → derruba infra
#    make build      → compila o binário para ./bin/pergo
#    make generate   → regenera arquivos templ
#    make test       → testes rápidos
#    make test-race  → testes com race detector
#    make lint       → golangci-lint
# ─────────────────────────────────────────────────────────────

.PHONY: dev prod infra infra-down build generate test test-race lint clean help

# Carrega variáveis do .env se ele existir (sem expor no shell pai)
ifneq (,$(wildcard .env))
  include .env
  export
endif

BINARY     := ./bin/pergo
BUILD_FLAGS := -ldflags="-s -w"

# ─── Dev ─────────────────────────────────────────────────────

## dev: hot-reload com air (reinicia automaticamente a cada mudança)
dev: _check-air _check-templ
	@echo "→ Iniciando em modo dev com hot-reload..."
	@air

# ─── Produção ────────────────────────────────────────────────

## prod: build da imagem Docker e sobe tudo via docker compose
prod:
	@echo "→ Fazendo build e subindo em produção..."
	@docker compose --env-file .env up --build -d
	@echo "✓ Rodando em http://localhost:$${PERGO_SERVER_PORT:-8080}"

## prod-logs: acompanha os logs do container em produção
prod-logs:
	@docker compose --env-file .env logs -f pergo

## prod-down: derruba todos os containers
prod-down:
	@docker compose --env-file .env down

# ─── Infra local (dev sem container do app) ──────────────────

## infra: sobe apenas postgres e nats para desenvolvimento local
infra:
	@echo "→ Subindo infra (postgres + nats)..."
	@docker compose --env-file .env up postgres nats -d
	@echo "✓ Postgres em localhost:5432 | NATS em localhost:4222"

## infra-down: derruba a infra
infra-down:
	@docker compose --env-file .env down postgres nats

# ─── Build ───────────────────────────────────────────────────

## build: compila o binário otimizado para ./bin/pergo
build: generate
	@echo "→ Compilando..."
	@mkdir -p bin
	@go build $(BUILD_FLAGS) -o $(BINARY) ./cmd/pergo
	@echo "✓ Binário em $(BINARY)"

## generate: regenera os arquivos Go a partir dos templates templ
generate: _check-templ
	@templ generate ./...

# ─── Qualidade ───────────────────────────────────────────────

## test: executa testes rápidos (sem race detector)
test:
	@go test ./... -short

## test-race: executa testes com race detector
test-race:
	@go test ./... -race -count=1

## lint: análise estática com golangci-lint
lint:
	@golangci-lint run

## clean: remove binários e arquivos temporários
clean:
	@rm -rf bin/ tmp/

# ─── Help ────────────────────────────────────────────────────

## help: lista todos os targets disponíveis
help:
	@echo ""
	@echo "  PerGo — comandos disponíveis:"
	@echo ""
	@grep -E '^## ' Makefile | sed 's/## /  make /' | column -t -s ':'
	@echo ""

# ─── Checks internos ─────────────────────────────────────────

_check-air:
	@which air > /dev/null 2>&1 || (echo "✗ 'air' não encontrado. Instale: go install github.com/air-verse/air@latest" && exit 1)

_check-templ:
	@which templ > /dev/null 2>&1 || (echo "✗ 'templ' não encontrado. Instale: go install github.com/a-h/templ/cmd/templ@latest" && exit 1)
