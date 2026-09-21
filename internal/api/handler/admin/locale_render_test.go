package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"
	mw "github.com/pablojhp.pergo/internal/api/middleware"
	"github.com/pablojhp.pergo/templates/layout"
	"github.com/pablojhp.pergo/templates/pages"
)

func TestLocaleRender_Integration(t *testing.T) {
	e := echo.New()
	e.Use(mw.LocaleMiddleware())

	// Route 1: Login page (uses LoginBase)
	e.GET("/admin/login", func(c *echo.Context) error {
		return LoginPage(c, false)
	})

	// Route 2: Admin layout page (uses Base layout with Sidebar)
	e.GET("/admin/test-layout", func(c *echo.Context) error {
		return mw.Render(c, http.StatusOK, layout.Base("Test Page", pages.Login("")))
	})

	tests := []struct {
		name              string
		url               string
		cookie            *http.Cookie
		acceptLangHeader  string
		expectedStatus    int
		expectedLang      string
		expectedSubstrings []string
		unexpectedStrings []string
	}{
		{
			name:             "Login with pergo_locale=pt-BR cookie",
			url:              "/admin/login",
			cookie:           &http.Cookie{Name: mw.LocaleCookieName, Value: "pt-BR"},
			expectedStatus:   http.StatusOK,
			expectedLang:     `lang="pt-BR"`,
			expectedSubstrings: []string{
				`lang="pt-BR"`,
				"Faça login no console do operador",
				"<label for=\"password\">Senha</label>",
				"Entrar",
			},
			unexpectedStrings: []string{
				"Sign in to the operator console",
			},
		},
		{
			name:             "Login with Accept-Language: pt-BR header",
			url:              "/admin/login",
			acceptLangHeader: "pt-BR,pt;q=0.9,en;q=0.8",
			expectedStatus:   http.StatusOK,
			expectedLang:     `lang="pt-BR"`,
			expectedSubstrings: []string{
				`lang="pt-BR"`,
				"Faça login no console do operador",
				"<label for=\"password\">Senha</label>",
				"Entrar",
			},
			unexpectedStrings: []string{
				"Sign in to the operator console",
			},
		},
		{
			name:             "Login with pergo_locale=en-US cookie",
			url:              "/admin/login",
			cookie:           &http.Cookie{Name: mw.LocaleCookieName, Value: "en-US"},
			expectedStatus:   http.StatusOK,
			expectedLang:     `lang="en-US"`,
			expectedSubstrings: []string{
				`lang="en-US"`,
				"Sign in to the operator console",
				"<label for=\"password\">Password</label>",
				"Sign In",
			},
			unexpectedStrings: []string{
				"Faça login no console do operador",
			},
		},
		{
			name:           "Login fallback default to en-US when no cookie and no header",
			url:            "/admin/login",
			expectedStatus: http.StatusOK,
			expectedLang:   `lang="en-US"`,
			expectedSubstrings: []string{
				`lang="en-US"`,
				"Sign in to the operator console",
				"<label for=\"password\">Password</label>",
				"Sign In",
			},
		},
		{
			name:             "Cookie priority: pergo_locale=pt-BR overrides Accept-Language: en-US",
			url:              "/admin/login",
			cookie:           &http.Cookie{Name: mw.LocaleCookieName, Value: "pt-BR"},
			acceptLangHeader: "en-US,en;q=0.9",
			expectedStatus:   http.StatusOK,
			expectedLang:     `lang="pt-BR"`,
			expectedSubstrings: []string{
				`lang="pt-BR"`,
				"Faça login no console do operador",
				"Entrar",
			},
			unexpectedStrings: []string{
				"Sign in to the operator console",
			},
		},
		{
			name:             "Base layout and Sidebar with pergo_locale=pt-BR",
			url:              "/admin/test-layout",
			cookie:           &http.Cookie{Name: mw.LocaleCookieName, Value: "pt-BR"},
			expectedStatus:   http.StatusOK,
			expectedLang:     `lang="pt-BR"`,
			expectedSubstrings: []string{
				`lang="pt-BR"`,
				"Visão Geral",
				"Configurações",
				"Sair",
				`fonts.googleapis.com`,
				`Outfit:wght@500;600;700`,
				`Inter:wght@400;500;600;700`,
				`hx-post="/admin/locale"`,
			},
			unexpectedStrings: []string{
				">Overview<",
			},
		},
		{
			name:             "Base layout and Sidebar with pergo_locale=en-US",
			url:              "/admin/test-layout",
			cookie:           &http.Cookie{Name: mw.LocaleCookieName, Value: "en-US"},
			expectedStatus:   http.StatusOK,
			expectedLang:     `lang="en-US"`,
			expectedSubstrings: []string{
				`lang="en-US"`,
				"Overview",
				"Settings",
				"Logout",
				`fonts.googleapis.com`,
				`Outfit:wght@500;600;700`,
				`Inter:wght@400;500;600;700`,
				`hx-post="/admin/locale"`,
			},
			unexpectedStrings: []string{
				">Visão Geral<",
			},
		},
		{
			name:             "Base layout and Sidebar with Accept-Language: pt-BR",
			url:              "/admin/test-layout",
			acceptLangHeader: "pt-BR,pt;q=0.9",
			expectedStatus:   http.StatusOK,
			expectedLang:     `lang="pt-BR"`,
			expectedSubstrings: []string{
				`lang="pt-BR"`,
				"Visão Geral",
				"Configurações",
				"Sair",
			},
			unexpectedStrings: []string{
				">Overview<",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.url, nil)
			if tc.cookie != nil {
				req.AddCookie(tc.cookie)
			}
			if tc.acceptLangHeader != "" {
				req.Header.Set("Accept-Language", tc.acceptLangHeader)
			}
			rec := httptest.NewRecorder()

			e.ServeHTTP(rec, req)

			if rec.Code != tc.expectedStatus {
				t.Fatalf("expected status %d, got %d", tc.expectedStatus, rec.Code)
			}

			body := rec.Body.String()

			if !strings.Contains(body, tc.expectedLang) {
				t.Errorf("expected HTML to contain %q, but was not found", tc.expectedLang)
			}

			for _, s := range tc.expectedSubstrings {
				if !strings.Contains(body, s) {
					t.Errorf("expected response body to contain %q, but was not found", s)
				}
			}

			for _, s := range tc.unexpectedStrings {
				if strings.Contains(body, s) {
					t.Errorf("expected response body NOT to contain %q, but it did", s)
				}
			}
		})
	}
}
