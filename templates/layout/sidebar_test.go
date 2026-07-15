package layout

import "testing"

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
