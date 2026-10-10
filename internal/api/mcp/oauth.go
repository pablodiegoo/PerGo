package mcp

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// OAuthClient represents a registered OAuth 2.0 client application.
type OAuthClient struct {
	ClientID     string    `json:"client_id"`
	ClientSecret string    `json:"client_secret,omitempty"`
	ClientName   string    `json:"client_name"`
	RedirectURIs []string  `json:"redirect_uris"`
	CreatedAt    time.Time `json:"created_at"`
}

// AuthCode represents an issued authorization code with PKCE parameters.
type AuthCode struct {
	Code                string
	ClientID            string
	RedirectURI         string
	CodeChallenge       string
	CodeChallengeMethod string
	WorkspaceID         uuid.UUID
	ExpiresAt           time.Time
}

// OAuthToken represents an active access token with its associated workspace scope.
type OAuthToken struct {
	AccessToken  string
	RefreshToken string
	ClientID     string
	WorkspaceID  uuid.UUID
	ExpiresAt    time.Time
}

// TokenResponse represents the RFC 6749 JSON response from the token endpoint.
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

// OAuthManager manages RFC 7591 dynamic client registration, RFC 7636 PKCE authorization codes,
// and short-lived access tokens for remote MCP clients.
type OAuthManager struct {
	mu      sync.RWMutex
	clients map[string]*OAuthClient
	codes   map[string]*AuthCode
	tokens  map[string]*OAuthToken // keyed by AccessToken
	refresh map[string]*OAuthToken // keyed by RefreshToken
}

// NewOAuthManager creates and initializes a new thread-safe OAuthManager.
func NewOAuthManager() *OAuthManager {
	return &OAuthManager{
		clients: make(map[string]*OAuthClient),
		codes:   make(map[string]*AuthCode),
		tokens:  make(map[string]*OAuthToken),
		refresh: make(map[string]*OAuthToken),
	}
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// RegisterClient dynamically registers an OAuth client according to RFC 7591.
func (m *OAuthManager) RegisterClient(name string, redirectURIs []string) (*OAuthClient, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	name = strings.TrimSpace(name)
	if name == "" {
		name = "mcp-client"
	}

	clientID := "client_" + uuid.New().String()
	clientSecret := "sec_" + randomHex(16)

	client := &OAuthClient{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		ClientName:   name,
		RedirectURIs: redirectURIs,
		CreatedAt:    time.Now().UTC(),
	}

	m.clients[clientID] = client
	return client, nil
}

// GetClient retrieves a registered OAuth client by client_id.
func (m *OAuthManager) GetClient(clientID string) (*OAuthClient, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	client, ok := m.clients[clientID]
	return client, ok
}

// Authorize generates a single-use authorization code bound to the PKCE code_challenge.
func (m *OAuthManager) Authorize(clientID, redirectURI, codeChallenge, codeChallengeMethod string, wsID uuid.UUID) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if codeChallenge == "" {
		return "", errors.New("code_challenge is required for PKCE")
	}

	if codeChallengeMethod != "S256" {
		return "", errors.New("code_challenge_method must be S256")
	}

	// Auto-register client if unknown (permissive for mcp-remote dynamic flows)
	if clientID != "" {
		if _, exists := m.clients[clientID]; !exists {
			m.clients[clientID] = &OAuthClient{
				ClientID:     clientID,
				ClientName:   "mcp-remote-client",
				RedirectURIs: []string{redirectURI},
				CreatedAt:    time.Now().UTC(),
			}
		}
	}

	code := "code_" + randomHex(24)
	m.codes[code] = &AuthCode{
		Code:                code,
		ClientID:            clientID,
		RedirectURI:         redirectURI,
		CodeChallenge:       codeChallenge,
		CodeChallengeMethod: codeChallengeMethod,
		WorkspaceID:         wsID,
		ExpiresAt:           time.Now().UTC().Add(5 * time.Minute),
	}

	return code, nil
}

// ExchangeToken validates the PKCE verifier and issues an access token and refresh token.
func (m *OAuthManager) ExchangeToken(grantType, code, codeVerifier, clientID, redirectURI, refreshToken string) (*TokenResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	switch grantType {
	case "authorization_code":
		if code == "" {
			return nil, errors.New("missing authorization code")
		}

		authCode, exists := m.codes[code]
		if !exists || time.Now().UTC().After(authCode.ExpiresAt) {
			delete(m.codes, code)
			return nil, errors.New("invalid or expired authorization code")
		}

		// Verify PKCE code_verifier against code_challenge using SHA-256 base64url unpadded
		h := sha256.Sum256([]byte(codeVerifier))
		computedChallenge := base64.RawURLEncoding.EncodeToString(h[:])
		if subtle.ConstantTimeCompare([]byte(computedChallenge), []byte(authCode.CodeChallenge)) != 1 {
			return nil, errors.New("PKCE verification failed: invalid code_verifier")
		}

		if authCode.RedirectURI != "" && redirectURI != "" && authCode.RedirectURI != redirectURI {
			return nil, errors.New("redirect_uri mismatch")
		}

		// Single-use: delete used code
		delete(m.codes, code)

		accessToken := "pergo_mcp_at_" + randomHex(24)
		newRefreshToken := "pergo_mcp_rt_" + randomHex(24)

		tok := &OAuthToken{
			AccessToken:  accessToken,
			RefreshToken: newRefreshToken,
			ClientID:     clientID,
			WorkspaceID:  authCode.WorkspaceID,
			ExpiresAt:    time.Now().UTC().Add(1 * time.Hour),
		}

		m.tokens[accessToken] = tok
		m.refresh[newRefreshToken] = tok

		return &TokenResponse{
			AccessToken:  accessToken,
			TokenType:    "Bearer",
			ExpiresIn:    3600,
			RefreshToken: newRefreshToken,
			Scope:        "mcp:all",
		}, nil

	case "refresh_token":
		if refreshToken == "" {
			return nil, errors.New("missing refresh_token")
		}

		existingTok, exists := m.refresh[refreshToken]
		if !exists || time.Now().UTC().After(existingTok.ExpiresAt.Add(30*24*time.Hour)) {
			return nil, errors.New("invalid or expired refresh token")
		}

		newAccessToken := "pergo_mcp_at_" + randomHex(24)
		existingTok.AccessToken = newAccessToken
		existingTok.ExpiresAt = time.Now().UTC().Add(1 * time.Hour)
		m.tokens[newAccessToken] = existingTok

		return &TokenResponse{
			AccessToken:  newAccessToken,
			TokenType:    "Bearer",
			ExpiresIn:    3600,
			RefreshToken: refreshToken,
			Scope:        "mcp:all",
		}, nil

	default:
		return nil, fmt.Errorf("unsupported grant_type: %s", grantType)
	}
}

// ValidateAccessToken checks if an OAuth access token is valid and unexpired, returning its WorkspaceID.
func (m *OAuthManager) ValidateAccessToken(token string) (uuid.UUID, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	tok, exists := m.tokens[token]
	if !exists {
		return uuid.Nil, false
	}

	if time.Now().UTC().After(tok.ExpiresAt) {
		return uuid.Nil, false
	}

	return tok.WorkspaceID, true
}
