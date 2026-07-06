package main

import (
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
)

type Connection struct {
	ID             string
	Name           string
	Channel        string // "whatsapp", "whatsapp_cloud", "telegram"
	SenderIdentity string // "+5511999990001", "@pergo_bot", etc.
	Status         string // "active", "pending", "disconnected"
	IsDefault      bool
	ConnectedSince string
}

type Message struct {
	ID        string
	Direction string // "inbound" or "outbound"
	Body      string
	Time      string
	IsTemplate bool
	TemplateName string
}

type Conversation struct {
	From              string
	Channel           string
	RecipientIdentity string
	LastMessageBody   string
	LastMessageTime   time.Time
	TotalMessageCount int
	LastInboundTime   time.Time
}

var (
	connections   []Connection
	conversations []Conversation
	messagesMap   map[string][]Message // keyed by contact phone
	sessionsMu    sync.Mutex
)

func init() {
	// Seed connections
	connections = []Connection{
		{ID: "c1", Name: "WhatsApp Web Principal", Channel: "whatsapp", SenderIdentity: "+5511999990001", Status: "active", IsDefault: true, ConnectedSince: "2026-07-06 10:00"},
		{ID: "c2", Name: "WhatsApp Cloud Oficial", Channel: "whatsapp_cloud", SenderIdentity: "10283928192", Status: "active", IsDefault: true, ConnectedSince: "2026-07-06 10:05"},
		{ID: "c3", Name: "Telegram Bot Suporte", Channel: "telegram", SenderIdentity: "@pergo_bot", Status: "disconnected", IsDefault: true, ConnectedSince: "-"},
	}

	// Seed conversations & message map
	conversations = []Conversation{
		{
			From:              "+5511988887777",
			Channel:           "whatsapp",
			RecipientIdentity: "+5511999990001",
			LastMessageBody:   "Pode me enviar o boleto?",
			LastMessageTime:   time.Now().Add(-10 * time.Minute),
			TotalMessageCount: 3,
			LastInboundTime:   time.Now().Add(-10 * time.Minute),
		},
		{
			From:              "+5511977776666",
			Channel:           "whatsapp_cloud",
			RecipientIdentity: "10283928192",
			LastMessageBody:   "Obrigado pelo retorno!",
			LastMessageTime:   time.Now().Add(-25 * time.Hour), // Older than 24 hours
			TotalMessageCount: 2,
			LastInboundTime:   time.Now().Add(-25 * time.Hour),
		},
	}

	messagesMap = map[string][]Message{
		"+5511988887777": {
			{ID: uuid.New().String(), Direction: "inbound", Body: "Olá! Gostaria de saber sobre meu pedido.", Time: "13:00"},
			{ID: uuid.New().String(), Direction: "outbound", Body: "Olá! Claro, vou verificar para você.", Time: "13:02"},
			{ID: uuid.New().String(), Direction: "inbound", Body: "Pode me enviar o boleto?", Time: "13:05"},
		},
		"+5511977776666": {
			{ID: uuid.New().String(), Direction: "inbound", Body: "Quero atualizar minhas preferências de entrega.", Time: "Ontem 11:00"},
			{ID: uuid.New().String(), Direction: "outbound", Body: "Obrigado pelo retorno!", Time: "Ontem 11:02"},
		},
	}

	// Periodically generate new inbound messages for "+5511988887777" to test live polling
	go func() {
		counter := 1
		for {
			time.Sleep(8 * time.Second)
			sessionsMu.Lock()
			contact := "+5511988887777"
			newMsg := Message{
				ID:        uuid.New().String(),
				Direction: "inbound",
				Body:      fmt.Sprintf("Mensagem automática recebida #%d!", counter),
				Time:      time.Now().Format("15:04:05"),
			}
			messagesMap[contact] = append(messagesMap[contact], newMsg)
			
			// Update conversation summary
			for i, conv := range conversations {
				if conv.From == contact {
					conversations[i].LastMessageBody = newMsg.Body
					conversations[i].LastMessageTime = time.Now()
					conversations[i].LastInboundTime = time.Now()
					conversations[i].TotalMessageCount++
				}
			}
			counter++
			sessionsMu.Unlock()
		}
	}()
}

func main() {
	http.HandleFunc("/", handleIndex)
	http.HandleFunc("/connections", handleConnectionsPage)
	http.HandleFunc("/connections/create", handleCreateConnection)
	http.HandleFunc("/connections/delete", handleDeleteConnection)
	http.HandleFunc("/connections/test", handleTestConnection)
	http.HandleFunc("/connections/pair-qr", handlePairQR)
	
	http.HandleFunc("/inbox", handleInboxPage)
	http.HandleFunc("/inbox/conversations", handleConversationsList)
	http.HandleFunc("/inbox/chat", handleChatPanel)
	http.HandleFunc("/inbox/messages", handlePollMessages)
	http.HandleFunc("/inbox/send", handleSendMessage)
	http.HandleFunc("/inbox/new-message-modal", handleNewMessageModal)
	http.HandleFunc("/inbox/new-message-send", handleNewMessageSend)

	log.Println("Unified Spike Server starting on http://localhost:8089")
	if err := http.ListenAndServe(":8089", nil); err != nil {
		log.Fatalf("failed to start server: %v", err)
	}
}

func renderLayout(w http.ResponseWriter, activeTab string, content string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	
	connClass := "text-zinc-600 hover:bg-zinc-200/50"
	inboxClass := "text-zinc-600 hover:bg-zinc-200/50"
	if activeTab == "connections" {
		connClass = "bg-zinc-200 text-zinc-900"
	} else if activeTab == "inbox" {
		inboxClass = "bg-zinc-200 text-zinc-900"
	}

	fmt.Fprintf(w, `
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <title>PerGo Management Console</title>
    <script src="https://unpkg.com/htmx.org@2.0.10"></script>
    <script src="https://cdn.tailwindcss.com"></script>
    <style>
        .modal {
            transition: opacity 0.25s ease;
        }
        body.modal-active {
            overflow: hidden;
        }
    </style>
</head>
<body class="bg-zinc-50 font-sans h-screen flex overflow-hidden">
    <nav class="w-64 bg-zinc-900 border-r border-zinc-800 flex flex-col justify-between shrink-0 h-screen text-zinc-300">
        <div>
            <div class="px-6 py-5 border-b border-zinc-800 flex items-center justify-between">
                <h2 class="text-xl font-bold tracking-tight text-white">PerGo Console</h2>
                <span class="text-[10px] bg-indigo-500 text-white font-semibold px-2 py-0.5 rounded">SPIKE</span>
            </div>
            <ul class="p-4 space-y-2">
                <li>
                    <a href="/connections" class="flex items-center gap-3 px-4 py-2.5 text-sm font-medium rounded-lg transition-colors %s">
                        <svg xmlns="http://www.w3.org/2000/svg" class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13.828 10.172a4 4 0 00-5.656 0l-4 4a4 4 0 105.656 5.656l1.102-1.101m-.758-4.899a4 4 0 005.656 0l4-4a4 4 0 00-5.656-5.656l-1.1 1.1" />
                        </svg>
                        <span>Conexões</span>
                    </a>
                </li>
                <li>
                    <a href="/inbox" class="flex items-center gap-3 px-4 py-2.5 text-sm font-medium rounded-lg transition-colors %s">
                        <svg xmlns="http://www.w3.org/2000/svg" class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 12h.01M12 12h.01M16 12h.01M21 12c0 4.418-4.03 8-9 8a9.863 9.863 0 01-4.255-.949L3 20l1.395-3.72C3.512 15.042 3 13.574 3 12c0-4.418 4.03-8 9-8s9 3.582 9 8z" />
                        </svg>
                        <span>Conversas</span>
                    </a>
                </li>
            </ul>
        </div>
        <div class="p-4 border-t border-zinc-800 text-xs text-zinc-500 text-center">
            PerGo v1.0 Spike Dashboard
        </div>
    </nav>
    <main class="flex-1 flex flex-col overflow-hidden h-screen bg-zinc-100">
        %s
    </main>
    <div id="modal-container"></div>
    <script>
        function openModal() {
            var modal = document.getElementById('modal');
            if (modal) {
                modal.classList.remove('opacity-0', 'pointer-events-none');
                document.body.classList.add('modal-active');
            }
        }
        function closeModal() {
            var modal = document.getElementById('modal');
            if (modal) {
                modal.classList.add('opacity-0', 'pointer-events-none');
                document.body.classList.remove('modal-active');
                setTimeout(function() {
                    document.getElementById('modal-container').innerHTML = '';
                }, 200);
            }
        }
    </script>
</body>
</html>
`, connClass, inboxClass, content)
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/inbox", http.StatusFound)
}

func handleConnectionsPage(w http.ResponseWriter, r *http.Request) {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()

	var rowsHTML string
	for _, c := range connections {
		statusBadge := `<span class="bg-green-100 text-green-800 text-xs px-2 py-0.5 rounded-full font-semibold">Conectado</span>`
		if c.Status == "pending" {
			statusBadge = `<span class="bg-amber-100 text-amber-800 text-xs px-2 py-0.5 rounded-full font-semibold animate-pulse">Pendente</span>`
		} else if c.Status == "disconnected" {
			statusBadge = `<span class="bg-zinc-100 text-zinc-800 text-xs px-2 py-0.5 rounded-full font-semibold">Desconectado</span>`
		}

		channelIcon := ""
		if c.Channel == "whatsapp" {
			channelIcon = `<span class="bg-teal-500 text-white text-[10px] px-1.5 py-0.5 rounded">WhatsApp Web</span>`
		} else if c.Channel == "whatsapp_cloud" {
			channelIcon = `<span class="bg-blue-500 text-white text-[10px] px-1.5 py-0.5 rounded">WABA Cloud</span>`
		} else {
			channelIcon = `<span class="bg-sky-500 text-white text-[10px] px-1.5 py-0.5 rounded">Telegram Bot</span>`
		}

		rowsHTML += fmt.Sprintf(`
<tr class="border-b border-zinc-200 hover:bg-zinc-50" id="conn-row-%s">
    <td class="px-6 py-4 text-sm font-semibold text-zinc-900">%s</td>
    <td class="px-6 py-4 text-sm text-zinc-500">%s</td>
    <td class="px-6 py-4">%s</td>
    <td class="px-6 py-4 text-sm text-zinc-600">%s</td>
    <td class="px-6 py-4 text-sm text-zinc-500">%s</td>
    <td class="px-6 py-4 text-right">
        <div class="flex items-center justify-end gap-2">
            <button class="text-indigo-600 hover:text-indigo-900 font-semibold text-xs bg-indigo-50 px-2.5 py-1.5 rounded"
                    hx-get="/connections/test?id=%s"
                    hx-target="#modal-container"
                    hx-swap="innerHTML"
                    onclick="setTimeout(openModal, 50)">
                Testar
            </button>
            <button class="text-red-600 hover:text-red-900 font-semibold text-xs bg-red-50 px-2.5 py-1.5 rounded"
                    hx-delete="/connections/delete?id=%s"
                    hx-target="#conn-row-%s"
                    hx-swap="outerHTML">
                Excluir
            </button>
        </div>
    </td>
</tr>
`, c.ID, c.Name, channelIcon, statusBadge, c.SenderIdentity, c.ConnectedSince, c.ID, c.ID, c.ID)
	}

	content := fmt.Sprintf(`
<div class="flex-1 flex flex-col overflow-hidden p-6 gap-6">
    <div class="flex justify-between items-center flex-shrink-0">
        <div>
            <h1 class="text-2xl font-bold text-zinc-950">Canais de Conexão</h1>
            <p class="text-sm text-zinc-500">Gerencie múltiplas conexões de WhatsApp Web, WABA e Telegram do seu Workspace.</p>
        </div>
        <button class="bg-black hover:bg-zinc-800 text-white font-semibold text-sm px-4 py-2.5 rounded-lg shadow-sm"
                hx-get="/connections/create"
                hx-target="#modal-container"
                hx-swap="innerHTML"
                onclick="setTimeout(openModal, 50)">
            Nova Conexão
        </button>
    </div>
    <div class="flex-1 bg-white border border-zinc-200 rounded-xl shadow-sm overflow-hidden flex flex-col">
        <div class="overflow-y-auto flex-1">
            <table class="w-full text-left border-collapse">
                <thead>
                    <tr class="bg-zinc-50 border-b border-zinc-200 text-zinc-500 uppercase tracking-wider text-[10px] font-bold">
                        <th class="px-6 py-4">Nome da Conexão</th>
                        <th class="px-6 py-4">Canal</th>
                        <th class="px-6 py-4">Status</th>
                        <th class="px-6 py-4">Identidade de Disparo (From)</th>
                        <th class="px-6 py-4">Conectado Desde</th>
                        <th class="px-6 py-4 text-right">Ações</th>
                    </tr>
                </thead>
                <tbody class="divide-y divide-zinc-200">
                    %s
                </tbody>
            </table>
        </div>
    </div>
</div>
`, rowsHTML)

	renderLayout(w, "connections", content)
}

func handleCreateConnection(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `
<div id="modal" class="modal fixed inset-0 z-50 flex items-center justify-center bg-black/50 transition-opacity opacity-0 pointer-events-none">
    <div class="bg-white rounded-xl shadow-lg border border-zinc-200 w-full max-w-lg overflow-hidden flex flex-col">
        <div class="px-6 py-4 border-b border-zinc-200 flex justify-between items-center">
            <h3 class="font-bold text-zinc-950 text-base">Nova Conexão</h3>
            <button onclick="closeModal()" class="text-zinc-400 hover:text-zinc-600 text-lg">&times;</button>
        </div>
        <form hx-post="/connections/create"
              hx-target="#modal-body-area"
              hx-swap="innerHTML"
              class="flex flex-col flex-1"
        >
            <div class="p-6 flex flex-col gap-4 flex-1" id="modal-body-area">
                <div>
                    <label class="block text-xs font-bold text-zinc-500 uppercase mb-2">Canal</label>
                    <select name="channel" 
                            class="w-full rounded-lg border border-zinc-200 p-2.5 text-sm bg-white"
                            onchange="updateFields(this.value)">
                        <option value="whatsapp">WhatsApp Web (Instância QR)</option>
                        <option value="whatsapp_cloud">WhatsApp Cloud (API Oficial WABA)</option>
                        <option value="telegram">Telegram Bot</option>
                    </select>
                </div>
                <div>
                    <label class="block text-xs font-bold text-zinc-500 uppercase mb-2">Nome da Conexão</label>
                    <input type="text" name="name" required placeholder="Ex: Suporte Whats Web" class="w-full rounded-lg border border-zinc-200 p-2.5 text-sm" />
                </div>
                <div id="dynamic-inputs" class="flex flex-col gap-4">
                    <div>
                        <label class="block text-xs font-bold text-zinc-500 uppercase mb-2">Número de Telefone</label>
                        <input type="text" name="phone" placeholder="Ex: +5511999990001" class="w-full rounded-lg border border-zinc-200 p-2.5 text-sm" />
                        <p class="text-[10px] text-zinc-400 mt-1">Conexão não oficial. Risco de banimento em disparos massivos.</p>
                    </div>
                </div>
            </div>
            <div class="px-6 py-4 bg-zinc-50 border-t border-zinc-200 flex justify-end gap-2 flex-shrink-0">
                <button type="button" onclick="closeModal()" class="px-4 py-2 border border-zinc-200 rounded-lg text-sm font-semibold text-zinc-700 bg-white hover:bg-zinc-50">Cancelar</button>
                <button type="submit" class="px-4 py-2 bg-black hover:bg-zinc-800 text-white rounded-lg text-sm font-semibold">Salvar & Configurar</button>
            </div>
        </form>
    </div>
</div>
<script>
    function updateFields(val) {
        var container = document.getElementById('dynamic-inputs');
        if (val === 'whatsapp') {
            container.innerHTML = '<div><label class="block text-xs font-bold text-zinc-500 uppercase mb-2">Número de Telefone</label><input type="text" name="phone" placeholder="Ex: +5511999990001" class="w-full rounded-lg border border-zinc-200 p-2.5 text-sm" /><p class="text-[10px] text-zinc-400 mt-1">Conexão não oficial. Risco de banimento em disparos massivos.</p></div>';
        } else if (val === 'whatsapp_cloud') {
            container.innerHTML = '<div class="grid grid-cols-2 gap-4"><div><label class="block text-xs font-bold text-zinc-500 uppercase mb-2">ID Telefone (Meta)</label><input type="text" name="phone_id" required placeholder="Ex: 10283928192" class="w-full rounded-lg border border-zinc-200 p-2.5 text-sm" /></div><div><label class="block text-xs font-bold text-zinc-500 uppercase mb-2">ID Conta WABA</label><input type="text" name="waba_id" required placeholder="Ex: 92839281928" class="w-full rounded-lg border border-zinc-200 p-2.5 text-sm" /></div></div><div class="col-span-2"><label class="block text-xs font-bold text-zinc-500 uppercase mb-2">Token de Acesso</label><input type="password" name="token" required placeholder="Token Permanente da Meta" class="w-full rounded-lg border border-zinc-200 p-2.5 text-sm" /></div>';
        } else if (val === 'telegram') {
            container.innerHTML = '<div><label class="block text-xs font-bold text-zinc-500 uppercase mb-2">Bot Token</label><input type="password" name="token" required placeholder="Ex: 123456:ABC-DEF1234ghIkl-zyx" class="w-full rounded-lg border border-zinc-200 p-2.5 text-sm" /></div>';
        }
    }
</script>
`)
		return
	}

	channel := r.FormValue("channel")
	name := r.FormValue("name")

	sessionsMu.Lock()
	defer sessionsMu.Unlock()

	if channel == "whatsapp" {
		phone := r.FormValue("phone")
		newConn := Connection{
			ID:             "conn-" + uuid.New().String()[:8],
			Name:           name,
			Channel:        "whatsapp",
			SenderIdentity: phone,
			Status:         "pending",
			IsDefault:      false,
			ConnectedSince: "-",
		}
		connections = append(connections, newConn)

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `
<div class="flex flex-col items-center gap-4 text-center py-6">
    <h4 class="font-bold text-zinc-900 text-sm">Emparelhar WhatsApp Web</h4>
    <p class="text-xs text-zinc-500 max-w-sm">Escaneie o código QR abaixo com seu celular em WhatsApp &gt; Dispositivos Conectados.</p>
    <div class="h-48 w-48 bg-zinc-200 border-4 border-zinc-300 rounded flex items-center justify-center p-2 relative"
         hx-get="/connections/pair-qr?id=%s"
         hx-trigger="every 3s"
         hx-target="this"
         hx-swap="outerHTML"
    >
        <span class="text-zinc-600 text-xs font-semibold">Carregando QR Code...</span>
    </div>
    <p class="text-[10px] text-red-500 font-semibold bg-red-50 px-2 py-1 rounded">Risco de Banimento: Evite disparos automatizados intensos.</p>
</div>
`, newConn.ID)
		return
	}

	identity := "waba_cloud_sender"
	if channel == "telegram" {
		identity = "@telegram_bot_mock"
	} else if channel == "whatsapp_cloud" {
		identity = r.FormValue("phone_id")
	}

	newConn := Connection{
		ID:             "conn-" + uuid.New().String()[:8],
		Name:           name,
		Channel:        channel,
		SenderIdentity: identity,
		Status:         "active",
		IsDefault:      false,
		ConnectedSince: time.Now().Format("2006-01-02 15:04"),
	}
	connections = append(connections, newConn)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("HX-Refresh", "true")
	w.WriteHeader(http.StatusOK)
}

func handlePairQR(w http.ResponseWriter, r *http.Request) {
	connID := r.URL.Query().Get("id")
	
	sessionsMu.Lock()
	for i, c := range connections {
		if c.ID == connID {
			if c.Status == "pending" {
				connections[i].Status = "active"
				connections[i].ConnectedSince = time.Now().Format("2006-01-02 15:04")
				
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.Write([]byte(`
<div class="flex flex-col items-center gap-2 text-center py-8">
    <div class="h-12 w-12 rounded-full bg-green-100 flex items-center justify-center text-green-600">
        <svg xmlns="http://www.w3.org/2000/svg" class="h-6 w-6" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="3" d="M5 13l4 4L19 7" />
        </svg>
    </div>
    <p class="text-sm font-bold text-green-700">Dispositivo Emparelhado!</p>
    <p class="text-xs text-zinc-500">Esta tela recarregará em instantes...</p>
    <script>
        setTimeout(function() {
            closeModal();
            window.location.reload();
        }, 2000);
    </script>
</div>
`))
				sessionsMu.Unlock()
				return
			}
		}
	}
	sessionsMu.Unlock()

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(`
<div class="h-48 w-48 bg-zinc-200 border-4 border-zinc-300 rounded flex flex-col items-center justify-center p-2 relative">
    <div class="grid grid-cols-4 gap-1 w-32 h-32 opacity-40">
        <div class="bg-zinc-800"></div><div class="bg-zinc-800"></div><div></div><div class="bg-zinc-800"></div>
        <div></div><div class="bg-zinc-800"></div><div class="bg-zinc-800"></div><div></div>
        <div class="bg-zinc-800"></div><div></div><div class="bg-zinc-800"></div><div class="bg-zinc-800"></div>
        <div class="bg-zinc-800"></div><div class="bg-zinc-800"></div><div></div><div class="bg-zinc-800"></div>
    </div>
    <span class="absolute inset-0 flex items-center justify-center bg-white/80 text-zinc-800 font-bold text-xs">Simulando Leitura...</span>
</div>
`))
}

func handleDeleteConnection(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")

	sessionsMu.Lock()
	defer sessionsMu.Unlock()

	for i, c := range connections {
		if c.ID == id {
			connections = append(connections[:i], connections[i+1:]...)
			w.WriteHeader(http.StatusOK)
			return
		}
	}
	w.WriteHeader(http.StatusNotFound)
}

func handleTestConnection(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")

	if r.Method == http.MethodPost {
		// Process test send
		body := r.FormValue("body")
		to := r.FormValue("to")
		
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(fmt.Sprintf(`
<div class="bg-zinc-900 text-zinc-300 p-4 rounded-lg font-mono text-xs flex flex-col gap-1.5 border border-zinc-800 mt-4">
    <p class="text-green-400">[info] Initing connection test dispatch...</p>
    <p class="text-zinc-400">[debug] Target receiver: %s</p>
    <p class="text-zinc-400">[debug] Message payload: "%s"</p>
    <p class="text-green-400">[info] Event sent successfully! Trace-ID: test-%s</p>
</div>
`, to, body, uuid.New().String()[:8])))
		return
	}

	var conn Connection
	found := false
	sessionsMu.Lock()
	for _, c := range connections {
		if c.ID == id {
			conn = c
			found = true
		}
	}
	sessionsMu.Unlock()

	if !found {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `
<div id="modal" class="modal fixed inset-0 z-50 flex items-center justify-center bg-black/50 transition-opacity opacity-0 pointer-events-none">
    <div class="bg-white rounded-xl shadow-lg border border-zinc-200 w-full max-w-md overflow-hidden flex flex-col">
        <div class="px-6 py-4 border-b border-zinc-200 flex justify-between items-center">
            <h3 class="font-bold text-zinc-950 text-base">Testar Conexão: %s</h3>
            <button onclick="closeModal()" class="text-zinc-400 hover:text-zinc-600 text-lg">&times;</button>
        </div>
        <form hx-post="/connections/test?id=%s"
              hx-target="#test-status"
              hx-swap="innerHTML"
              class="p-6 flex flex-col gap-4"
        >
            <div>
                <label class="block text-xs font-bold text-zinc-500 uppercase mb-2">Destinatário</label>
                <input type="text" name="to" required placeholder="Ex: +5511999990002" class="w-full rounded-lg border border-zinc-200 p-2.5 text-sm" />
            </div>
            <div>
                <label class="block text-xs font-bold text-zinc-500 uppercase mb-2">Mensagem de Teste</label>
                <textarea name="body" required placeholder="Olá de teste!" class="w-full rounded-lg border border-zinc-200 p-2.5 text-sm h-20 resize-none"></textarea>
            </div>
            
            <button type="submit" class="w-full py-2.5 bg-indigo-600 hover:bg-indigo-700 text-white rounded-lg font-semibold text-sm transition-colors animate-pulse">
                Disparar Teste
            </button>
            <div id="test-status" class="mt-2"></div>
        </form>
    </div>
</div>
`, conn.Name, conn.ID)
}

func handleInboxPage(w http.ResponseWriter, r *http.Request) {
	content := `
<div class="flex-1 flex overflow-hidden h-full">
    <div class="w-72 bg-white border-r border-zinc-200 flex flex-col overflow-hidden h-full flex-shrink-0">
        <div class="p-4 border-b border-zinc-200 flex items-center justify-between flex-shrink-0">
            <h3 class="font-bold text-zinc-950 text-base">Mensagens</h3>
            <button class="bg-indigo-600 hover:bg-indigo-700 text-white text-xs font-semibold px-2.5 py-1.5 rounded-lg flex items-center gap-1.5 shadow-sm"
                    hx-get="/inbox/new-message-modal"
                    hx-target="#modal-container"
                    hx-swap="innerHTML"
                    onclick="setTimeout(openModal, 50)">
                <svg xmlns="http://www.w3.org/2000/svg" class="h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v16m8-8H4" />
                </svg>
                Novo Chat
            </button>
        </div>
        <div id="conv-list-container" 
             hx-get="/inbox/conversations" 
             hx-trigger="load, every 5s" 
             hx-swap="innerHTML" 
             class="flex-1 overflow-y-auto"
        >
            <div class="p-4 text-center text-zinc-400 text-xs">Carregando conversas...</div>
        </div>
    </div>
    <div id="chat-pane" class="flex-1 bg-zinc-50 relative flex items-center justify-center text-zinc-400 text-sm">
        Selecione uma conversa na lista lateral para carregar a linha do tempo de mensagens.
    </div>
</div>
`
	renderLayout(w, "inbox", content)
}

func handleConversationsList(w http.ResponseWriter, r *http.Request) {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()

	var listHTML string
	for _, conv := range conversations {
		channelIcon := ""
		if conv.Channel == "whatsapp" {
			channelIcon = `<span class="bg-teal-500 text-white text-[8px] px-1 rounded">WA Web</span>`
		} else if conv.Channel == "whatsapp_cloud" {
			channelIcon = `<span class="bg-blue-500 text-white text-[8px] px-1 rounded">WABA</span>`
		} else {
			channelIcon = `<span class="bg-sky-500 text-white text-[8px] px-1 rounded">Telegram</span>`
		}

		timeStr := conv.LastMessageTime.Format("15:04")
		listHTML += fmt.Sprintf(`
<div class="p-4 border-b border-zinc-100 hover:bg-zinc-50 cursor-pointer flex flex-col gap-1 transition-colors"
     hx-get="/inbox/chat?from=%s&channel=%s&to=%s"
     hx-target="#chat-pane"
     hx-swap="innerHTML"
>
    <div class="flex items-center justify-between">
        <span class="font-semibold text-sm text-zinc-900 truncate">%s</span>
        <span class="text-[10px] text-zinc-400">%s</span>
    </div>
    <div class="flex items-center justify-between gap-1.5">
        <span class="text-xs text-zinc-500 truncate">%s</span>
        <div class="flex items-center gap-1">
            %s
        </div>
    </div>
</div>
`, conv.From, conv.Channel, conv.RecipientIdentity, conv.From, timeStr, conv.LastMessageBody, channelIcon)
	}

	if len(conversations) == 0 {
		listHTML = `<div class="p-8 text-center text-zinc-400 text-xs italic">Nenhuma conversa ativa.</div>`
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(listHTML))
}

func handleChatPanel(w http.ResponseWriter, r *http.Request) {
	from := r.URL.Query().Get("from")
	channel := r.URL.Query().Get("channel")
	to := r.URL.Query().Get("to")

	sessionsMu.Lock()
	defer sessionsMu.Unlock()

	messages := messagesMap[from]

	var bubblesHTML string
	for _, m := range messages {
		bubblesHTML += renderBubble(m)
	}

	var lastID string = "LAST_ID"
	if len(messages) > 0 {
		lastID = messages[len(messages)-1].ID
	}

	isWabaBlocked := false
	var lastInbound time.Time
	for _, c := range conversations {
		if c.From == from && c.Channel == channel {
			lastInbound = c.LastInboundTime
		}
	}
	if channel == "whatsapp_cloud" && !lastInbound.IsZero() {
		if time.Since(lastInbound) > 24*time.Hour {
			isWabaBlocked = true
		}
	}

	wabaBlockerBanner := ""
	wabaControls := ""
	inputFieldAttr := ""

	if channel == "whatsapp_cloud" {
		wabaControls = `
<button type="button" 
        class="h-10 px-3 border border-zinc-200 rounded-lg text-zinc-700 bg-white hover:bg-zinc-50 text-xs font-semibold flex items-center gap-1.5 flex-shrink-0 shadow-sm"
        hx-get="/inbox/new-message-modal?type=template_only&from=` + from + `&channel=whatsapp_cloud&to=` + to + `"
        hx-target="#modal-container"
        hx-swap="innerHTML"
        onclick="setTimeout(openModal, 50)"
>
    <svg xmlns="http://www.w3.org/2000/svg" class="h-4 w-4 text-indigo-500" fill="none" viewBox="0 0 24 24" stroke="currentColor">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 16l4.586-4.586a2 2 0 012.828 0L16 16m-2-2l1.586-1.586a2 2 0 012.828 0L20 14m-6-6h.01M6 20h12a2 2 0 002-2V6a2 2 0 00-2-2H6a2 2 0 00-2 2v12a2 2 0 002 2z" />
    </svg>
    <span>Templates</span>
</button>
`
		if isWabaBlocked {
			inputFieldAttr = `disabled placeholder="A janela de 24h fechou. Utilize o botão 'Templates' ao lado para reabrir."`
			wabaBlockerBanner = `
<div class="px-4 py-2 bg-amber-50 border-b border-amber-200 text-amber-800 text-xs font-semibold flex justify-between items-center flex-shrink-0">
    <span>⚠️ Janela de atendimento (24h) fechada para este contato. Somente envios de Templates Meta são autorizados.</span>
</div>
`
		}
	}

	avatarLetter := "?"
	if len(from) > 0 {
		avatarLetter = string(from[len(from)-1])
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `
<div class="flex flex-col h-full w-full" id="chat-panel-inner">
    <div class="flex items-center gap-3 px-4 py-3 border-b border-zinc-200 bg-white flex-shrink-0">
        <div class="h-9 w-9 rounded-full bg-gradient-to-br from-indigo-400 to-purple-500 flex items-center justify-center text-white font-semibold text-sm">
            %s
        </div>
        <div>
            <p class="font-semibold text-sm text-zinc-900">%s</p>
            <p class="text-xs text-zinc-400">%s · via %s</p>
        </div>
    </div>
    
    %s

    <div id="chat-messages" class="flex-1 overflow-y-auto px-4 py-3 flex flex-col gap-2 bg-zinc-50">
        %s
    </div>

    <div id="chat-poll-anchor"
         hx-get="/inbox/messages?from=%s&channel=%s&to=%s&after_id=%s"
         hx-target="#chat-messages"
         hx-swap="beforeend scroll:bottom"
         hx-trigger="every 3s"
         class="hidden"
    ></div>

    <div class="px-4 py-3 border-t border-zinc-200 bg-white flex-shrink-0">
        <form hx-post="/inbox/send"
              hx-target="#chat-send-status"
              hx-swap="innerHTML"
              hx-on::after-request="if(event.detail.successful){document.getElementById('chat-textarea').value='';}"
              class="flex items-end gap-2"
        >
            <input type="hidden" name="from" value="%s" />
            <input type="hidden" name="channel" value="%s" />
            <input type="hidden" name="to" value="%s" />
            
            %s

            <textarea id="chat-textarea"
                      name="body"
                      rows="1"
                      %s
                      placeholder="Digite uma mensagem... (Enter para enviar)"
                      class="flex-1 resize-none rounded-lg border border-zinc-200 bg-zinc-50 px-3 py-2 text-sm text-zinc-800 focus:outline-none focus:ring-2 focus:ring-indigo-400 disabled:opacity-50"
                      style="min-height:40px; max-height:120px;"
                      onkeydown="if(event.key==='Enter'&&!event.shiftKey){event.preventDefault(); if(!this.disabled){ this.form.dispatchEvent(new Event('submit', {bubbles: true, cancelable: true})); } }"
            ></textarea>
            
            <button type="submit" %s class="h-10 px-4 rounded-lg bg-indigo-600 hover:bg-indigo-700 text-white font-semibold text-sm transition-colors disabled:opacity-50">
                Enviar
            </button>
        </form>
        <div id="chat-send-status" class="mt-1 text-xs text-zinc-400 min-h-[16px]"></div>
    </div>
</div>
<script>
    var vp = document.getElementById("chat-messages");
    if (vp) { vp.scrollTop = vp.scrollHeight; }
</script>
`, avatarLetter, from, channel, to, wabaBlockerBanner, bubblesHTML, from, channel, to, lastID, from, channel, to, wabaControls, inputFieldAttr, inputFieldAttr)
}

func handlePollMessages(w http.ResponseWriter, r *http.Request) {
	from := r.URL.Query().Get("from")
	afterID := r.URL.Query().Get("after_id")

	sessionsMu.Lock()
	defer sessionsMu.Unlock()

	messages := messagesMap[from]
	var newMessages []Message
	found := false
	if afterID == "LAST_ID" || afterID == "" {
		found = true
	}

	for _, m := range messages {
		if found {
			newMessages = append(newMessages, m)
		} else if m.ID == afterID {
			found = true
		}
	}

	if len(newMessages) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	var responseHTML string
	for _, m := range newMessages {
		responseHTML += renderBubble(m)
	}

	newLastID := newMessages[len(newMessages)-1].ID
	to := r.URL.Query().Get("to")
	channel := r.URL.Query().Get("channel")
	responseHTML += fmt.Sprintf(`
<div id="chat-poll-anchor"
     hx-get="/inbox/messages?from=%s&channel=%s&to=%s&after_id=%s"
     hx-target="#chat-messages"
     hx-swap="beforeend scroll:bottom"
     hx-trigger="every 3s"
     hx-swap-oob="true"
     class="hidden"
></div>
`, from, channel, to, newLastID)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(responseHTML))
}

func handleSendMessage(w http.ResponseWriter, r *http.Request) {
	from := r.FormValue("from")
	body := r.FormValue("body")

	if body == "" {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`<span class="text-red-500">Erro: mensagem vazia</span>`))
		return
	}

	sessionsMu.Lock()
	newMsg := Message{
		ID:        uuid.New().String(),
		Direction: "outbound",
		Body:      body,
		Time:      time.Now().Format("15:04"),
	}
	messagesMap[from] = append(messagesMap[from], newMsg)

	for i, c := range conversations {
		if c.From == from {
			conversations[i].LastMessageBody = body
			conversations[i].LastMessageTime = time.Now()
		}
	}
	sessionsMu.Unlock()

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(`<span class="text-green-500">Enviado!</span>`))
}

func handleNewMessageModal(w http.ResponseWriter, r *http.Request) {
	modalType := r.URL.Query().Get("type")
	fromContact := r.URL.Query().Get("from")
	
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	
	if modalType == "template_only" {
		fmt.Fprintf(w, `
<div id="modal" class="modal fixed inset-0 z-50 flex items-center justify-center bg-black/50 transition-opacity opacity-0 pointer-events-none">
    <div class="bg-white rounded-xl shadow-lg border border-zinc-200 w-full max-w-md overflow-hidden flex flex-col">
        <div class="px-6 py-4 border-b border-zinc-200 flex justify-between items-center">
            <h3 class="font-bold text-zinc-950 text-base">Enviar Template Meta (WABA)</h3>
            <button onclick="closeModal()" class="text-zinc-400 hover:text-zinc-600 text-lg">&times;</button>
        </div>
        <form hx-post="/inbox/new-message-send"
              hx-swap="none"
              hx-on::after-request="if(event.detail.successful){closeModal();}"
              class="p-6 flex flex-col gap-4"
        >
            <input type="hidden" name="to" value="%s" />
            <input type="hidden" name="channel" value="whatsapp_cloud" />
            <input type="hidden" name="is_template" value="true" />
            
            <div>
                <label class="block text-xs font-bold text-zinc-500 uppercase mb-2">Template</label>
                <select name="template_name" class="w-full rounded-lg border border-zinc-200 p-2.5 text-sm bg-white" onchange="showTemplatePreview(this.value)">
                    <option value="welcome_optin">welcome_optin (Confirmação de Cadastro)</option>
                    <option value="delivery_update">delivery_update (Atualização de Envio)</option>
                </select>
            </div>
            
            <div id="template-vars" class="flex flex-col gap-3 bg-zinc-50 p-3 rounded-lg border border-zinc-200">
                <p class="text-xs font-bold text-zinc-600">Variáveis do Template</p>
                <div class="flex items-center gap-2">
                    <span class="text-xs text-zinc-400 font-semibold">{{1}} (Nome)</span>
                    <input type="text" name="param_1" required placeholder="Ex: Carlos" class="flex-1 rounded-lg border border-zinc-200 px-2.5 py-1.5 text-xs" />
                </div>
            </div>

            <div class="flex justify-end gap-2 mt-2">
                <button type="button" onclick="closeModal()" class="px-4 py-2 border border-zinc-200 rounded-lg text-sm font-semibold text-zinc-700 bg-white hover:bg-zinc-50">Cancelar</button>
                <button type="submit" class="px-4 py-2 bg-indigo-600 hover:bg-indigo-700 text-white rounded-lg text-sm font-semibold">Disparar Template</button>
            </div>
        </form>
    </div>
</div>
<script>
    function showTemplatePreview(val) {
        var container = document.getElementById('template-vars');
        if (val === 'welcome_optin') {
            container.innerHTML = '<p class="text-xs font-bold text-zinc-600">Variáveis do Template</p><div class="flex items-center gap-2"><span class="text-xs text-zinc-400 font-semibold">{{1}} (Nome)</span><input type="text" name="param_1" required placeholder="Ex: Carlos" class="flex-1 rounded-lg border border-zinc-200 px-2.5 py-1.5 text-xs" /></div>';
        } else if (val === 'delivery_update') {
            container.innerHTML = '<p class="text-xs font-bold text-zinc-600">Variáveis de Atualização</p><div class="flex items-center gap-2"><span class="text-xs text-zinc-400 font-semibold">{{1}} (Nome)</span><input type="text" name="param_1" required placeholder="Ex: Carlos" class="flex-1 rounded-lg border border-zinc-200 px-2.5 py-1.5 text-xs" /></div><div class="flex items-center gap-2"><span class="text-xs text-zinc-400 font-semibold">{{2}} (Código)</span><input type="text" name="param_2" required placeholder="Ex: BR123456789" class="flex-1 rounded-lg border border-zinc-200 px-2.5 py-1.5 text-xs" /></div>';
        }
    }
</script>
`, fromContact)
		return
	}

	fmt.Fprint(w, `
<div id="modal" class="modal fixed inset-0 z-50 flex items-center justify-center bg-black/50 transition-opacity opacity-0 pointer-events-none">
    <div class="bg-white rounded-xl shadow-lg border border-zinc-200 w-full max-w-md overflow-hidden flex flex-col">
        <div class="px-6 py-4 border-b border-zinc-200 flex justify-between items-center">
            <h3 class="font-bold text-zinc-950 text-base">Iniciar Nova Conversa</h3>
            <button onclick="closeModal()" class="text-zinc-400 hover:text-zinc-600 text-lg">&times;</button>
        </div>
        <form hx-post="/inbox/new-message-send"
              hx-swap="none"
              hx-on::after-request="if(event.detail.successful){closeModal();}"
              class="p-6 flex flex-col gap-4"
        >
            <div>
                <label class="block text-xs font-bold text-zinc-500 uppercase mb-2">Destinatário</label>
                <input type="text" name="to" required placeholder="Ex: +5511999990002" class="w-full rounded-lg border border-zinc-200 p-2.5 text-sm" />
            </div>
            <div>
                <label class="block text-xs font-bold text-zinc-500 uppercase mb-2">Canal de Disparo</label>
                <select name="channel" class="w-full rounded-lg border border-zinc-200 p-2.5 text-sm bg-white" onchange="toggleNewChatFields(this.value)">
                    <option value="whatsapp">WhatsApp Web</option>
                    <option value="telegram">Telegram Bot</option>
                    <option value="whatsapp_cloud">WABA Cloud (API Oficial - Requer Template)</option>
                </select>
            </div>
            
            <div id="new-chat-message-input">
                <label class="block text-xs font-bold text-zinc-500 uppercase mb-2">Mensagem Inicial</label>
                <textarea name="body" required placeholder="Digite sua mensagem inicial..." class="w-full rounded-lg border border-zinc-200 p-2.5 text-sm h-20 resize-none"></textarea>
            </div>
            
            <div id="new-chat-waba-input" class="hidden flex-col gap-3">
                <div>
                    <label class="block text-xs font-bold text-zinc-500 uppercase mb-2">Template</label>
                    <select name="template_name" class="w-full rounded-lg border border-zinc-200 p-2.5 text-sm bg-white">
                        <option value="welcome_optin">welcome_optin (Confirmação de Cadastro)</option>
                        <option value="delivery_update">delivery_update (Atualização de Envio)</option>
                    </select>
                </div>
                <div class="flex items-center gap-2">
                    <span class="text-xs text-zinc-400 font-semibold">{{1}} (Nome)</span>
                    <input type="text" name="param_new_1" placeholder="Ex: Carlos" class="flex-1 rounded-lg border border-zinc-200 px-2.5 py-1.5 text-xs" />
                </div>
            </div>

            <div class="flex justify-end gap-2 mt-2">
                <button type="button" onclick="closeModal()" class="px-4 py-2 border border-zinc-200 rounded-lg text-sm font-semibold text-zinc-700 bg-white hover:bg-zinc-50">Cancelar</button>
                <button type="submit" class="px-4 py-2 bg-indigo-600 hover:bg-indigo-700 text-white rounded-lg text-sm font-semibold">Iniciar Conversa</button>
            </div>
        </form>
    </div>
</div>
<script>
    function toggleNewChatFields(val) {
        var msgInput = document.getElementById('new-chat-message-input');
        var wabaInput = document.getElementById('new-chat-waba-input');
        if (val === 'whatsapp_cloud') {
            msgInput.classList.add('hidden');
            wabaInput.classList.remove('hidden');
            wabaInput.classList.add('flex');
        } else {
            msgInput.classList.remove('hidden');
            wabaInput.classList.add('hidden');
            wabaInput.classList.remove('flex');
        }
    }
</script>
`)
}

func handleNewMessageSend(w http.ResponseWriter, r *http.Request) {
	to := r.FormValue("to")
	channel := r.FormValue("channel")
	isTemplate := r.FormValue("is_template") == "true" || channel == "whatsapp_cloud"

	var body string
	if isTemplate {
		tplName := r.FormValue("template_name")
		param1 := r.FormValue("param_1")
		if param1 == "" {
			param1 = r.FormValue("param_new_1")
		}
		if tplName == "welcome_optin" {
			body = fmt.Sprintf("[Template: %s] Olá %s, seu cadastro foi confirmado com sucesso!", tplName, param1)
		} else {
			param2 := r.FormValue("param_2")
			body = fmt.Sprintf("[Template: %s] Olá %s, seu pedido já foi despachado! Código de rastreio: %s", tplName, param1, param2)
		}
	} else {
		body = r.FormValue("body")
	}

	sessionsMu.Lock()
	defer sessionsMu.Unlock()

	newMsg := Message{
		ID:        uuid.New().String(),
		Direction: "outbound",
		Body:      body,
		Time:      time.Now().Format("15:04"),
	}
	messagesMap[to] = append(messagesMap[to], newMsg)

	exists := false
	for i, c := range conversations {
		if c.From == to && c.Channel == channel {
			conversations[i].LastMessageBody = body
			conversations[i].LastMessageTime = time.Now()
			if isTemplate {
				conversations[i].LastInboundTime = time.Now()
			}
			exists = true
		}
	}

	if !exists {
		conversations = append(conversations, Conversation{
			From:              to,
			Channel:           channel,
			RecipientIdentity: "primary_from_sender",
			LastMessageBody:   body,
			LastMessageTime:   time.Now(),
			TotalMessageCount: 1,
			LastInboundTime:   time.Now(),
		})
	}

	w.Header().Set("HX-Trigger", `{"showToast":{"text":"Nova conversa iniciada!"}}`)
	w.WriteHeader(http.StatusOK)
}

func renderBubble(m Message) string {
	if m.Direction == "inbound" {
		return fmt.Sprintf(`
<div class="msg-wrap flex items-end gap-2 justify-start mb-2" data-msg-id="%s">
    <div class="flex-shrink-0 h-7 w-7 rounded-full bg-zinc-300 flex items-center justify-center text-zinc-700 font-semibold text-xs select-none">
        ?
    </div>
    <div class="msg-bubble max-w-xs bg-white border border-zinc-200 rounded-2xl rounded-bl-none px-3 py-2 shadow-sm">
        <p class="text-sm text-zinc-800 whitespace-pre-wrap break-words">%s</p>
        <p class="text-[10px] text-zinc-400 text-right mt-1">%s</p>
    </div>
</div>
`, m.ID, m.Body, m.Time)
	} else {
		return fmt.Sprintf(`
<div class="msg-wrap flex items-end gap-2 justify-end mb-2" data-msg-id="%s">
    <div class="msg-bubble max-w-xs rounded-2xl rounded-br-none px-3 py-2 shadow-sm text-white" style="background-color:#3b82f6;">
        <p class="text-sm whitespace-pre-wrap break-words">%s</p>
        <p class="text-[10px] text-blue-200 text-right mt-1">%s</p>
    </div>
</div>
`, m.ID, m.Body, m.Time)
	}
}
