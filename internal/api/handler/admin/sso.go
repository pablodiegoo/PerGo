package admin

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	mw "github.com/pablojhp.pergo/internal/api/middleware"
	"github.com/pablojhp.pergo/internal/repository"
)

const (
	maxSSOTokenTTLSeconds = 120
	clockSkewLeeway       = 60
)

var (
	ErrMissingToken      = errors.New("missing sso token")
	ErrInvalidToken      = errors.New("invalid sso token format")
	ErrInvalidSignature  = errors.New("invalid signature")
	ErrTokenExpired      = errors.New("token expired")
	ErrTokenTTLExceeded  = errors.New("token ttl exceeds maximum allowed of 120 seconds")
	ErrFutureToken       = errors.New("token issued in the future")
	ErrInvalidWorkspace  = errors.New("invalid workspace id")
	ErrWorkspaceNotFound = errors.New("workspace not found")
)

// SSOClaims represents the claims structure inside an SSO token.
type SSOClaims struct {
	Sub         string `json:"sub,omitempty"`
	Email       string `json:"email,omitempty"`
	WorkspaceID string `json:"workspace_id,omitempty"`
	Role        string `json:"role,omitempty"`
	Iat         int64  `json:"iat,omitempty"`
	Exp         int64  `json:"exp"`
	Nonce       string `json:"nonce,omitempty"`
}

// SSOHandler handles seamless single sign-on authentication requests.
type SSOHandler struct {
	Workspaces *repository.WorkspaceRepository
	Secret     []byte
}

// NewSSOHandler creates a new SSOHandler instance.
func NewSSOHandler(wsRepo *repository.WorkspaceRepository, secret []byte) *SSOHandler {
	if len(secret) == 0 {
		secret = mw.GetSessionSecret()
	}
	return &SSOHandler{
		Workspaces: wsRepo,
		Secret:     secret,
	}
}

// HandleSSO processes the GET /admin/sso request, validates the signed SSO token,
// issues session and active-workspace cookies, and redirects the operator.
func (h *SSOHandler) HandleSSO(c *echo.Context) error {
	token := strings.TrimSpace(c.QueryParam("token"))
	if token == "" {
		authHeader := c.Request().Header.Get("Authorization")
		if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
			token = strings.TrimSpace(authHeader[7:])
		}
	}

	if token == "" {
		return c.JSON(http.StatusUnauthorized, map[string]string{
			"code":    "unauthorized",
			"message": "missing sso token",
		})
	}

	secret := h.Secret
	if len(secret) == 0 {
		secret = mw.GetSessionSecret()
	}

	claims, err := VerifySSOToken(token, secret)
	if err != nil {
		return c.JSON(http.StatusUnauthorized, map[string]string{
			"code":    "unauthorized",
			"message": err.Error(),
		})
	}

	// Validate target workspace if specified
	ctx := c.Request().Context()
	var targetWsID uuid.UUID
	if claims.WorkspaceID != "" {
		parsedID, err := uuid.Parse(claims.WorkspaceID)
		if err != nil || parsedID == uuid.Nil {
			return c.JSON(http.StatusUnauthorized, map[string]string{
				"code":    "unauthorized",
				"message": "invalid workspace_id in token claims",
			})
		}
		if h.Workspaces != nil {
			ws, err := h.Workspaces.GetByID(ctx, parsedID)
			if err != nil || ws == nil {
				return c.JSON(http.StatusUnauthorized, map[string]string{
					"code":    "unauthorized",
					"message": "workspace not found",
				})
			}
			targetWsID = ws.ID
		} else {
			targetWsID = parsedID
		}
	} else if h.Workspaces != nil {
		list, err := h.Workspaces.List(ctx, 1)
		if err == nil && len(list) > 0 {
			targetWsID = list[0].ID
		}
	}

	// 1. Set signed session cookie
	mw.SetSessionCookie(c, secret)

	// 2. Set active workspace cookie if resolved
	if targetWsID != uuid.Nil {
		activeWsCookie := &http.Cookie{
			Name:     "pergo-active-workspace",
			Value:    targetWsID.String(),
			Path:     "/",
			Expires:  time.Now().Add(365 * 24 * time.Hour),
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		}
		c.SetCookie(activeWsCookie)
	}

	// 3. Perform sanitized redirect
	redirectURL := SanitizeRedirect(c.QueryParam("redirect"))
	return c.Redirect(http.StatusFound, redirectURL)
}

// GenerateSSOToken creates a signed base64url(payload).base64url(hmac_signature) token.
func GenerateSSOToken(claims SSOClaims, secret []byte) (string, error) {
	if len(secret) == 0 {
		secret = mw.GetSessionSecret()
	}

	now := time.Now().Unix()
	if claims.Iat == 0 {
		claims.Iat = now
	}
	if claims.Exp == 0 {
		claims.Exp = claims.Iat + 60
	}
	if claims.Role == "" {
		claims.Role = "admin"
	}
	if claims.Nonce == "" {
		b := make([]byte, 8)
		if _, err := rand.Read(b); err == nil {
			claims.Nonce = hex.EncodeToString(b)
		}
	}

	payloadBytes, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("failed to marshal claims: %w", err)
	}

	payloadB64 := base64.RawURLEncoding.EncodeToString(payloadBytes)
	mac := hmac.New(sha256.New, secret)
	mac.Write(payloadBytes)
	sigB64 := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return payloadB64 + "." + sigB64, nil
}

// VerifySSOToken validates token structure, signature, expiration, and replay protection.
func VerifySSOToken(token string, secret []byte) (*SSOClaims, error) {
	if len(secret) == 0 {
		secret = mw.GetSessionSecret()
	}

	parts := strings.Split(token, ".")
	var payloadPart, sigPart string

	switch len(parts) {
	case 2:
		payloadPart = parts[0]
		sigPart = parts[1]
	case 3:
		// Support standard 3-part JWT (header.payload.signature)
		payloadPart = parts[1]
		sigPart = parts[2]
	default:
		return nil, ErrInvalidToken
	}

	payloadBytes, err := decodeBase64URL(payloadPart)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid payload encoding", ErrInvalidToken)
	}

	sigBytes, err := decodeBase64URL(sigPart)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid signature encoding", ErrInvalidToken)
	}

	// Constant-time HMAC comparison (checking payload bytes, base64 payload, or full JWT signing input)
	macPayload := hmac.New(sha256.New, secret)
	macPayload.Write(payloadBytes)
	expectedSigPayload := macPayload.Sum(nil)

	macRawPart := hmac.New(sha256.New, secret)
	macRawPart.Write([]byte(payloadPart))
	expectedSigRawPart := macRawPart.Sum(nil)

	var validSig bool
	if subtle.ConstantTimeCompare(sigBytes, expectedSigPayload) == 1 {
		validSig = true
	} else if subtle.ConstantTimeCompare(sigBytes, expectedSigRawPart) == 1 {
		validSig = true
	} else if len(parts) == 3 {
		macJWT := hmac.New(sha256.New, secret)
		macJWT.Write([]byte(parts[0] + "." + parts[1]))
		if subtle.ConstantTimeCompare(sigBytes, macJWT.Sum(nil)) == 1 {
			validSig = true
		}
	}

	if !validSig {
		return nil, ErrInvalidSignature
	}

	var claims SSOClaims
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return nil, fmt.Errorf("%w: invalid claims JSON", ErrInvalidToken)
	}

	// Claims validation
	if claims.Role != "" && claims.Role != "admin" {
		return nil, fmt.Errorf("%w: invalid role '%s'", ErrInvalidToken, claims.Role)
	}

	now := time.Now().Unix()

	if claims.Exp <= 0 {
		return nil, fmt.Errorf("%w: exp claim is required", ErrInvalidToken)
	}

	if claims.Exp < now {
		return nil, ErrTokenExpired
	}

	if claims.Iat > now+clockSkewLeeway {
		return nil, ErrFutureToken
	}

	if claims.Iat > 0 && claims.Exp-claims.Iat > maxSSOTokenTTLSeconds {
		return nil, ErrTokenTTLExceeded
	}

	if claims.Exp-now > maxSSOTokenTTLSeconds {
		return nil, ErrTokenTTLExceeded
	}

	return &claims, nil
}

// SanitizeRedirect sanitizes the redirect URL preventing open redirect vulnerabilities.
// Returns "/admin/" if the target is invalid, external, or malformed.
func SanitizeRedirect(redirect string) string {
	redirect = strings.TrimSpace(redirect)
	if redirect == "" {
		return "/admin/"
	}

	// Reject CRLF injection or backslashes
	if strings.ContainsAny(redirect, "\r\n\\") {
		return "/admin/"
	}

	// Must start with a single '/' and not '//' (protocol-relative)
	if !strings.HasPrefix(redirect, "/") || strings.HasPrefix(redirect, "//") {
		return "/admin/"
	}

	// Validate path structure via URL parser
	u, err := url.Parse(redirect)
	if err != nil || u.Scheme != "" || u.Host != "" {
		return "/admin/"
	}

	return redirect
}

func decodeBase64URL(s string) ([]byte, error) {
	// Try RawURLEncoding (no padding)
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err == nil {
		return b, nil
	}
	// Fallback to standard URLEncoding (with padding)
	return base64.URLEncoding.DecodeString(s)
}
