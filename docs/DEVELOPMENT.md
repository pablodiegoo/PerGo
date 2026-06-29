# Guia de Desenvolvimento (Development Guide)

Este documento descreve as diretrizes de código, a estrutura de pastas e as melhores práticas adotadas no desenvolvimento do **PerGo**.

---

## Pilha de Tecnologia e Requisitos

* **Go 1.26+** (floor 1.25)
* **Echo v5** (`github.com/labstack/echo/v5`) — Router HTTP e Middlewares
* **a-h/templ** (`github.com/a-h/templ`) — Compile-time type-safe HTML componentes
* **pgx/v5** (`github.com/jackc/pgx/v5`) — Driver PostgreSQL de alta performance
* **nats.go** (`github.com/nats-io/nats.go`) — Broker de filas JetStream duráveis
* **whatsmeow** (`go.mau.fi/whatsmeow`) — Conector não-oficial para WhatsApp Web

---

## Estrutura de Diretórios

O projeto segue um layout orientado ao domínio e infraestrutura:

```
├── cmd/
│   └── pergo/            # Ponto de entrada (Main), instanciação e graciosidade de shutdown
├── docs/                 # Documentação técnica e guias
├── internal/
│   ├── api/              # Handlers HTTP, roteador e middlewares da API / Admin
│   ├── channel/          # Adaptadores e lógica de disparo para cada provedor (Telegram, WABA, etc.)
│   ├── config/           # Parsing das variáveis de ambiente
│   ├── domain/           # Entidades e modelos de domínio (mensagens, workspaces, audit)
│   ├── platform/         # Utilitários globais de infraestrutura (criptografia, banco, NATS, shutdown)
│   ├── repository/       # Repositórios SQL (Workspace, APIKey, Credentials)
│   └── session/          # Gerenciamento de conexões de sessão (WhatsApp Web)
├── static/               # Assets estáticos (CSS, JS do HTMX/WebSockets)
├── templates/            # Componentes visuais (.templ) compilados para Go
└── migrations/           # Arquivos de migração SQL gerenciados pelo Goose
```

---

## Fluxo de Trabalho de Desenvolvimento

### 1. Modificar Páginas de UI (`.templ`)
Sempre que fizer alterações nos arquivos `.templ` dentro de `templates/`, você precisa compilar os mesmos para gerar os arquivos Go correspondentes:
```bash
make generate
```
*Dica: Durante o desenvolvimento ativo na UI, o `make dev` usando `air` detecta e executa essa compilação automaticamente a cada salvamento.*

### 2. Rodar Testes Unitários
Utilize o comando rápido para testes simples (sem detector de concorrência):
```bash
make test
```
Para garantir que não há condições de corrida nas goroutines concorrentes dos workers de envio:
```bash
make test-race
```

### 3. Análise Estática (Linter)
Antes de enviar qualquer commit, certifique-se de que o código segue as diretrizes do linter do projeto:
```bash
make lint
```

---

## Princípios de Design de Código

1. **Evitar Acoplamento:** Mantenha a camada HTTP (handlers) fina, delegando regras de envio e manipulação de credenciais para a camada `internal/channel/` ou `internal/repository/`.
2. **Injeção de Dependência Manual:** Não utilize frameworks mágicos de DI. As dependências devem ser passadas de forma explícita nos construtores (ex: `NewWorkspaceHandler(repo, extURL)`).
3. **Gerenciamento de Erros:** Sempre trate os erros, envelopando-os com contextos adicionais (`fmt.Errorf("falha ao salvar credencial: %w", err)`). Retorne erros como último argumento das assinaturas de função.
4. **Resiliência de Goroutines:** Ao disparar processos em paralelo (como o worker de consumo de NATS), trate panics internos para evitar a queda abrupta da aplicação.
