// Package security provides perimeter security, anti-SSRF mitigations, and data governance controls.
package security

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"
)

// ErrBlockedPrivateIP is returned when a destination IP address belongs to a private, loopback,
// link-local, or cloud metadata network forbidden by SSRF security policy.
var ErrBlockedPrivateIP = errors.New("security: destination IP address belongs to a private/forbidden range")

var cgnatPrefix = netip.MustParsePrefix("100.64.0.0/10")

// Options configures the SafeWebhookTransport and SafeWebhookClient.
type Options struct {
	Allowlist []string
	Timeout   time.Duration
}

// Option modifies Options.
type Option func(*Options)

// WithAllowlist configures permitted IP addresses or CIDR blocks (e.g. for local dev/testing).
func WithAllowlist(ipsOrCIDRs ...string) Option {
	return func(o *Options) {
		o.Allowlist = append(o.Allowlist, ipsOrCIDRs...)
	}
}

// WithAllowedIPs is an alias for WithAllowlist.
func WithAllowedIPs(ipsOrCIDRs ...string) Option {
	return WithAllowlist(ipsOrCIDRs...)
}

// WithTimeout sets the overall request timeout.
func WithTimeout(d time.Duration) Option {
	return func(o *Options) {
		o.Timeout = d
	}
}

// IPChecker validates IP addresses against allowlist overrides and forbidden SSRF ranges.
type IPChecker struct {
	allowedAddrs    []netip.Addr
	allowedPrefixes []netip.Prefix
	allowedHosts    []string
}

// NewIPChecker constructs an IPChecker from a list of IP addresses, CIDR blocks, or hostnames.
func NewIPChecker(allowlist []string) *IPChecker {
	checker := &IPChecker{}
	for _, item := range allowlist {
		entry := strings.TrimSpace(item)
		if entry == "" {
			continue
		}
		if prefix, err := netip.ParsePrefix(entry); err == nil {
			checker.allowedPrefixes = append(checker.allowedPrefixes, prefix)
			continue
		}
		if addr, err := netip.ParseAddr(entry); err == nil {
			checker.allowedAddrs = append(checker.allowedAddrs, addr.Unmap())
			continue
		}
		checker.allowedHosts = append(checker.allowedHosts, strings.ToLower(entry))
	}
	return checker
}

// IsAllowed checks if an address matches any configured allowlist rule.
func (c *IPChecker) IsAllowed(addr netip.Addr) bool {
	addr = addr.Unmap()
	for _, allowed := range c.allowedAddrs {
		if allowed == addr {
			return true
		}
	}
	for _, prefix := range c.allowedPrefixes {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

// IsHostAllowed checks if a hostname matches any configured allowlist rule.
func (c *IPChecker) IsHostAllowed(host string) bool {
	lower := strings.ToLower(strings.TrimSpace(host))
	for _, allowed := range c.allowedHosts {
		if allowed == lower {
			return true
		}
	}
	return false
}

// ValidateAddr verifies whether an address is allowed or blocked by the SSRF policy.
func (c *IPChecker) ValidateAddr(addr netip.Addr) error {
	addr = addr.Unmap()
	if c.IsAllowed(addr) {
		return nil
	}
	if IsForbiddenAddr(addr) {
		return ErrBlockedPrivateIP
	}
	return nil
}

// IsForbiddenAddr returns true if the netip.Addr falls into a restricted range (loopback, private,
// link-local, cloud metadata, CGNAT, multicast, or unspecified).
func IsForbiddenAddr(addr netip.Addr) bool {
	addr = addr.Unmap()
	return addr.IsUnspecified() ||
		addr.IsLoopback() ||
		addr.IsLinkLocalUnicast() ||
		addr.IsLinkLocalMulticast() ||
		addr.IsMulticast() ||
		addr.IsPrivate() ||
		cgnatPrefix.Contains(addr)
}

// IsForbiddenIP returns true if the net.IP falls into a restricted range.
func IsForbiddenIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip4 := ip.To4(); ip4 != nil {
		ip = ip4
	}
	if addr, ok := netip.AddrFromSlice(ip); ok {
		return IsForbiddenAddr(addr)
	}
	if ip.IsLoopback() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsPrivate() ||
		ip.IsUnspecified() ||
		ip.IsMulticast() {
		return true
	}
	if len(ip) == 4 && ip[0] == 100 && (ip[1]&0xc0) == 64 {
		return true
	}
	return false
}

// SafeWebhookTransport wraps http.Transport and enforces TCP-level socket inspection via net.Dialer.Control.
type SafeWebhookTransport struct {
	*http.Transport
	checker *IPChecker
}

// NewSafeWebhookTransport creates an http.RoundTripper immune to SSRF and DNS Rebinding.
// It intercepts every outgoing TCP socket connection in net.Dialer.Control right before the socket
// is bound or connected, verifying that the destination IP is strictly public.
func NewSafeWebhookTransport(opts ...Option) *SafeWebhookTransport {
	options := &Options{}
	for _, opt := range opts {
		opt(options)
	}

	checker := NewIPChecker(options.Allowlist)

	dialer := &net.Dialer{
		Timeout:   5 * time.Second,
		KeepAlive: 30 * time.Second,
		Control: func(network, address string, c syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				host = address
			}

			addr, err := netip.ParseAddr(host)
			if err != nil {
				ip := net.ParseIP(host)
				if ip == nil {
					return errors.New("security: invalid destination IP address")
				}
				if parsedAddr, ok := netip.AddrFromSlice(ip); ok {
					addr = parsedAddr
				} else {
					if IsForbiddenIP(ip) {
						return ErrBlockedPrivateIP
					}
					return nil
				}
			}

			return checker.ValidateAddr(addr)
		},
	}

	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dialer.DialContext,
		ResponseHeaderTimeout: 10 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	return &SafeWebhookTransport{
		Transport: transport,
		checker:   checker,
	}
}

// NewSafeWebhookClient returns an *http.Client configured with SafeWebhookTransport and redirect protections.
func NewSafeWebhookClient(opts ...Option) *http.Client {
	options := &Options{
		Timeout: 10 * time.Second,
	}
	for _, opt := range opts {
		opt(options)
	}

	transport := NewSafeWebhookTransport(opts...)

	return &http.Client{
		Transport: transport,
		Timeout:   options.Timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("security: stopped after 10 redirects")
			}
			// Defense-in-depth: proactively validate redirect destination URL before following
			if err := ValidateURL(req.URL.String(), options.Allowlist...); err != nil {
				return fmt.Errorf("security: redirect target blocked: %w", err)
			}
			return nil
		},
	}
}

// ValidateURL checks a URL string against SSRF policy: scheme, host presence, and resolved IP ranges.
func ValidateURL(rawURL string, allowlist ...string) error {
	u, err := url.ParseRequestURI(rawURL)
	if err != nil {
		return fmt.Errorf("security: invalid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return errors.New("security: URL scheme must be http or https")
	}
	hostname := u.Hostname()
	if hostname == "" {
		return errors.New("security: URL host cannot be empty")
	}

	checker := NewIPChecker(allowlist)

	// If host is an IP literal
	if addr, err := netip.ParseAddr(hostname); err == nil {
		return checker.ValidateAddr(addr)
	}

	lowerHost := strings.ToLower(hostname)
	if lowerHost == "localhost" || strings.HasSuffix(lowerHost, ".local") || strings.HasSuffix(lowerHost, ".internal") {
		if checker.IsHostAllowed(lowerHost) || checker.IsAllowed(netip.MustParseAddr("127.0.0.1")) {
			return nil
		}
		return ErrBlockedPrivateIP
	}

	// Resolve DNS hostnames to verify target IP addresses
	ips, err := net.LookupIP(hostname)
	if err == nil {
		for _, ip := range ips {
			if addr, ok := netip.AddrFromSlice(ip); ok {
				if err := checker.ValidateAddr(addr); err != nil {
					return err
				}
			}
		}
	}

	return nil
}
