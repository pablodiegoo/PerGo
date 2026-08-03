# Relatório 02: Matriz de Capacidades do PerGo v1.7 vs. Gaps de Mercado

## 1. Contexto do PerGo no Estado Atual (v1.7)

O **PerGo** é um Gateway CPaaS Omnichannel de código aberto e self-hosted, construído em Go (Echo v5, Templ + HTMX, NATS JetStream, PostgreSQL pgx/v5). 

Nas versões v1.0 a v1.7, o PerGo desenvolveu um núcleo extremamente maduro de gateway de mensageria com suporte nativo a:
1. **Multi-Canais Integrados**:
   - WhatsApp Web (não oficial via `whatsmeow`)
   - WhatsApp Cloud API (oficial Meta WABA)
   - Telegram Bot API
   - Instagram Direct DM
   - Email (SMTP, Amazon SES, Mautic com rastreamento de abertura/clique)
2. **Infraestrutura de Mensageria de Alta Performance**:
   - Ponto de entrada unificado `POST /messages`
   - Resiliência e durabilidade via NATS JetStream (garantia de entrega, retries e Dead Letter Queue)
   - Roteamento por Slugs amigáveis por Workspace
   - Staggered Dispatch (delays aleatórios de 1 a 3s) para proteção de contas não oficiais
   - Engine de Fallbacks Automáticos Inteligentes entre canais
3. **Funcionalidades Avançadas de WABA Oficial**:
   - Gestão de Janela de Sessão de 24 horas (rastreio de `last_inbound_at`, rejeição HTTP 422 pré-flight)
   - Lifecycle completo de Templates WABA (CRUD, sync Graph API, previewer visual, validação estrita de variáveis)
   - Suporte a **Meta Flows** (tokens HMAC, decodificação de `nfm_reply`, criptografia RSA/AES Data Exchange)
   - Suporte a **WhatsApp Commerce Catalogs** (envio de `product` e `product_list`, conversão de webhooks de pedido em eventos `order.created`)
4. **Ecossistema e Integrações Extensíveis**:
   - Conector nativo de sync bidirecional com **Chatwoot** (Live Chat multi-atendente)
   - Conector nativo assíncrono com **Typebot** (Chatbot flow builder)
   - Engine de **Stateful Handoff Routing** (controle de estado `bot_active` / `bot_paused_at` e resfriamento por inatividade)
   - Painel administrativo server-rendered em Templ/HTMX com Inbox Conversacional integrado

---

## 2. Matriz Comparativa de Funcionalidades (PerGo v1.7 vs Concorrentes)

| Funcionalidade / Recurso | PerGo (v1.7) | Twilio | 360dialog | Take Blip | Botconversa | Octadesk | Wati |
| :--- | :---: | :---: | :---: | :---: | :---: | :---: | :---: |
| **WhatsApp Web (QR Code não-oficial)** | ✅ Nativo (`whatsmeow`) | ❌ | ❌ | ❌ | ✅ | ⚠️ Limitado | ❌ |
| **WhatsApp Cloud API (WABA Oficial)** | ✅ Nativo | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| **Telegram & Instagram DM** | ✅ Nativo | ✅ (via Channels) | ❌ | ✅ | ❌ | ✅ | ❌ |
| **Email SMTP / SES / Mautic + Pixel** | ✅ Nativo | ✅ (SendGrid) | ❌ | ⚠️ Integração | ❌ | ✅ | ❌ |
| **API Unified Ingestion (`POST /messages`)** | ✅ Nativo | ✅ | ✅ | ✅ | ⚠️ Limitado | ⚠️ Limitado | ⚠️ Limitado |
| **Connection Slugs Routing** | ✅ Nativo | ❌ (usa SIDs) | ❌ | ❌ (usa GUIDs) | ❌ | ❌ | ❌ |
| **Smart Fallbacks Automáticos** | ✅ Nativo | ⚠️ Customizado | ❌ | ⚠️ Via Flow | ❌ | ❌ | ❌ |
| **Meta Flows & Commerce Catalogs** | ✅ Nativo | ⚠️ Requer code | ⚠️ Repassa payload | ✅ Nativo | ❌ | ❌ | ⚠️ Básico |
| **Visual Drag-and-Drop Flow Builder** | ❌ (usa Typebot) | ✅ (Studio) | ❌ | ✅ (Blip Builder) | ✅ (Canvas) | ⚠️ Robô simples | ⚠️ Robô simples |
| **Multi-Agent Helpdesk / SLA Tickets** | ⚠️ Básico (usa Chatwoot) | ❌ (usa Flex) | ❌ | ✅ (Blip Desk) | ⚠️ Simplificado | ✅ (Octadesk) | ✅ (Team Inbox) |
| **Broadcast / Campanhas em Massa** | ⚠️ Via API | ❌ (usa Twilio Engage) | ❌ | ✅ | ✅ (Broadcaster) | ⚠️ Básico | ✅ |
| **Zero Per-Message Markup (Self-Hosted)** | ✅ 100% Grátis em Infra | ❌ ($/msg) | ⚠️ Flat fee | ❌ (Markup alto) | ✅ Plano fixo | ❌ | ❌ |

---

## 3. Identificação e Classificação dos Gaps do PerGo

Para que o PerGo seja transformado em um **clone completo** de uma das soluções de mercado, analisamos os principais **gaps arquiteturais e funcionais** existentes hoje:

### Gap 1: Visual Drag-and-Drop Flow Builder Nativo
- **Descrição**: Criação visual de fluxos de bot no próprio painel administrativo do PerGo (estilo Botconversa ou Blip Builder).
- **Situação Atual**: O PerGo delega essa responsabilidade integrando perfeitamente com o **Typebot** (open-source).
- **Esforço para Tornar Nativo**: ALTO (exigiria engine de avaliação de nós em Go + frontend gráfico canvas).

### Gap 2: Advanced Multi-Agent Live Chat & Helpdesk (Ticketing/SLA)
- **Descrição**: Sistema de filas por departamento, controle rigoroso de SLA de primeira resposta, transferência de chamados entre agentes, notas internas e relatórios de produtividade por atendente.
- **Situação Atual**: O PerGo possui um Chat UI básico para visualização de conversas e delega o atendimento profissional ao **Chatwoot**.
- **Esforço para Tornar Nativo**: MÉDIO-ALTO (o PerGo já tem a tabela de contatos e inbox, mas precisa de lógica de filas, atribuição e SLA).

### Gap 3: Engine de Disparos em Massa & Automação de Campanhas (Broadcasting & Sequences)
- **Descrição**: Interface no admin para upload de listas CSV, agendamento de disparos, controle de vazão (throttling), pausa/retomada de campanhas e métricas de conversão.
- **Situação Atual**: O PerGo processa disparo via API REST (`POST /messages`), mas não possui a interface de campanha nem agendador massivo interno no admin.
- **Esforço para Tornar Nativo**: BAIXO-MÉDIO (a infraestrutura de filas NATS JetStream e rate-limiting do PerGo já suporta a vazão; falta a tabela de campanhas e a UI HTMX).

### Gap 4: SDKs de Cliente & Developer Portal (Twilio-Style)
- **Descrição**: SDKs em Go, Node.js, Python, PHP e C# + documentação interativa de API, gestão avançada de chaves API e Webhooks assinados via HMAC.
- **Situação Atual**: O PerGo já possui API REST limpa, hashing SHA-256 de API Keys e documentação OpenAPI.
- **Esforço para Tornar Nativo**: BAIXO (criação de pacotes wrapper de API e aprimoramento da documentação).

---
*Documento gerado como parte do ecossistema PerGo Wayfinder.*
