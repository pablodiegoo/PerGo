# Referências CPaaS / API de WhatsApp

Este documento lista repositórios de projetos de código aberto relevantes para o desenvolvimento do **PerGo**. Estes projetos atuam como plataformas CPaaS (Communications Platform as a Service) ou Gateways de API para o WhatsApp, fornecendo inspirações valiosas sobre arquitetura, boas práticas e modelos de integração (multidispositivo, mensageria e webhooks).

Todos os repositórios listados aqui foram clonados para o diretório `context/inspiration/` para fácil consulta local, sem afetar o projeto principal, já que estão no `.gitignore`.

## Repositórios Clonados

### 1. Evolution API (Node.js)
- **Origem:** [EvolutionAPI/evolution-api](https://github.com/EvolutionAPI/evolution-api)
- **Por que é relevante:** É uma das APIs não-oficiais mais completas e utilizadas. Suporta múltiplas instâncias, integrações nativas com Dify, Chatwoot, N8N e RabbitMQ/WebSockets para eventos. Serve de forte referência para as features desejadas num CPaaS moderno (especialmente filas e webhooks).

### 2. Evolution Go (Golang)
- **Origem:** [evolution-foundation/evolution-go](https://github.com/evolution-foundation/evolution-go)
- **Por que é relevante:** A reescrita oficial da Evolution API usando Golang. Assim como o PerGo, adota o Go e a biblioteca `whatsmeow`. É extremamente relevante para consultarmos como implementaram concorrência, integrações com NATS/RabbitMQ e banco de dados dentro de uma stack Go.

### 3. GOWA / go-whatsapp-web-multidevice (Golang)
- **Origem:** [aldinokemal/go-whatsapp-web-multidevice](https://github.com/aldinokemal/go-whatsapp-web-multidevice)
- **Por que é relevante:** Uma implementação bem popular em Go para um Gateway REST sobre o protocolo multi-device. Ótimo para avaliar como estruturam o controle de sessão e o disparo de webhooks, além da modelagem da API RESTful.

### 4. WuzAPI (Golang)
- **Origem:** [asternic/wuzapi](https://github.com/asternic/wuzapi)
- **Por que é relevante:** Outra implementação simples e eficiente de REST API para WhatsApp em Go. O foco na simplicidade pode inspirar uma arquitetura onde menos é mais e o alto desempenho importa.

### 5. Chatwoot (Ruby/Vue.js)
- **Origem:** [chatwoot/chatwoot](https://github.com/chatwoot/chatwoot)
- **Por que é relevante:** Embora seja uma plataforma de Helpdesk completa, é a principal referência open source de interface omnichannel. Para o desenvolvimento do frontend do PerGo (o "console de operadora"), entender como o Chatwoot gerencia conversas, contatos e canais será bastante inspirador.

### 6. Fonoster (Node.js)
- **Origem:** [fonoster/fonoster](https://github.com/fonoster/fonoster)
- **Por que é relevante:** Uma das principais alternativas open-source ao Twilio. Construído para ser uma plataforma CPaaS focada em voz, SMS e mensageria programável. Mostra como estruturar uma API aos moldes da gigante do mercado.

### 7. Somleng (Ruby)
- **Origem:** [somleng/somleng](https://github.com/somleng/somleng)
- **Por que é relevante:** CPaaS open-source com uma API compatível com o Twilio (TwiML). É fundamental para entender como arquitetar o roteamento de provedores (SIP, SMS, canais) agnóstico à aplicação que o consome.

### 8. Omni (TypeScript)
- **Origem:** [automagik-dev/omni](https://github.com/automagik-dev/omni)
- **Por que é relevante:** Plataforma de mensagens omnichannel orientada a eventos. Tem suporte a Telegram, WhatsApp, Slack, etc. Ajuda a inspirar como abstrair o formato de mensagens de diferentes redes (ex: JSON payload do Telegram vs WABA) para uma interface única.

### 9. Novu (TypeScript)
- **Origem:** [novuhq/novu](https://github.com/novuhq/novu)
- **Por que é relevante:** Plataforma open-source focada em infraestrutura de notificações e omnichannel (SMS, E-mail, Push, Chat). Excelente para visualizar estratégias de fallback de mensagens (ex: se falhar no WhatsApp, tente Telegram ou Email) e gerenciamento de providers.

### 10. Typebot (TypeScript)
- **Origem:** [baptisteArno/typebot.io](https://github.com/baptisteArno/typebot.io)
- **Por que é relevante:** Construtor de chatbots open-source (no-code). Útil para analisar fluxos conversacionais, integração com múltiplos canais (como WhatsApp) e a estruturação de mensagens interativas e variáveis.

---

*Estes repositórios servem de fonte de aprendizado. Para analisá-los, basta navegar na pasta `context/inspiration`.*
