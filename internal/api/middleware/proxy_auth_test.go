package middleware_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/pablojhp.pergo/internal/api/middleware"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/platform/postgres/tenant"
)

func computeTestSig(scope, actorID, actorEmail, actorRole, timestamp, masterKey string) string {
	payload := fmt.Sprintf("%s\n%s\n%s\n%s\n%s", scope, actorID, actorEmail, actorRole, timestamp)
	mac := hmac.New(sha256.New, []byte(masterKey))
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

func TestInternalProxyAuthMiddleware_ValidHMAC(t *testing.T) {
	masterKey := "super-secret-master-key-12345"
	mw := middleware.InternalProxyAuthMiddleware(masterKey)

	wsID := uuid.New()
	actorID := "user-usr_998877"
	actorEmail := "operator@company.com"
	actorRole := "operator"
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := computeTestSig(wsID.String(), actorID, actorEmail, actorRole, ts, masterKey)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/admin/inbox", nil)
	req.Header.Set(middleware.HeaderProxySignature, sig)
	req.Header.Set(middleware.HeaderWorkspaceScope, wsID.String())
	req.Header.Set(middleware.HeaderActorID, actorID)
	req.Header.Set(middleware.HeaderActorEmail, actorEmail)
	req.Header.Set(middleware.HeaderActorRole, actorRole)
	req.Header.Set(middleware.HeaderTimestamp, ts)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	handlerCalled := false
	handler := mw(func(c *echo.Context) error {
		handlerCalled = true
		return c.String(http.StatusOK, "ok")
	})

	err := handler(c)
	if err != nil {
		t.Fatalf("unexpected handler error: %v", err)
	}

	if !handlerCalled {
		t.Fatal("expected handler to be called")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got: %d", rec.Code)
	}

	// Verify Echo context variables
	if c.Get("workspace_id") != wsID {
		t.Errorf("expected workspace_id %v, got %v", wsID, c.Get("workspace_id"))
	}
	if c.Get("actor_id") != actorID {
		t.Errorf("expected actor_id %s, got %v", actorID, c.Get("actor_id"))
	}
	if c.Get("actor_email") != actorEmail {
		t.Errorf("expected actor_email %s, got %v", actorEmail, c.Get("actor_email"))
	}
	if c.Get("actor_role") != actorRole {
		t.Errorf("expected actor_role %s, got %v", actorRole, c.Get("actor_role"))
	}
	if c.Get("authenticated") != true {
		t.Errorf("expected authenticated=true, got %v", c.Get("authenticated"))
	}
	if c.Get("proxy_authenticated") != true {
		t.Errorf("expected proxy_authenticated=true, got %v", c.Get("proxy_authenticated"))
	}

	// Verify request context tenant and domain scope
	tenantID, hasTenant := tenant.WorkspaceIDFrom(c.Request().Context())
	if !hasTenant || tenantID != wsID {
		t.Errorf("expected tenant workspace ID %v, got %v (hasTenant=%v)", wsID, tenantID, hasTenant)
	}

	scope, scopeErr := domain.Require(c.Request().Context())
	if scopeErr != nil || scope.WorkspaceID() != wsID {
		t.Errorf("expected domain scope workspace ID %v, got %v (err=%v)", wsID, scope.WorkspaceID(), scopeErr)
	}
}

func TestInternalProxyAuthMiddleware_Sha256Prefix(t *testing.T) {
	masterKey := "master-key-prefix-test"
	mw := middleware.InternalProxyAuthMiddleware(masterKey)

	wsID := uuid.New()
	actorID := "user-admin"
	actorEmail := "admin@pergo.dev"
	actorRole := "admin"
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := "sha256=" + computeTestSig(wsID.String(), actorID, actorEmail, actorRole, ts, masterKey)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/admin/inbox", nil)
	req.Header.Set(middleware.HeaderProxySignature, sig)
	req.Header.Set(middleware.HeaderWorkspaceScope, wsID.String())
	req.Header.Set(middleware.HeaderActorID, actorID)
	req.Header.Set(middleware.HeaderActorEmail, actorEmail)
	req.Header.Set(middleware.HeaderActorRole, actorRole)
	req.Header.Set(middleware.HeaderTimestamp, ts)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	handler := mw(func(c *echo.Context) error {
		return c.String(http.StatusOK, "ok")
	})

	if err := handler(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got: %d", rec.Code)
	}
}

func TestInternalProxyAuthMiddleware_AbsentHeader_PassesThrough(t *testing.T) {
	mw := middleware.InternalProxyAuthMiddleware("some-key")

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/admin/inbox", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	called := false
	handler := mw(func(c *echo.Context) error {
		called = true
		return c.String(http.StatusOK, "passed-through")
	})

	err := handler(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("expected handler to be called when proxy header is absent")
	}
	if c.Get("proxy_authenticated") != nil {
		t.Errorf("proxy_authenticated should not be set when header absent, got %v", c.Get("proxy_authenticated"))
	}
}

func TestInternalProxyAuthMiddleware_ExpiredTimestamp(t *testing.T) {
	masterKey := "master-key"
	mw := middleware.InternalProxyAuthMiddleware(masterKey)

	wsID := uuid.New()
	actorID := "user-123"
	actorEmail := "test@test.com"
	actorRole := "operator"

	// 65 seconds in the past -> expired
	pastTS := strconv.FormatInt(time.Now().Unix()-65, 10)
	sig := computeTestSig(wsID.String(), actorID, actorEmail, actorRole, pastTS, masterKey)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/admin/inbox", nil)
	req.Header.Set(middleware.HeaderProxySignature, sig)
	req.Header.Set(middleware.HeaderWorkspaceScope, wsID.String())
	req.Header.Set(middleware.HeaderActorID, actorID)
	req.Header.Set(middleware.HeaderActorEmail, actorEmail)
	req.Header.Set(middleware.HeaderActorRole, actorRole)
	req.Header.Set(middleware.HeaderTimestamp, pastTS)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	handler := mw(func(c *echo.Context) error {
		t.Fatal("handler should not have been called")
		return nil
	})

	_ = handler(c)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized for expired timestamp, got: %d", rec.Code)
	}

	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse JSON response: %v", err)
	}
	if resp["code"] != "unauthorized" || resp["message"] != "expired proxy timestamp" {
		t.Errorf("unexpected error payload: %+v", resp)
	}
}

func TestInternalProxyAuthMiddleware_FutureTimestamp(t *testing.T) {
	masterKey := "master-key"
	mw := middleware.InternalProxyAuthMiddleware(masterKey)

	wsID := uuid.New()
	actorID := "user-123"
	actorEmail := "test@test.com"
	actorRole := "operator"

	// 70 seconds in the future -> outside 60s tolerance window
	futureTS := strconv.FormatInt(time.Now().Unix()+70, 10)
	sig := computeTestSig(wsID.String(), actorID, actorEmail, actorRole, futureTS, masterKey)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/admin/inbox", nil)
	req.Header.Set(middleware.HeaderProxySignature, sig)
	req.Header.Set(middleware.HeaderWorkspaceScope, wsID.String())
	req.Header.Set(middleware.HeaderActorID, actorID)
	req.Header.Set(middleware.HeaderActorEmail, actorEmail)
	req.Header.Set(middleware.HeaderActorRole, actorRole)
	req.Header.Set(middleware.HeaderTimestamp, futureTS)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	handler := mw(func(c *echo.Context) error {
		t.Fatal("handler should not have been called")
		return nil
	})

	_ = handler(c)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized for future timestamp, got: %d", rec.Code)
	}
}

func TestInternalProxyAuthMiddleware_MalformedTimestamp(t *testing.T) {
	masterKey := "master-key"
	mw := middleware.InternalProxyAuthMiddleware(masterKey)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/admin/inbox", nil)
	req.Header.Set(middleware.HeaderProxySignature, "some-sig")
	req.Header.Set(middleware.HeaderTimestamp, "not-a-number")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	handler := mw(func(c *echo.Context) error {
		t.Fatal("handler should not have been called")
		return nil
	})

	_ = handler(c)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for malformed timestamp, got: %d", rec.Code)
	}
}

func TestInternalProxyAuthMiddleware_InvalidSignature(t *testing.T) {
	masterKey := "real-master-key"
	mw := middleware.InternalProxyAuthMiddleware(masterKey)

	wsID := uuid.New()
	actorID := "user-123"
	actorEmail := "test@test.com"
	actorRole := "operator"
	ts := strconv.FormatInt(time.Now().Unix(), 10)

	// Tampered signature or signed with wrong key
	wrongSig := computeTestSig(wsID.String(), actorID, actorEmail, actorRole, ts, "wrong-master-key")

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/admin/inbox", nil)
	req.Header.Set(middleware.HeaderProxySignature, wrongSig)
	req.Header.Set(middleware.HeaderWorkspaceScope, wsID.String())
	req.Header.Set(middleware.HeaderActorID, actorID)
	req.Header.Set(middleware.HeaderActorEmail, actorEmail)
	req.Header.Set(middleware.HeaderActorRole, actorRole)
	req.Header.Set(middleware.HeaderTimestamp, ts)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	handler := mw(func(c *echo.Context) error {
		t.Fatal("handler should not have been called")
		return nil
	})

	_ = handler(c)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized for invalid signature, got: %d", rec.Code)
	}

	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse JSON response: %v", err)
	}
	if resp["code"] != "unauthorized" || resp["message"] != "invalid proxy signature" {
		t.Errorf("unexpected error payload: %+v", resp)
	}
}

func TestInternalProxyAuthMiddleware_BillingAdmin_Forbidden(t *testing.T) {
	masterKey := "master-key"
	mw := middleware.InternalProxyAuthMiddleware(masterKey)

	wsID := uuid.New()
	actorID := "user-billing"
	actorEmail := "billing@company.com"
	actorRole := "billing_admin"
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := computeTestSig(wsID.String(), actorID, actorEmail, actorRole, ts, masterKey)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/admin/inbox", nil)
	req.Header.Set(middleware.HeaderProxySignature, sig)
	req.Header.Set(middleware.HeaderWorkspaceScope, wsID.String())
	req.Header.Set(middleware.HeaderActorID, actorID)
	req.Header.Set(middleware.HeaderActorEmail, actorEmail)
	req.Header.Set(middleware.HeaderActorRole, actorRole)
	req.Header.Set(middleware.HeaderTimestamp, ts)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	handler := mw(func(c *echo.Context) error {
		t.Fatal("handler should not have been called for billing_admin")
		return nil
	})

	_ = handler(c)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for billing_admin role, got: %d", rec.Code)
	}
}

func TestInternalProxyAuthMiddleware_InvalidWorkspaceUUID(t *testing.T) {
	masterKey := "master-key"
	mw := middleware.InternalProxyAuthMiddleware(masterKey)

	actorID := "user-123"
	actorEmail := "test@test.com"
	actorRole := "operator"
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := computeTestSig("not-a-valid-uuid", actorID, actorEmail, actorRole, ts, masterKey)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/admin/inbox", nil)
	req.Header.Set(middleware.HeaderProxySignature, sig)
	req.Header.Set(middleware.HeaderWorkspaceScope, "not-a-valid-uuid")
	req.Header.Set(middleware.HeaderActorID, actorID)
	req.Header.Set(middleware.HeaderActorEmail, actorEmail)
	req.Header.Set(middleware.HeaderActorRole, actorRole)
	req.Header.Set(middleware.HeaderTimestamp, ts)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	handler := mw(func(c *echo.Context) error {
		t.Fatal("handler should not have been called")
		return nil
	})

	_ = handler(c)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for invalid workspace UUID, got: %d", rec.Code)
	}
}

func TestInternalProxyAuth_ChainWithSessionAuth(t *testing.T) {
	masterKey := "proxy-secret-key"
	proxyMW := middleware.InternalProxyAuthMiddleware(masterKey)
	sessionMW := middleware.SessionAuthMiddleware()

	wsID := uuid.New()
	actorID := "operator-1"
	actorEmail := "operator@domain.com"
	actorRole := "operator"
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := computeTestSig(wsID.String(), actorID, actorEmail, actorRole, ts, masterKey)

	e := echo.New()

	// Scenario 1: Request with valid proxy auth, NO cookie -> passes both middlewares!
	t.Run("valid proxy auth passes without session cookie", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/inbox", nil)
		req.Header.Set(middleware.HeaderProxySignature, sig)
		req.Header.Set(middleware.HeaderWorkspaceScope, wsID.String())
		req.Header.Set(middleware.HeaderActorID, actorID)
		req.Header.Set(middleware.HeaderActorEmail, actorEmail)
		req.Header.Set(middleware.HeaderActorRole, actorRole)
		req.Header.Set(middleware.HeaderTimestamp, ts)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		finalHandler := func(c *echo.Context) error {
			return c.String(http.StatusOK, "inbox-view")
		}

		// Chain: proxyMW -> sessionMW -> finalHandler
		chained := proxyMW(sessionMW(finalHandler))

		err := chained(c)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got: %d (body: %s)", rec.Code, rec.Body.String())
		}
	})

	// Scenario 2: Request with NO proxy auth, NO session cookie -> redirected by sessionMW
	t.Run("no proxy auth and no cookie redirects to login", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/inbox", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		finalHandler := func(c *echo.Context) error {
			return c.String(http.StatusOK, "inbox-view")
		}

		chained := proxyMW(sessionMW(finalHandler))

		err := chained(c)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if rec.Code != http.StatusFound {
			t.Fatalf("expected 302 Found redirect, got: %d", rec.Code)
		}
		if rec.Header().Get("Location") != "/admin/login" {
			t.Errorf("expected redirect to /admin/login, got: %s", rec.Header().Get("Location"))
		}
	})

	// Scenario 3: Request with INVALID proxy signature -> rejected with 401 immediately
	t.Run("invalid proxy signature rejected with 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/inbox", nil)
		req.Header.Set(middleware.HeaderProxySignature, "invalid-sig")
		req.Header.Set(middleware.HeaderWorkspaceScope, wsID.String())
		req.Header.Set(middleware.HeaderActorID, actorID)
		req.Header.Set(middleware.HeaderActorEmail, actorEmail)
		req.Header.Set(middleware.HeaderActorRole, actorRole)
		req.Header.Set(middleware.HeaderTimestamp, ts)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		finalHandler := func(c *echo.Context) error {
			return c.String(http.StatusOK, "inbox-view")
		}

		chained := proxyMW(sessionMW(finalHandler))

		_ = chained(c)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 Unauthorized, got: %d", rec.Code)
		}
	})
}
