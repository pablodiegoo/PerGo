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
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/pablojhp.pergo/internal/session"
)

// ProxyHealth represents the diagnostic check results for a configured proxy.
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

// verifyProxyConnectivity dials the proxy host:port with a timeout to verify reachability and measure latency.
// For SOCKS5 proxies, it executes a SOCKS5 greeting handshake (supporting both anonymous and username/password auth)
// to confirm protocol responsiveness.
func verifyProxyConnectivity(ctx context.Context, proxyURL string, timeout time.Duration) (bool, int64, error) {
	if timeout <= 0 {
		timeout = 3 * time.Second
	}

	target := strings.TrimSpace(proxyURL)
	var isSocks5 bool
	var username, password string
	var hasAuth bool

	if strings.Contains(target, "://") {
		if u, err := url.Parse(target); err == nil && u.Host != "" {
			scheme := strings.ToLower(u.Scheme)
			if scheme == "socks5" || scheme == "socks5h" {
				isSocks5 = true
			}
			if u.User != nil {
				username = u.User.Username()
				password, _ = u.User.Password()
				if username != "" {
					hasAuth = true
				}
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

		greeting := []byte{0x05, 0x01, 0x00}
		if hasAuth {
			greeting = []byte{0x05, 0x02, 0x00, 0x02}
		}

		if _, err := conn.Write(greeting); err != nil {
			return false, time.Since(start).Milliseconds(), fmt.Errorf("socks5 greeting write failed: %w", err)
		}

		buf := make([]byte, 2)
		if _, err := io.ReadFull(conn, buf); err != nil {
			return false, time.Since(start).Milliseconds(), fmt.Errorf("socks5 greeting read failed: %w", err)
		}

		if buf[0] != 0x05 {
			return false, time.Since(start).Milliseconds(), fmt.Errorf("socks5 handshake rejected: received version %d", buf[0])
		}

		if buf[1] == 0xFF {
			return false, time.Since(start).Milliseconds(), fmt.Errorf("socks5 handshake rejected: no acceptable authentication methods")
		}

		if buf[1] == 0x02 {
			if !hasAuth {
				return false, time.Since(start).Milliseconds(), fmt.Errorf("socks5 server requires authentication but none provided")
			}
			authReq := make([]byte, 0, 3+len(username)+len(password))
			authReq = append(authReq, 0x01, byte(len(username)))
			authReq = append(authReq, []byte(username)...)
			authReq = append(authReq, byte(len(password)))
			authReq = append(authReq, []byte(password)...)

			if _, err := conn.Write(authReq); err != nil {
				return false, time.Since(start).Milliseconds(), fmt.Errorf("socks5 auth write failed: %w", err)
			}

			authResp := make([]byte, 2)
			if _, err := io.ReadFull(conn, authResp); err != nil {
				return false, time.Since(start).Milliseconds(), fmt.Errorf("socks5 auth response read failed: %w", err)
			}
			if authResp[1] != 0x00 {
				return false, time.Since(start).Milliseconds(), fmt.Errorf("socks5 authentication failed (status %d)", authResp[1])
			}
		} else if buf[1] != 0x00 {
			return false, time.Since(start).Milliseconds(), fmt.Errorf("socks5 handshake rejected: unsupported method %d", buf[1])
		}
	}

	latency := time.Since(start).Milliseconds()
	return true, latency, nil
}

func diagnoseWhatsAppHealth(conn *repository.Connection, sm *session.Manager) (string, string, bool, bool, string) {
	socketStatus := "disconnected"
	pairingStatus := ""
	lastError := ""
	credentialsValid := false
	isAuthFailure := false

	if sm != nil {
		health, err := sm.SessionHealth(conn.ID)
		if err == nil && health != nil {
			if health.State != "" {
				socketStatus = string(health.State)
			}
			if health.LastError != "" {
				lastError = health.LastError
			}
		}

		if evt, ok := sm.GetPairingStateForWorkspace(conn.WorkspaceID, conn.ID.String()); ok && evt != nil {
			pairingStatus = evt.Status
		} else if conn.SenderIdentity != "" {
			if evt, ok := sm.GetPairingStateForWorkspace(conn.WorkspaceID, conn.SenderIdentity); ok && evt != nil {
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

	return socketStatus, pairingStatus, credentialsValid, isAuthFailure, lastError
}

func (s *Server) diagnoseTelegramHealth(ctx context.Context, conn *repository.Connection) (string, bool, bool, int64, string) {
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
		return "disconnected", false, true, 0, "missing bot token in credentials"
	}

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
		return "disconnected", false, false, 0, fmt.Sprintf("failed to build telegram health check request: %v", err)
	}

	start := time.Now()
	resp, err := httpClient.Do(req)
	tgLatency := time.Since(start).Milliseconds()

	if err != nil {
		return "disconnected", false, false, tgLatency, fmt.Sprintf("telegram health check request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return "connected", true, false, tgLatency, ""
	} else if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound {
		return "disconnected", false, true, tgLatency, fmt.Sprintf("telegram bot token unauthorized (HTTP %d)", resp.StatusCode)
	}
	return "disconnected", false, false, tgLatency, fmt.Sprintf("telegram bot API returned HTTP %d", resp.StatusCode)
}

func diagnoseWABAHealth(conn *repository.Connection) (string, bool, bool, string) {
	var wabaCreds struct {
		PhoneNumberID string `json:"phone_number_id"`
		Token         string `json:"token"`
		AccessToken   string `json:"access_token"`
	}

	if len(conn.Credentials) == 0 {
		return "not_applicable", false, true, "missing credentials for whatsapp_cloud connection"
	}
	if err := json.Unmarshal(conn.Credentials, &wabaCreds); err != nil {
		return "not_applicable", false, true, fmt.Sprintf("invalid credentials format: %v", err)
	}

	token := wabaCreds.Token
	if token == "" {
		token = wabaCreds.AccessToken
	}
	phoneID := strings.TrimSpace(wabaCreds.PhoneNumberID)
	token = strings.TrimSpace(token)

	if phoneID == "" || token == "" {
		return "not_applicable", false, true, "missing phone_number_id or token in credentials"
	}

	for _, ch := range phoneID {
		if ch < '0' || ch > '9' {
			return "not_applicable", false, true, "invalid phone_number_id format: expected numeric identifier"
		}
	}

	return "connected", true, false, ""
}

func computeConnectionSelfHealing(
	channelName string,
	proxyHealth *ProxyHealth,
	socketStatus string,
	isAuthFailure bool,
	credentialsValid bool,
	pairingStatus string,
	lastError string,
) (string, string, string) {
	var status string
	var selfHealingGuidance string

	if proxyHealth != nil && proxyHealth.Configured && !proxyHealth.Reachable {
		status = "degraded"
		selfHealingGuidance = "Inspect dedicated proxy host/port and firewall credentials"
		if lastError == "" && proxyHealth.Error != "" {
			lastError = fmt.Sprintf("proxy unreachable: %s", proxyHealth.Error)
		}
	} else if socketStatus == string(session.StateDisconnectedBanned) || isAuthFailure {
		status = "banned"
		selfHealingGuidance = "Session expired or banned by provider; halt reconnections and re-authenticate"
	} else if channelName == "whatsapp" && (!credentialsValid || pairingStatus == "qr_ready" || pairingStatus == "pairing" || pairingStatus == "unpaired") {
		status = "disconnected"
		selfHealingGuidance = "Trigger pairing via get_connection_qr_code"
	} else if channelName == "whatsapp" && socketStatus == string(session.StateDisconnected) {
		status = "disconnected"
		selfHealingGuidance = "Connection is disconnected; verify network connectivity or restart session via session manager"
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

	return status, selfHealingGuidance, lastError
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
		reachable, latency, checkErr := verifyProxyConnectivity(ctx, *conn.ProxyURL, 3*time.Second)
		proxyHealth.Reachable = reachable
		proxyHealth.LatencyMS = latency
		overallLatency = latency
		if checkErr != nil {
			proxyHealth.Error = checkErr.Error()
		}
	}

	channelName := strings.ToLower(strings.TrimSpace(conn.Channel))
	var socketStatus string
	pairingStatus := ""
	lastError := ""
	credentialsValid := false
	isAuthFailure := false

	switch channelName {
	case "whatsapp":
		socketStatus, pairingStatus, credentialsValid, isAuthFailure, lastError = diagnoseWhatsAppHealth(conn, s.sessionManager)

	case "telegram":
		var tgLatency int64
		socketStatus, credentialsValid, isAuthFailure, tgLatency, lastError = s.diagnoseTelegramHealth(ctx, conn)
		if tgLatency > 0 {
			overallLatency = tgLatency
		}

	case "whatsapp_cloud":
		socketStatus, credentialsValid, isAuthFailure, lastError = diagnoseWABAHealth(conn)

	default:
		socketStatus = "not_applicable"
		credentialsValid = len(conn.Credentials) > 0
	}

	status, selfHealingGuidance, lastError := computeConnectionSelfHealing(
		channelName,
		proxyHealth,
		socketStatus,
		isAuthFailure,
		credentialsValid,
		pairingStatus,
		lastError,
	)

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
