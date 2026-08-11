package middleware

import (
	"github.com/labstack/echo/v5"
)

// SecurityConfig defines the configuration for SecurityHeaders middleware.
type SecurityConfig struct {
	// XContentTypeOptions sets the X-Content-Type-Options header.
	// Default: "nosniff"
	XContentTypeOptions string

	// XFrameOptions sets the X-Frame-Options header.
	// Default: "DENY"
	XFrameOptions string

	// XXSSProtection sets the X-XSS-Protection header.
	// Default: "1; mode=block"
	XXSSProtection string

	// HSTSMaxAge sets the max-age directive in Strict-Transport-Security header.
	// Default: "31536000; includeSubDomains"
	HSTSMaxAge string

	// ReferrerPolicy sets the Referrer-Policy header.
	// Default: "strict-origin-when-cross-origin"
	ReferrerPolicy string

	// ContentSecurityPolicy sets the Content-Security-Policy header.
	// Default: "" (header omitted if empty)
	ContentSecurityPolicy string
}

// DefaultSecurityConfig is the default configuration for SecurityHeaders.
var DefaultSecurityConfig = SecurityConfig{
	XContentTypeOptions: "nosniff",
	XFrameOptions:       "DENY",
	XXSSProtection:      "1; mode=block",
	HSTSMaxAge:          "31536000; includeSubDomains",
	ReferrerPolicy:      "strict-origin-when-cross-origin",
}

// SecurityHeaders returns a security headers middleware with default configuration.
func SecurityHeaders() echo.MiddlewareFunc {
	return SecurityHeadersWithConfig(DefaultSecurityConfig)
}

// SecurityHeadersWithConfig returns a security headers middleware with custom configuration.
func SecurityHeadersWithConfig(cfg SecurityConfig) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			res := c.Response()

			if cfg.XContentTypeOptions != "" {
				res.Header().Set("X-Content-Type-Options", cfg.XContentTypeOptions)
			}
			if cfg.XFrameOptions != "" {
				res.Header().Set("X-Frame-Options", cfg.XFrameOptions)
			}
			if cfg.XXSSProtection != "" {
				res.Header().Set("X-XSS-Protection", cfg.XXSSProtection)
			}
			if cfg.HSTSMaxAge != "" {
				res.Header().Set("Strict-Transport-Security", cfg.HSTSMaxAge)
			}
			if cfg.ReferrerPolicy != "" {
				res.Header().Set("Referrer-Policy", cfg.ReferrerPolicy)
			}
			if cfg.ContentSecurityPolicy != "" {
				res.Header().Set("Content-Security-Policy", cfg.ContentSecurityPolicy)
			}

			return next(c)
		}
	}
}
