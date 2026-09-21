package layout

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/pablojhp.pergo/internal/i18n"
)

func TestIsSettingsActive(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		{"/admin/connections", true},
		{"/admin/workspaces/123/webhooks", true},
		{"/admin/workspaces/123/campaigns", false}, // Campaigns must not trigger settings active
		{"/admin/campaigns", false},
		{"/admin/inbox", false},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			got := isSettingsActive(tc.path)
			if got != tc.expected {
				t.Errorf("isSettingsActive(%q) = %v; want %v", tc.path, got, tc.expected)
			}
		})
	}
}

func TestIsTopLevelActive(t *testing.T) {
	tests := []struct {
		path     string
		target   string
		expected bool
	}{
		{"/admin/campaigns", "/admin/campaigns", true},
		{"/admin/workspaces/123/campaigns", "/admin/campaigns", true},
		{"/admin/workspaces/123/webhooks", "/admin/campaigns", false},
		{"/admin/inbox", "/admin/inbox", true},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			got := isTopLevelActive(tc.path, tc.target)
			if got != tc.expected {
				t.Errorf("isTopLevelActive(%q, %q) = %v; want %v", tc.path, tc.target, got, tc.expected)
			}
		})
	}
}

func TestIsSubmenuActive(t *testing.T) {
	tests := []struct {
		path     string
		target   string
		expected bool
	}{
		{"/admin/workspace", "/admin/workspace", true},
		{"/admin/workspaces/123", "/admin/workspace", true},
		{"/admin/workspaces/123/webhooks", "/admin/workspace", false},
		{"/admin/workspaces/123/webhooks", "/admin/webhooks", true},
		{"/admin/workspaces/123/campaigns", "/admin/workspace", false},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			got := isSubmenuActive(tc.path, tc.target)
			if got != tc.expected {
				t.Errorf("isSubmenuActive(%q, %q) = %v; want %v", tc.path, tc.target, got, tc.expected)
			}
		})
	}
}

func TestSidebar_LocalizationAndLanguageSwitcher(t *testing.T) {
	// 1. English (Default)
	var bufEn bytes.Buffer
	ctxEn := context.Background()
	if err := Sidebar().Render(ctxEn, &bufEn); err != nil {
		t.Fatalf("Sidebar().Render(en) failed: %v", err)
	}
	htmlEn := bufEn.String()

	if !strings.Contains(htmlEn, "Overview") {
		t.Errorf("expected English sidebar to contain 'Overview', got:\n%s", htmlEn)
	}
	if !strings.Contains(htmlEn, "Settings") {
		t.Errorf("expected English sidebar to contain 'Settings', got:\n%s", htmlEn)
	}
	if !strings.Contains(htmlEn, "Logout") {
		t.Errorf("expected English sidebar to contain 'Logout', got:\n%s", htmlEn)
	}
	if !strings.Contains(htmlEn, `hx-post="/admin/locale"`) {
		t.Errorf("expected sidebar to contain language switcher with hx-post=\"/admin/locale\"")
	}
	if !strings.Contains(htmlEn, `hx-vals='{"locale": "pt-BR"}'`) {
		t.Errorf("expected language switcher to contain pt-BR toggle value")
	}
	if !strings.Contains(htmlEn, `hx-vals='{"locale": "en-US"}'`) {
		t.Errorf("expected language switcher to contain en-US toggle value")
	}

	// 2. Portuguese (pt-BR)
	var bufPt bytes.Buffer
	ctxPt := i18n.WithLocale(context.Background(), "pt-BR")
	if err := Sidebar().Render(ctxPt, &bufPt); err != nil {
		t.Fatalf("Sidebar().Render(pt-BR) failed: %v", err)
	}
	htmlPt := bufPt.String()

	if !strings.Contains(htmlPt, "Visão Geral") {
		t.Errorf("expected Portuguese sidebar to contain 'Visão Geral', got:\n%s", htmlPt)
	}
	if !strings.Contains(htmlPt, "Configurações") {
		t.Errorf("expected Portuguese sidebar to contain 'Configurações', got:\n%s", htmlPt)
	}
	if !strings.Contains(htmlPt, "Sair") {
		t.Errorf("expected Portuguese sidebar to contain 'Sair', got:\n%s", htmlPt)
	}

	// 3. Zero-Emoji Rule
	for _, ch := range htmlEn + htmlPt {
		code := int(ch)
		if (0x1F300 <= code && code <= 0x1FAFF) || (0x2600 <= code && code <= 0x27BF) {
			t.Errorf("found disallowed emoji character %c (U+%X) in sidebar output", ch, code)
		}
	}
}

