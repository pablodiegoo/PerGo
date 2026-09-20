package main_test

import (
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func collectMarkdownFiles(t *testing.T, root string, extraFiles ...string) []string {
	t.Helper()
	mdFiles := append([]string{}, extraFiles...)
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
	return mdFiles
}

func resolveLinkPath(root, fileDir, rawTarget string) (string, bool) {
	// Skip external links, mailto, anchor-only links
	if strings.HasPrefix(rawTarget, "http://") ||
		strings.HasPrefix(rawTarget, "https://") ||
		strings.HasPrefix(rawTarget, "mailto:") ||
		strings.HasPrefix(rawTarget, "#") {
		return "", false
	}

	// Strip fragment anchor (#section)
	targetClean := rawTarget
	if idx := strings.Index(targetClean, "#"); idx != -1 {
		targetClean = targetClean[:idx]
	}
	if targetClean == "" {
		return "", false
	}

	// Parse URL to handle possible queries or url-encoding
	parsedURL, err := url.Parse(targetClean)
	if err == nil && parsedURL.Path != "" {
		targetClean = parsedURL.Path
	}

	// Resolve target path relative to current file directory or repo root
	if strings.HasPrefix(targetClean, "/") {
		return filepath.Join(root, targetClean), true
	}
	return filepath.Join(fileDir, targetClean), true
}

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
	mdFiles := collectMarkdownFiles(t, root, filepath.Join(root, "README.md"))

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
	mdFiles := collectMarkdownFiles(t, root,
		filepath.Join(root, "README.md"),
		filepath.Join(root, "CONTRIBUTING.md"),
	)

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
			resolvedPath, ok := resolveLinkPath(root, fileDir, rawTarget)
			if !ok {
				continue
			}

			// Check if target file or directory exists
			if _, err := os.Stat(resolvedPath); os.IsNotExist(err) {
				t.Errorf("In %s: dead link target %q (resolved to %s)", relFile, rawTarget, resolvedPath)
			}
		}
	}
}

// TestDocumentation_READMEOverhaul verifies that README.md satisfies all
// acceptance criteria from Issue #131: visual assets, differentiators, hero inbox,
// 2x2 showcase, interactive architecture, PerGo Cloud spotlight, quickstart, and agent discovery.
func TestDocumentation_READMEOverhaul(t *testing.T) {
	root := findRepoRoot(t)
	readmePath := filepath.Join(root, "README.md")
	content, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("Failed to read README.md: %v", err)
	}
	text := string(content)

	assertAsset := func(relAsset string) {
		t.Helper()
		if !strings.Contains(text, relAsset) {
			t.Errorf("README.md must reference asset %s", relAsset)
		}
		if _, err := os.Stat(filepath.Join(root, relAsset)); os.IsNotExist(err) {
			t.Errorf("Asset %s does not exist on disk", relAsset)
		}
	}

	// 1. Header banner SVG
	assertAsset("docs/assets/pergo-banner.svg")

	// 2. All 5 screenshot previews must be referenced and exist
	screenshots := []string{
		"docs/assets/screenshots/inbox-hero.png",
		"docs/assets/screenshots/dashboard.png",
		"docs/assets/screenshots/devices-qr.png",
		"docs/assets/screenshots/campaigns.png",
		"docs/assets/screenshots/api-docs.png",
	}
	for _, sc := range screenshots {
		assertAsset(sc)
	}

	// 3. Top hero preview: inbox-hero.png must appear right beneath the header/badges, before Overview and 2x2 showcase
	inboxIdx := strings.Index(text, "inbox-hero.png")
	overviewIdx := strings.Index(text, "## Overview")
	dashboardIdx := strings.Index(text, "dashboard.png")
	if inboxIdx == -1 {
		t.Errorf("README.md missing top hero inbox preview (inbox-hero.png)")
	}
	if overviewIdx != -1 && inboxIdx > overviewIdx {
		t.Errorf("Top hero preview (inbox-hero.png) must appear right beneath header, before Overview section")
	}
	if dashboardIdx != -1 && inboxIdx > dashboardIdx {
		t.Errorf("Top hero preview (inbox-hero.png) must appear before secondary feature showcase screenshots")
	}

	// 4. Badges / Shields
	for _, badge := range []string{"Go", "MIT", "Docker", "PostgreSQL", "NATS", "OpenAPI"} {
		if !strings.Contains(text, badge) {
			t.Errorf("README.md missing badge/technology indicator for %q", badge)
		}
	}

	// 5. Competitive Differentiators matrix
	for _, diff := range []string{"Evolution API", "WPPConnect", "Twilio", "Zenvia", "<50MB"} {
		if !strings.Contains(text, diff) {
			t.Errorf("README.md missing competitive differentiator item %q", diff)
		}
	}

	// 6. Interactive Architecture (Mermaid)
	if !strings.Contains(text, "```mermaid") {
		t.Errorf("README.md must include a mermaid architecture diagram")
	}
	for _, flowItem := range []string{"Inbound", "Outbound", "JetStream"} {
		if !strings.Contains(text, flowItem) {
			t.Errorf("Mermaid architecture diagram missing flow node %q", flowItem)
		}
	}

	// 7. PerGo Cloud Spotlight & Early Access CTA
	cloudCTA := "mailto:pablodiegoo@gmail.com?subject=[PerGo%20Cloud]%20Early%20Access%20Request"
	if !strings.Contains(text, cloudCTA) {
		t.Errorf("README.md must include early access CTA link: %s", cloudCTA)
	}
	if !strings.Contains(text, "PerGo Cloud") {
		t.Errorf("README.md must contain a dedicated 'PerGo Cloud' section")
	}
	if !strings.Contains(text, "Self-Hosted") && !strings.Contains(text, "Community") {
		t.Errorf("PerGo Cloud section must include comparison table with Self-Hosted Community")
	}

	// 8. 60-Second Quickstart with verifiable 1-Click VPS installer pointing to pablodiegoo/PerGo on main
	expectedCurl := "curl -fsSL https://raw.githubusercontent.com/pablodiegoo/PerGo/main/install.sh | bash"
	if !strings.Contains(text, expectedCurl) {
		t.Errorf("README.md must include 1-Click VPS installer pointing to: %s", expectedCurl)
	}

	// 9. Agent-Ready CPaaS (/llms.txt and MCP diagnostics)
	if !strings.Contains(text, "/llms.txt") {
		t.Errorf("README.md must reference /llms.txt")
	}
	if !strings.Contains(text, "Model Context Protocol") && !strings.Contains(text, "MCP") {
		t.Errorf("README.md must reference Model Context Protocol (MCP)")
	}

	// 10. Hygiene: No legacy OmniGo or local paths
	if strings.Contains(text, "OmniGo") {
		t.Errorf("README.md must not contain legacy string 'OmniGo'")
	}
	if strings.Contains(text, "file:///home/pablo") {
		t.Errorf("README.md must not contain local path 'file:///home/pablo'")
	}
}
