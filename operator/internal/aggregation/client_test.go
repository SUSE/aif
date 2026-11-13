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

package aggregation

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	stdlog "log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-logr/logr/funcr"
	"github.com/rancher/remotedialer"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	testNamespace = "cattle-system"
	testSecret    = "aif-operator-api-service"
	// waitTimeout bounds every "eventually" check. The brief allows 15 s;
	// with the test timing below the tunnel normally connects in well under
	// a second.
	waitTimeout = 15 * time.Second
)

// testTiming keeps poll and backoff short so the tests run fast.
var testTiming = timing{poll: 20 * time.Millisecond, backoff: 20 * time.Millisecond}

// fakeRancher is a TLS remotedialer server standing in for Rancher's
// /v3/connect endpoint. The bearer token is used as the remotedialer client
// key, so a session for a token exists only if that token authenticated.
type fakeRancher struct {
	t        *testing.T
	rd       *remotedialer.Server
	srv      *httptest.Server
	listener *trackingListener

	mu     sync.Mutex
	tokens []string // bearer tokens of every authenticated connect

	handshakeErrors atomic.Int32 // TLS handshakes the server rejected
}

func newFakeRancher(t *testing.T) *fakeRancher {
	t.Helper()
	f := &fakeRancher{t: t}
	f.rd = remotedialer.New(f.authorize, remotedialer.DefaultErrorWriter)
	f.start("127.0.0.1:0")
	t.Cleanup(f.stop)
	return f
}

func (f *fakeRancher) authorize(req *http.Request) (string, bool, error) {
	token, ok := strings.CutPrefix(req.Header.Get("Authorization"), "Bearer ")
	if !ok || token == "" {
		return "", false, nil
	}
	f.mu.Lock()
	f.tokens = append(f.tokens, token)
	f.mu.Unlock()
	return token, true, nil
}

// start listens on addr and serves the remotedialer server over TLS with
// httptest's fixed localhost certificate, so a restarted server presents the
// same certificate as before.
func (f *fakeRancher) start(addr string) {
	f.t.Helper()
	var l net.Listener
	var err error
	// Re-binding the address of a server that just closed can briefly fail.
	for deadline := time.Now().Add(5 * time.Second); ; {
		if l, err = net.Listen("tcp", addr); err == nil || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		f.t.Fatalf("listen %s: %v", addr, err)
	}
	f.listener = &trackingListener{Listener: l}
	f.srv = httptest.NewUnstartedServer(f.rd)
	f.srv.Listener = f.listener
	f.srv.Config.ErrorLog = stdlog.New(handshakeErrorCounter{&f.handshakeErrors}, "", 0)
	f.srv.StartTLS()
}

// stop closes the server and every connection it accepted. httptest's Close
// leaves hijacked (websocket) connections open, so close them explicitly.
func (f *fakeRancher) stop() {
	f.srv.Close()
	f.listener.closeAll()
}

func (f *fakeRancher) restart() {
	f.t.Helper()
	addr := f.listener.Addr().String()
	f.stop()
	f.start(addr)
}

func (f *fakeRancher) url() string { return f.srv.URL + "/v3/connect" }

// caPEM returns the server certificate, which is self-signed, as PEM.
func (f *fakeRancher) caPEM() []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: f.srv.Certificate().Raw})
}

func (f *fakeRancher) seenTokens() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.tokens...)
}

// get sends an HTTP request to the operator through the tunnel that
// authenticated with token, the way Rancher's aggregation layer does.
func (f *fakeRancher) get(token, path string) (*http.Response, error) {
	c := &http.Client{
		Transport: &http.Transport{DialContext: f.rd.Dialer(token)},
		Timeout:   5 * time.Second,
	}
	return c.Get("http://aif-operator" + path)
}

type trackingListener struct {
	net.Listener
	mu    sync.Mutex
	conns []net.Conn
}

func (l *trackingListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err == nil {
		l.mu.Lock()
		l.conns = append(l.conns, c)
		l.mu.Unlock()
	}
	return c, err
}

func (l *trackingListener) closeAll() {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, c := range l.conns {
		_ = c.Close()
	}
	l.conns = nil
}

type handshakeErrorCounter struct{ n *atomic.Int32 }

func (h handshakeErrorCounter) Write(p []byte) (int, error) {
	if strings.Contains(string(p), "TLS handshake error") {
		h.n.Add(1)
	}
	return len(p), nil
}

func connectionSecret(rawURL, token string, ca []byte) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: testSecret},
		Data: map[string][]byte{
			"url":    []byte(rawURL),
			"token":  []byte(token),
			"ca.crt": ca,
		},
	}
}

// tokenMarker appears in every token the tests use, so a leaked token is
// easy to spot in the captured log.
const tokenMarker = "s3cr3t"

// startRun runs the tunnel client until the test ends, checks that it returns
// once its context is cancelled, and checks that its log never contains a
// token.
func startRun(t *testing.T, c client.Reader, handler http.Handler) {
	t.Helper()
	logs := &logBuffer{}
	logger := funcr.New(logs.write, funcr.Options{Verbosity: 10})
	ctx, cancel := context.WithCancel(log.IntoContext(context.Background(), logger))
	done := make(chan struct{})
	go func() {
		defer close(done)
		run(ctx, c, testNamespace, testSecret, handler, testTiming)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("Run did not return after its context was cancelled")
		}
		if out := logs.String(); strings.Contains(out, tokenMarker) {
			t.Errorf("the tunnel log contains a token:\n%s", out)
		}
	})
}

type logBuffer struct {
	mu sync.Mutex
	sb strings.Builder
}

func (b *logBuffer) write(prefix, args string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sb.WriteString(prefix + " " + args + "\n")
}

func (b *logBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.sb.String()
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(waitTimeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %s waiting for %s", waitTimeout, what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	})
}

func TestRun_ServesHandlerOverTunnel(t *testing.T) {
	f := newFakeRancher(t)
	c := fake.NewClientBuilder().WithObjects(connectionSecret(f.url(), "s3cr3t-tok-1", f.caPEM())).Build()

	gotPath := make(chan string, 1)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath <- r.URL.Path
		_, _ = io.WriteString(w, "hello")
	})
	startRun(t, c, handler)

	eventually(t, "a tunnel session for the token", func() bool { return f.rd.HasSession("s3cr3t-tok-1") })

	resp, err := f.get("s3cr3t-tok-1", "/x")
	if err != nil {
		t.Fatalf("GET /x through the tunnel: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "hello" {
		t.Fatalf("GET /x: status %d body %q, want 200 %q", resp.StatusCode, body, "hello")
	}
	if p := <-gotPath; p != "/x" {
		t.Errorf("handler saw path %q, want /x", p)
	}
	if got := f.seenTokens(); len(got) == 0 || got[0] != "s3cr3t-tok-1" {
		t.Errorf("tunnel connect bearer tokens = %v, want [s3cr3t-tok-1]", got)
	}
}

func TestRun_ReconnectsOnTokenChange(t *testing.T) {
	f := newFakeRancher(t)
	c := fake.NewClientBuilder().WithObjects(connectionSecret(f.url(), "s3cr3t-old", f.caPEM())).Build()
	startRun(t, c, okHandler())

	eventually(t, "a tunnel session for the old token", func() bool { return f.rd.HasSession("s3cr3t-old") })

	var s corev1.Secret
	key := client.ObjectKey{Namespace: testNamespace, Name: testSecret}
	if err := c.Get(context.Background(), key, &s); err != nil {
		t.Fatalf("get secret: %v", err)
	}
	s.Data["token"] = []byte("s3cr3t-new")
	if err := c.Update(context.Background(), &s); err != nil {
		t.Fatalf("update secret: %v", err)
	}

	eventually(t, "a tunnel session for the new token", func() bool { return f.rd.HasSession("s3cr3t-new") })
	eventually(t, "the old-token session to close", func() bool { return !f.rd.HasSession("s3cr3t-old") })

	resp, err := f.get("s3cr3t-new", "/")
	if err != nil {
		t.Fatalf("GET through the new tunnel: %v", err)
	}
	_ = resp.Body.Close()
}

func TestRun_ReconnectsAfterServerRestart(t *testing.T) {
	f := newFakeRancher(t)
	c := fake.NewClientBuilder().WithObjects(connectionSecret(f.url(), "s3cr3t-tok", f.caPEM())).Build()
	startRun(t, c, okHandler())

	eventually(t, "the first tunnel session", func() bool { return f.rd.HasSession("s3cr3t-tok") })
	before := len(f.seenTokens())

	f.restart()

	eventually(t, "a new tunnel session after the restart", func() bool {
		return len(f.seenTokens()) > before && f.rd.HasSession("s3cr3t-tok")
	})
	resp, err := f.get("s3cr3t-tok", "/")
	if err != nil {
		t.Fatalf("GET through the re-established tunnel: %v", err)
	}
	_ = resp.Body.Close()
}

func TestRun_VerifiesTLS(t *testing.T) {
	tests := []struct {
		name string
		ca   func(t *testing.T) []byte
	}{
		{name: "ca.crt does not match the server certificate", ca: otherCAPEM},
		{name: "empty ca.crt uses system roots only", ca: func(*testing.T) []byte { return nil }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeRancher(t)
			c := fake.NewClientBuilder().WithObjects(connectionSecret(f.url(), "s3cr3t-tok", tt.ca(t))).Build()
			startRun(t, c, okHandler())

			// The client must keep trying, and every attempt must fail the
			// TLS handshake before the token is ever sent.
			eventually(t, "several rejected TLS handshakes", func() bool { return f.handshakeErrors.Load() >= 3 })
			if got := f.seenTokens(); len(got) != 0 {
				t.Fatalf("the server received bearer tokens %v over unverified TLS", got)
			}
			if f.rd.HasSession("s3cr3t-tok") {
				t.Fatal("a tunnel session exists despite an untrusted server certificate")
			}
		})
	}
}

func TestTLSConfigFor_TrustsOnlyTheSecretCA(t *testing.T) {
	ca := otherCAPEM(t)
	cfg, err := tlsConfigFor(ca)
	if err != nil {
		t.Fatalf("tlsConfigFor: %v", err)
	}
	want := x509.NewCertPool()
	want.AppendCertsFromPEM(ca)
	if cfg.RootCAs == nil || !cfg.RootCAs.Equal(want) {
		t.Fatal("RootCAs must hold only the Secret's ca.crt, not the system roots as well")
	}
	if cfg.InsecureSkipVerify {
		t.Fatal("InsecureSkipVerify must never be set")
	}
}

func TestTLSConfigFor_EmptyCAUsesSystemRoots(t *testing.T) {
	cfg, err := tlsConfigFor(nil)
	if err != nil {
		t.Fatalf("tlsConfigFor: %v", err)
	}
	if cfg.RootCAs != nil {
		t.Fatal("RootCAs must be nil (system roots) when ca.crt is empty")
	}
	if cfg.InsecureSkipVerify {
		t.Fatal("InsecureSkipVerify must never be set")
	}
}

func TestRun_SecretDeletedClosesTunnel(t *testing.T) {
	f := newFakeRancher(t)
	c := fake.NewClientBuilder().WithObjects(connectionSecret(f.url(), "s3cr3t-tok", f.caPEM())).Build()
	startRun(t, c, okHandler())

	eventually(t, "the first tunnel session", func() bool { return f.rd.HasSession("s3cr3t-tok") })

	if err := c.Delete(context.Background(), connectionSecret(f.url(), "s3cr3t-tok", nil)); err != nil {
		t.Fatalf("delete secret: %v", err)
	}
	// The Secret is re-read every poll interval; allow a few for scheduling.
	deadline := time.Now().Add(10 * testTiming.poll)
	for f.rd.HasSession("s3cr3t-tok") {
		if time.Now().After(deadline) {
			t.Fatal("the tunnel session is still open after the Secret was deleted")
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Back to waiting: no reconnect while the Secret is missing ...
	before := len(f.seenTokens())
	time.Sleep(10 * testTiming.poll)
	if got := len(f.seenTokens()); got != before {
		t.Fatalf("the client reconnected %d time(s) without a Secret", got-before)
	}
	// ... and a new Secret brings the tunnel back.
	if err := c.Create(context.Background(), connectionSecret(f.url(), "s3cr3t-tok-2", f.caPEM())); err != nil {
		t.Fatalf("create secret: %v", err)
	}
	eventually(t, "a tunnel session once the Secret is recreated", func() bool { return f.rd.HasSession("s3cr3t-tok-2") })
}

func TestRun_MissingSecretWaits(t *testing.T) {
	f := newFakeRancher(t)
	c := fake.NewClientBuilder().Build()
	startRun(t, c, okHandler())

	// Several poll intervals without a Secret: no connection attempt.
	time.Sleep(10 * testTiming.poll)
	if got := f.seenTokens(); len(got) != 0 || f.handshakeErrors.Load() != 0 {
		t.Fatalf("connection attempted without a Secret (tokens %v)", got)
	}

	if err := c.Create(context.Background(), connectionSecret(f.url(), "s3cr3t-tok", f.caPEM())); err != nil {
		t.Fatalf("create secret: %v", err)
	}
	eventually(t, "a tunnel session once the Secret exists", func() bool { return f.rd.HasSession("s3cr3t-tok") })
}

// otherCAPEM returns a self-signed CA certificate unrelated to the server's.
func otherCAPEM(t *testing.T) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "other-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}
