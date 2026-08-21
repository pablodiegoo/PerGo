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

	// Test GET /docs
	req := httptest.NewRequest(http.MethodGet, "/docs", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/html")
	body := rec.Body.String()
	assert.Contains(t, body, "<title>PerGo API Reference</title>")
	assert.Contains(t, body, `data-url="/docs/openapi.yaml"`)
	assert.Contains(t, body, `<script src="/docs/scalar.js"></script>`)
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
