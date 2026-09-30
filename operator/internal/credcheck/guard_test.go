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
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

// Test servers listen on loopback, which the real policy forbids. The test
// classifier treats 127.0.0.1 as a public origin and 127.0.0.2 as private
// address space; everything else keeps the real classification.
var (
	testPublicIP  = netip.MustParseAddr("127.0.0.1")
	testPrivateIP = netip.MustParseAddr("127.0.0.2")
)

func testClassify(ip netip.Addr) destClass {
	switch ip.Unmap() {
	case testPublicIP:
		return destPublic
	case testPrivateIP:
		return destPrivate
	}
	return classifyIP(ip)
}

func TestMain(m *testing.M) {
	classifyDest = testClassify
	os.Exit(m.Run())
}

func useRealClassifier(t *testing.T) {
	t.Helper()
	classifyDest = classifyIP
	t.Cleanup(func() { classifyDest = testClassify })
}

// privateServer starts a plain-HTTP server on 127.0.0.2 (test "private" space)
// and counts the requests it receives.
func privateServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	ln, err := net.Listen("tcp", testPrivateIP.String()+":0")
	if err != nil {
		t.Skipf("cannot listen on %s: %v", testPrivateIP, err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	_ = srv.Listener.Close()
	srv.Listener = ln
	srv.Start()
	t.Cleanup(srv.Close)
	return srv, &hits
}

func redirectServer(target string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target, http.StatusFound)
	}))
}

func TestClassifyIP(t *testing.T) {
	cases := map[string]destClass{
		"127.0.0.1":        destForbidden,
		"::1":              destForbidden,
		"::ffff:127.0.0.1": destForbidden,
		"169.254.169.254":  destForbidden,
		"fe80::1":          destForbidden,
		"0.0.0.0":          destForbidden,
		"::":               destForbidden,
		"10.43.0.1":        destPrivate,
		"172.16.0.1":       destPrivate,
		"192.168.1.10":     destPrivate,
		"fd00::1":          destPrivate,
		"100.64.0.1":       destPrivate,
		"::ffff:10.0.0.1":  destPrivate,
		"8.8.8.8":          destPublic,
		"2606:4700::1111":  destPublic,
	}
	for addr, want := range cases {
		if got := classifyIP(netip.MustParseAddr(addr)); got != want {
			t.Errorf("classifyIP(%s)=%d want %d", addr, got, want)
		}
	}
}

func TestOriginIsPublic(t *testing.T) {
	orig := lookupNetIP
	defer func() { lookupNetIP = orig }()
	cases := []struct {
		name  string
		addrs []string
		err   error
		want  bool
	}{
		{"public", []string{"8.8.8.8"}, nil, true},
		{"private", []string{"10.0.0.5"}, nil, false},
		{"mixed", []string{"8.8.8.8", "10.0.0.5"}, nil, false},
		{"unresolvable fails closed", nil, errors.New("no such host"), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lookupNetIP = func(context.Context, string, string) ([]netip.Addr, error) {
				out := make([]netip.Addr, 0, len(tc.addrs))
				for _, a := range tc.addrs {
					out = append(out, netip.MustParseAddr(a))
				}
				return out, tc.err
			}
			if got := originIsPublic(context.Background(), "repo.example.com"); got != tc.want {
				t.Fatalf("originIsPublic=%v want %v", got, tc.want)
			}
		})
	}
}

func TestProbeHelmIndexRefusesRedirectToMetadata(t *testing.T) {
	origin := redirectServer("http://169.254.169.254/latest/meta-data/")
	defer origin.Close()
	res := ProbeHelmIndex(context.Background(), origin.URL, "", "", nil, false)
	if res.Status != StatusError || !strings.Contains(res.Message, errBlockedDestination.Error()) {
		t.Fatalf("redirect to metadata endpoint not refused by the guard: %+v", res)
	}
}

func TestProbeHelmIndexRefusesRedirectFromPublicToPrivate(t *testing.T) {
	internal, hits := privateServer(t)
	origin := redirectServer(internal.URL + "/index.yaml")
	defer origin.Close()
	if res := ProbeHelmIndex(context.Background(), origin.URL, "", "", nil, false); res.Status != StatusError {
		t.Fatalf("public origin redirected into private space: %+v", res)
	}
	if hits.Load() != 0 {
		t.Fatal("private destination was contacted")
	}
}

func TestProbeHelmIndexAllowsPrivateMirror(t *testing.T) {
	internal, hits := privateServer(t)
	if res := ProbeHelmIndex(context.Background(), internal.URL, "", "", nil, false); res.Status != StatusOK {
		t.Fatalf("private mirror refused: %+v", res)
	}
	if hits.Load() != 1 {
		t.Fatalf("hits=%d want 1", hits.Load())
	}
}

func TestProbeHelmIndexDenyPrivateNetworksRefusesPrivateMirror(t *testing.T) {
	internal, hits := privateServer(t)
	ctx := DenyPrivateNetworks(context.Background())
	if res := ProbeHelmIndex(ctx, internal.URL, "", "", nil, false); res.Status != StatusError {
		t.Fatalf("private origin reached under DenyPrivateNetworks: %+v", res)
	}
	if hits.Load() != 0 {
		t.Fatal("private destination was contacted")
	}
}

func TestCheckHost(t *testing.T) {
	orig := lookupNetIP
	defer func() { lookupNetIP = orig }()
	lookupNetIP = func(_ context.Context, _, host string) ([]netip.Addr, error) {
		switch host {
		case "public.example.com":
			return []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil
		case "private.example.com":
			return []netip.Addr{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("10.0.0.5")}, nil
		case "metadata.example.com":
			return []netip.Addr{netip.MustParseAddr("169.254.169.254")}, nil
		}
		return nil, errors.New("no such host")
	}
	deny := DenyPrivateNetworks(context.Background())
	cases := []struct {
		name    string
		ctx     context.Context
		host    string
		blocked bool
	}{
		{"public", deny, "public.example.com", false},
		{"private allowed", context.Background(), "private.example.com", false},
		{"private denied", deny, "private.example.com", true},
		{"private literal denied", deny, "192.168.1.10", true},
		{"forbidden always", context.Background(), "metadata.example.com", true},
		{"forbidden literal", context.Background(), "169.254.169.254", true},
		{"unresolvable allowed", deny, "missing.example.com", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := CheckHost(tc.ctx, tc.host); (err != nil) != tc.blocked {
				t.Fatalf("CheckHost(%s)=%v want blocked=%v", tc.host, err, tc.blocked)
			}
		})
	}
}

func TestProbeHelmIndexRefusesTLSDowngradeRedirect(t *testing.T) {
	var plainHit atomic.Bool
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		plainHit.Store(true)
		w.WriteHeader(http.StatusOK)
	}))
	defer plain.Close()
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plain.URL+"/index.yaml", http.StatusFound)
	}))
	defer origin.Close()
	if res := ProbeHelmIndex(context.Background(), origin.URL, "user", "pass", nil, true); res.Status != StatusError {
		t.Fatalf("https to http redirect followed: %+v", res)
	}
	if plainHit.Load() {
		t.Fatal("plain-http destination was contacted")
	}
}

func TestProbeHelmIndexRefusesHostnameResolvingToLoopback(t *testing.T) {
	useRealClassifier(t)
	var hit atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit.Store(true)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	if res := ProbeHelmIndex(context.Background(), "http://localhost:"+port, "", "", nil, false); res.Status != StatusError {
		t.Fatalf("loopback hostname probed: %+v", res)
	}
	if hit.Load() {
		t.Fatal("loopback destination was contacted")
	}
}

func TestProbeChartRefusesRedirectIntoPrivateOrMetadata(t *testing.T) {
	internal, hits := privateServer(t)
	for name, target := range map[string]string{
		"metadata": "https://169.254.169.254/charts/index.yaml",
		"private":  "https://" + internal.Listener.Addr().String() + "/index.yaml",
	} {
		t.Run(name, func(t *testing.T) {
			origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, target, http.StatusFound)
			}))
			defer origin.Close()
			result := ProbeChart(context.Background(), origin.URL+"/charts", "sample", "", "", testChartCA(origin))
			if result.Status != "error" || result.Reason != reasonConnectionFailed {
				t.Fatalf("redirect followed: %+v", result)
			}
		})
	}
	if hits.Load() != 0 {
		t.Fatal("private destination was contacted")
	}
}

func TestProbeRegistryRefusesBearerRealmIntoPrivateSpace(t *testing.T) {
	internal, hits := privateServer(t)
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="`+internal.URL+`/token",service="registry"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer origin.Close()
	res := ProbeRegistryWithCAAndInsecure(context.Background(), hostOf(t, origin.URL), "user", "pass", nil, true)
	if res.Status != StatusError {
		t.Fatalf("bearer realm in private space fetched: %+v", res)
	}
	if hits.Load() != 0 {
		t.Fatal("private token realm was contacted")
	}
}
