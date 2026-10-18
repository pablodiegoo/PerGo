package chat_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var portugueseDiacriticsRe = regexp.MustCompile(`[ãõçáéíóúêôàÃÕÇÁÉÍÓÚÊÔÀ]`)

// TestGovernance_ChatEngineAndAgentDocs enforces Rule #6 (English-only chat engine & MCP code),
// .templ codegen presence, and AGENTS.md context-pointer hygiene.
func TestGovernance_ChatEngineAndAgentDocs(t *testing.T) {
	dirsToScan := []string{
		".",
		filepath.Join("..", "api", "mcp"),
	}

	for _, dir := range dirsToScan {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read dir %s: %v", dir, err)
		}
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			fullPath := filepath.Join(dir, name)
			content, err := os.ReadFile(fullPath)
			if err != nil {
				t.Fatalf("read %s: %v", fullPath, err)
			}
			if match := portugueseDiacriticsRe.FindString(string(content)); match != "" {
				t.Errorf("CONTEXT-MAP.md Rule #6 violation: %s contains non-English character %q", fullPath, match)
			}
		}
	}

	templatesDir := filepath.Join("..", "..", "templates")
	if info, err := os.Stat(templatesDir); err == nil && info.IsDir() {
		err = filepath.WalkDir(templatesDir, func(path string, d os.DirEntry, walkErr error) error {
			if walkErr != nil || d.IsDir() || !strings.HasSuffix(path, ".templ") {
				return walkErr
			}
			genPath := strings.TrimSuffix(path, ".templ") + "_templ.go"
			if _, statErr := os.Stat(genPath); os.IsNotExist(statErr) {
				t.Errorf("missing generated templ file %s for %s (run templ generate)", genPath, path)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk templates dir: %v", err)
		}
	}

	agentsBytes, err := os.ReadFile(filepath.Join("..", "..", "AGENTS.md"))
	if err != nil {
		t.Fatalf("read AGENTS.md: %v", err)
	}
	lines := strings.Split(string(agentsBytes), "\n")
	if len(lines) > 60 {
		t.Errorf("AGENTS.md sprawl detected (%d lines > 60): keep only concise context pointers in AGENTS.md", len(lines))
	}
}
