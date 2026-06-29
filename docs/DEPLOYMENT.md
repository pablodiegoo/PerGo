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
