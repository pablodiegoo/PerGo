# Guia de Implantação (Deployment Guide)

Este guia orienta na implantação do **PerGo** em ambientes de produção utilizando Docker e Docker Compose.

---

## Estrutura da Imagem Docker

O PerGo utiliza um arquivo `Dockerfile` multi-stage otimizado para produção:
1. **Stage 1 (Builder):** Compila o código Go e gera os templates do `a-h/templ` a partir de uma imagem base do Go com Alpine.
2. **Stage 2 (Runtime):** Utiliza uma imagem minimalista e segura do Google (**Distroless Static**), contendo apenas o binário compilado e os certificados de CA para conexões HTTPS externas com as APIs do Telegram e Facebook. Isso reduz drasticamente a superfície de ataque e o tamanho final da imagem.

---

## Implantação via Docker Compose

O arquivo `docker-compose.yml` padrão inclui três serviços:
1. **`postgres`**: Banco de dados PostgreSQL 16 com volume persistente em `pgdata`.
2. **`nats`**: Broker NATS com suporte a JetStream ativado (`-js` e persistência de dados em `natsdata`).
3. **`pergo`**: O próprio container da aplicação compilando a partir do diretório local.

### Comandos de Controle

Para compilar a imagem e iniciar toda a infraestrutura em background (produção):
```bash
make prod
```

Para acompanhar os logs de execução da aplicação PerGo:
```bash
make prod-logs
```

Para parar todos os serviços da aplicação:
```bash
make prod-down
```

---

## Lista de Verificação (Checklist) para Produção

Antes de colocar o servidor em produção, certifique-se de configurar as seguintes variáveis de ambiente no arquivo `.env`:

### 1. URL Externa Segura (HTTPS)
* **Variável:** `PERGO_EXTERNAL_URL`
* **Configuração:** Deve apontar para o domínio HTTPS público através do qual o servidor é acessível pela internet (ex: `https://api.pergo.meu-app.com`).
* **Importante:** Sem HTTPS e com domínios inválidos ou IPs locais, o registro automático de webhooks para o Telegram falhará e a Meta não validará a URL de callback do webhook para o WhatsApp Cloud.

### 2. Chave Mestra de Criptografia (KEK)
* **Variável:** `PERGO_KEK_BASE64`
* **Configuração:** Deve ser configurada com uma chave de criptografia de 32 bytes codificada em Base64.
* **Importante:** Nunca modifique essa chave após o início do uso do PerGo, pois todas as credenciais de canais existentes no banco de dados ficarão ilegíveis e corrompidas.
* **Gerar nova chave:**
  ```bash
  openssl rand -base64 32
  ```

### 3. Senha Administrativa Forte
* **Variável:** `PERGO_ADMIN_PASSWORD`
* **Configuração:** Defina uma senha forte de administrador para o acesso ao painel administrativo. Não utilize a senha padrão `pergo-dev-2026`.

### 4. Isolamento da Porta de Debug
* O PerGo expõe endpoints de profiling e expvar na porta `6060` (configurada via `PERGO_DEBUG_PORT`).
* **Atenção:** Garanta que a porta `6060` **não esteja exposta** para a internet pública e fique restrita apenas a acessos de monitoramento interno da sua infraestrutura/VPN.

---

## Ambientes de Implantação (Deployment Targets)

O **PerGo** foi projetado para rodar em infraestrutura containerizada:
* **Docker / Docker Compose**: Recomendado para implantações de nó único (single-node) em ambientes de homologação ou produção de médio porte. Toda a infraestrutura básica (PostgreSQL, NATS JetStream e PerGo) é gerenciada e orquestrada pelo Docker Compose, usando a rede interna compartilhada.
* **Containers Isolados**: O binário compilado pode rodar de forma isolada em qualquer orquestrador compatível com Docker (como AWS ECS, Google Cloud Run ou Kubernetes) contanto que tenha conectividade de rede com as instâncias externas do PostgreSQL e NATS JetStream. <!-- VERIFY: Compatibilidade com orquestradores de nuvem AWS ECS, Google Cloud Run e Kubernetes em produção -->

---

## Pipeline de Build (Build Pipeline)

O processo de build do PerGo é automatizado e encapsulado no [Dockerfile](file:///home/pablo/Coding/PerGo/Dockerfile) multi-stage, seguindo os seguintes passos:

1. **Geração de Código (Template compilation)**: A ferramenta `a-h/templ` é executada para compilar os templates HTML tipo-seguros localizados no diretório [templates/](file:///home/pablo/Coding/PerGo/templates) para código Go nativo (`templ generate ./...`).
2. **Resolução de Dependências**: As dependências declaradas no [go.mod](file:///home/pablo/Coding/PerGo/go.mod) são baixadas (`go mod download`).
3. **Compilação Estática**: O binário do Go é compilado com a flag `CGO_ENABLED=0` e flags de otimização `-ldflags="-w -s"` para remover tabelas de símbolos e informações de debug, resultando em um binário estático e enxuto.
4. **Construção da Imagem de Runtime**: O binário compilado, os arquivos estáticos de assets ([static/](file:///home/pablo/Coding/PerGo/static)) e os certificados de CA atualizados do builder são copiados para a imagem final baseada em `gcr.io/distroless/static-debian12`, garantindo uma imagem final com menos de 50MB e alta segurança.

Você também pode realizar o build local do binário (fora de containers) usando o comando:
```bash
make build
```

---

## Configuração do Ambiente (Environment Setup)

Para configurar o ambiente de produção do zero:

1. **Provisionar Infraestrutura de Apoio**: Suba uma instância segura do PostgreSQL (versão 15+) e um servidor NATS configurado com suporte a JetStream (com opções de persistência ativas).
2. **Criar Configurações locais/variáveis**: Crie um arquivo `.env` com base no arquivo modelo [.env.example](file:///home/pablo/Coding/PerGo/.env.example).
3. **Gerar Segredos**:
   - Defina a senha do administrador via `PERGO_ADMIN_PASSWORD`.
   - Gere uma chave de criptografia mestra segura de 32 bytes codificada em base64 usando:
     ```bash
     openssl rand -base64 32
     ```
     e salve-a na variável `PERGO_KEK_BASE64`.
   - Configure a URL externa pública estável e segura (`https`) em `PERGO_EXTERNAL_URL`.
4. **Executar Migrações de Banco de Dados**: Ao iniciar, o contêiner do PerGo executa automaticamente as migrações integradas do banco via `goose` e o setup interno do whatsmeow, preparando o esquema de banco de dados.

---

## Procedimento de Recuperação e Reversão (Rollback Procedure)

Caso ocorra um problema de estabilidade após uma atualização, siga o procedimento de rollback abaixo:

1. **Identificar a Versão Anterior**: Localize a tag de imagem Docker anterior estável ou o commit Git estável correspondente no seu pipeline de CD.
2. **Reter a Chave Mestra (KEK)**: 
   > [!IMPORTANT]
   > Durante qualquer processo de reversão ou deploy, a chave mestra `PERGO_KEK_BASE64` **NÃO deve ser alterada ou perdida**. Ela é o único meio de descriptografar os tokens de acesso e credenciais de canais existentes no banco de dados. Mudar a KEK resultará na impossibilidade de ler as sessões ativas e exigirá o re-emparelhamento de todos os canais de WhatsApp/Telegram.
3. **Reverter a Imagem/Binário**:
   - Atualize a referência da imagem no arquivo de deploy ou atualize o código local para o commit anterior.
   - Execute o deploy da versão antiga:
     ```bash
     docker compose up -d --build
     ```
4. **Migrações de Banco de Dados**: As migrações do banco de dados no PerGo usam esquemas compatíveis com versões anteriores (esquema aditivo). Se um rollback precisar desfazer alterações estruturais de banco de dados, utilize uma ferramenta de migração dedicada para rodar os comandos de *down* correspondentes. <!-- VERIFY: Compatibilidade de rollback de banco de dados em produção e comandos down automatizados -->

---

## Monitoramento (Monitoring)

O PerGo disponibiliza três mecanismos nativos para monitoramento de saúde e telemetria:

### 1. Probes de Saúde (Health & Readiness)
* **Liveness Probe (`GET /healthz`)**: Disponível na porta HTTP do servidor. Sempre retorna `200 OK` (corpo `ok`). Usado por orquestradores para verificar se o processo está respondendo.
* **Readiness Probe (`GET /readyz`)**: Disponível na porta HTTP do servidor. Executa conexões reais (Ping) ao banco de dados PostgreSQL e ao broker NATS JetStream. Retorna `200 OK` se ambos estiverem acessíveis, ou `503 Service Unavailable` em caso de falha de conexão.

### 2. Logs Estruturados
* Todos os logs da aplicação são direcionados para a saída padrão (`stdout`) estruturados em formato JSON usando o pacote nativo `log/slog`.
* As mensagens de logs incluem o atributo `trace_id` correlacionado de ponta a ponta na API HTTP e no processamento assíncrono em workers.

### 3. Métricas e Profiling Interno
* Expostos na porta de debug isolada `PERGO_DEBUG_PORT` (padrão `6060` no host `127.0.0.1`):
  * **/debug/vars**: Expõe métricas de tempo de execução (`expvar`) do Go, incluindo contadores específicos do PerGo como `audit_drops` (que indica eventos de auditoria descartados por buffer cheio).
  * **/debug/pprof/**: Fornece endpoints de profiling de CPU, memória, blocos e concorrência para análise detalhada e diagnóstico sob carga.
