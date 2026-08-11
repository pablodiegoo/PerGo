package netpolicy_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"github.com/pablojhp.pergo/internal/platform/netpolicy"
)

func TestValidateAddr_BlockedRanges(t *testing.T) {
	tests := []struct {
		name    string
		ip      string
		blocked bool
	}{
		// Private IPv4
		{"private 10.x", "10.0.0.1", true},
		{"private 10.255.255.254", "10.255.255.254", true},
		{"private 172.16.x", "172.16.0.1", true},
		{"private 172.31.x", "172.31.255.254", true},
		{"private 192.168.x", "192.168.1.1", true},
		{"private 192.168.255.254", "192.168.255.254", true},

		// Loopback IPv4 & IPv6
		{"loopback 127.0.0.1", "127.0.0.1", true},
		{"loopback 127.0.0.2", "127.0.0.2", true},
		{"loopback ::1", "::1", true},

		// Link-local IPv4 & IPv6
		{"link-local 169.254.169.254", "169.254.169.254", true},
		{"link-local fe80::1", "fe80::1", true},

		// Carrier-grade NAT (100.64.0.0/10)
		{"cgnat 100.64.0.1", "100.64.0.1", true},
		{"cgnat 100.127.255.254", "100.127.255.254", true},

		// Multicast IPv4 & IPv6
		{"multicast 224.0.0.1", "224.0.0.1", true},
		{"multicast ff02::1", "ff02::1", true},

		// Unspecified IPv4 & IPv6
		{"unspecified 0.0.0.0", "0.0.0.0", true},
		{"unspecified ::", "::", true},

		// IPv4-mapped IPv6 loopback & private
		{"ipv4-mapped loopback", "::ffff:127.0.0.1", true},
		{"ipv4-mapped private 10.x", "::ffff:10.0.0.1", true},
		{"ipv4-mapped link-local", "::ffff:169.254.169.254", true},

		// Public IPs (should NOT be blocked)
		{"public 8.8.8.8", "8.8.8.8", false},
		{"public 1.1.1.1", "1.1.1.1", false},
		{"public 93.184.216.34", "93.184.216.34", false},
		{"public 2606:2800:220:1:248:1893:25c8:1946", "2606:2800:220:1:248:1893:25c8:1946", false},
	}

	checker := netpolicy.NewRestrictedIPChecker(nil)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr := netip.MustParseAddr(tt.ip)
			err := checker.ValidateAddr(addr)
			if tt.blocked {
				if !errors.Is(err, netpolicy.ErrRestrictedIP) {
					t.Errorf("expected ErrRestrictedIP for %s, got %v", tt.ip, err)
				}
			} else {
				if err != nil {
					t.Errorf("expected no error for public IP %s, got %v", tt.ip, err)
				}
			}
		})
	}
}

func TestValidateAddr_AllowlistOverride(t *testing.T) {
	allowlist := []string{
		"127.0.0.1",
		"10.0.0.0/8",
		"169.254.169.254",
	}

	checker := netpolicy.NewRestrictedIPChecker(allowlist)

	tests := []struct {
		name    string
		ip      string
		allowed bool
	}{
		{"allowlisted loopback 127.0.0.1", "127.0.0.1", true},
		{"non-allowlisted loopback 127.0.0.2", "127.0.0.2", false},
		{"allowlisted CIDR 10.1.2.3", "10.1.2.3", true},
		{"allowlisted AWS metadata IP", "169.254.169.254", true},
		{"non-allowlisted private 192.168.1.1", "192.168.1.1", false},
		{"public IP 8.8.8.8", "8.8.8.8", true},
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
				if !errors.Is(err, netpolicy.ErrRestrictedIP) {
					t.Errorf("expected ErrRestrictedIP for %s, got %v", tt.ip, err)
				}
			}
		})
	}
}

func TestNewPublicHTTPClient_LoopbackBlocked(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer ts.Close()

	client := netpolicy.NewPublicHTTPClient()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, ts.URL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	_, err = client.Do(req)
	if err == nil {
		t.Fatalf("expected request to loopback test server to fail due to SSRF policy, but it succeeded")
	}

	if !errors.Is(err, netpolicy.ErrRestrictedIP) {
		t.Logf("got expected connection error: %v", err)
	}
}

func TestNewPublicHTTPClient_AllowlistOverride(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer ts.Close()

	// Pass loopback IP to allowlist
	client := netpolicy.NewPublicHTTPClient(
		netpolicy.WithAllowedIPs("127.0.0.1", "::1"),
		netpolicy.WithTimeout(5*time.Second),
	)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, ts.URL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("expected request to succeed with allowlist, got: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
}
