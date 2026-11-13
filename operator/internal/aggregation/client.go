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

// Package aggregation connects the operator to Rancher's API aggregation
// layer. Rancher's APIService controller writes a connection Secret holding
// the /v3/connect URL, a bearer token and (optionally) its internal CA. The
// operator dials that URL as an outbound websocket tunnel (remotedialer) and
// serves its HTTP handler only over the tunnel, so it needs no listening port.
package aggregation

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/gorilla/websocket"
	"github.com/rancher/remotedialer"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// Keys Rancher's APIService controller writes into the connection Secret.
const (
	secretKeyURL   = "url"
	secretKeyToken = "token"
	secretKeyCA    = "ca.crt"
)

const (
	defaultPollInterval = 10 * time.Second
	defaultBackoff      = 5 * time.Second
	handshakeTimeout    = 10 * time.Second
	readHeaderTimeout   = 30 * time.Second
)

// timing holds the intervals Run uses; tests shorten them.
type timing struct {
	poll    time.Duration // how often the Secret is re-read
	backoff time.Duration // delay before retrying a failed or short-lived tunnel
}

// Run keeps a tunnel to Rancher open and serves handler over it. It reads the
// url, token and ca.crt keys of the Secret namespace/secretName, waits while
// the Secret is missing or incomplete, reconnects after a failure (5 s
// backoff), restarts the tunnel when any of those keys change and closes it
// when the Secret is deleted (the Secret is re-read every 10 s). It blocks
// until ctx is done.
//
// handler is reachable only through the tunnel, i.e. only via Rancher, which
// authenticates the user and sets the Impersonate-* headers.
func Run(ctx context.Context, c client.Reader, namespace, secretName string, handler http.Handler) {
	run(ctx, c, namespace, secretName, handler, timing{poll: defaultPollInterval, backoff: defaultBackoff})
}

func run(ctx context.Context, c client.Reader, namespace, secretName string, handler http.Handler, t timing) {
	tn := &tunnel{
		reader:  c,
		key:     client.ObjectKey{Namespace: namespace, Name: secretName},
		handler: handler,
		timing:  t,
		logger:  log.FromContext(ctx).WithName("aggregation").WithValues("secret", namespace+"/"+secretName),
	}
	tn.run(ctx)
}

// connConfig is the connection data read from the Secret. Never log it.
type connConfig struct {
	url   string
	token string
	ca    []byte
}

func (c connConfig) equal(o connConfig) bool {
	return c.url == o.url && c.token == o.token && bytes.Equal(c.ca, o.ca)
}

type tunnel struct {
	reader  client.Reader
	key     client.ObjectKey
	handler http.Handler
	timing  timing
	logger  logr.Logger
}

func (tn *tunnel) run(ctx context.Context) {
	for {
		cfg, ok := tn.waitForConfig(ctx)
		if !ok {
			return
		}
		if !sleep(ctx, tn.serve(ctx, cfg)) {
			return
		}
	}
}

// readConfig reads the connection Secret. The returned error describes what
// is missing; it never contains Secret values.
func (tn *tunnel) readConfig(ctx context.Context) (connConfig, error) {
	var s corev1.Secret
	if err := tn.reader.Get(ctx, tn.key, &s); err != nil {
		return connConfig{}, err
	}
	cfg := connConfig{
		url:   strings.TrimSpace(string(s.Data[secretKeyURL])),
		token: strings.TrimSpace(string(s.Data[secretKeyToken])),
		ca:    bytes.TrimSpace(s.Data[secretKeyCA]),
	}
	if cfg.url == "" || cfg.token == "" {
		return connConfig{}, errors.New("connection secret has no url or token yet")
	}
	return cfg, nil
}

// waitForConfig polls until the Secret is complete. It logs each distinct
// reason for waiting once. It returns false when ctx is done.
func (tn *tunnel) waitForConfig(ctx context.Context) (connConfig, bool) {
	lastReason := ""
	for {
		cfg, err := tn.readConfig(ctx)
		if err == nil {
			return cfg, true
		}
		if reason := err.Error(); reason != lastReason {
			tn.logger.Info("waiting for the tunnel connection secret", "reason", reason)
			lastReason = reason
		}
		if !sleep(ctx, tn.timing.poll) {
			return connConfig{}, false
		}
	}
}

type connectResult struct {
	connected bool // the websocket handshake succeeded
	err       error
}

// serve runs one tunnel with cfg until it ends, the Secret changes or ctx is
// done, and returns how long to wait before the next attempt.
func (tn *tunnel) serve(ctx context.Context, cfg connConfig) time.Duration {
	sessCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	start := time.Now()
	result := make(chan connectResult, 1)
	go func() { result <- tn.connect(sessCtx, cfg) }()

	ticker := time.NewTicker(tn.timing.poll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			cancel()
			<-result
			tn.logger.Info("tunnel stopped")
			return 0
		case r := <-result:
			if !r.connected {
				tn.logger.Error(r.err, "tunnel connection failed", "retryIn", tn.timing.backoff)
				return tn.timing.backoff
			}
			// A tunnel that ran for a while (e.g. dropped by a Rancher pod
			// restart) is retried at once; one that dropped quickly waits.
			wait := tn.timing.backoff
			if time.Since(start) >= tn.timing.backoff {
				wait = 0
			}
			tn.logger.Info("tunnel disconnected", "reason", r.err.Error(), "retryIn", wait)
			return wait
		case <-ticker.C:
			next, err := tn.readConfig(ctx)
			// A deleted Secret ends the tunnel; run goes back to waiting for
			// it. A Secret that is otherwise unreadable keeps the current
			// tunnel; the next connect attempt waits for it.
			if apierrors.IsNotFound(err) {
				tn.logger.Info("tunnel connection secret deleted; disconnecting")
				cancel()
				<-result
				return 0
			}
			if err == nil && !next.equal(cfg) {
				tn.logger.Info("tunnel connection secret changed; reconnecting")
				cancel()
				<-result
				return 0
			}
		}
	}
}

// connect dials the tunnel and serves the handler over it until the tunnel
// ends or ctx is done.
func (tn *tunnel) connect(ctx context.Context, cfg connConfig) connectResult {
	wsURL, err := websocketURL(cfg.url)
	if err != nil {
		return connectResult{err: err}
	}
	tlsConfig, err := tlsConfigFor(cfg.ca)
	if err != nil {
		return connectResult{err: err}
	}
	dialer := &websocket.Dialer{
		Proxy:            http.ProxyFromEnvironment,
		HandshakeTimeout: handshakeTimeout,
		TLSClientConfig:  tlsConfig,
	}
	headers := http.Header{"Authorization": []string{"Bearer " + cfg.token}}
	conn, resp, err := dialer.DialContext(ctx, wsURL.String(), headers)
	if err != nil {
		if resp != nil {
			_ = resp.Body.Close()
			err = fmt.Errorf("%w (HTTP status %d)", err, resp.StatusCode)
		}
		return connectResult{err: err}
	}
	tn.logger.Info("tunnel connected", "host", wsURL.Host)

	// session.Serve does not watch ctx; closing the websocket ends it.
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	defer func() { _ = conn.Close() }()

	l := newPipeListener()
	srv := &http.Server{
		Handler:           tn.handler,
		ReadHeaderTimeout: readHeaderTimeout,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	go func() { _ = srv.Serve(l) }()
	defer func() { _ = srv.Close() }()

	// Every stream Rancher opens is served by handler; the local dialer never
	// reaches the network, so allowing any requested address is safe.
	allowAll := func(string, string) bool { return true }
	session := remotedialer.NewClientSessionWithDialer(allowAll, conn, l.Dial)
	defer session.Close()

	_, err = session.Serve(ctx)
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	return connectResult{connected: true, err: err}
}

// websocketURL converts the Secret's https URL into a wss URL. Errors never
// include the URL itself.
func websocketURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, errors.New("connection secret url is not a valid URL")
	}
	if u.User != nil {
		return nil, errors.New("connection secret url must not contain credentials")
	}
	switch u.Scheme {
	case "https", "wss":
		u.Scheme = "wss"
	default:
		// The token must never travel unencrypted.
		return nil, fmt.Errorf("connection secret url has scheme %q; only https is supported", u.Scheme)
	}
	return u, nil
}

// tlsConfigFor trusts only ca (Rancher's internal CA) when it is non-empty,
// and the system roots when it is empty. Server certificates are always
// verified.
func tlsConfigFor(ca []byte) (*tls.Config, error) {
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if len(ca) == 0 {
		return cfg, nil
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(ca) {
		return nil, errors.New("connection secret ca.crt contains no PEM certificate")
	}
	cfg.RootCAs = pool
	return cfg, nil
}

// sleep waits for d and reports whether ctx is still active.
func sleep(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// pipeListener is an in-memory net.Listener: remotedialer's local dialer
// hands each tunnelled stream to the HTTP server as one end of a net.Pipe.
type pipeListener struct {
	conns chan net.Conn
	done  chan struct{}
	once  sync.Once
}

func newPipeListener() *pipeListener {
	return &pipeListener{conns: make(chan net.Conn), done: make(chan struct{})}
}

func (l *pipeListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}

func (l *pipeListener) Close() error {
	l.once.Do(func() { close(l.done) })
	return nil
}

func (l *pipeListener) Addr() net.Addr { return pipeAddr{} }

// Dial matches remotedialer.Dialer. The address Rancher asks for is ignored.
func (l *pipeListener) Dial(ctx context.Context, _, _ string) (net.Conn, error) {
	local, remote := net.Pipe()
	select {
	case l.conns <- remote:
		return local, nil
	case <-l.done:
		_ = local.Close()
		_ = remote.Close()
		return nil, net.ErrClosed
	case <-ctx.Done():
		_ = local.Close()
		_ = remote.Close()
		return nil, ctx.Err()
	}
}

type pipeAddr struct{}

func (pipeAddr) Network() string { return "pipe" }
func (pipeAddr) String() string  { return "aggregation-tunnel" }
