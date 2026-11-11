package mcp

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/server"
	"github.com/pablojhp.pergo/internal/api/middleware"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/platform/crypto"
	"github.com/pablojhp.pergo/internal/platform/postgres/tenant"
	"github.com/pablojhp.pergo/internal/repository"
)

// UniversalGateway provides the unified Streamable HTTP gateway, dual-path authentication
// (deterministic Bearer API keys & OAuth 2.0 PKCE), and standard auto-discovery endpoints.
type UniversalGateway struct {
	server           *Server
	streamableServer *server.StreamableHTTPServer
	oauthManager     *OAuthManager
	externalURL      string
	sessionSecret    []byte
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

// SetSessionSecret configures the HMAC secret used to verify admin session cookies and OAuth CSRF tokens.
func (g *UniversalGateway) SetSessionSecret(secret []byte) {
	if g == nil {
		return
	}
	if len(secret) == 0 {
		g.sessionSecret = nil
		return
	}
	cp := make([]byte, len(secret))
	copy(cp, secret)
	g.sessionSecret = cp
}

func (g *UniversalGateway) effectiveSessionSecret() []byte {
	if len(g.sessionSecret) > 0 {
		return g.sessionSecret
	}
	return middleware.GetSessionSecret()
}

// ComputeOAuthCSRFToken derives an HMAC-SHA256 CSRF token bound to the active session cookie value.
func (g *UniversalGateway) ComputeOAuthCSRFToken(sessionCookieValue string) string {
	mac := hmac.New(sha256.New, g.effectiveSessionSecret())
	mac.Write([]byte("oauth-csrf:" + sessionCookieValue))
	return hex.EncodeToString(mac.Sum(nil))
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
// Requires an authenticated admin session cookie ("pergo-session") and explicit POST consent with CSRF verification.
func (g *UniversalGateway) HandleOAuthAuthorize(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error":             "method_not_allowed",
			"error_description": "only GET and POST are supported on /oauth/authorize",
		})
		return
	}

	secret := g.effectiveSessionSecret()
	cookie, cookieErr := r.Cookie("pergo-session")
	if cookieErr != nil || cookie.Value == "" || !middleware.VerifySessionCookie(cookie.Value, secret) {
		nextURI := r.URL.RequestURI()
		if nextURI == "" {
			nextURI = "/oauth/authorize"
		}
		http.Redirect(w, r, "/admin/login?next="+url.QueryEscape(nextURI), http.StatusFound)
		return
	}

	var (
		responseType        string
		clientID            string
		redirectURI         string
		codeChallenge       string
		codeChallengeMethod string
		state               string
		wsIDStr             string
	)

	if r.Method == http.MethodPost {
		_ = r.ParseForm()
		responseType = r.FormValue("response_type")
		clientID = r.FormValue("client_id")
		redirectURI = r.FormValue("redirect_uri")
		codeChallenge = r.FormValue("code_challenge")
		codeChallengeMethod = r.FormValue("code_challenge_method")
		state = r.FormValue("state")
		wsIDStr = r.FormValue("workspace_id")
	} else {
		q := r.URL.Query()
		responseType = q.Get("response_type")
		clientID = q.Get("client_id")
		redirectURI = q.Get("redirect_uri")
		codeChallenge = q.Get("code_challenge")
		codeChallengeMethod = q.Get("code_challenge_method")
		state = q.Get("state")
		wsIDStr = q.Get("workspace_id")
	}

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

	if r.Method == http.MethodGet {
		csrfToken := g.ComputeOAuthCSRFToken(cookie.Value)
		var workspaces []repository.Workspace
		if g.server != nil && g.server.wsRepo != nil && g.server.wsRepo.Pool() != nil {
			if list, err := g.server.wsRepo.List(r.Context(), 50); err == nil {
				workspaces = list
			}
		}

		clientDisplay := clientID
		if clientDisplay == "" {
			clientDisplay = "MCP Client"
		}
		if regClient, ok := g.oauthManager.GetClient(clientID); ok && regClient.ClientName != "" {
			clientDisplay = regClient.ClientName + " (" + clientID + ")"
		}

		g.renderOAuthConsentHTML(w, oauthConsentView{
			ClientID:            clientID,
			ClientDisplay:       clientDisplay,
			RedirectURI:         redirectURI,
			ResponseType:        responseType,
			CodeChallenge:       codeChallenge,
			CodeChallengeMethod: codeChallengeMethod,
			State:               state,
			SelectedWorkspaceID: wsIDStr,
			Workspaces:          workspaces,
			CSRFToken:           csrfToken,
		})
		return
	}

	// POST /oauth/authorize — verify CSRF token and explicit operator decision
	submittedCSRF := r.FormValue("csrf_token")
	expectedCSRF := g.ComputeOAuthCSRFToken(cookie.Value)
	if submittedCSRF == "" || subtle.ConstantTimeCompare([]byte(submittedCSRF), []byte(expectedCSRF)) != 1 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error":             "invalid_csrf_token",
			"error_description": "CSRF token validation failed",
		})
		return
	}

	decision := strings.ToLower(strings.TrimSpace(r.FormValue("decision")))
	if decision == "deny" {
		if redirectURI != "" {
			if u, err := url.Parse(redirectURI); err == nil && u.Scheme != "" {
				targetQ := u.Query()
				targetQ.Set("error", "access_denied")
				if state != "" {
					targetQ.Set("state", state)
				}
				u.RawQuery = targetQ.Encode()
				http.Redirect(w, r, u.String(), http.StatusFound)
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error":             "access_denied",
			"error_description": "The operator denied the authorization request",
		})
		return
	}

	if decision != "approve" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error":             "invalid_request",
			"error_description": "decision must be 'approve' or 'deny'",
		})
		return
	}

	var wsID uuid.UUID
	if wsIDStr != "" {
		wsID, _ = uuid.Parse(wsIDStr)
	}
	if wsID == uuid.Nil && g.server != nil && g.server.wsRepo != nil && g.server.wsRepo.Pool() != nil {
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

type oauthConsentView struct {
	ClientID            string
	ClientDisplay       string
	RedirectURI         string
	ResponseType        string
	CodeChallenge       string
	CodeChallengeMethod string
	State               string
	SelectedWorkspaceID string
	Workspaces          []repository.Workspace
	CSRFToken           string
}

func (g *UniversalGateway) renderOAuthConsentHTML(w http.ResponseWriter, v oauthConsentView) {
	var wsOptions strings.Builder
	hasSelected := false
	for _, ws := range v.Workspaces {
		idStr := ws.ID.String()
		selected := ""
		if v.SelectedWorkspaceID != "" && idStr == v.SelectedWorkspaceID {
			selected = " selected"
			hasSelected = true
		}
		fmt.Fprintf(&wsOptions, `<option value="%s"%s>%s (%s)</option>`,
			html.EscapeString(idStr),
			selected,
			html.EscapeString(ws.Name),
			html.EscapeString(idStr),
		)
	}
	if len(v.Workspaces) == 0 || (v.SelectedWorkspaceID != "" && !hasSelected) {
		val := v.SelectedWorkspaceID
		label := "Default Workspace"
		if val != "" {
			label = val
		}
		fmt.Fprintf(&wsOptions, `<option value="%s" selected>%s</option>`,
			html.EscapeString(val),
			html.EscapeString(label),
		)
	}

	redirectDisplay := v.RedirectURI
	if redirectDisplay == "" {
		redirectDisplay = "(direct token response)"
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Frame-Options", "DENY")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintf(w, `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Authorize MCP Access — PerGo</title>
  <style>
    * { box-sizing: border-box; }
    body {
      margin: 0;
      min-height: 100vh;
      display: flex;
      align-items: center;
      justify-content: center;
      padding: 24px;
      background: #fbfbfa;
      color: #37352f;
      font-family: Inter, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
      font-size: 14px;
      line-height: 1.5;
    }
    .card {
      width: 100%%;
      max-width: 460px;
      background: #ffffff;
      border: 1px solid #e9e9e7;
      border-radius: 16px;
      padding: 24px;
    }
    .header {
      display: flex;
      align-items: center;
      gap: 12px;
      margin-bottom: 16px;
    }
    .badge-icon {
      width: 36px;
      height: 36px;
      border-radius: 10px;
      background: #fbfbfa;
      border: 1px solid #e9e9e7;
      display: flex;
      align-items: center;
      justify-content: center;
      color: #0284c7;
      flex-shrink: 0;
    }
    h1 {
      margin: 0;
      font-size: 18px;
      font-weight: 600;
      letter-spacing: -0.25px;
    }
    .subtitle {
      margin: 0 0 16px 0;
      color: #787774;
      font-size: 13px;
    }
    .meta {
      background: #fbfbfa;
      border: 1px solid #e9e9e7;
      border-radius: 12px;
      padding: 12px;
      margin-bottom: 16px;
    }
    .meta-row {
      margin-bottom: 8px;
      font-size: 13px;
      word-break: break-all;
    }
    .meta-row:last-child { margin-bottom: 0; }
    .label {
      display: block;
      font-size: 11px;
      font-weight: 600;
      text-transform: uppercase;
      letter-spacing: 0.5px;
      color: #787774;
      margin-bottom: 4px;
    }
    select {
      width: 100%%;
      height: 38px;
      padding: 0 12px;
      border: 1px solid #e9e9e7;
      border-radius: 12px;
      background: #ffffff;
      color: #37352f;
      font-size: 13px;
      margin-bottom: 20px;
    }
    .actions {
      display: flex;
      gap: 12px;
      justify-content: flex-end;
    }
    button {
      height: 38px;
      padding: 0 16px;
      border-radius: 12px;
      font-size: 13px;
      font-weight: 600;
      cursor: pointer;
    }
    .btn-secondary {
      background: #fbfbfa;
      color: #37352f;
      border: 1px solid #e9e9e7;
    }
    .btn-primary {
      background: #0369a1;
      color: #ffffff;
      border: 1px solid #0369a1;
    }
  </style>
</head>
<body>
  <div class="card">
    <div class="header">
      <div class="badge-icon">
        <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"></path>
        </svg>
      </div>
      <div>
        <h1>Authorize MCP Client</h1>
      </div>
    </div>
    <p class="subtitle">An external MCP agent is requesting scoped access to your PerGo workspace.</p>
    <div class="meta">
      <div class="meta-row">
        <span class="label">Client ID</span>
        <code>%s</code>
      </div>
      <div class="meta-row">
        <span class="label">Redirect URI</span>
        <code>%s</code>
      </div>
    </div>
    <form method="POST" action="/oauth/authorize">
      <input type="hidden" name="csrf_token" value="%s">
      <input type="hidden" name="response_type" value="%s">
      <input type="hidden" name="client_id" value="%s">
      <input type="hidden" name="redirect_uri" value="%s">
      <input type="hidden" name="code_challenge" value="%s">
      <input type="hidden" name="code_challenge_method" value="%s">
      <input type="hidden" name="state" value="%s">
      <label class="label" for="workspace_id">Workspace</label>
      <select id="workspace_id" name="workspace_id">%s</select>
      <div class="actions">
        <button type="submit" name="decision" value="deny" class="btn-secondary">Deny</button>
        <button type="submit" name="decision" value="approve" class="btn-primary">Approve</button>
      </div>
    </form>
  </div>
</body>
</html>`,
		html.EscapeString(v.ClientDisplay),
		html.EscapeString(redirectDisplay),
		html.EscapeString(v.CSRFToken),
		html.EscapeString(v.ResponseType),
		html.EscapeString(v.ClientID),
		html.EscapeString(v.RedirectURI),
		html.EscapeString(v.CodeChallenge),
		html.EscapeString(v.CodeChallengeMethod),
		html.EscapeString(v.State),
		wsOptions.String(),
	)
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
