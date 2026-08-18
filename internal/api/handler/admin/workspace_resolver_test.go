package admin_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/pablojhp.pergo/internal/api/handler/admin"
	mw "github.com/pablojhp.pergo/internal/api/middleware"
	"github.com/pablojhp.pergo/internal/platform/postgres/tenant"
)

func TestResolveWorkspaceID_FromContext(t *testing.T) {
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

func TestResolveWorkspaceID_FromCookie(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/admin/tags", nil)
	expectedID := uuid.New()
	req.AddCookie(&http.Cookie{
		Name:  mw.ActiveWorkspaceCookieName,
		Value: expectedID.String(),
	})
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
