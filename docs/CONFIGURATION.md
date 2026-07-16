# Configuração do Sistema (Environment Variables)

O **PerGo** segue o padrão de 12-factor app para configurações, utilizando variáveis de ambiente para gerenciar a conexão com bancos de dados, chaves de criptografia, armazenamento S3 de mídias e comportamento do servidor.

Abaixo está o mapeamento detalhado das variáveis de ambiente disponíveis.

---

## Variáveis do Servidor e Banco de Dados

### `PERGO_DATABASE_URL`
* **Descrição:** String de conexão (DSN) com o banco de dados PostgreSQL.
* **Padrão:** `postgres://postgres:postgres@localhost:5432/pergo?sslmode=disable`
* **Exemplo de Produção:** `postgres://user:password@10.128.0.3:5432/pergo_db?sslmode=require`

### `PERGO_NATS_URL`
* **Descrição:** Endereço de conexão com o servidor NATS (com suporte a JetStream ativado).
* **Padrão:** `nats://localhost:4222`

### `PERGO_SERVER_PORT`
* **Descrição:** Porta TCP onde o servidor HTTP do PerGo (Console Admin + API pública + Webhooks) irá escutar.
* **Padrão:** `8080`

### `PERGO_DEBUG_PORT`
* **Descrição:** Porta TCP exclusiva para endpoints de profiling (`pprof`) e monitoramento/métricas. Fica isolada por questões de segurança.
* **Padrão:** `6060`

### `PERGO_EXTERNAL_URL`
* **Descrição:** A URL externa pública segura através da qual o seu servidor PerGo é acessado pela internet. Essencial para o registro automático de webhooks (Telegram/Meta).
* **Padrão:** `http://localhost:8080`
* **Exemplo de Produção:** `https://api.pergo.app`

---

## Segurança e Painel Administrativo

### `PERGO_KEK_BASE64`
* **Descrição:** Chave Mestra de Criptografia (Key Encryption Key) no formato **Base64**. Deve corresponder a uma chave de exatamente 32 bytes (256 bits) para ser utilizada no algoritmo AES-256-GCM. Usada para criptografar as credenciais dos canais no banco de dados.
* **Padrão:** *(Sem padrão — obrigatório definir em produção)*
* **Como gerar:**
  ```bash
  openssl rand -base64 32
  ```

### `PERGO_ADMIN_PASSWORD`
* **Descrição:** Senha de acesso para a console de gerenciamento (`/admin`).
* **Padrão:** `pergo-dev-2026`

---

## Armazenamento S3 (Mídias e Anexos)

As variáveis abaixo configuram o armazenamento S3 compatível (MinIO, AWS S3, Cloudflare R2, etc.) utilizado para guardar mídias recebidas ou enviadas.

| Variável | Variável Alternativa | Padrão | Descrição |
|----------|----------------------|--------|-----------|
| `PERGO_S3_ENDPOINT` | `S3_ENDPOINT` | `""` (vazio) | URL customizada para provedores S3 (ex: `https://s3.amazonaws.com` ou URL do R2/MinIO) |
| `PERGO_S3_BUCKET` | `S3_BUCKET` | `""` (vazio) | Nome do Bucket S3 para armazenar os arquivos |
| `PERGO_S3_ACCESS_KEY` | `S3_ACCESS_KEY` | `""` (vazio) | Chave de Acesso S3 |
| `PERGO_S3_SECRET_KEY` | `S3_SECRET_KEY` | `""` (vazio) | Chave Secreta S3 |
| `PERGO_S3_REGION` | `S3_REGION` | `"us-east-1"` | Região S3 |
| `PERGO_S3_USE_PATH_STYLE` | `S3_USE_PATH_STYLE` | `"false"` | Se `"true"`, força o cliente S3 a usar requests em formato path-style (`endpoint/bucket/file`) |

---

## Tabela Geral de Variáveis de Ambiente

Abaixo está o mapeamento unificado de todas as variáveis de ambiente aceitas pelo PerGo, seus valores padrão e se são obrigatórias ou opcionais.

| Variável | Variável Alternativa | Valor Padrão | Obrigatória? | Descrição |
|----------|----------------------|--------------|--------------|-----------|
| `PERGO_DATABASE_URL` | - | `postgres://postgres:postgres@localhost:5432/pergo?sslmode=disable` | Sim | URL (DSN) para conexão com o banco de dados PostgreSQL. |
| `PERGO_NATS_URL` | - | `nats://localhost:4222` | Sim | Endereço do servidor NATS JetStream para enfileiramento de mensagens. |
| `PERGO_SERVER_PORT` | - | `8080` | Não | Porta do servidor HTTP. |
| `PERGO_DEBUG_PORT` | - | `6060` | Não | Porta para endpoints de profiling (`pprof`) e expvar (deve ser isolada em produção). |
| `PERGO_EXTERNAL_URL` | - | `http://localhost:8080` | Não | URL pública externa usada para compor links de mídia e callbacks. |
| `PERGO_KEK_BASE64` | - | *Vazio (inseguro por padrão)* | Sim (Produção) | Chave mestre de criptografia (Key Encryption Key) AES-256-GCM em formato Base64. |
| `PERGO_ADMIN_PASSWORD` | - | `pergo-dev-2026` | Não | Senha de acesso para a console de gerenciamento `/admin`. |
| `PERGO_SESSION_SECRET` | - | *Gerado aleatoriamente* | Não | Chave secreta de sessão de cookie (usada no login). Se vazia, os logins expiram a cada reinício de processo. |
| `PERGO_MAX_WHATSAPP_CONNECTIONS` | - | `5` | Não | Limite de conexões ativas de WhatsApp Web (whatsmeow) simultâneas permitidas por workspace. |
| `PERGO_S3_ENDPOINT` | `S3_ENDPOINT` | `""` | Não | Endpoint personalizado do servidor S3 (ex: AWS, MinIO, Cloudflare R2). |
| `PERGO_S3_BUCKET` | `S3_BUCKET` | `""` | Não | Nome do Bucket no storage S3. |
| `PERGO_S3_ACCESS_KEY` | `S3_ACCESS_KEY` | `""` | Não | Access Key ID para o bucket S3. |
| `PERGO_S3_SECRET_KEY` | `S3_SECRET_KEY` | `""` | Não | Secret Access Key para o bucket S3. |
| `PERGO_S3_REGION` | `S3_REGION` | `us-east-1` | Não | Região onde o bucket S3 está hospedado. |
| `PERGO_S3_USE_PATH_STYLE` | `S3_USE_PATH_STYLE` | `false` | Não | Se definida como `true`, força o uso de URLs estilo Path (comum em MinIO local). |

---

## Estrutura de Configuração (Go Struct)

No código fonte, a configuração é carregada do ambiente no início da aplicação em uma estrutura Go chamada [Config](file:///home/pablo/Coding/PerGo/internal/config/config.go#L10) definida em [internal/config/config.go](file:///home/pablo/Coding/PerGo/internal/config/config.go).

```go
type Config struct {
	DatabaseURL    string
	NATSUrl        string
	ServerPort     string
	DebugPort      string
	KEKBase64      string
	KEKBytes       []byte // Decodificado de KEKBase64
	AdminPassword  string
	S3Endpoint     string
	S3Bucket       string
	S3AccessKey    string
	S3SecretKey    string
	S3Region       string
	S3UsePathStyle bool
	ExternalURL    string
}
```

A função [Load](file:///home/pablo/Coding/PerGo/internal/config/config.go#L28) em [internal/config/config.go](file:///home/pablo/Coding/PerGo/internal/config/config.go) lê essas variáveis e faz o fallback de valores adequadamente.

Outras variáveis especiais também são acessadas de maneira pontual na inicialização de módulos específicos:
- `PERGO_SESSION_SECRET` é acessado através de [getSessionSecret](file:///home/pablo/Coding/PerGo/internal/api/middleware/session.go#L95) no arquivo [internal/api/middleware/session.go](file:///home/pablo/Coding/PerGo/internal/api/middleware/session.go) para criptografar cookies de sessão.
- `PERGO_MAX_WHATSAPP_CONNECTIONS` é verificado em [StartPairing](file:///home/pablo/Coding/PerGo/internal/session/qr.go#L58) em [internal/session/qr.go](file:///home/pablo/Coding/PerGo/internal/session/qr.go) para impor um teto de conexões ativas do canal WhatsApp Web.

---

## Configurações Obrigatórias vs Opcionais

### Configurações Obrigatórias
* **Banco de Dados PostgreSQL (`PERGO_DATABASE_URL`)**: O banco é o sistema de registro central. Sem ele, a aplicação falhará em inicializar.
* **NATS JetStream (`PERGO_NATS_URL`)**: O PerGo usa NATS JetStream para enfileiramento confiável de mensagens e execução assíncrona. Sem a conexão com o broker, o servidor não sobe.
* **Chave Mestra de Criptografia (`PERGO_KEK_BASE64`) (Apenas em Produção)**: Usada para encriptar com AES-256-GCM as credenciais de provedores e sessões dos canais gravadas no banco de dados. Caso não seja configurada em ambiente de desenvolvimento, o servidor emitirá um aviso e utilizará uma chave insegura padrão de desenvolvimento. **Nunca utilize o padrão inseguro em produção.**

### Configurações Opcionais
* **Armazenamento S3**: As variáveis `PERGO_S3_*` são opcionais. Se não configuradas, o sistema continuará operando normalmente para o envio de mensagens de texto simples, porém as funcionalidades de envio e recebimento de mídias anexas (imagens, áudios e documentos) que exigem persistência temporária/permanente de arquivos estarão indisponíveis.
* **Segurança de Cookies (`PERGO_SESSION_SECRET`)**: Caso não seja fornecida, o sistema gerará uma chave de 32 bytes randômica a cada inicialização da aplicação. No entanto, isso fará com que qualquer administrador autenticado na console seja desconectado toda vez que o servidor for reiniciado. Recomendado definir para ambientes de produção estáveis.
* **Porta do Servidor (`PERGO_SERVER_PORT` e `PERGO_DEBUG_PORT`)**: Porta principal por padrão escuta na `8080`, e a de debug/profile na `6060`.
* **Limite de WhatsApp Web (`PERGO_MAX_WHATSAPP_CONNECTIONS`)**: Limita a quantidade de conexões ativas via WhatsApp Web utilizando whatsmeow por workspace (padrão `5`). Se ultrapassado, tentativas de emparelhamento de QR Code adicionais serão rejeitadas.

---

## Sobrescritas e Configuração por Ambiente

O PerGo carrega variáveis do sistema operacional de forma direta para respeitar os princípios do *12-Factor App*. No entanto, o fluxo de sobrescrita e setup varia por ambiente:

### Ambiente de Desenvolvimento (Local)
Para facilitar o desenvolvimento, o projeto disponibiliza um arquivo modelo de exemplo [.env.example](file:///home/pablo/Coding/PerGo/.env.example).
1. Copie o arquivo de exemplo para criar a sua configuração local:
   ```bash
   cp .env.example .env
   ```
2. Modifique os valores desejados. Por exemplo, a URL do banco PostgreSQL local ou credenciais do MinIO de desenvolvimento.
3. Carregue o arquivo no ambiente do seu terminal antes de rodar o PerGo:
   ```bash
   source .env && go run ./cmd/pergo
   ```

### Ambiente Docker / Docker Compose
No arquivo [docker-compose.yml](file:///home/pablo/Coding/PerGo/docker-compose.yml), a configuração local é estruturada com base nos containers.
O arquivo carrega as configurações mapeando variáveis de ambiente locais do host ou definindo valores default. Por exemplo, a URL do banco de dados aponta para o nome do serviço no compose (`postgres:5432`) ao invés de `localhost`.
Para personalizar as variáveis rodando via compose, é possível definir um arquivo `.env` na raiz do projeto, que será injetado automaticamente nos serviços do compose.

### Ambiente de Produção
Em produção, utilize a infraestrutura de orquestração (Kubernetes, AWS ECS, systemd, etc.) para injetar as variáveis de forma segura no processo:
* Mantenha a chave `PERGO_KEK_BASE64` guardada de forma segura (ex: AWS Secrets Manager, Vault) e nunca a exponha em commits de código.
* Force conexões SSL seguras no PostgreSQL configurando `sslmode=require` na variável `PERGO_DATABASE_URL`.
* Bloqueie acessos externos à porta de debug `PERGO_DEBUG_PORT` (padrão `6060`), pois ela expõe rotas críticas `/debug/pprof/` de profiling de memória/CPU que podem ser exploradas para ataques de negação de serviço.

---

## Valores Padrão

Os valores padrão para todas as variáveis de ambiente opcionais do PerGo estão definidos na coluna **Valor Padrão** da [Tabela Geral de Variáveis de Ambiente](#tabela-geral-de-variaveis-de-ambiente). Se uma variável opcional não for definida no seu ambiente local ou de produção, o sistema adotará automaticamente o respectivo valor padrão documentado.
