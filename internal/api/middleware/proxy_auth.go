// Package middleware provides Echo v5 middleware functions for the PerGo API and Admin panel.
package middleware

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/pablojhp.pergo/internal/config"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/platform/postgres/tenant"
)

const (
	// HeaderProxySignature is the HMAC-SHA256 signature header for internal reverse proxy attestation.
	HeaderProxySignature = "X-PerGo-Proxy-Signature"
	// HeaderWorkspaceScope is the authoritative target workspace UUID header.
	HeaderWorkspaceScope = "X-PerGo-Workspace-Scope"
	// HeaderActorID is the authenticated operator/user ID header.
	HeaderActorID = "X-PerGo-Actor-ID"
	// HeaderActorEmail is the authenticated operator email header.
	HeaderActorEmail = "X-PerGo-Actor-Email"
	// HeaderActorRole is the RBAC role header (e.g. admin, operator, member, billing_admin).
	HeaderActorRole = "X-PerGo-Actor-Role"
	// HeaderTimestamp is the unix timestamp header for replay attack prevention.
	HeaderTimestamp = "X-PerGo-Timestamp"
	// HeaderEmbedded indicates if the client requested frameless embedded rendering mode.
	HeaderEmbedded = "X-PerGo-Embedded"
)

// InternalProxyAuthMiddleware validates reverse-proxy attestation headers injected
// by PergoCloud or trusted API gateways.
//
// When X-PerGo-Proxy-Signature is present:
//  1. Validates unix timestamp in X-PerGo-Timestamp: verifies |time.Now().Unix() - timestamp| <= 60.
//     If expired or invalid format, returns HTTP 401 JSON: {"code":"unauthorized","message":"expired proxy timestamp"}.
//  2. Computes HMAC-SHA256 signature using masterKey over:
//     fmt.Sprintf("%s\n%s\n%s\n%s\n%s", scope, actorID, actorEmail, actorRole, timestamp)
//  3. Uses crypto/subtle.ConstantTimeCompare to verify signature.
//     If mismatch, returns HTTP 401 JSON: {"code":"unauthorized","message":"invalid proxy signature"}.
//  4. Enforces RBAC restrictions: billing_admin is strictly denied access to PII inbox data (403 Forbidden).
//  5. Parses scope as UUID and stores in context:
//     c.Set("workspace_id", wsID)
//     c.Set("actor_id", actorID)
//     c.Set("actor_email", actorEmail)
//     c.Set("actor_role", actorRole)
//     c.Set("authenticated", true)
//     c.Set("proxy_authenticated", true)
//     Also injects tenant and operator scope into request context.
//  6. Proceeds to handler.
//
// When X-PerGo-Proxy-Signature is absent, calls next(c) to allow existing session/auth middleware to evaluate.
func InternalProxyAuthMiddleware(masterKeys ...string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			sig := strings.TrimSpace(c.Request().Header.Get(HeaderProxySignature))
			if sig == "" {
				return next(c)
			}

			// 1. Validate unix timestamp (|time.Now().Unix() - timestamp| <= 60)
			rawTS := strings.TrimSpace(c.Request().Header.Get(HeaderTimestamp))
			if rawTS == "" {
				return c.JSON(http.StatusUnauthorized, map[string]string{
					"code":    "unauthorized",
					"message": "expired proxy timestamp",
				})
			}

			ts, err := strconv.ParseInt(rawTS, 10, 64)
			if err != nil {
				return c.JSON(http.StatusUnauthorized, map[string]string{
					"code":    "unauthorized",
					"message": "expired proxy timestamp",
				})
			}

			diff := time.Now().Unix() - ts
			if diff < 0 {
				diff = -diff
			}
			if diff > 60 {
				return c.JSON(http.StatusUnauthorized, map[string]string{
					"code":    "unauthorized",
					"message": "expired proxy timestamp",
				})
			}

			// 2. Resolve Master Key
			var masterKey string
			if len(masterKeys) > 0 && masterKeys[0] != "" {
				masterKey = masterKeys[0]
			} else {
				masterKey = os.Getenv("PERGO_MASTER_KEY")
			}

			if masterKey == "" {
				return c.JSON(http.StatusUnauthorized, map[string]string{
					"code":    "unauthorized",
					"message": "invalid proxy signature",
				})
			}

			// 3. Compute and Verify HMAC-SHA256 signature
			scope := c.Request().Header.Get(HeaderWorkspaceScope)
			actorID := c.Request().Header.Get(HeaderActorID)
			actorEmail := c.Request().Header.Get(HeaderActorEmail)
			actorRole := c.Request().Header.Get(HeaderActorRole)
			timestamp := c.Request().Header.Get(HeaderTimestamp)

			payload := fmt.Sprintf("%s\n%s\n%s\n%s\n%s", scope, actorID, actorEmail, actorRole, timestamp)
			mac := hmac.New(sha256.New, []byte(masterKey))
			mac.Write([]byte(payload))
			expectedHex := hex.EncodeToString(mac.Sum(nil))

			candidateSig := strings.TrimPrefix(sig, "sha256=")
			if subtle.ConstantTimeCompare([]byte(candidateSig), []byte(expectedHex)) != 1 {
				return c.JSON(http.StatusUnauthorized, map[string]string{
					"code":    "unauthorized",
					"message": "invalid proxy signature",
				})
			}

			// 4. RBAC Check: Billing_Admin strictly denied access per ADR-0006
			if strings.EqualFold(actorRole, "billing_admin") {
				return c.JSON(http.StatusForbidden, map[string]string{
					"code":    "forbidden",
					"message": "billing admin role cannot access inbox",
				})
			}

			// 5. Parse scope as UUID
			wsID, err := uuid.Parse(strings.TrimSpace(scope))
			if err != nil || wsID == uuid.Nil {
				return c.JSON(http.StatusUnauthorized, map[string]string{
					"code":    "unauthorized",
					"message": "invalid workspace scope",
				})
			}

			// 6. Store in context and request context
			c.Set("workspace_id", wsID)
			c.Set("actor_id", actorID)
			c.Set("actor_email", actorEmail)
			c.Set("actor_role", actorRole)
			c.Set("authenticated", true)
			c.Set("proxy_authenticated", true)

			ctx := c.Request().Context()
			ctx = tenant.WithWorkspaceID(ctx, wsID)
			ctx = domain.ContextWithScope(ctx, domain.NewOperatorScope(wsID))
			c.SetRequest(c.Request().WithContext(ctx))

			return next(c)
		}
	}
}

// InternalProxyAuth returns InternalProxyAuthMiddleware configured from a *config.Config.
func InternalProxyAuth(cfg *config.Config) echo.MiddlewareFunc {
	if cfg == nil {
		return InternalProxyAuthMiddleware()
	}
	return InternalProxyAuthMiddleware(cfg.MasterKey)
}

// SignProxyPayload computes the HMAC-SHA256 signature for proxy attestation headers.
func SignProxyPayload(scope, actorID, actorEmail, actorRole, timestamp, masterKey string) string {
	payload := fmt.Sprintf("%s\n%s\n%s\n%s\n%s", scope, actorID, actorEmail, actorRole, timestamp)
	mac := hmac.New(sha256.New, []byte(masterKey))
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}
