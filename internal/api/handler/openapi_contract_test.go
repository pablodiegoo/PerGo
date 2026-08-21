package handler_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/pablojhp.pergo/api"
	"github.com/pablojhp.pergo/internal/api/handler"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// OpenAPIValidationDoc maps the root structure of an OpenAPI 3.1 YAML document.
type OpenAPIValidationDoc struct {
	OpenAPI    string                            `yaml:"openapi"`
	Info       OpenAPIInfo                       `yaml:"info"`
	Paths      map[string]map[string]interface{} `yaml:"paths"`
	Components struct {
		Schemas map[string]OpenAPISchema `yaml:"schemas"`
	} `yaml:"components"`
}

type OpenAPIInfo struct {
	Title   string `yaml:"title"`
	Version string `yaml:"version"`
}

type OpenAPISchema struct {
	Type       string                   `yaml:"type"`
	Required   []string                 `yaml:"required"`
	Properties map[string]OpenAPISchema `yaml:"properties"`
	Enum       []string                 `yaml:"enum"`
}

func loadOpenAPISpec(t *testing.T) ([]byte, OpenAPIValidationDoc) {
	t.Helper()

	var data []byte
	// 1. Try embedded package bytes
	if len(api.OpenAPIYAML) > 0 {
		data = api.OpenAPIYAML
	} else {
		// 2. Try loading from file path
		path := filepath.Join("..", "..", "..", "api", "openapi.yaml")
		var err error
		data, err = os.ReadFile(path)
		require.NoError(t, err, "openapi.yaml file must exist at api/openapi.yaml")
	}

	var doc OpenAPIValidationDoc
	err := yaml.Unmarshal(data, &doc)
	require.NoError(t, err, "openapi.yaml must be valid YAML")
	return data, doc
}

func TestOpenAPI31Contract_SpecificationHeader(t *testing.T) {
	_, doc := loadOpenAPISpec(t)

	// OpenAPI 3.1.x header assertion
	assert.Contains(t, doc.OpenAPI, "3.1.", "OpenAPI spec must use version 3.1.x")
	assert.NotEmpty(t, doc.Info.Title, "OpenAPI spec must define info.title")
	assert.NotEmpty(t, doc.Info.Version, "OpenAPI spec must define info.version")
}

func TestOpenAPI31Contract_RequiredEndpointsPresent(t *testing.T) {
	_, doc := loadOpenAPISpec(t)

	requiredPaths := []struct {
		Path   string
		Method string
	}{
		{"/messages", "post"},
		{"/health", "get"},
		{"/health/ready", "get"},
		{"/health/live", "get"},
		{"/api/v1/me", "get"},
		{"/api/v1/waba/flows/data-exchange", "post"},
		{"/api/v1/connections", "get"},
		{"/api/v1/connections/pair", "post"},
		{"/api/v1/connections/waba", "post"},
		{"/api/v1/connections/telegram", "post"},
		{"/api/v1/connections/{id}", "delete"},
		{"/api/v1/connections/{id}/flow-public-key", "get"},
		{"/api/v1/workspaces/webhook-secret", "post"},
		{"/api/v1/workspaces/webhook-secret", "get"},
		{"/api/v1/workspaces/flow-webhook-url", "post"},
		{"/api/v1/webhooks/subscriptions", "get"},
		{"/api/v1/webhooks/subscriptions", "post"},
		{"/api/v1/campaigns", "get"},
		{"/api/v1/campaigns", "post"},
		{"/api/v1/waba/templates", "get"},
		{"/api/v1/waba/templates", "post"},
		{"/api/v1/workspaces", "get"},
		{"/api/v1/workspaces", "post"},
		{"/docs", "get"},
		{"/docs/openapi.yaml", "get"},
		{"/openapi.yaml", "get"},
		{"/docs/scalar.js", "get"},
	}

	for _, req := range requiredPaths {
		pathItem, ok := doc.Paths[req.Path]
		assert.Truef(t, ok, "path %s must be documented in openapi.yaml", req.Path)
		if ok {
			_, methodOk := pathItem[req.Method]
			assert.Truef(t, methodOk, "method %s for path %s must be documented in openapi.yaml", req.Method, req.Path)
		}
	}
}

func TestOpenAPI31Contract_EchoRouteCoverage(t *testing.T) {
	_, doc := loadOpenAPISpec(t)

	e := echo.New()
	docsHandler := handler.NewDocsHandler()
	docsHandler.RegisterRoutes(e)

	healthHandler := &handler.HealthHandler{}
	healthHandler.RegisterRoutes(e)

	for _, r := range e.Router().Routes() {
		// All registered public documentation and health routes must exist in OpenAPI spec
		if r.Path == "/docs" || r.Path == "/docs/openapi.yaml" || r.Path == "/openapi.yaml" || r.Path == "/health" || r.Path == "/health/ready" || r.Path == "/health/live" {
			_, ok := doc.Paths[r.Path]
			assert.Truef(t, ok, "registered Echo route %s must be documented in openapi.yaml", r.Path)
		}
	}
}

func TestOpenAPI31Contract_InteractiveAndFlowSchemas(t *testing.T) {
	_, doc := loadOpenAPISpec(t)
	schemas := doc.Components.Schemas

	// 1. Verify Interactive schema
	interactiveSchema, ok := schemas["Interactive"]
	require.True(t, ok, "Interactive schema must be defined in components.schemas")
	assert.Contains(t, interactiveSchema.Properties, "type")
	assert.Contains(t, interactiveSchema.Properties, "body")
	assert.Contains(t, interactiveSchema.Properties, "action")

	// 2. Verify Action schema has buttons, sections, and flow properties
	actionSchema, ok := schemas["Action"]
	require.True(t, ok, "Action schema must be defined in components.schemas")
	assert.Contains(t, actionSchema.Properties, "buttons")
	assert.Contains(t, actionSchema.Properties, "sections")
	assert.Contains(t, actionSchema.Properties, "flow_token")
	assert.Contains(t, actionSchema.Properties, "flow_id")
	assert.Contains(t, actionSchema.Properties, "flow_cta")
	assert.Contains(t, actionSchema.Properties, "flow_action")
	assert.Contains(t, actionSchema.Properties, "flow_action_payload")

	// 3. Verify Product payload schemas
	productSchema, ok := schemas["ProductPayload"]
	require.True(t, ok, "ProductPayload schema must be defined in components.schemas")
	assert.Contains(t, productSchema.Properties, "catalog_id")
	assert.Contains(t, productSchema.Properties, "product_retailer_id")
	assert.Contains(t, productSchema.Properties, "sections")

	// 4. Verify Flow Data Exchange Request & Response schemas
	flowExchangeReq, ok := schemas["FlowDataExchangeRequest"]
	require.True(t, ok, "FlowDataExchangeRequest schema must be defined in components.schemas")
	assert.Contains(t, flowExchangeReq.Properties, "encrypted_flow_data")
	assert.Contains(t, flowExchangeReq.Properties, "encrypted_aes_key")
	assert.Contains(t, flowExchangeReq.Properties, "initial_vector")

	// 5. Verify Flow Public Key Response schema
	flowKeyResp, ok := schemas["FlowPublicKeyResponse"]
	require.True(t, ok, "FlowPublicKeyResponse schema must be defined in components.schemas")
	assert.Contains(t, flowKeyResp.Properties, "public_key_pem")
}

func TestOpenAPI31Contract_CreateMessageRequest_InteractiveValidation(t *testing.T) {
	e := echo.New()

	interactiveJSON := `{
		"to": "+5511999999999",
		"channel": "whatsapp_cloud",
		"type": "interactive",
		"fallback_behavior": "degrade",
		"interactive": {
			"type": "button",
			"body": {
				"text": "Escolha o tipo de atendimento:"
			},
			"action": {
				"buttons": [
					{
						"type": "reply",
						"reply": {
							"id": "btn_suporte",
							"title": "Suporte Técnico"
						}
					},
					{
						"type": "reply",
						"reply": {
							"id": "btn_comercial",
							"title": "Falar com Vendas"
						}
					}
				]
			}
		}
	}`

	req := httptest.NewRequest(http.MethodPost, "/messages", bytes.NewReader([]byte(interactiveJSON)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	var payload domain.CreateMessageRequest
	err := c.Bind(&payload)
	require.NoError(t, err)

	valErr := domain.ValidateMessage(&payload)
	assert.Nil(t, valErr, "valid interactive button request should produce nil validation error")
	assert.Equal(t, "button", payload.Interactive.Type)
	assert.Len(t, payload.Interactive.Action.Buttons, 2)
	assert.Equal(t, "degrade", payload.FallbackBehavior)
}

func TestOpenAPI31Contract_FlowDataExchangeRequest_Binding(t *testing.T) {
	e := echo.New()

	flowReqJSON := `{
		"encrypted_flow_data": "dGVzdC1mbG93LWRhdGE=",
		"encrypted_aes_key": "dGVzdC1hZXMta2V5",
		"initial_vector": "dGVzdC1pdi12ZWN0b3I="
	}`

	req := httptest.NewRequest(http.MethodPost, "/api/v1/waba/flows/data-exchange", bytes.NewReader([]byte(flowReqJSON)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	var flowReq handler.FlowDataExchangeRequest
	err := c.Bind(&flowReq)
	require.NoError(t, err)
	assert.Equal(t, "dGVzdC1mbG93LWRhdGE=", flowReq.EncryptedFlowData)
	assert.Equal(t, "dGVzdC1hZXMta2V5", flowReq.EncryptedAESKey)
	assert.Equal(t, "dGVzdC1pdi12ZWN0b3I=", flowReq.InitialVector)
}
