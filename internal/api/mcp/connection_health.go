package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/pablojhp.pergo/internal/session"
)

// ProxyHealth represents the diagnostic probe results for a configured proxy.
type ProxyHealth struct {
	Configured bool   `json:"configured"`
	Reachable  bool   `json:"reachable"`
	LatencyMS  int64  `json:"latency_ms"`
	Error      string `json:"error,omitempty"`
}

// ConnectionHealthDiagnosticResult represents the structured diagnostic telemetry and guidance.
type ConnectionHealthDiagnosticResult struct {
	ConnectionID        uuid.UUID    `json:"connection_id"`
	WorkspaceID         uuid.UUID    `json:"workspace_id"`
	Name                string       `json:"name"`
	Channel             string       `json:"channel"`
	Status              string       `json:"status"`
	SocketStatus        string       `json:"socket_status"`
	ProxyHealth         *ProxyHealth `json:"proxy_health"`
	CredentialsValid    bool         `json:"credentials_valid"`
	LatencyMS           int64        `json:"latency_ms"`
	PairingStatus       string       `json:"pairing_status,omitempty"`
	LastError           string       `json:"last_error,omitempty"`
	SelfHealingGuidance string       `json:"self_healing_guidance"`
}

// probeProxyTCP dials the proxy host:port with a timeout to verify reachability and measure latency.
// For SOCKS5 proxies, it executes a SOCKS5 greeting handshake to confirm protocol responsiveness.
func probeProxyTCP(ctx context.Context, proxyURL string, timeout time.Duration) (bool, int64, error) {
	if timeout <= 0 {
		timeout = 3 * time.Second
	}

	target := strings.TrimSpace(proxyURL)
	var isSocks5 bool
	if strings.Contains(target, "://") {
		if u, err := url.Parse(target); err == nil && u.Host != "" {
			scheme := strings.ToLower(u.Scheme)
			if scheme == "socks5" || scheme == "socks5h" {
				isSocks5 = true
			}
			target = u.Host
			if !strings.Contains(target, ":") {
				switch scheme {
				case "https":
					target += ":443"
				case "socks5", "socks5h":
					target += ":1080"
				default:
					target += ":80"
				}
			}
		}
	}

	if !strings.Contains(target, ":") {
		target += ":80"
	}

	start := time.Now()
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "tcp", target)
	if err != nil {
		return false, time.Since(start).Milliseconds(), err
	}
	defer conn.Close()

	if isSocks5 {
		if deadline, ok := ctx.Deadline(); ok {
			_ = conn.SetDeadline(deadline)
		} else {
			_ = conn.SetDeadline(time.Now().Add(timeout))
		}

		if _, err := conn.Write([]byte{0x05, 0x01, 0x00}); err != nil {
			return false, time.Since(start).Milliseconds(), fmt.Errorf("socks5 greeting write failed: %w", err)
		}

		buf := make([]byte, 2)
		if _, err := io.ReadFull(conn, buf); err != nil {
			return false, time.Since(start).Milliseconds(), fmt.Errorf("socks5 greeting read failed: %w", err)
		}

		if buf[0] != 0x05 || buf[1] != 0x00 {
			return false, time.Since(start).Milliseconds(), fmt.Errorf("socks5 handshake rejected: received version %d method %d", buf[0], buf[1])
		}
	}

	latency := time.Since(start).Milliseconds()
	return true, latency, nil
}

// handleDiagnoseConnectionHealth implements the diagnose_connection_health MCP tool.
func (s *Server) handleDiagnoseConnectionHealth(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	wsIDStr, err := request.RequireString("workspace_id")
	if err != nil {
		return mcp.NewToolResultError("missing or invalid workspace_id argument"), nil
	}

	workspaceID, err := uuid.Parse(wsIDStr)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("invalid workspace_id UUID: %v", err)), nil
	}

	connIDStr, err := request.RequireString("connection_id")
	if err != nil {
		return mcp.NewToolResultError("missing or invalid connection_id argument"), nil
	}

	connID, err := uuid.Parse(connIDStr)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("invalid connection_id UUID: %v", err)), nil
	}

	// Validate workspace existence
	if _, err := s.wsRepo.GetByID(ctx, workspaceID); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("workspace not found: %v", err)), nil
	}

	// Validate connection existence and workspace ownership
	conn, err := s.connectionRepo.GetByID(ctx, connID)
	if err != nil || conn == nil || conn.WorkspaceID != workspaceID {
		return mcp.NewToolResultError("connection not found for workspace"), nil
	}

	proxyHealth := &ProxyHealth{
		Configured: false,
		Reachable:  false,
		LatencyMS:  0,
	}
	var overallLatency int64

	if conn.ProxyURL != nil && strings.TrimSpace(*conn.ProxyURL) != "" {
		proxyHealth.Configured = true
		reachable, latency, probeErr := probeProxyTCP(ctx, *conn.ProxyURL, 3*time.Second)
		proxyHealth.Reachable = reachable
		proxyHealth.LatencyMS = latency
		overallLatency = latency
		if probeErr != nil {
			proxyHealth.Error = probeErr.Error()
		}
	}

	channelName := strings.ToLower(strings.TrimSpace(conn.Channel))
	socketStatus := "not_applicable"
	pairingStatus := ""
	lastError := ""
	credentialsValid := false
	isAuthFailure := false

	switch channelName {
	case "whatsapp":
		socketStatus = "disconnected"
		if s.sessionManager != nil {
			health, err := s.sessionManager.SessionHealth(conn.ID)
			if err == nil && health != nil {
				if health.State != "" {
					socketStatus = string(health.State)
				}
				if health.LastError != "" {
					lastError = health.LastError
				}
			}

			if evt, ok := s.sessionManager.GetPairingStateForWorkspace(conn.WorkspaceID, conn.ID.String()); ok && evt != nil {
				pairingStatus = evt.Status
			} else if conn.SenderIdentity != "" {
				if evt, ok := s.sessionManager.GetPairingStateForWorkspace(conn.WorkspaceID, conn.SenderIdentity); ok && evt != nil {
					pairingStatus = evt.Status
				}
			}
		}

		if socketStatus == string(session.StateConnected) || (conn.JID != nil && strings.TrimSpace(*conn.JID) != "") || pairingStatus == "paired" {
			credentialsValid = true
		}
		if socketStatus == string(session.StateDisconnectedBanned) || strings.Contains(strings.ToLower(lastError), "banned") || strings.Contains(strings.ToLower(lastError), "logged out") {
			isAuthFailure = true
		}

	case "telegram":
		var tgToken string
		var tgConfig struct {
			Token string `json:"token"`
		}
		if len(conn.Credentials) > 0 {
			if err := json.Unmarshal(conn.Credentials, &tgConfig); err == nil && strings.TrimSpace(tgConfig.Token) != "" {
				tgToken = strings.TrimSpace(tgConfig.Token)
			} else {
				var rawStr string
				if err := json.Unmarshal(conn.Credentials, &rawStr); err == nil && strings.TrimSpace(rawStr) != "" {
					tgToken = strings.TrimSpace(rawStr)
				} else {
					tgToken = strings.TrimSpace(string(conn.Credentials))
				}
			}
		}

		if tgToken == "" {
			lastError = "missing bot token in credentials"
			isAuthFailure = true
		} else {
			baseURL := s.telegramBaseURL
			if baseURL == "" {
				baseURL = "https://api.telegram.org"
			}
			endpoint := fmt.Sprintf("%s/bot%s/getMe", baseURL, tgToken)

			httpClient := s.httpClient
			if httpClient == nil {
				httpClient = &http.Client{Timeout: 3 * time.Second}
			}

			reqCtx, reqCancel := context.WithTimeout(ctx, 3*time.Second)
			defer reqCancel()

			req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, endpoint, nil)
			if err != nil {
				lastError = fmt.Sprintf("failed to build telegram probe request: %v", err)
			} else {
				start := time.Now()
				resp, err := httpClient.Do(req)
				tgLatency := time.Since(start).Milliseconds()
				overallLatency = tgLatency

				if err != nil {
					lastError = fmt.Sprintf("telegram probe request failed: %v", err)
				} else {
					defer resp.Body.Close()
					if resp.StatusCode == http.StatusOK {
						credentialsValid = true
						socketStatus = "connected"
					} else if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound {
						lastError = fmt.Sprintf("telegram bot token unauthorized (HTTP %d)", resp.StatusCode)
						isAuthFailure = true
						socketStatus = "disconnected"
					} else {
						lastError = fmt.Sprintf("telegram bot API returned HTTP %d", resp.StatusCode)
						socketStatus = "disconnected"
					}
				}
			}
		}

	case "whatsapp_cloud":
		var wabaCreds struct {
			PhoneNumberID string `json:"phone_number_id"`
			Token         string `json:"token"`
			AccessToken   string `json:"access_token"`
		}

		if len(conn.Credentials) == 0 {
			lastError = "missing credentials for whatsapp_cloud connection"
			isAuthFailure = true
		} else if err := json.Unmarshal(conn.Credentials, &wabaCreds); err != nil {
			lastError = fmt.Sprintf("invalid credentials format: %v", err)
			isAuthFailure = true
		} else {
			token := wabaCreds.Token
			if token == "" {
				token = wabaCreds.AccessToken
			}
			phoneID := strings.TrimSpace(wabaCreds.PhoneNumberID)
			token = strings.TrimSpace(token)

			if phoneID == "" || token == "" {
				lastError = "missing phone_number_id or token in credentials"
				isAuthFailure = true
			} else {
				isNumeric := true
				for _, ch := range phoneID {
					if ch < '0' || ch > '9' {
						isNumeric = false
						break
					}
				}
				if !isNumeric {
					lastError = "invalid phone_number_id format: expected numeric identifier"
					isAuthFailure = true
				} else {
					credentialsValid = true
					socketStatus = "connected"
				}
			}
		}

	default:
		socketStatus = "not_applicable"
		credentialsValid = len(conn.Credentials) > 0
	}

	var status string
	var selfHealingGuidance string

	if proxyHealth.Configured && !proxyHealth.Reachable {
		status = "degraded"
		selfHealingGuidance = "Inspect dedicated proxy host/port and firewall credentials"
		if lastError == "" && proxyHealth.Error != "" {
			lastError = fmt.Sprintf("proxy unreachable: %s", proxyHealth.Error)
		}
	} else if socketStatus == string(session.StateDisconnectedBanned) || isAuthFailure {
		status = "banned"
		selfHealingGuidance = "Session expired or banned by provider; halt reconnections and re-authenticate"
	} else if channelName == "whatsapp" && (socketStatus == string(session.StateDisconnected) || !credentialsValid || pairingStatus == "qr_ready" || pairingStatus == "pairing") {
		status = "disconnected"
		selfHealingGuidance = "Trigger pairing via get_connection_qr_code"
	} else if socketStatus == string(session.StateConnecting) || socketStatus == string(session.StateReconnecting) {
		status = "degraded"
		selfHealingGuidance = "Connection is establishing or reconnecting; wait for socket handshake to complete"
	} else if !credentialsValid {
		status = "error"
		selfHealingGuidance = "Session expired or banned by provider; halt reconnections and re-authenticate"
	} else {
		status = "healthy"
		selfHealingGuidance = "Connection is healthy and active"
	}

	res := ConnectionHealthDiagnosticResult{
		ConnectionID:        conn.ID,
		WorkspaceID:         conn.WorkspaceID,
		Name:                conn.Name,
		Channel:             conn.Channel,
		Status:              status,
		SocketStatus:        socketStatus,
		ProxyHealth:         proxyHealth,
		CredentialsValid:    credentialsValid,
		LatencyMS:           overallLatency,
		PairingStatus:       pairingStatus,
		LastError:           lastError,
		SelfHealingGuidance: selfHealingGuidance,
	}

	resBytes, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to marshal diagnostic result: %v", err)), nil
	}

	return mcp.NewToolResultText(string(resBytes)), nil
}
