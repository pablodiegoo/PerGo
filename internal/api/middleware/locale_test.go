package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/pablojhp.pergo/internal/i18n"
)

func TestLocaleMiddleware_Hierarchy(t *testing.T) {
	tests := []struct {
		name           string
		cookie         *http.Cookie
		acceptLang     string
		expectedLocale string
	}{
		{
			name:           "Default fallback when no cookie or header",
			cookie:         nil,
			acceptLang:     "",
			expectedLocale: "en-US",
		},
		{
			name:           "Accept-Language Portuguese resolves to pt-BR",
			cookie:         nil,
			acceptLang:     "pt-BR,pt;q=0.9,en;q=0.8",
			expectedLocale: "pt-BR",
		},
		{
			name:           "Accept-Language English resolves to en-US",
			cookie:         nil,
			acceptLang:     "en-US,en;q=0.9",
			expectedLocale: "en-US",
		},
		{
			name:           "Accept-Language quality weighting prioritizes higher q",
			cookie:         nil,
			acceptLang:     "en;q=0.5,pt-BR;q=0.9",
			expectedLocale: "pt-BR",
		},
		{
			name:           "Accept-Language with unsupported primary falls back to supported secondary",
			cookie:         nil,
			acceptLang:     "es-ES,es;q=0.9,pt;q=0.8,en;q=0.7",
			expectedLocale: "pt-BR",
		},
		{
			name:           "Cookie takes precedence over Accept-Language header",
			cookie:         &http.Cookie{Name: LocaleCookieName, Value: "pt-BR"},
			acceptLang:     "en-US,en;q=0.9",
			expectedLocale: "pt-BR",
		},
		{
			name:           "Cookie en-US takes precedence over Accept-Language pt-BR",
			cookie:         &http.Cookie{Name: LocaleCookieName, Value: "en-US"},
			acceptLang:     "pt-BR,pt;q=0.9",
			expectedLocale: "en-US",
		},
		{
			name:           "Invalid cookie falls back to Accept-Language",
			cookie:         &http.Cookie{Name: LocaleCookieName, Value: "unknown-lang"},
			acceptLang:     "pt-BR,pt;q=0.9",
			expectedLocale: "pt-BR",
		},
		{
			name:           "Invalid cookie and invalid header fall back to default en-US",
			cookie:         &http.Cookie{Name: LocaleCookieName, Value: "unknown-lang"},
			acceptLang:     "fr-FR,de-DE",
			expectedLocale: "en-US",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := echo.New()
			req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
			if tt.cookie != nil {
				req.AddCookie(tt.cookie)
			}
			if tt.acceptLang != "" {
				req.Header.Set("Accept-Language", tt.acceptLang)
			}
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			var capturedCtxLocale string
			var capturedEchoLocale string

			handler := func(c *echo.Context) error {
				capturedCtxLocale = i18n.GetLocale(c.Request().Context())
				if val, ok := c.Get("locale").(string); ok {
					capturedEchoLocale = val
				}
				return c.NoContent(http.StatusOK)
			}

			mw := LocaleMiddleware()
			err := mw(handler)(c)
			if err != nil {
				t.Fatalf("unexpected middleware error: %v", err)
			}

			if capturedCtxLocale != tt.expectedLocale {
				t.Errorf("expected context locale '%s', got '%s'", tt.expectedLocale, capturedCtxLocale)
			}
			if capturedEchoLocale != tt.expectedLocale {
				t.Errorf("expected echo context locale '%s', got '%s'", tt.expectedLocale, capturedEchoLocale)
			}
		})
	}
}

func TestSetLocaleCookie(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/admin/locale", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	SetLocaleCookie(c, "pt-BR")

	cookies := rec.Result().Cookies()
	var found *http.Cookie
	for _, ck := range cookies {
		if ck.Name == LocaleCookieName {
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
	if found.MaxAge != LocaleCookieMaxAge {
		t.Errorf("expected cookie max-age %d, got %d", LocaleCookieMaxAge, found.MaxAge)
	}
	if found.SameSite != http.SameSiteLaxMode {
		t.Errorf("expected cookie SameSite=Lax, got %v", found.SameSite)
	}
}
