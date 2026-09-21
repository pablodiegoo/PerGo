package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"
	mw "github.com/pablojhp.pergo/internal/api/middleware"
)

func TestSetLocale_FormValue(t *testing.T) {
	e := echo.New()
	form := url.Values{}
	form.Set("locale", "pt-BR")

	req := httptest.NewRequest(http.MethodPost, "/admin/locale", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", "/admin/dashboard")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := SetLocale(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Assert redirect status
	if rec.Code != http.StatusSeeOther {
		t.Errorf("expected status 303, got %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/admin/dashboard" {
		t.Errorf("expected redirect to /admin/dashboard, got '%s'", loc)
	}

	// Assert HX-Refresh header
	if hx := rec.Header().Get("HX-Refresh"); hx != "true" {
		t.Errorf("expected HX-Refresh: true, got '%s'", hx)
	}

	// Assert pergo_locale cookie
	cookies := rec.Result().Cookies()
	var found *http.Cookie
	for _, ck := range cookies {
		if ck.Name == mw.LocaleCookieName {
			found = ck
			break
		}
	}
	if found == nil {
		t.Fatal("expected pergo_locale cookie to be set")
	}
	if found.Value != "pt-BR" {
		t.Errorf("expected cookie value 'pt-BR', got '%s'", found.Value)
	}
	if found.Path != "/" {
		t.Errorf("expected cookie path '/', got '%s'", found.Path)
	}
	if found.MaxAge != mw.LocaleCookieMaxAge {
		t.Errorf("expected cookie MaxAge %d, got %d", mw.LocaleCookieMaxAge, found.MaxAge)
	}
	if found.SameSite != http.SameSiteLaxMode {
		t.Errorf("expected cookie SameSite=Lax, got %v", found.SameSite)
	}
}

func TestSetLocale_JSON(t *testing.T) {
	e := echo.New()
	body := `{"locale": "en-US"}`

	req := httptest.NewRequest(http.MethodPost, "/admin/locale", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := SetLocale(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Assert status 200
	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	// Assert HX-Refresh
	if hx := rec.Header().Get("HX-Refresh"); hx != "true" {
		t.Errorf("expected HX-Refresh: true, got '%s'", hx)
	}

	// Assert JSON payload
	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode JSON response: %v", err)
	}
	if resp["success"] != true || resp["locale"] != "en-US" {
		t.Errorf("unexpected response body: %v", resp)
	}

	// Assert cookie set to en-US
	cookies := rec.Result().Cookies()
	var found *http.Cookie
	for _, ck := range cookies {
		if ck.Name == mw.LocaleCookieName {
			found = ck
			break
		}
	}
	if found == nil || found.Value != "en-US" {
		t.Errorf("expected pergo_locale cookie with value en-US, got %v", found)
	}
}

func TestSetLocale_HTMX(t *testing.T) {
	e := echo.New()
	form := url.Values{}
	form.Set("locale", "pt-BR")

	req := httptest.NewRequest(http.MethodPost, "/admin/locale", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := SetLocale(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
	if hx := rec.Header().Get("HX-Refresh"); hx != "true" {
		t.Errorf("expected HX-Refresh: true, got '%s'", hx)
	}
}

func TestSetLocale_InvalidLocale(t *testing.T) {
	e := echo.New()
	form := url.Values{}
	form.Set("locale", "es-ES")

	req := httptest.NewRequest(http.MethodPost, "/admin/locale", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := SetLocale(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode JSON response: %v", err)
	}
	if resp["error"] != "unsupported_locale" {
		t.Errorf("expected error 'unsupported_locale', got '%s'", resp["error"])
	}

	// Ensure no pergo_locale cookie was set
	cookies := rec.Result().Cookies()
	for _, ck := range cookies {
		if ck.Name == mw.LocaleCookieName {
			t.Errorf("did not expect pergo_locale cookie to be set on error")
		}
	}
}
