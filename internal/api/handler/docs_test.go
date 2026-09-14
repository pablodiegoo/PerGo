package handler_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/pablojhp.pergo/internal/api/handler"
	"github.com/stretchr/testify/assert"
)

func TestDocsHandler_GetDocs(t *testing.T) {
	e := echo.New()
	docsHandler := handler.NewDocsHandler()
	docsHandler.RegisterRoutes(e)

	// Test GET /docs defaults to /api/openapi.json
	t.Run("Default renders Scalar pointing to /api/openapi.json", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/docs", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Header().Get("Content-Type"), "text/html")
		body := rec.Body.String()
		assert.Contains(t, body, "<title>PerGo API Reference</title>")
		assert.Contains(t, body, `data-url="/api/openapi.json"`)
		assert.Contains(t, body, `<script src="/docs/scalar.js"></script>`)
	})

	// Test GET /docs?spec=yaml switches to /docs/openapi.yaml
	t.Run("Query ?spec=yaml renders Scalar pointing to YAML spec", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/docs?spec=yaml", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Header().Get("Content-Type"), "text/html")
		body := rec.Body.String()
		assert.Contains(t, body, `data-url="/docs/openapi.yaml"`)
	})
}

func TestDocsHandler_GetOpenAPISpec(t *testing.T) {
	e := echo.New()
	docsHandler := handler.NewDocsHandler()
	docsHandler.RegisterRoutes(e)

	pathsToTest := []string{
		"/docs/openapi.yaml",
		"/openapi.yaml",
		"/api/openapi.yaml",
	}

	for _, p := range pathsToTest {
		req := httptest.NewRequest(http.MethodGet, p, nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code, "path %s must return 200 OK", p)
		assert.Contains(t, rec.Header().Get("Content-Type"), "yaml")
		body := rec.Body.String()
		assert.Contains(t, body, "openapi: 3.1.0")
		assert.Contains(t, body, "PerGo Omnichannel CPaaS API")
		assert.Contains(t, body, "/messages:")
		assert.Contains(t, body, "FlowDataExchangeRequest:")
	}
}

func TestDocsHandler_GetOpenAPISpecJSON(t *testing.T) {
	e := echo.New()
	docsHandler := handler.NewDocsHandler()
	docsHandler.RegisterRoutes(e)

	pathsToTest := []string{
		"/docs/openapi.json",
		"/openapi.json",
		"/api/openapi.json",
	}

	for _, p := range pathsToTest {
		req := httptest.NewRequest(http.MethodGet, p, nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code, "path %s must return 200 OK", p)
		assert.Contains(t, rec.Header().Get("Content-Type"), "application/json")
		body := rec.Body.String()
		assert.Contains(t, body, `"openapi": "3.1.0"`)
		assert.Contains(t, body, `"title": "PerGo Omnichannel CPaaS API"`)
		assert.Contains(t, body, `"/messages":`)
		assert.Contains(t, body, `"FlowDataExchangeRequest":`)
	}
}

func TestDocsHandler_GetScalarJS(t *testing.T) {
	e := echo.New()
	docsHandler := handler.NewDocsHandler()
	docsHandler.RegisterRoutes(e)

	req := httptest.NewRequest(http.MethodGet, "/docs/scalar.js", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "javascript")
	assert.Greater(t, rec.Body.Len(), 1000, "Scalar bundle must not be empty")
}

func TestDocsHandler_GetLLMsTxt(t *testing.T) {
	e := echo.New()
	docsHandler := handler.NewDocsHandler()
	docsHandler.RegisterRoutes(e)

	req := httptest.NewRequest(http.MethodGet, "/llms.txt", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "text/markdown; charset=utf-8", rec.Header().Get("Content-Type"))
	body := rec.Body.String()
	// Validate llmstxt.org v2 format:
	// 1. Single H1 header
	assert.Contains(t, body, "# PerGo Omnichannel CPaaS Gateway")
	// 2. Blockquote summary
	assert.Contains(t, body, "> PerGo é uma plataforma de comunicação omnichannel")
	// 3. Rules and critical architecture context
	assert.Contains(t, body, "POST /messages")
	assert.Contains(t, body, "MCP")
	// 4. Curated markdown links in H2 sections
	assert.Contains(t, body, "## Documentação Principal")
	assert.Contains(t, body, "- [")
	// 5. Canonical llmstxt.org v2 optional section
	assert.Contains(t, body, "## Optional")
}

func TestDocsHandler_GetLLMsFullTxt(t *testing.T) {
	e := echo.New()
	docsHandler := handler.NewDocsHandler()
	docsHandler.RegisterRoutes(e)

	req := httptest.NewRequest(http.MethodGet, "/llms-full.txt", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "text/markdown; charset=utf-8", rec.Header().Get("Content-Type"))
	body := rec.Body.String()
	// Validate comprehensive documentation payload
	assert.Greater(t, len(body), 5000, "llms-full.txt must contain the complete documentation set (> 5KB)")
	assert.Contains(t, body, "# PerGo Omnichannel CPaaS - Full Developer Documentation")
	assert.Contains(t, body, "Headless CPaaS")
	assert.Contains(t, body, "X-PerGo-Signature")
	assert.Contains(t, body, "whatsmeow")
	assert.Contains(t, body, "Model Context Protocol")
}


