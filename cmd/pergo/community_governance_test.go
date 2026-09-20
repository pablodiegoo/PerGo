package main_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working dir: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find repo root with go.mod from %s", dir)
		}
		dir = parent
	}
}

func TestCodeOfConduct(t *testing.T) {
	root := findRepoRoot(t)
	cocPath := filepath.Join(root, "CODE_OF_CONDUCT.md")

	content, err := os.ReadFile(cocPath)
	if err != nil {
		t.Fatalf("CODE_OF_CONDUCT.md missing at repository root: %v", err)
	}

	text := string(content)
	if !strings.Contains(text, "Contributor Covenant") || !strings.Contains(text, "2.1") {
		t.Errorf("CODE_OF_CONDUCT.md must adhere to Contributor Covenant v2.1")
	}

	expectedEmail := "pablodiegoo@gmail.com"
	if !strings.Contains(text, expectedEmail) {
		t.Errorf("CODE_OF_CONDUCT.md must designate %q as the reporting contact", expectedEmail)
	}
}

func TestSecurityPolicy(t *testing.T) {
	root := findRepoRoot(t)
	secPath := filepath.Join(root, "SECURITY.md")

	content, err := os.ReadFile(secPath)
	if err != nil {
		t.Fatalf("SECURITY.md missing at repository root: %v", err)
	}

	text := string(content)

	// Verify supported versions table
	if !strings.Contains(text, "main") || !strings.Contains(text, "v1.x") {
		t.Errorf("SECURITY.md must detail supported versions including main and v1.x")
	}

	// Verify Private Vulnerability Reporting & email fallback
	if !strings.Contains(strings.ToLower(text), "private vulnerability reporting") {
		t.Errorf("SECURITY.md must describe GitHub Private Vulnerability Reporting")
	}

	expectedEmail := "pablodiegoo@gmail.com"
	if !strings.Contains(text, expectedEmail) {
		t.Errorf("SECURITY.md must include fallback contact email %q", expectedEmail)
	}
}

func TestIssueTemplates(t *testing.T) {
	root := findRepoRoot(t)
	templatesDir := filepath.Join(root, ".github", "ISSUE_TEMPLATE")

	files := []string{"bug_report.yml", "feature_request.yml", "channel_request.yml", "config.yml"}
	for _, f := range files {
		path := filepath.Join(templatesDir, f)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("Missing issue template file %s: %v", f, err)
		}

		var parsed map[string]interface{}
		if err := yaml.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("Issue template %s is invalid YAML: %v", f, err)
		}
	}

	// Verify bug_report.yml contents
	bugData, _ := os.ReadFile(filepath.Join(templatesDir, "bug_report.yml"))
	bugStr := string(bugData)
	for _, term := range []string{"OS", "Go version", "WhatsApp Web", "WABA", "Telegram", "PII", "reproduce"} {
		if !strings.Contains(strings.ToLower(bugStr), strings.ToLower(term)) {
			t.Errorf("bug_report.yml should mention %q", term)
		}
	}

	// Verify feature_request.yml contents
	featData, _ := os.ReadFile(filepath.Join(templatesDir, "feature_request.yml"))
	featStr := string(featData)
	for _, term := range []string{"use case", "proposal", "impact"} {
		if !strings.Contains(strings.ToLower(featStr), strings.ToLower(term)) {
			t.Errorf("feature_request.yml should mention %q", term)
		}
	}

	// Verify channel_request.yml contents
	chanData, _ := os.ReadFile(filepath.Join(templatesDir, "channel_request.yml"))
	chanStr := string(chanData)
	for _, term := range []string{"Discord", "Instagram", "RCS", "Email"} {
		if !strings.Contains(strings.ToLower(chanStr), strings.ToLower(term)) {
			t.Errorf("channel_request.yml should mention %q", term)
		}
	}

	// Verify config.yml contents
	cfgData, _ := os.ReadFile(filepath.Join(templatesDir, "config.yml"))
	cfgStr := string(cfgData)
	for _, term := range []string{"Discussions", "contact_links"} {
		if !strings.Contains(strings.ToLower(cfgStr), strings.ToLower(term)) {
			t.Errorf("config.yml should mention %q", term)
		}
	}
}

func TestPullRequestTemplate(t *testing.T) {
	root := findRepoRoot(t)
	prTemplatePath := filepath.Join(root, ".github", "pull_request_template.md")

	data, err := os.ReadFile(prTemplatePath)
	if err != nil {
		t.Fatalf("pull_request_template.md missing: %v", err)
	}

	content := string(data)
	requiredElements := []string{
		"make test-race",
		"make lint",
		"conventional commit",
		"documentation",
		"screenshot",
	}

	for _, elem := range requiredElements {
		if !strings.Contains(strings.ToLower(content), strings.ToLower(elem)) {
			t.Errorf("pull_request_template.md missing required checklist item %q", elem)
		}
	}
}

func TestContributingGuide(t *testing.T) {
	root := findRepoRoot(t)
	contribPath := filepath.Join(root, "CONTRIBUTING.md")

	data, err := os.ReadFile(contribPath)
	if err != nil {
		t.Fatalf("CONTRIBUTING.md missing: %v", err)
	}

	content := string(data)

	// Must reference PerGo instead of legacy OmniGo URLs
	if strings.Contains(content, "OmniGo") {
		t.Errorf("CONTRIBUTING.md must not reference legacy OmniGo URLs")
	}
	if !strings.Contains(content, "https://github.com/pablodiegoo/PerGo") {
		t.Errorf("CONTRIBUTING.md must reference https://github.com/pablodiegoo/PerGo")
	}

	// Must reference main instead of master
	if strings.Contains(content, "`master`") {
		t.Errorf("CONTRIBUTING.md must reference branch `main` rather than `master`")
	}
	if !strings.Contains(content, "`main`") {
		t.Errorf("CONTRIBUTING.md must reference default branch `main`")
	}

	// Must reference Code of Conduct and Security Policy
	if !strings.Contains(content, "CODE_OF_CONDUCT.md") {
		t.Errorf("CONTRIBUTING.md should reference CODE_OF_CONDUCT.md")
	}
	if !strings.Contains(content, "SECURITY.md") {
		t.Errorf("CONTRIBUTING.md should reference SECURITY.md")
	}

	// Must reference correct canonical docs paths
	if !strings.Contains(content, "docs/getting-started/installation.md") {
		t.Errorf("CONTRIBUTING.md should link to docs/getting-started/installation.md")
	}
	if !strings.Contains(content, "docs/development/index.md") {
		t.Errorf("CONTRIBUTING.md should link to docs/development/index.md")
	}
}




