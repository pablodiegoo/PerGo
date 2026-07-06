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
│   ├── platform/         # Utilitários globais de infraestrutura (criptografia, banco, NATS, shutdown, migrations)
│   ├── repository/       # Repositórios SQL (Workspace, APIKey, Connection)
│   └── session/          # Gerenciamento de conexões de sessão (WhatsApp Web)
├── static/               # Assets estáticos (CSS, JS do HTMX/WebSockets)
└── templates/            # Componentes visuais (.templ) compilados para Go
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

---

## Local Setup

Para configurar o ambiente de desenvolvimento local, siga os passos abaixo:

1. **Instalar Pré-requisitos**:
   - **Go 1.26+** (floor 1.25)
   - **Docker** e **Docker Compose** para gerenciar serviços de infraestrutura.
   - **Air** (hot-reload): `go install github.com/air-verse/air@latest`
   - **Templ** (compilador HTML): `go install github.com/a-h/templ/cmd/templ@latest`

2. **Configurar Variáveis de Ambiente**:
   - Copie o arquivo de exemplo:
     ```bash
     cp .env.example .env
     ```
   - Edite o arquivo `.env` gerado. Certifique-se de configurar a variável `PERGO_KEK_BASE64` com uma chave AES-256 válida gerada via Base64 (ex: `openssl rand -base64 32`).

3. **Iniciar a Infraestrutura**:
   - Suba os contêineres do PostgreSQL e NATS JetStream:
     ```bash
     make infra
     ```
   - Isso iniciará o PostgreSQL na porta `5433` (mapeada para `5432`) e o NATS na porta `4222` localmente.

4. **Executar a Aplicação**:
   - Inicie o servidor em modo de desenvolvimento com hot-reload automático:
     ```bash
     make dev
     ```
   - O PerGo executará automaticamente as migrações SQL pendentes (via Goose) no banco de dados na primeira inicialização.
   - O painel administrativo estará acessível em `http://localhost:8080/admin` usando as credenciais definidas em seu `.env` (`PERGO_ADMIN_PASSWORD`).

---

## Build Commands

O projeto gerencia tarefas comuns de build e desenvolvimento por meio do `Makefile`. Os principais alvos disponíveis são:

* **Desenvolvimento e Build**:
  - `make dev`: Inicia o servidor local com recarregamento em tempo real (hot-reload) via `air` e geração de templates via `templ`.
  - `make build`: Compila o binário de produção otimizado em `./bin/pergo` (executa `make generate` automaticamente).
  - `make generate`: Compila os arquivos de template `.templ` em arquivos Go (`templ generate ./...`).
  - `make clean`: Remove binários de compilação e diretórios temporários (`./bin/` e `./tmp/`).

* **Infraestrutura e Contêineres**:
  - `make infra`: Inicia apenas os serviços de PostgreSQL e NATS em background usando Docker Compose.
  - `make infra-down`: Derruba apenas a infraestrutura local.
  - `make prod`: Constrói a imagem Docker local e sobe todos os serviços (aplicativo + banco + mensageria) em produção via Docker Compose.
  - `make prod-logs`: Acompanha a saída de logs do aplicativo rodando em produção.
  - `make prod-down`: Encerra e remove todos os contêineres de produção.

---

## Code Style (golangci-lint, go fmt)

Para garantir consistência e legibilidade no repositório, o código deve seguir as seguintes diretrizes:

1. **Formatação de Código (Go)**:
   - Sempre utilize a ferramenta padrão `go fmt ./...` para formatar arquivos de código Go antes de realizar commits.
2. **Formatação de Templates**:
   - Os arquivos de template HTML (`.templ`) devem ser gerados e validados usando `templ generate ./...` ou formatados através de comandos específicos da ferramenta `templ fmt`.
3. **Análise Estática (Linter)**:
   - Utilizamos o `golangci-lint` para validação de boas práticas e detecção de possíveis falhas de segurança/performance.
   - Execute o linter antes de submeter alterações:
     ```bash
     make lint
     ```
   - Todos os arquivos editados devem passar sem avisos ou erros reportados pelo linter.

---

## Branch Conventions

Adotamos regras de nomenclatura e estrutura de branch para manter o fluxo de desenvolvimento limpo:

* **Branch Principal**:
  - A branch principal do repositório é a `master`. Todas as novas funcionalidades e correções são integradas nela.
* **Ramificação de Branches**:
  - Crie sempre uma nova branch a partir de `master` para desenvolver qualquer tarefa.
  - Nomeie suas branches usando o padrão de prefixo descritivo em letras minúsculas:
    - `feat/<descricao>`: para novas funcionalidades.
    - `fix/<descricao>`: para correção de bugs.
    - `refactor/<descricao>`: para refatoração de código sem alteração lógica/funcional.
    - `docs/<descricao>`: para alterações na documentação.
* **Mensagens de Commit**:
  - Seguimos a convenção de **Conventional Commits** para descrever as alterações na linha do tempo:
    - Exemplo: `feat(ui): implement workspace dashboard view`
    - Exemplo: `fix(session): repair disconnect event race condition`

---

## PR Process

O processo de integração de código no **PerGo** segue as etapas abaixo:

1. **Testes Locais**:
   - Antes de abrir um Pull Request (PR), certifique-se de que os testes locais de concorrência e linter estejam passando com sucesso:
     ```bash
     make test-race
     make lint
     ```
2. **Abertura do Pull Request**:
   - Submeta sua branch e abra um PR apontando para a branch `master`.
   - Descreva de forma objetiva no corpo do PR as alterações realizadas e quaisquer dependências de configuração ou migração de banco introduzidas.
3. **Revisão de Código (Code Review)**:
   - Todo PR deve receber revisão e aprovação de pelo menos um desenvolvedor antes de ser considerado pronto para integração.
4. **Integração/Merge**:
   - Uma vez aprovado e com o pipeline de integração contínua verde, o PR deve ser mesclado na branch `master` preferencialmente utilizando a estratégia de **Squash and Merge** para manter o histórico de commits linear e legível.
