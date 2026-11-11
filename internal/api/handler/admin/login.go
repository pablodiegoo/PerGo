package admin

import (
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	mw "github.com/pablojhp.pergo/internal/api/middleware"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/pablojhp.pergo/templates/layout"
	"github.com/pablojhp.pergo/templates/pages"
)

func sanitizeNextRedirect(raw string) string {
	next := strings.TrimSpace(raw)
	if strings.HasPrefix(next, "/") && !strings.HasPrefix(next, "//") && !strings.HasPrefix(next, "/\\") {
		return next
	}
	return ""
}

// LoginPage renders the login form using a minimal layout (no sidebar, no HTMX polling).
func LoginPage(c *echo.Context, showError bool) error {
	msg := ""
	if showError {
		msg = "Invalid password"
	}
	next := sanitizeNextRedirect(c.QueryParam("next"))
	login := pages.Login(msg, next)
	return mw.Render(c, http.StatusOK, layout.LoginBase("Login", login))
}

// LoginPost handles the login form submission.
func LoginPost(c *echo.Context, wsRepo *repository.WorkspaceRepository, adminPassword string) error {
	password := c.FormValue("password")
	next := sanitizeNextRedirect(c.FormValue("next"))
	if next == "" {
		next = sanitizeNextRedirect(c.QueryParam("next"))
	}

	if password != adminPassword {
		login := pages.Login("Invalid password", next)
		return mw.Render(c, http.StatusUnauthorized, layout.LoginBase("Login", login))
	}

	// Set session cookie
	secret := mw.GetSessionSecret()
	mw.SetSessionCookie(c, secret)

	// Set active workspace cookie if not already set or invalid
	var activeWSID uuid.UUID
	if cookie, err := c.Cookie(mw.ActiveWorkspaceCookieName); err == nil && cookie != nil && cookie.Value != "" {
		if parsed, parseErr := uuid.Parse(cookie.Value); parseErr == nil && parsed != uuid.Nil {
			activeWSID = parsed
		}
	}

	if activeWSID == uuid.Nil && wsRepo != nil {
		if ws, err := wsRepo.GetEarliest(c.Request().Context()); err == nil && ws != nil {
			activeWSID = ws.ID
			mw.SetActiveWorkspaceCookie(c, activeWSID)
		}
	}

	if next != "" {
		return c.Redirect(http.StatusFound, next)
	}
	return c.Redirect(http.StatusFound, "/admin/")
}

// Logout clears the session and redirects to the login page.
func Logout(c *echo.Context) error {
	mw.ClearSessionCookie(c)
	return c.Redirect(http.StatusFound, "/admin/login")
}
