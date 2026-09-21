package admin

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/labstack/echo/v5"
	mw "github.com/pablojhp.pergo/internal/api/middleware"
	"github.com/pablojhp.pergo/internal/i18n"
)

// SetLocaleRequest represents the payload for setting the active locale.
type SetLocaleRequest struct {
	Locale string `json:"locale" form:"locale"`
}

// SetLocale handles POST /admin/locale to switch the UI presentation language.
// It accepts form-encoded values or a JSON payload.
func SetLocale(c *echo.Context) error {
	var requestedLocale string

	contentType := c.Request().Header.Get("Content-Type")
	if strings.Contains(contentType, "application/json") {
		var req SetLocaleRequest
		if err := json.NewDecoder(c.Request().Body).Decode(&req); err == nil {
			requestedLocale = req.Locale
		}
	}

	if requestedLocale == "" {
		requestedLocale = c.FormValue("locale")
	}
	if requestedLocale == "" {
		requestedLocale = c.QueryParam("locale")
	}

	requestedLocale = strings.TrimSpace(requestedLocale)
	normLocale := i18n.NormalizeLocale(requestedLocale)
	if normLocale == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error":   "unsupported_locale",
			"message": "unsupported or invalid locale; supported: en-US, pt-BR",
		})
	}

	// Set pergo_locale cookie (Path=/, SameSite=Lax, MaxAge=31536000)
	mw.SetLocaleCookie(c, normLocale)

	// Instruct HTMX to refresh the entire page to reflect language change
	c.Response().Header().Set("HX-Refresh", "true")

	// If request came from HTMX, 200 OK without body is standard
	if c.Request().Header.Get("HX-Request") == "true" {
		return c.NoContent(http.StatusOK)
	}

	// If JSON request/response is expected
	if strings.Contains(contentType, "application/json") || strings.Contains(c.Request().Header.Get("Accept"), "application/json") {
		return c.JSON(http.StatusOK, map[string]any{
			"success": true,
			"locale":  normLocale,
		})
	}

	// For standard HTML form submissions, redirect back to Referer
	referer := c.Request().Header.Get("Referer")
	if referer == "" {
		referer = "/admin/"
	}
	return c.Redirect(http.StatusSeeOther, referer)
}
