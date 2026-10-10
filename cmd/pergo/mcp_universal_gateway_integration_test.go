package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pablojhp.pergo/internal/api/mcp"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/platform/crypto"
	"github.com/pablojhp.pergo/internal/repository"
)

// Test 1: Streamable HTTP /mcp with Bearer API Key authentication executing tools/list and tools/call.
func TestUniversalMCPGateway_BearerAuth_StreamableHTTP(t *testing.T) {
	pool := getTestPool(t)
	if pool == nil {
		t.Skip("skipping: PostgreSQL not available")
	}

	ctx := context.Background()

	// Clean up
	_, _ = pool.Exec(ctx, "DELETE FROM chat_messages")
	_, _ = pool.Exec(ctx, "DELETE FROM chats")
	_, _ = pool.Exec(ctx, "DELETE FROM contact_identities")
	_, _ = pool.Exec(ctx, "DELETE FROM contacts")
	_, _ = pool.Exec(ctx, "DELETE FROM connections")
	_, _ = pool.Exec(ctx, "DELETE FROM api_keys")
	_, _ = pool.Exec(ctx, "DELETE FROM workspaces")

	wsRepo := repository.NewWorkspaceRepository(pool)
	apiKeyRepo := repository.NewAPIKeyRepository(pool)
	contactRepo := repository.NewContactRepository(pool)
	chatRepo := repository.NewChatRepository(pool)

	ws, err := wsRepo.Create(ctx, "Universal Gateway Test Workspace")
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	// Create API Key
	_, rawAPIKey, err := apiKeyRepo.Create(ctx, ws.ID, "Universal Gateway Test Key")
	if err != nil {
		t.Fatalf("failed to create api key: %v", err)
	}

	// Create Contact & Chat
	contact, err := contactRepo.CreateContact(ctx, ws.ID, "Sherlock Holmes", nil, map[string]string{"phone": "+447900000001"})
	if err != nil {
		t.Fatalf("failed to create contact: %v", err)
	}

	chat, err := chatRepo.FindOrCreateChat(ctx, ws.ID, nil, contact.ID)
	if err != nil {
		t.Fatalf("failed to create chat: %v", err)
	}

	err = chatRepo.AddChatMessage(ctx, &domain.ChatMessage{
		ChatID:      chat.ID,
		WorkspaceID: ws.ID,
		UID:         "msg-tracer-001",
		Direction:   domain.DirectionInbound,
		SenderType:  domain.SenderTypeContact,
		SenderName:  "Sherlock Holmes",
		Body:        "The game is afoot!",
		CreatedAt:   time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("failed to add chat message: %v", err)
	}

	kek := make([]byte, 32)
	copy(kek, []byte("dev-development-key-32-bytes-kek"))
	enc, _ := crypto.NewEncryptor(kek)
	connRepo := repository.NewConnectionRepository(pool, enc)

	mcpSrv := mcp.NewServer(
		wsRepo,
		connRepo,
		contactRepo,
		nil,
		nil,
		apiKeyRepo,
		nil,
		nil,
		nil,
		nil,
		"",
		mcp.WithChatRepo(chatRepo),
	)

	gateway := mcp.NewUniversalGateway(mcpSrv, "")
	ts := httptest.NewServer(gateway)
	defer ts.Close()

	client := ts.Client()

	// 1. JSON-RPC initialize over HTTP POST /mcp
	initReq := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2024-11-05",
			"clientInfo": map[string]any{
				"name":    "antigravity-client",
				"version": "1.0.0",
			},
		},
	}
	initBytes, _ := json.Marshal(initReq)

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp", bytes.NewReader(initBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+rawAPIKey)

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST /mcp initialize error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status 200 OK for initialize, got %d: %s", resp.StatusCode, string(body))
	}

	var initRes map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&initRes)
	if initRes["id"].(float64) != 1 {
		t.Errorf("expected response id 1, got %v", initRes["id"])
	}

	// 2. JSON-RPC tools/list over HTTP POST /mcp
	toolsListReq := map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/list",
		"params":  map[string]any{},
	}
	toolsListBytes, _ := json.Marshal(toolsListReq)

	req2, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp", bytes.NewReader(toolsListBytes))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Authorization", "Bearer "+rawAPIKey)

	resp2, err := client.Do(req2)
	if err != nil {
		t.Fatalf("POST /mcp tools/list error: %v", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp2.Body)
		t.Fatalf("expected status 200 OK for tools/list, got %d: %s", resp2.StatusCode, string(body))
	}

	var listRes map[string]any
	_ = json.NewDecoder(resp2.Body).Decode(&listRes)
	resObj, ok := listRes["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result object in tools/list response: %+v", listRes)
	}
	toolsArr, ok := resObj["tools"].([]any)
	if !ok || len(toolsArr) == 0 {
		t.Fatalf("expected tools array in tools/list result")
	}

	foundListChats := false
	foundQuotas := false
	for _, toolItem := range toolsArr {
		tMap, ok := toolItem.(map[string]any)
		if ok {
			if tMap["name"] == "list_chats" {
				foundListChats = true
			}
			if tMap["name"] == "workspace_quotas" {
				foundQuotas = true
			}
		}
	}

	if !foundListChats {
		t.Errorf("expected 'list_chats' tool to be present in tools/list")
	}
	if !foundQuotas {
		t.Errorf("expected 'workspace_quotas' tool to be present in tools/list")
	}

	// 3. JSON-RPC tools/call for list_chats
	callReq := map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "list_chats",
			"arguments": map[string]any{
				"unread": false,
			},
		},
	}
	callBytes, _ := json.Marshal(callReq)

	req3, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp", bytes.NewReader(callBytes))
	req3.Header.Set("Content-Type", "application/json")
	req3.Header.Set("Authorization", "Bearer "+rawAPIKey)

	resp3, err := client.Do(req3)
	if err != nil {
		t.Fatalf("POST /mcp tools/call list_chats error: %v", err)
	}
	defer resp3.Body.Close()

	if resp3.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp3.Body)
		t.Fatalf("expected status 200 OK for list_chats, got %d: %s", resp3.StatusCode, string(body))
	}

	var callRes map[string]any
	_ = json.NewDecoder(resp3.Body).Decode(&callRes)
	callResult, ok := callRes["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result object in list_chats call: %+v", callRes)
	}
	contentArr := callResult["content"].([]any)
	textObj := contentArr[0].(map[string]any)
	chatText := textObj["text"].(string)

	if !strings.Contains(chatText, "Sherlock Holmes") {
		t.Errorf("expected contact 'Sherlock Holmes' in list_chats output: %s", chatText)
	}

	// 4. JSON-RPC tools/call for workspace_quotas
	quotasReq := map[string]any{
		"jsonrpc": "2.0",
		"id":      4,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "workspace_quotas",
			"arguments": map[string]any{},
		},
	}
	quotasBytes, _ := json.Marshal(quotasReq)

	req4, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp", bytes.NewReader(quotasBytes))
	req4.Header.Set("Content-Type", "application/json")
	req4.Header.Set("Authorization", "Bearer "+rawAPIKey)

	resp4, err := client.Do(req4)
	if err != nil {
		t.Fatalf("POST /mcp tools/call workspace_quotas error: %v", err)
	}
	defer resp4.Body.Close()

	if resp4.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp4.Body)
		t.Fatalf("expected status 200 OK for workspace_quotas, got %d: %s", resp4.StatusCode, string(body))
	}

	var quotasRes map[string]any
	_ = json.NewDecoder(resp4.Body).Decode(&quotasRes)
	quotasResult := quotasRes["result"].(map[string]any)
	quotasContent := quotasResult["content"].([]any)
	quotasText := quotasContent[0].(map[string]any)["text"].(string)

	if !strings.Contains(quotasText, "Universal Gateway Test Workspace") {
		t.Errorf("expected workspace name in quotas output: %s", quotasText)
	}
	if !strings.Contains(quotasText, `"plan": "pro"`) {
		t.Errorf("expected plan 'pro' in quotas output: %s", quotasText)
	}
}

// Test 2: Unauthenticated /mcp challenge & OAuth 2.0 PKCE flow with tool execution.
func TestUniversalMCPGateway_OAuthPKCE_Flow(t *testing.T) {
	pool := getTestPool(t)
	if pool == nil {
		t.Skip("skipping: PostgreSQL not available")
	}

	ctx := context.Background()

	wsRepo := repository.NewWorkspaceRepository(pool)
	contactRepo := repository.NewContactRepository(pool)
	chatRepo := repository.NewChatRepository(pool)

	ws, err := wsRepo.Create(ctx, "OAuth PKCE Test Workspace")
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	contact, _ := contactRepo.CreateContact(ctx, ws.ID, "John Watson", nil, map[string]string{"phone": "+447900000002"})
	_, _ = chatRepo.FindOrCreateChat(ctx, ws.ID, nil, contact.ID)

	mcpSrv := mcp.NewServer(
		wsRepo,
		nil,
		contactRepo,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		"",
		mcp.WithChatRepo(chatRepo),
	)

	gateway := mcp.NewUniversalGateway(mcpSrv, "")

	mux := http.NewServeMux()
	mux.Handle("/mcp", gateway)
	mux.HandleFunc("/oauth/register", gateway.HandleOAuthRegister)
	mux.HandleFunc("/oauth/authorize", gateway.HandleOAuthAuthorize)
	mux.HandleFunc("/oauth/token", gateway.HandleOAuthToken)

	ts := httptest.NewServer(mux)
	defer ts.Close()

	client := ts.Client()

	// Step 1: Unauthenticated request to /mcp initiates OAuth 2.0 PKCE challenge
	unauthReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	unauthReq.Header.Set("Content-Type", "application/json")

	unauthResp, err := client.Do(unauthReq)
	if err != nil {
		t.Fatalf("unauthenticated request failed: %v", err)
	}
	defer unauthResp.Body.Close()

	if unauthResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized for unauthenticated /mcp, got %d", unauthResp.StatusCode)
	}

	wwwAuth := unauthResp.Header.Get("WWW-Authenticate")
	if !strings.Contains(wwwAuth, "Bearer") || !strings.Contains(wwwAuth, "resource_metadata") {
		t.Errorf("expected WWW-Authenticate header with resource_metadata, got: %s", wwwAuth)
	}

	// Step 2: Dynamic Client Registration via POST /oauth/register
	regPayload := `{"client_name":"mcp-remote-test","redirect_uris":["http://localhost:54321/callback"]}`
	regResp, err := client.Post(ts.URL+"/oauth/register", "application/json", strings.NewReader(regPayload))
	if err != nil {
		t.Fatalf("POST /oauth/register error: %v", err)
	}
	defer regResp.Body.Close()

	if regResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 Created for registration, got %d", regResp.StatusCode)
	}

	var regData map[string]any
	_ = json.NewDecoder(regResp.Body).Decode(&regData)
	clientID, ok := regData["client_id"].(string)
	if !ok || clientID == "" {
		t.Fatalf("expected client_id in registration response")
	}

	// Step 3: Authorization code request with PKCE challenge
	codeVerifier := "test-pkce-verifier-string-with-sufficient-entropy-12345678"
	h := sha256.Sum256([]byte(codeVerifier))
	codeChallenge := base64.RawURLEncoding.EncodeToString(h[:])

	authParams := url.Values{}
	authParams.Set("response_type", "code")
	authParams.Set("client_id", clientID)
	authParams.Set("redirect_uri", "http://localhost:54321/callback")
	authParams.Set("code_challenge", codeChallenge)
	authParams.Set("code_challenge_method", "S256")
	authParams.Set("state", "state_abc_123")
	authParams.Set("workspace_id", ws.ID.String())

	authReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/oauth/authorize?"+authParams.Encode(), nil)

	// Don't follow redirects automatically so we can inspect 302
	noFollowClient := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	authResp, err := noFollowClient.Do(authReq)
	if err != nil {
		t.Fatalf("GET /oauth/authorize error: %v", err)
	}
	defer authResp.Body.Close()

	if authResp.StatusCode != http.StatusFound {
		t.Fatalf("expected 302 Found redirect for authorization, got %d", authResp.StatusCode)
	}

	locHeader := authResp.Header.Get("Location")
	redirectURL, err := url.Parse(locHeader)
	if err != nil {
		t.Fatalf("failed to parse redirect Location: %v", err)
	}

	authCode := redirectURL.Query().Get("code")
	if authCode == "" {
		t.Fatalf("expected authorization code in redirect query parameters, got: %s", locHeader)
	}
	if redirectURL.Query().Get("state") != "state_abc_123" {
		t.Errorf("expected state 'state_abc_123', got: %s", redirectURL.Query().Get("state"))
	}

	// Step 4: Token exchange via POST /oauth/token with PKCE verification
	tokenParams := url.Values{}
	tokenParams.Set("grant_type", "authorization_code")
	tokenParams.Set("code", authCode)
	tokenParams.Set("code_verifier", codeVerifier)
	tokenParams.Set("client_id", clientID)
	tokenParams.Set("redirect_uri", "http://localhost:54321/callback")

	tokResp, err := client.PostForm(ts.URL+"/oauth/token", tokenParams)
	if err != nil {
		t.Fatalf("POST /oauth/token error: %v", err)
	}
	defer tokResp.Body.Close()

	if tokResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(tokResp.Body)
		t.Fatalf("expected 200 OK for token exchange, got %d: %s", tokResp.StatusCode, string(body))
	}

	var tokData mcp.TokenResponse
	_ = json.NewDecoder(tokResp.Body).Decode(&tokData)
	if tokData.AccessToken == "" {
		t.Fatalf("expected access_token in token response")
	}
	if tokData.TokenType != "Bearer" {
		t.Errorf("expected token_type 'Bearer', got: %s", tokData.TokenType)
	}

	// Step 5: Execute tools/call using OAuth Access Token
	callReq := map[string]any{
		"jsonrpc": "2.0",
		"id":      10,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "list_chats",
			"arguments": map[string]any{},
		},
	}
	callBytes, _ := json.Marshal(callReq)

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp", bytes.NewReader(callBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tokData.AccessToken)

	mcpResp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST /mcp with OAuth token error: %v", err)
	}
	defer mcpResp.Body.Close()

	if mcpResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(mcpResp.Body)
		t.Fatalf("expected status 200 OK for OAuth-authenticated call, got %d: %s", mcpResp.StatusCode, string(body))
	}

	var mcpCallRes map[string]any
	_ = json.NewDecoder(mcpResp.Body).Decode(&mcpCallRes)
	resObj := mcpCallRes["result"].(map[string]any)
	contentArr := resObj["content"].([]any)
	text := contentArr[0].(map[string]any)["text"].(string)

	if !strings.Contains(text, "John Watson") {
		t.Errorf("expected contact 'John Watson' in list_chats output: %s", text)
	}

	// Step 6: Execute workspace_quotas with OAuth Access Token
	qReq := map[string]any{
		"jsonrpc": "2.0",
		"id":      11,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "workspace_quotas",
			"arguments": map[string]any{},
		},
	}
	qBytes, _ := json.Marshal(qReq)

	reqQ, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp", bytes.NewReader(qBytes))
	reqQ.Header.Set("Content-Type", "application/json")
	reqQ.Header.Set("Authorization", "Bearer "+tokData.AccessToken)

	mcpQResp, err := client.Do(reqQ)
	if err != nil {
		t.Fatalf("POST /mcp workspace_quotas with OAuth token error: %v", err)
	}
	defer mcpQResp.Body.Close()

	if mcpQResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(mcpQResp.Body)
		t.Fatalf("expected status 200 OK for workspace_quotas, got %d: %s", mcpQResp.StatusCode, string(body))
	}

	var mcpQRes map[string]any
	_ = json.NewDecoder(mcpQResp.Body).Decode(&mcpQRes)
	resQObj := mcpQRes["result"].(map[string]any)
	qContentArr := resQObj["content"].([]any)
	qText := qContentArr[0].(map[string]any)["text"].(string)

	if !strings.Contains(qText, "OAuth PKCE Test Workspace") {
		t.Errorf("expected workspace name in quotas output: %s", qText)
	}
}

// Test 3: Auto-Discovery endpoints /.well-known/mcp and /.well-known/opencode.
func TestUniversalMCPGateway_AutoDiscoveryEndpoints(t *testing.T) {
	mcpSrv := mcp.NewServer(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, "")
	gateway := mcp.NewUniversalGateway(mcpSrv, "http://pergo.local:8080")

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/mcp", gateway.HandleWellKnownMCP)
	mux.HandleFunc("/.well-known/mcp.json", gateway.HandleWellKnownMCP)
	mux.HandleFunc("/.well-known/opencode", gateway.HandleWellKnownOpenCode)
	mux.HandleFunc("/.well-known/opencode.json", gateway.HandleWellKnownOpenCode)
	mux.HandleFunc("/.well-known/oauth-protected-resource", gateway.HandleWellKnownOAuthProtectedResource)
	mux.HandleFunc("/.well-known/oauth-authorization-server", gateway.HandleWellKnownOAuthAuthorizationServer)

	ts := httptest.NewServer(mux)
	defer ts.Close()

	client := ts.Client()

	// 1. GET /.well-known/mcp
	resp1, err := client.Get(ts.URL + "/.well-known/mcp")
	if err != nil {
		t.Fatalf("GET /.well-known/mcp error: %v", err)
	}
	defer resp1.Body.Close()

	if resp1.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK for /.well-known/mcp, got %d", resp1.StatusCode)
	}
	if !strings.Contains(resp1.Header.Get("Content-Type"), "application/json") {
		t.Errorf("expected application/json content type, got: %s", resp1.Header.Get("Content-Type"))
	}

	var mcpDiscovery map[string]any
	_ = json.NewDecoder(resp1.Body).Decode(&mcpDiscovery)

	if mcpDiscovery["name"] != "PerGo Universal MCP Gateway" {
		t.Errorf("expected name 'PerGo Universal MCP Gateway', got %v", mcpDiscovery["name"])
	}

	snippets, ok := mcpDiscovery["snippets"].(map[string]any)
	if !ok {
		t.Fatalf("expected snippets in /.well-known/mcp")
	}
	if _, exists := snippets["claude_code"]; !exists {
		t.Errorf("expected claude_code snippet in /.well-known/mcp")
	}
	if _, exists := snippets["mcp_remote"]; !exists {
		t.Errorf("expected mcp_remote snippet in /.well-known/mcp")
	}
	if _, exists := snippets["opencode"]; !exists {
		t.Errorf("expected opencode snippet in /.well-known/mcp")
	}

	// 2. GET /.well-known/opencode
	resp2, err := client.Get(ts.URL + "/.well-known/opencode")
	if err != nil {
		t.Fatalf("GET /.well-known/opencode error: %v", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK for /.well-known/opencode, got %d", resp2.StatusCode)
	}

	var openCodeDiscovery map[string]any
	_ = json.NewDecoder(resp2.Body).Decode(&openCodeDiscovery)
	mcpBlock, ok := openCodeDiscovery["mcp"].(map[string]any)
	if !ok {
		t.Fatalf("expected mcp block in /.well-known/opencode")
	}
	if _, exists := mcpBlock["pergo"]; !exists {
		t.Errorf("expected 'pergo' entry under mcp block in /.well-known/opencode")
	}

	// 3. GET /.well-known/oauth-protected-resource
	resp3, err := client.Get(ts.URL + "/.well-known/oauth-protected-resource")
	if err != nil {
		t.Fatalf("GET /.well-known/oauth-protected-resource error: %v", err)
	}
	defer resp3.Body.Close()

	if resp3.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK for /.well-known/oauth-protected-resource, got %d", resp3.StatusCode)
	}

	// 4. GET /.well-known/oauth-authorization-server
	resp4, err := client.Get(ts.URL + "/.well-known/oauth-authorization-server")
	if err != nil {
		t.Fatalf("GET /.well-known/oauth-authorization-server error: %v", err)
	}
	defer resp4.Body.Close()

	if resp4.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK for /.well-known/oauth-authorization-server, got %d", resp4.StatusCode)
	}
}

// Test 4: pergo mcp CLI Stdio server initialization.
func TestUniversalMCPGateway_StdioServerInitialization(t *testing.T) {
	mcpSrv := mcp.NewServer(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, "")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()
	var stderr bytes.Buffer

	done := make(chan error, 1)
	go func() {
		done <- mcp.RunStdio(ctx, mcpSrv, nil, []string{}, stdinReader, stdoutWriter, &stderr)
	}()

	// Send JSON-RPC initialize request
	initMessage := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"stdio-test","version":"1.0.0"}}}` + "\n"
	go func() {
		_, _ = stdinWriter.Write([]byte(initMessage))
		time.Sleep(50 * time.Millisecond)
		_ = stdinWriter.Close()
	}()

	scanner := bufio.NewScanner(stdoutReader)
	var mu sync.Mutex
	var receivedInit bool

	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		var rpcResp map[string]any
		if err := json.Unmarshal([]byte(line), &rpcResp); err == nil {
			if id, ok := rpcResp["id"].(float64); ok && id == 1 {
				mu.Lock()
				receivedInit = true
				mu.Unlock()
				break
			}
		}
	}

	cancel()
	_ = stdoutReader.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}

	mu.Lock()
	defer mu.Unlock()
	if !receivedInit {
		t.Errorf("expected JSON-RPC response for initialize over stdio")
	}
}
