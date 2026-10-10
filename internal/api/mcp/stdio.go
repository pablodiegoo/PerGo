package mcp

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/google/uuid"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/platform/crypto"
	"github.com/pablojhp.pergo/internal/platform/postgres/tenant"
	"github.com/pablojhp.pergo/internal/repository"
)

// RunStdio executes the MCP server communicating via standard input/output (stdio) for local piped clients.
func RunStdio(
	ctx context.Context,
	srv *Server,
	apiKeyRepo *repository.APIKeyRepository,
	args []string,
	stdin io.Reader,
	stdout, stderr io.Writer,
) error {
	fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
	fs.SetOutput(stderr)

	apiKeyFlag := fs.String("api-key", "", "PerGo API key for authenticated operations (fallback: PERGO_API_KEY)")
	wsIDFlag := fs.String("workspace-id", "", "Workspace UUID scope (fallback: PERGO_WORKSPACE_ID)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	apiKey := strings.TrimSpace(*apiKeyFlag)
	if apiKey == "" {
		apiKey = strings.TrimSpace(os.Getenv("PERGO_API_KEY"))
	}

	wsIDStr := strings.TrimSpace(*wsIDFlag)
	if wsIDStr == "" {
		wsIDStr = strings.TrimSpace(os.Getenv("PERGO_WORKSPACE_ID"))
	}

	if apiKey != "" && apiKeyRepo != nil && len(apiKey) >= 8 {
		prefix := apiKey[:8]
		keyRecord, err := apiKeyRepo.GetByPrefix(ctx, prefix)
		if err != nil || !crypto.VerifyAPIKey(apiKey, keyRecord.KeyHash) {
			fmt.Fprintf(stderr, "warning: invalid API key provided for MCP stdio: %v\n", err)
			return fmt.Errorf("invalid API key: %w", err)
		}

		ctx = tenant.WithWorkspaceID(ctx, keyRecord.WorkspaceID)
		ctx = domain.ContextWithScope(ctx, domain.NewWorkspaceScope(keyRecord.WorkspaceID, domain.CapabilityWorkspaceScoped))
	} else if wsIDStr != "" {
		if parsedID, err := uuid.Parse(wsIDStr); err == nil {
			ctx = tenant.WithWorkspaceID(ctx, parsedID)
			ctx = domain.ContextWithScope(ctx, domain.NewWorkspaceScope(parsedID, domain.CapabilityWorkspaceScoped))
		}
	}

	stdioServer := mcpserver.NewStdioServer(srv.MCPServer)
	return stdioServer.Listen(ctx, stdin, stdout)
}
