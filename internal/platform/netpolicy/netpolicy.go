// Package netpolicy provides outbound network security policies, including SSRF prevention.
package netpolicy

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"syscall"
	"time"
)

// ErrRestrictedIP is returned when a destination IP address is blocked by netpolicy SSRF protection.
var ErrRestrictedIP = errors.New("restricted IP address blocked by netpolicy")

var cgnatPrefix = netip.MustParsePrefix("100.64.0.0/10")

// Options configures the SSRF-safe HTTP client.
type Options struct {
	AllowedRanges []string
	Timeout       time.Duration
}

// Option modifies Options.
type Option func(*Options)

// WithAllowedIPs adds IP addresses or CIDR ranges to the allowlist.
func WithAllowedIPs(ipsOrCIDRs ...string) Option {
	return func(o *Options) {
		o.AllowedRanges = append(o.AllowedRanges, ipsOrCIDRs...)
	}
}

// WithAllowlist is an alias for WithAllowedIPs to satisfy configurable allowlist requirements.
func WithAllowlist(ipsOrCIDRs ...string) Option {
	return WithAllowedIPs(ipsOrCIDRs...)
}

// WithTimeout sets the HTTP client timeout.
func WithTimeout(d time.Duration) Option {
	return func(o *Options) {
		o.Timeout = d
	}
}

// RestrictedIPChecker validates whether a netip.Addr is restricted by SSRF policy.
type RestrictedIPChecker struct {
	allowedAddrs    []netip.Addr
	allowedPrefixes []netip.Prefix
}

// NewRestrictedIPChecker creates a new RestrictedIPChecker with optional allowlist entries.
func NewRestrictedIPChecker(allowlist []string) *RestrictedIPChecker {
	checker := &RestrictedIPChecker{}
	for _, entry := range allowlist {
		if entry == "" {
			continue
		}
		// Try parsing as CIDR prefix first
		prefix, err := netip.ParsePrefix(entry)
		if err == nil {
			checker.allowedPrefixes = append(checker.allowedPrefixes, prefix)
			continue
		}
		// Try parsing as single IP addr
		addr, err := netip.ParseAddr(entry)
		if err == nil {
			checker.allowedAddrs = append(checker.allowedAddrs, addr.Unmap())
			continue
		}
	}
	return checker
}

// ValidateAddr checks if the given netip.Addr is allowed according to the SSRF policy.
func (c *RestrictedIPChecker) ValidateAddr(addr netip.Addr) error {
	addr = addr.Unmap()

	// 1. Check allowlist overrides first
	for _, allowedAddr := range c.allowedAddrs {
		if allowedAddr == addr {
			return nil
		}
	}
	for _, prefix := range c.allowedPrefixes {
		if prefix.Contains(addr) {
			return nil
		}
	}

	// 2. Block restricted IP ranges
	if addr.IsUnspecified() ||
		addr.IsLoopback() ||
		addr.IsLinkLocalUnicast() ||
		addr.IsLinkLocalMulticast() ||
		addr.IsMulticast() ||
		addr.IsPrivate() ||
		cgnatPrefix.Contains(addr) {
		return ErrRestrictedIP
	}

	return nil
}

// NewPublicHTTPClient returns an *http.Client configured with TCP-level (ControlContext) SSRF validation.
func NewPublicHTTPClient(opts ...Option) *http.Client {
	options := &Options{
		Timeout: 30 * time.Second,
	}
	for _, opt := range opts {
		opt(options)
	}

	checker := NewRestrictedIPChecker(options.AllowedRanges)

	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
		ControlContext: func(ctx context.Context, network, address string, c syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				host = address
			}

			addr, err := netip.ParseAddr(host)
			if err != nil {
				return fmt.Errorf("netpolicy: invalid IP address %q: %w", host, err)
			}

			if err := checker.ValidateAddr(addr); err != nil {
				return err
			}
			return nil
		},
	}

	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	return &http.Client{
		Transport: transport,
		Timeout:   options.Timeout,
	}
}
