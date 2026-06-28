package admin

import (
	"net/http"

	"github.com/labstack/echo/v5"

	mw "github.com/pablojhp.pergo/internal/api/middleware"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/pablojhp.pergo/templates/layout"
	"github.com/pablojhp.pergo/templates/pages"
)

// LoginPage renders the login form using a minimal layout (no sidebar, no HTMX polling).
func LoginPage(c *echo.Context, showError bool) error {
	msg := ""
	if showError {
		msg = "Invalid password"
	}
	login := pages.Login(msg)
	return mw.Render(c, http.StatusOK, layout.LoginBase("Login", login))
}

// LoginPost handles the login form submission.
func LoginPost(c *echo.Context, wsRepo *repository.WorkspaceRepository, adminPassword string) error {
	password := c.FormValue("password")

	if password != adminPassword {
		login := pages.Login("Invalid password")
		return mw.Render(c, http.StatusUnauthorized, layout.LoginBase("Login", login))
	}

	// Set session cookie
	secret := mw.GetSessionSecret()
	mw.SetSessionCookie(c, secret)

	return c.Redirect(http.StatusFound, "/admin/")
}

// Logout clears the session and redirects to the login page.
func Logout(c *echo.Context) error {
	mw.ClearSessionCookie(c)
	return c.Redirect(http.StatusFound, "/admin/login")
}
