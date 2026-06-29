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
