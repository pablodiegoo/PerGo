# Configuração de Canais e Provedores (Webhooks e Credenciais)

Este guia explica passo a passo como obter as credenciais e configurar os webhooks nos provedores de mensagens para integrá-los com o **PerGo**.

---

## 1. WhatsApp Web (whatsmeow)
O canal do WhatsApp Web funciona emulando um dispositivo conectado através do protocolo oficial do WhatsApp Web. 

### Configuração:
* **Sem configurações externas:** Não é necessário criar contas de desenvolvedor na Meta ou configurar webhooks adicionais.
* **Ativação:**
  1. Vá até o painel administrativo do PerGo (`/admin`).
  2. Navegue até a aba **Canais** e selecione o canal do WhatsApp Web.
  3. Clique em **Conectar / Gerar QR Code**.
  4. Abra o WhatsApp no seu celular, vá em **Aparelhos Conectados** -> **Conectar um aparelho** e escaneie o código exibido na tela do PerGo.

---

## 2. Telegram Bot
A integração com o Telegram utiliza a API oficial de Bots.

### Passo 1: Criar o Bot no Telegram
1. Abra o aplicativo do Telegram e pesquise por [@BotFather](https://t.me/BotFather) (o bot oficial de criação de bots).
2. Envie o comando `/newbot`.
3. Escolha um nome de exibição para o seu bot (ex: `PerGo Gateway`).
4. Escolha um username para o seu bot, que obrigatoriamente deve terminar em `bot` (ex: `pergo_gateway_bot`).
5. O BotFather gerará um **Token de Acesso (Bot Token)** (ex: `123456789:ABCdefGhIJKlmNoPQRsTUVwxyZ`). Guarde esse token.

### Passo 2: Configurar no PerGo
1. Vá até o painel do PerGo -> **Canais** -> **Telegram**.
2. Cole o **Bot Token** gerado.
3. Se o seu servidor PerGo estiver rodando sob uma URL pública segura (**HTTPS**):
   * O PerGo registrará o webhook automaticamente no Telegram assim que você salvar as credenciais.
   * O webhook será registrado apontando para: `https://[seu-dominio]/webhooks/telegram` protegida por um token secreto gerado dinamicamente no cabeçalho `X-Telegram-Bot-Api-Secret-Token`.
4. Se você estiver rodando em **localhost (HTTP)**:
   * O Telegram não aceita webhooks em URLs inseguras (HTTP). Para testar localmente, utilize uma ferramenta de túnel como o **ngrok** (`ngrok http 8080`) e configure o `PERGO_EXTERNAL_URL` com a URL do túnel HTTPS fornecida.

---

## 3. WhatsApp Cloud API (WABA)
A API de Nuvem oficial da Meta requer uma conta de desenvolvedor no Facebook Business.

### Passo 1: Criar aplicativo na Meta
1. Acesse o portal [Meta for Developers](https://developers.facebook.com/) e faça login.
2. Clique em **Meus Aplicativos** -> **Criar aplicativo**.
3. Selecione o tipo de aplicativo **Negócios (Business)** e avance.
4. Preencha os dados básicos e vincule a uma conta do Gerenciador de Negócios (Business Manager).
5. Painel do Aplicativo -> Procure por **WhatsApp** na lista de produtos e clique em **Configurar**.

### Passo 2: Obter Credenciais
Navegue no menu esquerdo até **WhatsApp** -> **Início rápido** ou **Configuração de API**:
* **ID do número de telefone (Phone Number ID):** Código de identificação do número que enviará as mensagens.
* **ID da conta do WhatsApp Business (WABA ID):** Código da conta empresarial WABA correspondente.
* **Token de Acesso Permanente:**
  * No painel de controle do seu Gerenciador de Negócios, vá em **Configurações do negócio** -> **Usuários** -> **Usuários do sistema**.
  * Crie um usuário do sistema (com função de administrador).
  * Clique em **Gerar novo token**, selecione o seu aplicativo do WhatsApp e marque as permissões `whatsapp_business_messaging` e `whatsapp_business_management`.
  * Copie esse token gerado (tokens gerados aqui são permanentes e não expiram).

### Passo 3: Configurar o Webhook de Entrada
Para que o PerGo receba notificações de mensagens entregues, lidas, falhas e novas mensagens recebidas dos clientes (abrindo a janela de 24 horas):

1. No portal Meta for Developers, no menu esquerdo do WhatsApp, clique em **Configuração (Configuration)**.
2. Em **Webhook**, clique em **Editar**.
3. Insira as informações:
   * **URL de retorno (Callback URL):** `https://[seu-dominio-pergo]/webhooks/waba`
   * **Token de verificação (Verify Token):** Escolha uma palavra-chave aleatória de sua preferência (você usará essa mesma palavra no painel do PerGo).
4. Clique em **Verificar e salvar**.
5. Em **Campos de webhook**, clique em **Gerenciar** e marque a caixa de seleção **messages**. Isso é **obrigatório** para receber mensagens de clientes e atualizar os status no PerGo.

### Passo 4: Salvar no PerGo
1. Vá ao painel do PerGo -> **Canais** -> **WhatsApp Cloud**.
2. Insira o **Phone Number ID**, o **WABA ID**, o **Token de Acesso Permanente** e o **Verify Token** (o mesmo configurado na etapa 3.3).
3. Salve as credenciais.
