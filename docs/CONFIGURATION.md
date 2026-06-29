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
* **Descrição:** Senha de acesso para o usuário administrador (`admin`) na console de gerenciamento (`/admin`).
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
