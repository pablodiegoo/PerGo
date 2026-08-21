package handler

import (
	"net/http"

	"github.com/labstack/echo/v5"
	"github.com/pablojhp.pergo/api"
)

// DocsHandler serves the embedded Scalar developer documentation portal and OpenAPI specification.
type DocsHandler struct{}

// NewDocsHandler creates a new DocsHandler.
func NewDocsHandler() *DocsHandler {
	return &DocsHandler{}
}

// RegisterRoutes mounts the documentation endpoints on Echo.
func (h *DocsHandler) RegisterRoutes(e *echo.Echo) {
	e.GET("/docs", h.ServePortal)
	e.GET("/docs/", h.ServePortal)
	e.GET("/docs/openapi.yaml", h.ServeOpenAPISpec)
	e.GET("/openapi.yaml", h.ServeOpenAPISpec)
	e.GET("/api/openapi.yaml", h.ServeOpenAPISpec)
	e.GET("/docs/scalar.js", h.ServeScalarJS)
}

// ServePortal renders the standalone offline Scalar developer documentation portal.
func (h *DocsHandler) ServePortal(c *echo.Context) error {
	html := `<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>PerGo API Reference</title>
    <link rel="icon" type="image/svg+xml" href="data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 100 100'><text y='.9em' font-size='90'>⚡</text></svg>">
    <style>
      body {
        margin: 0;
        padding: 0;
        height: 100vh;
        width: 100vw;
        background-color: #0f172a;
      }
    </style>
  </head>
  <body>
    <script
      id="api-reference"
      data-url="/docs/openapi.yaml"
      data-configuration='{"theme":"purple","layout":"modern","darkMode":true}'
    ></script>
    <script src="/docs/scalar.js"></script>
  </body>
</html>`

	c.Response().Header().Set("Content-Type", "text/html; charset=utf-8")
	return c.String(http.StatusOK, html)
}

// ServeOpenAPISpec returns the raw OpenAPI 3.1 YAML document.
func (h *DocsHandler) ServeOpenAPISpec(c *echo.Context) error {
	c.Response().Header().Set("Content-Type", "application/yaml; charset=utf-8")
	return c.Blob(http.StatusOK, "application/yaml; charset=utf-8", api.OpenAPIYAML)
}

// ServeScalarJS returns the standalone offline Scalar bundle JavaScript.
func (h *DocsHandler) ServeScalarJS(c *echo.Context) error {
	c.Response().Header().Set("Content-Type", "application/javascript; charset=utf-8")
	c.Response().Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	return c.Blob(http.StatusOK, "application/javascript; charset=utf-8", api.ScalarJS)
}
