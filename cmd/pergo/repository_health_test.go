package main_test

import (
	"bytes"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestVerifyDocs_GovernanceFiles ensures that all repository governance and community health
// files are present and non-empty (>0 bytes) as required by Issue #132.
func TestVerifyDocs_GovernanceFiles(t *testing.T) {
	root := findRepoRoot(t)

	governanceFiles := []string{
		"CODE_OF_CONDUCT.md",
		"SECURITY.md",
		"CONTRIBUTING.md",
		filepath.Join(".github", "pull_request_template.md"),
	}

	// Dynamically match all .github/ISSUE_TEMPLATE/*.yml templates
	templates, err := filepath.Glob(filepath.Join(root, ".github", "ISSUE_TEMPLATE", "*.yml"))
	if err != nil || len(templates) == 0 {
		t.Fatalf("no issue templates found matching .github/ISSUE_TEMPLATE/*.yml (err: %v)", err)
	}
	for _, tmpl := range templates {
		rel, _ := filepath.Rel(root, tmpl)
		governanceFiles = append(governanceFiles, rel)
	}

	for _, rel := range governanceFiles {
		fullPath := filepath.Join(root, rel)
		info, err := os.Stat(fullPath)
		if err != nil {
			t.Errorf("required governance file missing: %s (%v)", rel, err)
			continue
		}
		if info.IsDir() {
			t.Errorf("expected %s to be a file, but it is a directory", rel)
			continue
		}
		if info.Size() == 0 {
			t.Errorf("governance file %s must be non-empty, but size is 0 bytes", rel)
		}
	}
}

// TestVerifyDocs_ZeroLegacyStringsAcrossRepository ensures zero occurrences of legacy OmniGo
// or local paths (file:///home/pablo/) across all documentation and source files.
func TestVerifyDocs_ZeroLegacyStringsAcrossRepository(t *testing.T) {
	root := findRepoRoot(t)

	// Dissect strings so the test file itself does not match the forbidden substrings literally.
	forbiddenLegacyName := "Omni" + "Go"
	forbiddenLocalPath := "file://" + "/home/pablo/"

	ignoredDirs := map[string]bool{
		".git":          true,
		"bin":           true,
		"tmp":           true,
		"node_modules":  true,
		".pytest_cache": true,
	}

	// Test files that assert the absence of forbidden strings are permitted to mention them in assertions
	isExemptTestFile := func(relPath string) bool {
		switch relPath {
		case "cmd/pergo/documentation_test.go",
			"cmd/pergo/community_governance_test.go",
			"cmd/pergo/repository_health_test.go",
			"scripts/verify-docs.sh":
			return true
		default:
			return false
		}
	}

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, _ := filepath.Rel(root, path)

		// Skip ignored directories
		if info.IsDir() {
			if ignoredDirs[info.Name()] {
				return filepath.SkipDir
			}
			return nil
		}

		if isExemptTestFile(relPath) {
			return nil
		}

		// Check documentation (.md, .txt) and source files (.go, .templ, .sh, .yml, Makefile, Dockerfile, etc.)
		base := filepath.Base(path)
		ext := filepath.Ext(path)
		isSourceOrDoc := false
		switch ext {
		case ".md", ".txt", ".go", ".templ", ".sh", ".yml", ".yaml", ".js", ".json", ".html", ".sql", ".toml", ".env":
			isSourceOrDoc = true
		}
		if base == "Makefile" || base == "Dockerfile" || base == ".env.example" || base == "LICENSE" {
			isSourceOrDoc = true
		}

		if isSourceOrDoc {
			// Scan file contents
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			content := string(data)
			if strings.Contains(content, forbiddenLegacyName) {
				t.Errorf("File %s contains forbidden legacy reference %q", relPath, forbiddenLegacyName)
			}
			if strings.Contains(content, forbiddenLocalPath) {
				t.Errorf("File %s contains forbidden local path %q", relPath, forbiddenLocalPath)
			}
		}

		return nil
	})

	if err != nil {
		t.Fatalf("Failed to scan repository files: %v", err)
	}
}

// TestVerifyDocs_MarkdownRelativeLinksResolve ensures all relative Markdown links
// in README.md and docs/ point to valid existing files on disk.
func TestVerifyDocs_MarkdownRelativeLinksResolve(t *testing.T) {
	root := findRepoRoot(t)
	mdFiles := collectMarkdownFiles(t, root,
		filepath.Join(root, "README.md"),
		filepath.Join(root, "CONTRIBUTING.md"),
		filepath.Join(root, "CODE_OF_CONDUCT.md"),
		filepath.Join(root, "SECURITY.md"),
	)

	linkRegex := regexp.MustCompile(`\[[^\]]+\]\(([^)]+)\)`)

	for _, file := range mdFiles {
		content, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("Failed to read file %s: %v", file, err)
		}
		fileDir := filepath.Dir(file)
		relFile, _ := filepath.Rel(root, file)

		matches := linkRegex.FindAllStringSubmatch(string(content), -1)
		for _, m := range matches {
			rawTarget := strings.TrimSpace(m[1])
			resolvedPath, ok := resolveLinkPath(root, fileDir, rawTarget)
			if !ok {
				continue
			}

			// Parse URL to handle possible queries or anchors
			cleanPath := resolvedPath
			if u, err := url.Parse(rawTarget); err == nil && u.Path != "" {
				if strings.HasPrefix(u.Path, "/") {
					cleanPath = filepath.Join(root, u.Path)
				} else {
					cleanPath = filepath.Join(fileDir, u.Path)
				}
			}

			if _, err := os.Stat(cleanPath); os.IsNotExist(err) {
				t.Errorf("In %s: dead link target %q (resolved to %s)", relFile, rawTarget, cleanPath)
			}
		}
	}
}

// TestVerifyDocs_ScriptExecution verifies that scripts/verify-docs.sh exists, is executable,
// and passes with exit code 0 when run against the repository.
func TestVerifyDocs_ScriptExecution(t *testing.T) {
	root := findRepoRoot(t)
	scriptPath := filepath.Join(root, "scripts", "verify-docs.sh")

	info, err := os.Stat(scriptPath)
	if err != nil {
		t.Fatalf("scripts/verify-docs.sh must exist: %v", err)
	}

	// Verify executable permission
	if info.Mode()&0111 == 0 {
		t.Errorf("scripts/verify-docs.sh must be executable (mode is %v)", info.Mode())
	}

	cmd := exec.Command(scriptPath)
	cmd.Dir = root
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("scripts/verify-docs.sh failed with error: %v\nStdout:\n%s\nStderr:\n%s", err, stdout.String(), stderr.String())
	}
}

// TestVerifyDocs_MakefileTarget verifies that 'make verify-docs' target is defined
// and completes cleanly with exit code 0.
func TestVerifyDocs_MakefileTarget(t *testing.T) {
	root := findRepoRoot(t)

	cmd := exec.Command("make", "verify-docs")
	cmd.Dir = root
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("'make verify-docs' failed: %v\nStdout:\n%s\nStderr:\n%s", err, stdout.String(), stderr.String())
	}
}
