package middleware

import (
	"github.com/labstack/echo/v5"
)

const (
	// DiscoveryLinkHeaderValue is the RFC 8288 compliant Link header pointing to agent manifests and specifications.
	DiscoveryLinkHeaderValue = `</llms.txt>; rel="describedby", </llms-full.txt>; rel="alternate"; type="text/markdown"`
)

// AgentDiscoveryMiddleware injects RFC 8288 Link headers for public agent discovery endpoints.
func AgentDiscoveryMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			c.Response().Header().Set("Link", DiscoveryLinkHeaderValue)
			return next(c)
		}
	}
}
