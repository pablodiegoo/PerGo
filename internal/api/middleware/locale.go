// Package middleware provides Echo v5 middleware functions for PerGo.
package middleware

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/pablojhp.pergo/internal/i18n"
)

const (
	// LocaleCookieName is the cookie storing the user's selected language.
	LocaleCookieName = "pergo_locale"
	// LocaleCookieMaxAge is 1 year in seconds (365 days).
	LocaleCookieMaxAge = 31536000
)

// LocaleMiddleware resolves the active language according to the 3-tier hierarchy:
// 1. Cookie "pergo_locale"
// 2. "Accept-Language" HTTP header
// 3. Default canonical "en-US"
// It injects the resolved locale into the standard request context and Echo context.
func LocaleMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			loc := ResolveLocale(c)

			// 1. Inject into standard request context
			req := c.Request()
			ctx := i18n.WithLocale(req.Context(), loc)
			c.SetRequest(req.WithContext(ctx))

			// 2. Set on Echo context for easy retrieval in handlers
			c.Set("locale", loc)

			return next(c)
		}
	}
}

// ResolveLocale resolves the locale for an Echo request without mutating the context.
func ResolveLocale(c *echo.Context) string {
	// Tier 1: Cookie pergo_locale
	if cookie, err := c.Cookie(LocaleCookieName); err == nil && cookie != nil && cookie.Value != "" {
		if norm := i18n.NormalizeLocale(cookie.Value); norm != "" {
			return norm
		}
	}

	// Tier 2: Accept-Language header
	if acceptLang := c.Request().Header.Get("Accept-Language"); acceptLang != "" {
		if matched := ParseAcceptLanguage(acceptLang); matched != "" {
			return matched
		}
	}

	// Tier 3: Default canonical English
	return i18n.DefaultLocale()
}

// SetLocaleCookie sets the pergo_locale cookie with 1-year expiration and Lax SameSite.
func SetLocaleCookie(c *echo.Context, locale string) {
	norm := i18n.NormalizeLocale(locale)
	if norm == "" {
		norm = i18n.DefaultLocale()
	}

	cookie := &http.Cookie{
		Name:     LocaleCookieName,
		Value:    norm,
		Path:     "/",
		MaxAge:   LocaleCookieMaxAge,
		Expires:  time.Now().Add(365 * 24 * time.Hour),
		SameSite: http.SameSiteLaxMode,
		HttpOnly: false,
	}
	c.SetCookie(cookie)
}

// LocaleFrom retrieves the locale from standard context, falling back to DefaultLocale.
func LocaleFrom(ctx context.Context) string {
	return i18n.GetLocale(ctx)
}

type langQuality struct {
	tag string
	q   float64
}

// ParseAcceptLanguage parses RFC 7231 Accept-Language header quality values
// and returns the best matching supported locale, or empty string if none match.
func ParseAcceptLanguage(header string) string {
	var candidates []langQuality

	parts := strings.Split(header, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		subparts := strings.Split(part, ";")
		tag := strings.TrimSpace(subparts[0])
		q := 1.0

		for _, param := range subparts[1:] {
			param = strings.TrimSpace(param)
			if strings.HasPrefix(param, "q=") {
				if parsedQ, err := strconv.ParseFloat(param[2:], 64); err == nil {
					q = parsedQ
				}
			}
		}

		candidates = append(candidates, langQuality{tag: tag, q: q})
	}

	// Sort by quality weight descending (stable to preserve client preference order when weights match)
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].q > candidates[j].q
	})

	for _, c := range candidates {
		if norm := i18n.NormalizeLocale(c.tag); norm != "" {
			return norm
		}
	}

	return ""
}
