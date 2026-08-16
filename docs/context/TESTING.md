# Guia de Testes (Testing Guide)

Este guia documenta a estratégia de testes do **PerGo**, explicando como executar, escrever e validar o comportamento do gateway de mensagens.

---

## Tipos de Teste

### 1. Testes Unitários
Testam funções isoladas, algoritmos de criptografia e helpers de parse.
* Executam rápido e não necessitam de infraestrutura externa (Postgres/NATS).
* Para rodar os testes unitários da aplicação:
  ```bash
  make test
  ```

### 2. Testes de Concorrência
Garantem a segurança das sessões WebSocket e filas concorrentes sob condições concorrentes severas.
* Utilizam a flag `-race` do Go para detectar conflitos de escrita e leitura de memória.
* Executar via Makefile:
  ```bash
  make test-race
  ```

### 3. Testes de Integração
Validam o fluxo de ponta a ponta: do recebimento do webhook ou envio de requisição HTTP REST, passando pelo enfileiramento no NATS JetStream, até a gravação da auditoria no PostgreSQL.
* Utilizam bancos reais de teste e mocks dos canais de mensagem externos (Telegram/WABA).
* Podem rodar utilizando contêineres locais ou via `testcontainers-go`.

---

## Como Escrever Testes

### Boas Práticas:
* **Uso de Interfaces para Mocks:** Em vez de depender de servidores reais do Telegram ou Meta, utilize as interfaces `Publisher` (para filas) e `Dispatcher` (para disparos de canais) injetadas via construtor nos handlers. Isso possibilita criar mocks leves em tempo de compilação.
* **Testes Baseados em Tabelas (Table-Driven Tests):** Estruture testes usando cenários em arrays para cobrir múltiplos fluxos lógicos e erros esperados de forma concisa.
* **Cleanup após Testes:** Sempre libere conexões a bancos de dados temporários e encerre canais de NATS após a execução de cada suite.

---

## Executando Linter e Validação Estática

Além dos testes funcionais, o linter estático verifica boas práticas de escrita, imports não utilizados e segurança básica:
```bash
make lint
```
Certifique-se de que o linter roda localmente sem reportar alertas ou erros antes de enviar alterações de código.

---

## Test Framework and Setup
O PerGo utiliza o framework de testes padrão do Go (`testing`) sem dependências de frameworks externos (como `testify`), mantendo a base de código leve e livre de dependências extras desnecessárias.

### Pré-requisitos e Configuração:
* **Banco de Dados de Teste (PostgreSQL):** Os testes de integração requerem um banco de dados PostgreSQL. Por padrão, a conexão é feita usando a URL configurada na variável de ambiente `PERGO_DATABASE_URL` (para repositório/migrações) ou `PERGO_TEST_DSN` (para o comando principal). Se não estiverem definidas, os testes utilizam os seguintes valores padrão:
  * Repositórios: `postgres://postgres:postgres@localhost:5432/pergo?sslmode=disable`
  * Comando principal: `postgres://postgres:postgres@localhost:5432/pergo_test?sslmode=disable`
* **NATS Server:** Os testes que validam o enfileiramento via JetStream se conectam em `nats://localhost:4222` (`nats.DefaultURL`).
* **Mocks de Redes e Serviços Externos:** Serviços de terceiros (como WhatsApp Web/whatsmeow, Telegram e WABA) utilizam mocks ou dublês de teste injetados por meio das interfaces `Publisher` (para o NATS) e `Dispatcher` (para disparos de canais).
* **Tratamento de Indisponibilidade (Skip):** Se o banco de dados PostgreSQL ou o servidor NATS local não estiverem acessíveis durante a execução dos testes de integração, os testes serão automaticamente ignorados (`t.Skip`), permitindo que a suite de testes unitários rápidos execute sem falhar.

---

## Running Tests
Os testes podem ser executados usando o `go test` padrão ou através dos targets definidos no `Makefile`:

1. **Testes Unitários e Rápidos (Sem race detector):**
   Executa testes filtrados pelo modo `-short`, pulando operações mais pesadas.
   ```bash
   make test
   ```
   *Equivalente a:* `go test ./... -short`

2. **Testes de Concorrência (Com race detector):**
   Executa todos os testes da aplicação ativando o detector de condições de corrida.
   ```bash
   make test-race
   ```
   *Equivalente a:* `go test ./... -race -count=1`

3. **Testes de Integração Sequenciais (Banco de Dados Compartilhado):**
   Como as diferentes suítes de teste realizam migrações Goose concorrentes no mesmo banco de dados, eles devem ser rodados sequencialmente (`-p 1`) e com a URL correta:
   ```bash
   PERGO_DATABASE_URL="postgres://postgres:postgres@localhost:5433/pergo?sslmode=disable" go test -p 1 ./...
   ```

3. **Execução Manual com Filtro:**
   Para rodar apenas um pacote ou teste específico:
   ```bash
   go test ./internal/repository -run TestConnectionRepository
   ```

---

## Writing New Tests
Ao criar novos recursos ou corrigir bugs no PerGo, siga estas convenções de escrita de testes:

1. **Testes Baseados em Tabelas (Table-Driven Tests):**
   Use estruturas de dados para definir múltiplos cenários de entrada e saídas esperadas, iterando sobre eles em um único teste do Go.
   ```go
   func TestExemplo(t *testing.T) {
       tests := []struct {
           name    string
           input   string
           wantErr bool
       }{
           {"sucesso", "dado_valido", false},
           {"erro", "", true},
       }
       for _, tt := range tests {
           t.Run(tt.name, func(t *testing.T) {
               // Executa teste
           })
       }
   }
   ```

2. **Uso de Helpers para Preparação de Banco de Dados:**
   Utilize ou crie funções auxiliares (como `getTestPoolWithMigrations` em [connection_test.go](file:///home/pablo/Coding/PerGo/internal/repository/connection_test.go#L16)) para obter um pool com migrações aplicadas automaticamente. Certifique-se de realizar o `Clean up` (deleção dos registros das tabelas afetadas) antes ou depois de cada teste para evitar interferências.

3. **Mocking e Injeção de Dependências:**
   Não instancie clientes de API reais em testes. Defina e injete interfaces que possam ser substituídas por mocks simples na suite de testes.

---

## Coverage Requirements
Para garantir a qualidade e robustez do PerGo, os testes de cobertura devem cobrir a lógica de negócio principal (`internal/domain`), os fluxos de despacho de mensagens (`internal/channel`) e as filas/workers (`internal/platform/queue`).

1. **Gerar Relatório de Cobertura:**
   Use o comando abaixo para gerar o perfil de cobertura detalhado:
   ```bash
   go test -coverprofile=coverage.out ./...
   ```

2. **Visualizar Relatório de Cobertura:**
   Para visualizar a cobertura de forma gráfica no navegador:
   ```bash
   go tool cover -html=coverage.out
   ```

3. **Meta de Cobertura:**
   Recomenda-se manter a cobertura geral de código acima de **80%**, com foco em 100% de cobertura nos algoritmos de criptografia e tratamento de concorrência.

---

## CI Integration
<!-- VERIFY: CI integration using GitHub Actions runner with PostgreSQL and NATS services -->
O PerGo está preparado para ser integrado a pipelines de Integração Contínua (CI) como o GitHub Actions. Toda alteração proposta em Pull Requests ou mesclada na branch principal deve executar automaticamente a validação de qualidade:

### Exemplo de Workflow do GitHub Actions (`.github/workflows/ci.yml`):
```yaml
name: CI

on:
  push:
    branches: [ master, main ]
  pull_request:
    branches: [ master, main ]

jobs:
  test:
    name: Test and Lint
    runs-on: ubuntu-latest

    services:
      postgres:
        image: postgres:16
        env:
          POSTGRES_USER: postgres
          POSTGRES_PASSWORD: postgres
          POSTGRES_DB: pergo
        ports:
          - 5432:5432
        options: >-
          --health-cmd pg_isready
          --health-interval 10s
          --health-timeout 5s
          --health-retries 5

      nats:
        image: nats:2.10-alpine
        ports:
          - 4222:4222
        options: >-
          --health-cmd "nc -z localhost 4222"
          --health-interval 10s
          --health-timeout 5s
          --health-retries 5

    steps:
      - name: Checkout Code
        uses: actions/checkout@v4

      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version: '1.25.x'
          cache: true

      - name: Run Tests
        env:
          PERGO_DATABASE_URL: postgres://postgres:postgres@localhost:5432/pergo?sslmode=disable
          PERGO_NATS_URL: nats://localhost:4222
        run: go test ./... -race -count=1

      - name: Run golangci-lint
        uses: golangci/golangci-lint-action@v6
        with:
          version: v1.64
```
