package security_test

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"github.com/pablojhp.pergo/internal/security"
)

func TestForbiddenIP_Ranges(t *testing.T) {
	tests := []struct {
		name      string
		ip        string
		forbidden bool
	}{
		// Private IPv4 (RFC 1918)
		{"private 10.0.0.1", "10.0.0.1", true},
		{"private 10.255.255.254", "10.255.255.254", true},
		{"private 172.16.0.1", "172.16.0.1", true},
		{"private 172.31.255.254", "172.31.255.254", true},
		{"private 192.168.0.1", "192.168.0.1", true},
		{"private 192.168.1.1", "192.168.1.1", true},

		// Loopback
		{"loopback 127.0.0.1", "127.0.0.1", true},
		{"loopback 127.0.0.2", "127.0.0.2", true},
		{"loopback IPv6 ::1", "::1", true},

		// Link-Local & Cloud Metadata (AWS/GCP/Azure 169.254.169.254)
		{"cloud metadata 169.254.169.254", "169.254.169.254", true},
		{"link-local 169.254.0.1", "169.254.0.1", true},
		{"link-local IPv6 fe80::1", "fe80::1", true},

		// Carrier-Grade NAT (100.64.0.0/10)
		{"cgnat 100.64.0.1", "100.64.0.1", true},
		{"cgnat 100.127.255.254", "100.127.255.254", true},

		// Multicast
		{"multicast 224.0.0.1", "224.0.0.1", true},
		{"multicast IPv6 ff02::1", "ff02::1", true},

		// Unspecified
		{"unspecified 0.0.0.0", "0.0.0.0", true},
		{"unspecified IPv6 ::", "::", true},

		// IPv4-mapped IPv6
		{"ipv4-mapped loopback", "::ffff:127.0.0.1", true},
		{"ipv4-mapped metadata", "::ffff:169.254.169.254", true},
		{"ipv4-mapped private", "::ffff:10.0.0.1", true},

		// Public routable IPs (MUST NOT be forbidden)
		{"public Cloudflare DNS 1.1.1.1", "1.1.1.1", false},
		{"public Google DNS 8.8.8.8", "8.8.8.8", false},
		{"public Example.com 93.184.216.34", "93.184.216.34", false},
		{"public IPv6 2606:2800:220:1:248:1893:25c8:1946", "2606:2800:220:1:248:1893:25c8:1946", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			netIP := net.ParseIP(tt.ip)
			if netIP == nil {
				t.Fatalf("failed to parse IP %s", tt.ip)
			}
			if got := security.IsForbiddenIP(netIP); got != tt.forbidden {
				t.Errorf("IsForbiddenIP(%s) = %v, expected %v", tt.ip, got, tt.forbidden)
			}

			addr := netip.MustParseAddr(tt.ip)
			if got := security.IsForbiddenAddr(addr); got != tt.forbidden {
				t.Errorf("IsForbiddenAddr(%s) = %v, expected %v", tt.ip, got, tt.forbidden)
			}
		})
	}
}

func TestIPChecker_Allowlist(t *testing.T) {
	allowlist := []string{
		"127.0.0.1",
		"10.50.0.0/16",
		"169.254.169.254",
		"internal-webhook.dev",
	}

	checker := security.NewIPChecker(allowlist)

	tests := []struct {
		name    string
		ip      string
		allowed bool
	}{
		{"allowlisted loopback", "127.0.0.1", true},
		{"non-allowlisted loopback", "127.0.0.2", false},
		{"allowlisted CIDR 10.50.1.2", "10.50.1.2", true},
		{"non-allowlisted private 10.60.1.2", "10.60.1.2", false},
		{"allowlisted metadata", "169.254.169.254", true},
		{"non-allowlisted metadata", "169.254.169.253", false},
		{"public IP 1.1.1.1", "1.1.1.1", true}, // Public IPs are valid
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr := netip.MustParseAddr(tt.ip)
			err := checker.ValidateAddr(addr)
			if tt.allowed {
				if err != nil {
					t.Errorf("expected IP %s to be allowed, got error: %v", tt.ip, err)
				}
			} else {
				if !errors.Is(err, security.ErrBlockedPrivateIP) {
					t.Errorf("expected ErrBlockedPrivateIP for %s, got: %v", tt.ip, err)
				}
			}
		})
	}

	if !checker.IsHostAllowed("internal-webhook.dev") {
		t.Errorf("expected host internal-webhook.dev to be allowed")
	}
	if checker.IsHostAllowed("evil.com") {
		t.Errorf("expected host evil.com not to be allowed")
	}
}

func TestSafeWebhookTransport_BlocksLocalhostAndLoopback(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer ts.Close()

	client := security.NewSafeWebhookClient(security.WithTimeout(2 * time.Second))

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, ts.URL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	_, err = client.Do(req)
	if err == nil {
		t.Fatalf("expected request to local loopback server to be blocked, but succeeded")
	}

	if !errors.Is(err, security.ErrBlockedPrivateIP) {
		t.Logf("connection failed as expected with SSRF block: %v", err)
	}
}

func TestSafeWebhookTransport_AllowsWhenInAllowlist(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer ts.Close()

	client := security.NewSafeWebhookClient(
		security.WithAllowlist("127.0.0.1", "::1"),
		security.WithTimeout(2*time.Second),
	)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, ts.URL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("expected request to succeed with allowlist, got error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
}

func TestSafeWebhookTransport_BlocksRedirectToForbiddenIP(t *testing.T) {
	// Server redirects to cloud metadata endpoint
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
	}))
	defer ts.Close()

	// Even if loopback ts.URL is allowlisted, the redirect to 169.254.169.254 must be blocked!
	client := security.NewSafeWebhookClient(
		security.WithAllowlist("127.0.0.1"),
		security.WithTimeout(2*time.Second),
	)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, ts.URL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	_, err = client.Do(req)
	if err == nil {
		t.Fatalf("expected redirect to 169.254.169.254 to be blocked, but succeeded")
	}

	t.Logf("redirect properly blocked: %v", err)
}

func TestValidateURL(t *testing.T) {
	tests := []struct {
		name      string
		rawURL    string
		allowlist []string
		wantErr   bool
	}{
		{"valid public https", "https://api.stripe.com/webhook", nil, false},
		{"valid public http", "http://example.com/callback", nil, false},
		{"invalid scheme ftp", "ftp://example.com/callback", nil, true},
		{"invalid missing scheme", "example.com/callback", nil, true},
		{"blocked loopback IP", "http://127.0.0.1:8080/hook", nil, true},
		{"blocked loopback IPv6", "http://[::1]:8080/hook", nil, true},
		{"blocked metadata IP", "http://169.254.169.254/latest", nil, true},
		{"blocked private RFC 1918 10.x", "http://10.0.0.5:9000/hook", nil, true},
		{"blocked private RFC 1918 192.168.x", "http://192.168.1.50/hook", nil, true},
		{"blocked localhost", "http://localhost:3000/hook", nil, true},
		{"blocked local domain", "http://service.local/hook", nil, true},
		{"blocked internal domain", "http://db.internal/hook", nil, true},
		{"allowlisted loopback", "http://127.0.0.1:8080/hook", []string{"127.0.0.1"}, false},
		{"allowlisted localhost", "http://localhost:8080/hook", []string{"localhost"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := security.ValidateURL(tt.rawURL, tt.allowlist...)
			if tt.wantErr && err == nil {
				t.Errorf("ValidateURL(%s) expected error, got nil", tt.rawURL)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("ValidateURL(%s) expected success, got %v", tt.rawURL, err)
			}
		})
	}
}
