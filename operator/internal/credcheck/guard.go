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

package credcheck

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"syscall"
	"time"

	"github.com/SUSE/aif-operator/internal/infra/safehttp"
)

// destClass classifies a dial destination for the probe SSRF guard.
type destClass int

const (
	// destPublic is a globally routable address.
	destPublic destClass = iota
	// destPrivate is internal address space (RFC1918, IPv6 ULA, CGNAT, ...).
	// On-prem and air-gap mirrors legitimately live here.
	destPrivate
	// destForbidden is never a repository: loopback, link-local (including the
	// cloud metadata endpoint), and unspecified addresses.
	destForbidden
)

var errBlockedDestination = errors.New("probe destination is not allowed")

// Test seams: loopback test servers must be classifiable as public/private, and
// origin resolution must be controllable without real DNS.
var (
	classifyDest = classifyIP
	lookupNetIP  = net.DefaultResolver.LookupNetIP
)

func classifyIP(ip netip.Addr) destClass {
	ip = ip.Unmap()
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
		return destForbidden
	}
	if safehttp.IsBlockedIP(ip) {
		return destPrivate
	}
	return destPublic
}

// guardedTransport returns a transport whose dialer re-checks every connection
// (each redirect hop, an OCI bearer realm, DNS rebinding) against the resolved
// IP. Forbidden destinations are always refused. When the configured origin
// host resolves only to public addresses, private destinations are refused too,
// so a public repository cannot redirect or rebind the probe into the cluster
// network; a repository configured on a private address keeps working.
//
// Behind an HTTP(S) proxy the dial goes to the proxy, so the guard then only
// sees the proxy address.
func guardedTransport(ctx context.Context, originHost string) *http.Transport {
	strict := originIsPublic(ctx, originHost)
	dialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
	dialer.Control = func(_, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return err
		}
		ip, err := netip.ParseAddr(host)
		if err != nil {
			return fmt.Errorf("unresolved address %q", host)
		}
		switch classifyDest(ip) {
		case destForbidden:
			return errBlockedDestination
		case destPrivate:
			if strict {
				return errBlockedDestination
			}
		}
		return nil
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = dialer.DialContext
	return transport
}

// originIsPublic reports whether host resolves only to public addresses. A host
// that cannot be resolved gets the strict policy (fail closed); its dial fails
// anyway unless DNS changes in between.
func originIsPublic(ctx context.Context, host string) bool {
	if ip, err := netip.ParseAddr(host); err == nil {
		return classifyDest(ip) == destPublic
	}
	addrs, err := lookupNetIP(ctx, "ip", host)
	if err != nil || len(addrs) == 0 {
		return true
	}
	for _, addr := range addrs {
		if classifyDest(addr) != destPublic {
			return false
		}
	}
	return true
}

// checkProbeRedirect caps redirect hops, refuses an https to http downgrade, and
// drops the credential when a redirect leaves the requested host.
func checkProbeRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("stopped after 10 redirects")
	}
	if via[0].URL.Scheme == "https" && req.URL.Scheme != "https" {
		return errors.New("redirect downgrades TLS")
	}
	if req.URL.Host != via[0].URL.Host {
		req.Header.Del("Authorization")
	}
	return nil
}
