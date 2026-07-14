package safehttp

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"
)

func TestIsBlockedIP(t *testing.T) {
	cases := []struct {
		ip      string
		blocked bool
	}{
		{"127.0.0.1", true}, {"::1", true},
		{"10.0.0.5", true}, {"172.16.0.1", true}, {"192.168.1.1", true},
		{"169.254.169.254", true}, {"fe80::1", true}, {"fc00::1", true},
		{"100.64.0.1", true}, {"0.0.0.0", true}, {"::", true},
		{"::ffff:127.0.0.1", true},  // IPv4-mapped loopback
		{"64:ff9b::7f00:1", true},   // NAT64-mapped 127.0.0.1
		{"64:ff9b::a00:5", true},    // NAT64-mapped 10.0.0.5
		{"64:ff9b::808:808", false}, // NAT64-mapped 8.8.8.8 (public → allowed)
		// IANA special-use ranges (not globally reachable)
		{"0.1.2.3", true},           // 0.0.0.0/8
		{"192.0.0.8", true},         // 192.0.0.0/24
		{"192.0.2.5", true},         // TEST-NET-1
		{"198.18.5.5", true},        // benchmarking 198.18.0.0/15
		{"198.51.100.7", true},      // TEST-NET-2
		{"203.0.113.9", true},       // TEST-NET-3
		{"240.0.0.1", true},         // reserved
		{"255.255.255.255", true},   // limited broadcast (within 240/4)
		{"2001:db8::1", true},       // IPv6 documentation
		{"2001::1", true},           // Teredo
		{"2002:c0a8:0101::1", true}, // 6to4
		{"8.8.8.8", false}, {"1.1.1.1", false}, {"2606:4700:4700::1111", false},
	}
	for _, c := range cases {
		got := IsBlockedIP(netip.MustParseAddr(c.ip))
		if got != c.blocked {
			t.Errorf("IsBlockedIP(%s) = %v, want %v", c.ip, got, c.blocked)
		}
	}
}

func TestClientBlocksLoopbackDial(t *testing.T) {
	// httptest servers listen on 127.0.0.1, which the filter must refuse.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()

	_, err := NewClient(5 * time.Second).Get(srv.URL)
	if err == nil {
		t.Fatal("expected dial to loopback to be blocked, got nil error")
	}
}
