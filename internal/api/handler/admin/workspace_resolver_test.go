package admin_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/pablojhp.pergo/internal/api/handler/admin"
	mw "github.com/pablojhp.pergo/internal/api/middleware"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/platform/postgres/tenant"
)

func TestResolveWorkspaceID_FromDomainScope(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/admin/tags", nil)
	expectedID := uuid.New()
	req = req.WithContext(domain.ContextWithWorkspaceID(req.Context(), expectedID))
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	id, err := admin.ResolveWorkspaceIDForTest(c)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if id != expectedID {
		t.Fatalf("expected workspace ID %s, got %s", expectedID, id)
	}
}

func TestResolveWorkspaceID_FromTenantContext(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/admin/tags", nil)
	expectedID := uuid.New()
	req = req.WithContext(tenant.WithWorkspaceID(req.Context(), expectedID))
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	id, err := admin.ResolveWorkspaceIDForTest(c)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if id != expectedID {
		t.Fatalf("expected workspace ID %s, got %s", expectedID, id)
	}
}

func TestResolveWorkspaceID_CookieIgnored(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/admin/tags", nil)
	expectedID := uuid.New()
	req.AddCookie(&http.Cookie{
		Name:  mw.ActiveWorkspaceCookieName,
		Value: expectedID.String(),
	})
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Cookie is intentionally ignored; must fail without context
	_, err := admin.ResolveWorkspaceIDForTest(c)
	if err == nil {
		t.Fatalf("expected error since cookie is no longer used for workspace resolution, got nil")
	}
}

func TestResolveWorkspaceID_PathParamIgnored(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/admin/workspaces/11111111-1111-1111-1111-111111111111/tags", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath("/admin/workspaces/:workspace_id/tags")
	c.SetPathValues(echo.PathValues{{Name: "workspace_id", Value: "11111111-1111-1111-1111-111111111111"}})

	// Path parameter is intentionally ignored; must fail without context
	_, err := admin.ResolveWorkspaceIDForTest(c)
	if err == nil {
		t.Fatalf("expected error since path parameter is no longer used for workspace resolution, got nil")
	}
}

func TestResolveWorkspaceID_Missing(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/admin/tags", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	_, err := admin.ResolveWorkspaceIDForTest(c)
	if err == nil {
		t.Fatalf("expected error for missing workspace ID, got nil")
	}
}

