/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package safehttp provides an HTTP client hardened against SSRF: it refuses to
// connect to loopback, private, link-local, or other internal addresses. The
// check runs at dial time on the resolved IP, so it also defeats DNS rebinding
// and applies to every redirect hop.
package safehttp

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"syscall"
	"time"
)

// cgnat is the RFC 6598 carrier-grade NAT range (100.64.0.0/10), which
// netip.Addr.IsPrivate does not cover.
var cgnat = netip.MustParsePrefix("100.64.0.0/10")

// nat64WellKnown is the RFC 6052 well-known NAT64 prefix (64:ff9b::/96). An
// address in this range embeds an IPv4 address in its low 32 bits, which the
// netip range predicates do not see — so a NAT64-mapped internal target (e.g.
// 64:ff9b::7f00:1 for 127.0.0.1) could otherwise slip through.
var nat64WellKnown = netip.MustParsePrefix("64:ff9b::/96")

// specialUse holds IANA "not globally reachable" ranges that netip's predicates
// do not classify (benchmarking, reserved, documentation, and legacy transition
// ranges). Some networks route these internally, so block them too. A denylist
// can never be exhaustive; this is defense-in-depth beneath the primary control
// (the operator only fetches the admin-configured catalog URL).
var specialUse = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),       // "this network" (RFC 1122)
	netip.MustParsePrefix("192.0.0.0/24"),    // IETF protocol assignments
	netip.MustParsePrefix("192.0.2.0/24"),    // TEST-NET-1 (documentation)
	netip.MustParsePrefix("192.88.99.0/24"),  // 6to4 relay anycast (deprecated)
	netip.MustParsePrefix("198.18.0.0/15"),   // benchmarking (RFC 2544)
	netip.MustParsePrefix("198.51.100.0/24"), // TEST-NET-2 (documentation)
	netip.MustParsePrefix("203.0.113.0/24"),  // TEST-NET-3 (documentation)
	netip.MustParsePrefix("240.0.0.0/4"),     // reserved + limited broadcast (255.255.255.255)
	netip.MustParsePrefix("2001::/32"),       // Teredo
	netip.MustParsePrefix("2001:db8::/32"),   // IPv6 documentation
	netip.MustParsePrefix("2002::/16"),       // 6to4 (deprecated)
}

// IsBlockedIP reports whether ip is an internal/unsafe destination.
func IsBlockedIP(ip netip.Addr) bool {
	ip = ip.Unmap() // treat IPv4-mapped IPv6 as IPv4
	// NAT64: re-check the embedded IPv4 so mapped internal addresses are caught
	// while mapped public addresses stay allowed.
	if nat64WellKnown.Contains(ip) {
		b := ip.As16()
		if IsBlockedIP(netip.AddrFrom4([4]byte{b[12], b[13], b[14], b[15]})) {
			return true
		}
	}
	if ip.IsLoopback() ||
		ip.IsPrivate() || // RFC1918 + IPv6 ULA (fc00::/7)
		ip.IsLinkLocalUnicast() || // 169.254.0.0/16 (incl. cloud metadata), fe80::/10
		ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() ||
		ip.IsUnspecified() || // 0.0.0.0, ::
		cgnat.Contains(ip) {
		return true
	}
	for _, p := range specialUse {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

// NewClient returns an *http.Client whose dialer rejects blocked addresses and
// caps redirects at 5 (each hop is re-checked at dial time).
func NewClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: timeout}
	dialer.Control = func(_, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return err
		}
		ip, err := netip.ParseAddr(host)
		if err != nil {
			return fmt.Errorf("unresolved address %q", host)
		}
		if IsBlockedIP(ip) {
			return fmt.Errorf("blocked internal address %s", ip)
		}
		return nil
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: &http.Transport{DialContext: dialer.DialContext},
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("stopped after 5 redirects")
			}
			return nil
		},
	}
}
