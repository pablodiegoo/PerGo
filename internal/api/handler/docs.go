package handler

import (
	"net/http"

	"github.com/labstack/echo/v5"
	"github.com/pablojhp.pergo/api"
	"github.com/pablojhp.pergo/internal/api/middleware"
)

// DocsHandler serves the embedded Scalar developer documentation portal and OpenAPI specification.
type DocsHandler struct{}

// NewDocsHandler creates a new DocsHandler.
func NewDocsHandler() *DocsHandler {
	return &DocsHandler{}
}

// RegisterRoutes mounts the documentation endpoints on Echo.
func (h *DocsHandler) RegisterRoutes(e *echo.Echo) {
	disc := middleware.AgentDiscoveryMiddleware()
	e.GET("/docs", h.ServePortal, disc)
	e.GET("/docs/", h.ServePortal, disc)

	// OpenAPI YAML endpoints
	e.GET("/docs/openapi.yaml", h.ServeOpenAPISpecYAML)
	e.GET("/openapi.yaml", h.ServeOpenAPISpecYAML)
	e.GET("/api/openapi.yaml", h.ServeOpenAPISpecYAML)

	// OpenAPI JSON endpoints
	e.GET("/docs/openapi.json", h.ServeOpenAPISpecJSON)
	e.GET("/openapi.json", h.ServeOpenAPISpecJSON)
	e.GET("/api/openapi.json", h.ServeOpenAPISpecJSON)

	// Scalar standalone JS asset
	e.GET("/docs/scalar.js", h.ServeScalarJS)

	// Curated agent index endpoint (/llms.txt)
	e.GET("/llms.txt", h.ServeLLMsTxt)

	// Full agent documentation endpoint (/llms-full.txt)
	e.GET("/llms-full.txt", h.ServeLLMsFullTxt)
}

// ServePortal renders the standalone offline Scalar developer documentation portal.
func (h *DocsHandler) ServePortal(c *echo.Context) error {
	specURL := "/api/openapi.json"
	if c.QueryParam("format") == "yaml" || c.QueryParam("spec") == "yaml" {
		specURL = "/docs/openapi.yaml"
	}

	html := `<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>PerGo API Reference</title>
    <link rel="describedby" href="/llms.txt" />
    <link rel="alternate" type="text/markdown" href="/llms-full.txt" title="Full Documentation for LLMs" />
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
      data-url="` + specURL + `"
      data-configuration='{"theme":"purple","layout":"modern","darkMode":true,"showSidebar":true}'
    ></script>
    <script src="/docs/scalar.js"></script>
  </body>
</html>`

	c.Response().Header().Set("Content-Type", "text/html; charset=utf-8")
	return c.String(http.StatusOK, html)
}

// ServeOpenAPISpec returns the raw OpenAPI 3.1 YAML document (backward compatibility).
func (h *DocsHandler) ServeOpenAPISpec(c *echo.Context) error {
	return h.ServeOpenAPISpecYAML(c)
}

// ServeOpenAPISpecYAML returns the raw OpenAPI 3.1 YAML document.
func (h *DocsHandler) ServeOpenAPISpecYAML(c *echo.Context) error {
	c.Response().Header().Set("Content-Type", "application/yaml; charset=utf-8")
	return c.Blob(http.StatusOK, "application/yaml; charset=utf-8", api.OpenAPIYAML)
}

// ServeOpenAPISpecJSON returns the raw OpenAPI 3.1 JSON document.
func (h *DocsHandler) ServeOpenAPISpecJSON(c *echo.Context) error {
	c.Response().Header().Set("Content-Type", "application/json; charset=utf-8")
	return c.Blob(http.StatusOK, "application/json; charset=utf-8", api.OpenAPIJSON)
}

// ServeScalarJS returns the standalone offline Scalar bundle JavaScript.
func (h *DocsHandler) ServeScalarJS(c *echo.Context) error {
	c.Response().Header().Set("Content-Type", "application/javascript; charset=utf-8")
	c.Response().Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	return c.Blob(http.StatusOK, "application/javascript; charset=utf-8", api.ScalarJS)
}

// ServeLLMsTxt returns the curated agent index markdown adhering to llmstxt.org v2 format.
func (h *DocsHandler) ServeLLMsTxt(c *echo.Context) error {
	return c.Blob(http.StatusOK, "text/markdown; charset=utf-8", api.LLMsTxt)
}

// ServeLLMsFullTxt returns the complete developer documentation markdown payload.
func (h *DocsHandler) ServeLLMsFullTxt(c *echo.Context) error {
	return c.Blob(http.StatusOK, "text/markdown; charset=utf-8", api.LLMsFullTxt)
}
