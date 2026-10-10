package mcp

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/server"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/platform/crypto"
	"github.com/pablojhp.pergo/internal/platform/postgres/tenant"
)

// UniversalGateway provides the unified Streamable HTTP gateway, dual-path authentication
// (deterministic Bearer API keys & OAuth 2.0 PKCE), and standard auto-discovery endpoints.
type UniversalGateway struct {
	server           *Server
	streamableServer *server.StreamableHTTPServer
	oauthManager     *OAuthManager
	externalURL      string
}

// NewUniversalGateway creates and initializes a new UniversalGateway.
func NewUniversalGateway(srv *Server, externalURL string) *UniversalGateway {
	if srv.StreamableServer == nil {
		srv.StreamableServer = server.NewStreamableHTTPServer(
			srv.MCPServer,
			server.WithEndpointPath("/mcp"),
			server.WithDisableLocalhostProtection(true),
			server.WithSessionIdManager(&server.StatelessSessionIdManager{}),
		)
	}

	return &UniversalGateway{
		server:           srv,
		streamableServer: srv.StreamableServer,
		oauthManager:     NewOAuthManager(),
		externalURL:      strings.TrimRight(externalURL, "/"),
	}
}

// OAuthManager returns the underlying OAuthManager.
func (g *UniversalGateway) OAuthManager() *OAuthManager {
	return g.oauthManager
}

// StreamableServer returns the underlying StreamableHTTPServer.
func (g *UniversalGateway) StreamableServer() *server.StreamableHTTPServer {
	return g.streamableServer
}

func (g *UniversalGateway) getBaseURL(r *http.Request) string {
	if g.externalURL != "" {
		return g.externalURL
	}
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	host := r.Host
	if h := r.Header.Get("X-Forwarded-Host"); h != "" {
		host = h
	}
	if host == "" {
		host = "localhost:8080"
	}
	return scheme + "://" + host
}

// ServeHTTP handles the /mcp endpoint implementing RFC 2025-03-26 Streamable HTTP.
// Path A: Requests with Authorization: Bearer <API_KEY | ACCESS_TOKEN> authenticate immediately.
// Path B: Requests without Bearer headers initiate an OAuth 2.0 PKCE browser authorization flow.
func (g *UniversalGateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// CORS Headers
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS, DELETE")
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Mcp-Session-Id, *")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
			token := strings.TrimSpace(parts[1])
			if token != "" {
				var authenticated bool
				var wsID uuid.UUID

				// 1. Check OAuth access token
				if id, ok := g.oauthManager.ValidateAccessToken(token); ok {
					authenticated = true
					wsID = id
				}

				// 2. Check API Key repository
				if !authenticated && g.server.apiKeyRepo != nil && len(token) >= 8 {
					prefix := token[:8]
					apiKey, err := g.server.apiKeyRepo.GetByPrefix(r.Context(), prefix)
					if err == nil && crypto.VerifyAPIKey(token, apiKey.KeyHash) {
						authenticated = true
						wsID = apiKey.WorkspaceID
					}
				}

				// 3. Check System Operator Master Key
				if !authenticated && os.Getenv("PERGO_MASTER_KEY") != "" {
					expected := os.Getenv("PERGO_MASTER_KEY")
					if subtle.ConstantTimeCompare([]byte(token), []byte(expected)) == 1 {
						authenticated = true
						wsID = uuid.Nil
					}
				}

				if authenticated {
					ctx := r.Context()
					if wsID != uuid.Nil {
						ctx = tenant.WithWorkspaceID(ctx, wsID)
						ctx = domain.ContextWithScope(ctx, domain.NewWorkspaceScope(wsID, domain.CapabilityWorkspaceScoped))
					}
					g.streamableServer.ServeHTTP(w, r.WithContext(ctx))
					return
				}

				// Bearer token present but invalid
				w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token", error_description="The access token or API key is invalid or expired"`)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"error":             "invalid_token",
					"error_description": "The access token or API key is invalid or expired",
				})
				return
			}
		}
	}

	// Missing Bearer token: initiate OAuth 2.0 PKCE browser authorization flow
	baseURL := g.getBaseURL(r)
	w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+baseURL+`/.well-known/oauth-protected-resource"`)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error":                  "unauthorized",
		"message":                "Authentication required. Present Authorization: Bearer <API_KEY> or complete OAuth 2.0 PKCE flow.",
		"authorization_endpoint": baseURL + "/oauth/authorize",
		"token_endpoint":         baseURL + "/oauth/token",
		"resource_metadata":      baseURL + "/.well-known/oauth-protected-resource",
	})
}

// HandleOAuthRegister handles RFC 7591 dynamic client registration.
func (g *UniversalGateway) HandleOAuthRegister(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method_not_allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ClientName   string   `json:"client_name"`
		RedirectURIs []string `json:"redirect_uris"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error":             "invalid_request",
			"error_description": "Failed to decode request body: " + err.Error(),
		})
		return
	}

	client, err := g.oauthManager.RegisterClient(req.ClientName, req.RedirectURIs)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error":             "invalid_request",
			"error_description": err.Error(),
		})
		return
	}

	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(client)
}

// HandleOAuthAuthorize handles RFC 6749 / RFC 7636 browser authorization requests.
func (g *UniversalGateway) HandleOAuthAuthorize(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	q := r.URL.Query()
	responseType := q.Get("response_type")
	clientID := q.Get("client_id")
	redirectURI := q.Get("redirect_uri")
	codeChallenge := q.Get("code_challenge")
	codeChallengeMethod := q.Get("code_challenge_method")
	state := q.Get("state")
	wsIDStr := q.Get("workspace_id")

	if responseType != "code" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error":             "unsupported_response_type",
			"error_description": "response_type must be 'code'",
		})
		return
	}

	if codeChallenge == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error":             "invalid_request",
			"error_description": "code_challenge is required for PKCE",
		})
		return
	}

	if codeChallengeMethod == "" {
		codeChallengeMethod = "S256"
	}

	var wsID uuid.UUID
	if wsIDStr != "" {
		wsID, _ = uuid.Parse(wsIDStr)
	}
	if wsID == uuid.Nil && g.server.wsRepo != nil {
		if workspaces, err := g.server.wsRepo.List(r.Context(), 1); err == nil && len(workspaces) > 0 {
			wsID = workspaces[0].ID
		}
	}

	code, err := g.oauthManager.Authorize(clientID, redirectURI, codeChallenge, codeChallengeMethod, wsID)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error":             "invalid_request",
			"error_description": err.Error(),
		})
		return
	}

	if redirectURI != "" {
		u, err := url.Parse(redirectURI)
		if err == nil {
			targetQ := u.Query()
			targetQ.Set("code", code)
			if state != "" {
				targetQ.Set("state", state)
			}
			u.RawQuery = targetQ.Encode()
			http.Redirect(w, r, u.String(), http.StatusFound)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"code":  code,
		"state": state,
	})
}

// HandleOAuthToken handles token exchange for authorization_code and refresh_token grants.
func (g *UniversalGateway) HandleOAuthToken(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method_not_allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	_ = r.ParseForm()
	grantType := r.FormValue("grant_type")
	code := r.FormValue("code")
	codeVerifier := r.FormValue("code_verifier")
	clientID := r.FormValue("client_id")
	redirectURI := r.FormValue("redirect_uri")
	refreshToken := r.FormValue("refresh_token")

	// Fallback to JSON body if form fields were not present
	if grantType == "" && strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
			grantType = body["grant_type"]
			code = body["code"]
			codeVerifier = body["code_verifier"]
			clientID = body["client_id"]
			redirectURI = body["redirect_uri"]
			refreshToken = body["refresh_token"]
		}
	}

	tokRes, err := g.oauthManager.ExchangeToken(grantType, code, codeVerifier, clientID, redirectURI, refreshToken)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error":             "invalid_grant",
			"error_description": err.Error(),
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(tokRes)
}

// HandleWellKnownMCP serves machine-readable server metadata and configuration snippets at /.well-known/mcp.
func (g *UniversalGateway) HandleWellKnownMCP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")

	baseURL := g.getBaseURL(r)

	tools := g.server.MCPServer.ListTools()
	type ToolCard struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		InputSchema any    `json:"inputSchema"`
	}

	toolCards := make([]ToolCard, 0, len(tools))
	for _, t := range tools {
		toolCards = append(toolCards, ToolCard{
			Name:        t.Tool.Name,
			Description: t.Tool.Description,
			InputSchema: t.Tool.InputSchema,
		})
	}

	snippets := map[string]any{
		"claude_code": map[string]string{
			"command": "claude mcp add pergo --transport http " + baseURL + "/mcp",
		},
		"mcp_remote": map[string]string{
			"command": "npx -y mcp-remote@latest " + baseURL + "/mcp",
		},
		"opencode": map[string]any{
			"mcp": map[string]any{
				"pergo": map[string]any{
					"type": "remote",
					"url":  baseURL + "/mcp",
					"headers": map[string]string{
						"Authorization": "Bearer {env:PERGO_API_KEY}",
					},
				},
			},
		},
		"antigravity": map[string]any{
			"server": map[string]any{
				"url":       baseURL + "/mcp",
				"transport": "streamable-http",
				"headers": map[string]string{
					"Authorization": "Bearer ${PERGO_API_KEY}",
				},
			},
		},
		"cursor": map[string]string{
			"type":    "command",
			"command": "npx -y mcp-remote@latest " + baseURL + "/mcp",
		},
		"stdio": map[string]string{
			"command": "pergo mcp --api-key <API_KEY>",
		},
	}

	doc := map[string]any{
		"name":        "PerGo Universal MCP Gateway",
		"version":     "1.2.0",
		"description": "Universal Omnichannel CPaaS and Shared Team Inbox Model Context Protocol (MCP) Gateway",
		"endpoint":    baseURL + "/mcp",
		"transports":  []string{"streamable-http", "stdio", "sse"},
		"authentication": map[string]any{
			"methods": []string{"bearer_api_key", "oauth2_pkce"},
			"bearer": map[string]string{
				"header":      "Authorization: Bearer <API_KEY>",
				"description": "Deterministic API key authentication for headless agents",
			},
			"oauth2": map[string]any{
				"authorization_endpoint":           baseURL + "/oauth/authorize",
				"token_endpoint":                   baseURL + "/oauth/token",
				"registration_endpoint":            baseURL + "/oauth/register",
				"code_challenge_methods_supported": []string{"S256"},
				"grant_types_supported":            []string{"authorization_code", "refresh_token"},
			},
		},
		"tools":    toolCards,
		"snippets": snippets,
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(doc)
}

// HandleWellKnownOpenCode serves machine-readable OpenCode configuration at /.well-known/opencode.
func (g *UniversalGateway) HandleWellKnownOpenCode(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")

	baseURL := g.getBaseURL(r)
	tools := g.server.MCPServer.ListTools()

	doc := map[string]any{
		"$schema": "https://opencode.ai/schema.json",
		"version": "1.0",
		"mcp": map[string]any{
			"pergo": map[string]any{
				"type":        "remote",
				"url":         baseURL + "/mcp",
				"enabled":     true,
				"description": "PerGo Omnichannel CPaaS and Shared Team Inbox MCP Server",
				"headers": map[string]string{
					"Authorization": "Bearer {env:PERGO_API_KEY}",
				},
			},
		},
		"metadata": map[string]any{
			"server":      "PerGo Universal MCP Gateway",
			"version":     "1.2.0",
			"tools_count": len(tools),
		},
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(doc)
}

// HandleWellKnownOAuthProtectedResource serves RFC 9728 Protected Resource Metadata.
func (g *UniversalGateway) HandleWellKnownOAuthProtectedResource(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")

	baseURL := g.getBaseURL(r)

	doc := map[string]any{
		"resource":                 baseURL + "/mcp",
		"authorization_servers":    []string{baseURL},
		"scopes_supported":         []string{"mcp:all"},
		"bearer_methods_supported": []string{"header"},
		"resource_name":            "PerGo Universal MCP Gateway",
		"resource_documentation":   baseURL + "/docs",
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(doc)
}

// HandleWellKnownOAuthAuthorizationServer serves RFC 8414 Authorization Server Metadata.
func (g *UniversalGateway) HandleWellKnownOAuthAuthorizationServer(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")

	baseURL := g.getBaseURL(r)

	doc := map[string]any{
		"issuer":                                baseURL,
		"authorization_endpoint":                baseURL + "/oauth/authorize",
		"token_endpoint":                        baseURL + "/oauth/token",
		"registration_endpoint":                 baseURL + "/oauth/register",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none"},
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(doc)
}
