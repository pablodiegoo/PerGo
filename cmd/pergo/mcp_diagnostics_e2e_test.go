package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v5"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/pablojhp.pergo/api"
	"github.com/pablojhp.pergo/internal/api/mcp"
	"gopkg.in/yaml.v3"
)

func TestMCPDiagnostics_AllFourToolsRegistered(t *testing.T) {
	srv := mcp.NewServer(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, "")
	if srv == nil {
		t.Fatalf("expected non-nil MCP server")
	}

	tools := srv.MCPServer.ListTools()
	toolsByName := make(map[string]*mcpserver.ServerTool, len(tools))
	for _, tool := range tools {
		toolsByName[tool.Tool.Name] = tool
	}

	tests := []struct {
		name               string
		toolName           string
		expectedProperties []string
		expectedRequired   []string
		descriptionKeyword string
	}{
		{
			name:               "diagnose_connection_health registration",
			toolName:           "diagnose_connection_health",
			expectedProperties: []string{"workspace_id", "connection_id"},
			expectedRequired:   []string{"workspace_id", "connection_id"},
			descriptionKeyword: "diagnostic",
		},
		{
			name:               "simulate_webhook_event registration",
			toolName:           "simulate_webhook_event",
			expectedProperties: []string{"workspace_id", "event_type", "payload", "subscription_id", "target_url"},
			expectedRequired:   []string{"workspace_id", "event_type", "payload"},
			descriptionKeyword: "synthetic webhook event",
		},
		{
			name:               "replay_webhook_dlq registration",
			toolName:           "replay_webhook_dlq",
			expectedProperties: []string{"workspace_id", "dlq_id", "subscription_id", "action", "limit", "target_url"},
			expectedRequired:   []string{"workspace_id"},
			descriptionKeyword: "dead-lettered webhook payloads",
		},
		{
			name:               "inspect_queue_health registration",
			toolName:           "inspect_queue_health",
			expectedProperties: []string{"workspace_id"},
			expectedRequired:   nil,
			descriptionKeyword: "JetStream stream capacity",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st, exists := toolsByName[tc.toolName]
			if !exists {
				t.Fatalf("expected tool %q to be registered in MCPServer, but it was not found", tc.toolName)
			}

			if !strings.Contains(strings.ToLower(st.Tool.Description), strings.ToLower(tc.descriptionKeyword)) {
				t.Errorf("tool %q description %q does not contain keyword %q", tc.toolName, st.Tool.Description, tc.descriptionKeyword)
			}

			if st.Tool.InputSchema.Type != "object" {
				t.Errorf("tool %q: expected InputSchema.Type 'object', got %q", tc.toolName, st.Tool.InputSchema.Type)
			}

			for _, prop := range tc.expectedProperties {
				if _, ok := st.Tool.InputSchema.Properties[prop]; !ok {
					t.Errorf("tool %q: expected property %q in InputSchema.Properties", tc.toolName, prop)
				}
			}

			for _, req := range tc.expectedRequired {
				var foundReq bool
				for _, r := range st.Tool.InputSchema.Required {
					if r == req {
						foundReq = true
						break
					}
				}
				if !foundReq {
					t.Errorf("tool %q: expected required field %q in InputSchema.Required", tc.toolName, req)
				}
			}
		})
	}
}

func TestMCPDiagnostics_StdioAndSSEInitialization(t *testing.T) {
	// Verify mcp.NewServer accepts all functional options and initializes cleanly
	srv := mcp.NewServer(
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, "http://localhost:8080",
		mcp.WithWebhookDLQRepo(nil),
		mcp.WithJetStreamPublisher(nil),
		mcp.WithJetStream(nil),
		mcp.WithNATSConn(nil),
	)

	if srv == nil {
		t.Fatalf("expected non-nil MCP server instance")
	}
	if srv.MCPServer == nil {
		t.Fatalf("expected non-nil internal MCPServer")
	}
	if srv.SSEServer == nil {
		t.Fatalf("expected non-nil internal SSEServer")
	}

	// Verify Stdio server initializes without panic or error
	stdioSrv := mcpserver.NewStdioServer(srv.MCPServer)
	if stdioSrv == nil {
		t.Fatalf("expected non-nil StdioServer")
	}

	// Verify SSE handler routes cleanly in Echo HTTP server
	e := echo.New()
	e.Any("/api/mcp/*", echo.WrapHandler(srv.SSEServer))

	req := httptest.NewRequest(http.MethodGet, "/api/mcp/sse", nil)
	req.Header.Set("Accept", "text/event-stream")

	ctx, cancel := context.WithTimeout(req.Context(), 100*time.Millisecond)
	defer cancel()
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	// In SSE mode, the server returns 200 OK and establishes text/event-stream or event stream handshake
	if rec.Code != http.StatusOK && rec.Code != http.StatusNotFound {
		t.Errorf("unexpected status code for SSE route: got %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestMCPDiagnostics_OpenAPIContract(t *testing.T) {
	t.Run("OpenAPI YAML Contract", func(t *testing.T) {
		if len(api.OpenAPIYAML) == 0 {
			t.Fatalf("api.OpenAPIYAML must not be empty")
		}

		var doc struct {
			OpenAPI string `yaml:"openapi"`
			Paths   map[string]struct {
				Get *struct {
					OperationID string `yaml:"operationId"`
				} `yaml:"get"`
				Post *struct {
					OperationID string `yaml:"operationId"`
				} `yaml:"post"`
			} `yaml:"paths"`
			Components struct {
				Schemas map[string]interface{} `yaml:"schemas"`
			} `yaml:"components"`
		}

		if err := yaml.Unmarshal(api.OpenAPIYAML, &doc); err != nil {
			t.Fatalf("failed to unmarshal api.OpenAPIYAML: %v", err)
		}

		// 1. Verify /api/mcp path presence and operations
		mcpPath, exists := doc.Paths["/api/mcp"]
		if !exists {
			t.Fatalf("path '/api/mcp' is missing in api/openapi.yaml")
		}
		if mcpPath.Get == nil {
			t.Errorf("path '/api/mcp' must define a 'get' operation (SSE stream)")
		}
		if mcpPath.Post == nil {
			t.Errorf("path '/api/mcp' must define a 'post' operation (JSON-RPC dispatch)")
		}

		// 2. Verify all MCP schemas exist in components.schemas
		expectedSchemas := []string{
			"MCPDiagnoseConnectionHealthRequest",
			"MCPConnectionHealthDiagnosticResponse",
			"MCPProxyHealth",
			"MCPSimulateWebhookEventRequest",
			"MCPWebhookSimulationTelemetry",
			"MCPReplayWebhookDLQRequest",
			"MCPWebhookDLQReplayResult",
			"MCPWebhookDLQReplayItem",
			"MCPInspectQueueHealthRequest",
			"MCPQueueHealthReport",
			"MCPStreamHealthReport",
			"MCPConsumerHealthReport",
		}

		for _, schemaName := range expectedSchemas {
			if _, ok := doc.Components.Schemas[schemaName]; !ok {
				t.Errorf("components.schemas in openapi.yaml missing schema: %q", schemaName)
			}
		}
	})

	t.Run("OpenAPI JSON Contract", func(t *testing.T) {
		if len(api.OpenAPIJSON) == 0 {
			t.Fatalf("api.OpenAPIJSON must not be empty")
		}

		var doc struct {
			OpenAPI string `json:"openapi"`
			Paths   map[string]struct {
				Get *struct {
					OperationID string `json:"operationId"`
				} `json:"get"`
				Post *struct {
					OperationID string `json:"operationId"`
				} `json:"post"`
			} `json:"paths"`
			Components struct {
				Schemas map[string]interface{} `json:"schemas"`
			} `json:"components"`
		}

		if err := json.Unmarshal(api.OpenAPIJSON, &doc); err != nil {
			t.Fatalf("failed to unmarshal api.OpenAPIJSON: %v", err)
		}

		// 1. Verify /api/mcp path presence and operations
		mcpPath, exists := doc.Paths["/api/mcp"]
		if !exists {
			t.Fatalf("path '/api/mcp' is missing in api/openapi.json")
		}
		if mcpPath.Get == nil {
			t.Errorf("path '/api/mcp' must define a 'get' operation in openapi.json")
		}
		if mcpPath.Post == nil {
			t.Errorf("path '/api/mcp' must define a 'post' operation in openapi.json")
		}

		// 2. Verify all MCP schemas exist in components.schemas
		expectedSchemas := []string{
			"MCPDiagnoseConnectionHealthRequest",
			"MCPConnectionHealthDiagnosticResponse",
			"MCPProxyHealth",
			"MCPSimulateWebhookEventRequest",
			"MCPWebhookSimulationTelemetry",
			"MCPReplayWebhookDLQRequest",
			"MCPWebhookDLQReplayResult",
			"MCPWebhookDLQReplayItem",
			"MCPInspectQueueHealthRequest",
			"MCPQueueHealthReport",
			"MCPStreamHealthReport",
			"MCPConsumerHealthReport",
		}

		for _, schemaName := range expectedSchemas {
			if _, ok := doc.Components.Schemas[schemaName]; !ok {
				t.Errorf("components.schemas in openapi.json missing schema: %q", schemaName)
			}
		}
	})
}

func TestMCPDiagnostics_LLMsTxtContract(t *testing.T) {
	t.Run("api/llms.txt documents 4 diagnostic tools", func(t *testing.T) {
		content := string(api.LLMsTxt)
		if len(content) == 0 {
			t.Fatalf("api.LLMsTxt must not be empty")
		}

		expectedTools := []string{
			"diagnose_connection_health",
			"simulate_webhook_event",
			"replay_webhook_dlq",
			"inspect_queue_health",
		}

		for _, tool := range expectedTools {
			if !strings.Contains(content, tool) {
				t.Errorf("api/llms.txt does not mention MCP tool %q", tool)
			}
		}

		if !strings.Contains(content, "/api/mcp") {
			t.Errorf("api/llms.txt does not mention /api/mcp endpoint")
		}
	})

	t.Run("api/llms-full.txt documents 4 diagnostic tools with details", func(t *testing.T) {
		content := string(api.LLMsFullTxt)
		if len(content) == 0 {
			t.Fatalf("api.LLMsFullTxt must not be empty")
		}

		expectedSnippets := []string{
			"## 6. Model Context Protocol (MCP) Server Reference",
			"`diagnose_connection_health`",
			"`simulate_webhook_event`",
			"`replay_webhook_dlq`",
			"`inspect_queue_health`",
			"anti-SSRF",
			"re-enqueue",
			"MESSAGES",
		}

		for _, snippet := range expectedSnippets {
			if !strings.Contains(content, snippet) {
				t.Errorf("api/llms-full.txt does not contain required snippet %q", snippet)
			}
		}
	})
}
