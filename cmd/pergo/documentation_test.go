package main_test

import (
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestDocumentation_CanonicalHierarchy verifies that the documentation
// has been reorganized into the canonical public directories and docs/context is deleted.
func TestDocumentation_CanonicalHierarchy(t *testing.T) {
	root := findRepoRoot(t)

	// 1. docs/context must NOT exist
	contextDir := filepath.Join(root, "docs", "context")
	if _, err := os.Stat(contextDir); !os.IsNotExist(err) {
		t.Errorf("docs/context directory should be removed after migration, but it still exists at %s", contextDir)
	}

	// 2. Canonical sections must exist and contain at least one markdown file
	requiredSections := []string{
		"getting-started",
		"architecture",
		"channels",
		"api",
		"deployment",
	}

	for _, section := range requiredSections {
		secDir := filepath.Join(root, "docs", section)
		info, err := os.Stat(secDir)
		if err != nil || !info.IsDir() {
			t.Errorf("Required documentation section docs/%s does not exist or is not a directory", section)
			continue
		}

		entries, err := os.ReadDir(secDir)
		if err != nil {
			t.Errorf("Failed to read directory docs/%s: %v", section, err)
			continue
		}

		hasMd := false
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
				hasMd = true
				break
			}
		}
		if !hasMd {
			t.Errorf("Section docs/%s contains no markdown files", section)
		}
	}
}

// TestDocumentation_NoDeadLocalOrLegacyLinks verifies that no local absolute links
// (file:///home/pablo) or legacy repo links (OmniGo) exist in docs or README.md.
func TestDocumentation_NoDeadLocalOrLegacyLinks(t *testing.T) {
	root := findRepoRoot(t)

	mdFiles := []string{filepath.Join(root, "README.md")}
	err := filepath.Walk(filepath.Join(root, "docs"), func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(path, ".md") {
			mdFiles = append(mdFiles, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Failed to scan docs directory: %v", err)
	}

	for _, file := range mdFiles {
		content, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("Failed to read file %s: %v", file, err)
		}
		relPath, _ := filepath.Rel(root, file)
		text := string(content)

		if strings.Contains(text, "file:///home/pablo") {
			t.Errorf("File %s contains local absolute path file:///home/pablo", relPath)
		}
		if strings.Contains(text, "OmniGo") {
			t.Errorf("File %s contains legacy reference to OmniGo", relPath)
		}
	}
}

// TestDocumentation_RelativeLinksResolve verifies that all relative markdown links
// between documentation files resolve to existing files on disk without 404s.
func TestDocumentation_RelativeLinksResolve(t *testing.T) {
	root := findRepoRoot(t)

	mdFiles := []string{
		filepath.Join(root, "README.md"),
		filepath.Join(root, "CONTRIBUTING.md"),
	}
	err := filepath.Walk(filepath.Join(root, "docs"), func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(path, ".md") {
			mdFiles = append(mdFiles, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Failed to scan docs directory: %v", err)
	}

	// Match markdown links: [label](target)
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

			// Skip external links, mailto, anchor-only links
			if strings.HasPrefix(rawTarget, "http://") ||
				strings.HasPrefix(rawTarget, "https://") ||
				strings.HasPrefix(rawTarget, "mailto:") ||
				strings.HasPrefix(rawTarget, "#") {
				continue
			}

			// Strip fragment anchor (#section)
			targetClean := rawTarget
			if idx := strings.Index(targetClean, "#"); idx != -1 {
				targetClean = targetClean[:idx]
			}
			if targetClean == "" {
				continue
			}

			// Parse URL to handle possible queries or url-encoding
			parsedURL, err := url.Parse(targetClean)
			if err == nil && parsedURL.Path != "" {
				targetClean = parsedURL.Path
			}

			// Resolve target path relative to current file directory
			var resolvedPath string
			if strings.HasPrefix(targetClean, "/") {
				// Absolute to repo root
				resolvedPath = filepath.Join(root, targetClean)
			} else {
				resolvedPath = filepath.Join(fileDir, targetClean)
			}

			// Check if target file or directory exists
			if _, err := os.Stat(resolvedPath); os.IsNotExist(err) {
				t.Errorf("In %s: dead link target %q (resolved to %s)", relFile, rawTarget, resolvedPath)
			}
		}
	}
}
