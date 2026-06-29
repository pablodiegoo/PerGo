# Como Começar (Getting Started)

Este guia ajuda você a configurar e executar o **PerGo** em seu ambiente local para fins de desenvolvimento e testes.

---

## Pré-requisitos

Certifique-se de ter os seguintes itens instalados na sua máquina:

* **Go 1.26+** (se preferir compilar localmente fora do Docker)
* **Docker** e **Docker Compose**
* **Air** (ferramenta de hot-reload para Go) — Instale com:
  ```bash
  go install github.com/air-verse/air@latest
  ```
* **Templ** (compilador de templates HTML para Go) — Instale com:
  ```bash
  go install github.com/a-h/templ/cmd/templ@latest
  ```

---

## 1. Clonar e Acessar o Repositório

Navegue até o diretório do projeto:
```bash
cd Coding/OmniGo
```

---

## 2. Configurar Variáveis de Ambiente

Copie o arquivo de exemplo do ambiente e configure as chaves necessárias:
```bash
cp .env.example .env
```
Abra o arquivo `.env` gerado no seu editor de preferência e confira os valores. Para testes de desenvolvimento, os valores padrões são suficientes. Certifique-se de que a variável `PERGO_KEK_BASE64` contém uma chave válida Base64 de 32 bytes (gerada usando `openssl rand -base64 32`).

---

## 3. Subir Infraestrutura Local

O PerGo depende do banco de dados **PostgreSQL** e do broker **NATS** (com JetStream ativo). Suba estes serviços de forma isolada usando o Docker Compose local:
```bash
make infra
```
*Isto irá iniciar o banco em `localhost:5432` e o NATS em `localhost:4222`.*

---

## 4. Compilar e Executar em Desenvolvimento

Gere os templates de UI estáticos e inicie o servidor com hot-reload automático (ele recompila o projeto a cada alteração de arquivo):
```bash
make dev
```
Na primeira inicialização, o PerGo executará automaticamente as migrações embutidas do banco de dados (usando `goose`), criando as tabelas necessárias: `workspaces`, `api_keys`, `channel_credentials` e `audit_logs`.

---

## 5. Primeiro Acesso e Configuração

1. Abra o navegador em: **`http://localhost:8080/admin`**
2. Faça login com as credenciais administrativas configuradas no seu `.env`:
   * **Usuário:** `admin`
   * **Senha:** *Sua senha configurada em `PERGO_ADMIN_PASSWORD`* (padrão de desenvolvimento: `pergo-dev-2026`).
3. Crie o seu primeiro **Workspace** (ex: "Empresa Principal" ou "Ambiente de Testes").
4. Clique no Workspace criado para acessar a tela de detalhes:
   * **Gerar API Key:** Clique em *Generate Key* e anote a chave gerada. Você usará essa chave no cabeçalho `Authorization: Bearer <key>` para enviar mensagens pela API REST do PerGo.
   * **Configurar Canais:** Adicione as credenciais de envio para o Telegram Bot, WhatsApp Cloud ou conecte o WhatsApp Web escaneando o QR Code gerado pelo painel.
