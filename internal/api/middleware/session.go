// Package middleware provides Echo v5 middleware functions for the OmniGo API.
package middleware

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/labstack/echo/v5"
)

const (
	sessionCookieName = "omnigo-session"
	sessionSecretLen  = 32
)

var (
	cachedSecret     []byte
	cachedSecretOnce sync.Once
)

// SessionAuthMiddleware returns an Echo middleware that checks for an
// authenticated session cookie and redirects to /admin/login if not authenticated.
// The session is a signed cookie containing "authenticated=true".
func SessionAuthMiddleware() echo.MiddlewareFunc {
	secret := getSessionSecret()
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			cookie, err := c.Cookie(sessionCookieName)
			if err != nil || cookie.Value == "" {
				return c.Redirect(http.StatusFound, "/admin/login")
			}

			// Verify the cookie signature
			if !VerifySessionCookie(cookie.Value, secret) {
				return c.Redirect(http.StatusFound, "/admin/login")
			}

			return next(c)
		}
	}
}

// SetSessionCookie sets a signed session cookie on the response.
func SetSessionCookie(c *echo.Context, secret []byte) {
	value := signSessionCookie("authenticated=true", secret)
	cookie := &http.Cookie{
		Name:     sessionCookieName,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}
	c.SetCookie(cookie)
}

// ClearSessionCookie removes the session cookie.
func ClearSessionCookie(c *echo.Context) {
	cookie := &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	}
	c.SetCookie(cookie)
}

// GetSessionSecret returns the session signing secret.
func GetSessionSecret() []byte {
	return getSessionSecret()
}

func getSessionSecret() []byte {
	cachedSecretOnce.Do(func() {
		if secret := os.Getenv("OMNIGO_SESSION_SECRET"); secret != "" {
			cachedSecret = []byte(secret)
			return
		}
		// Generate a random secret at boot (single-operator model — cookie survives restarts only within same process)
		secret := make([]byte, sessionSecretLen)
		if _, err := rand.Read(secret); err != nil {
			// Fallback to a fixed secret if crypto/rand fails (should never happen)
			secret = []byte("omnigo-session-fallback-secret-do-not-use")
		}
		cachedSecret = secret
	})
	return cachedSecret
}

// signSessionCookie creates an HMAC-signed cookie value: payload.signature
func signSessionCookie(payload string, secret []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(payload))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + sig
}

// VerifySessionCookie verifies the HMAC signature of a session cookie.
func VerifySessionCookie(value string, secret []byte) bool {
	parts := strings.SplitN(value, ".", 2)
	if len(parts) != 2 {
		return false
	}
	payloadDecoded, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return false
	}
	sigDecoded, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}

	mac := hmac.New(sha256.New, secret)
	mac.Write(payloadDecoded)
	expected := mac.Sum(nil)

	return hmac.Equal(sigDecoded, expected)
}
