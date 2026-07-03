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

// Package credcheck performs live authentication probes against container
// registries using the Docker Registry v2 protocol.
package credcheck

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Status is the outcome of a credential probe.
type Status string

const (
	// StatusOK means the credentials authenticated successfully.
	StatusOK Status = "ok"
	// StatusFailed means the endpoint was reached but rejected the credentials.
	StatusFailed Status = "failed"
	// StatusError means the endpoint could not be reached (DNS/dial/timeout/etc).
	StatusError Status = "error"
)

// Result is the outcome of a probe. Message never contains secret values.
type Result struct {
	Status  Status
	Message string
}

const probeTimeout = 10 * time.Second

// ProbeRegistry checks that (username, password) authenticate against the
// registry at host, following the Docker Registry v2 bearer-token handshake.
func ProbeRegistry(ctx context.Context, host, username, password string) Result {
	return probe(ctx, http.DefaultClient, "https", host, username, password)
}

func probe(ctx context.Context, client *http.Client, scheme, host, username, password string) Result {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	base := scheme + "://" + host + "/v2/"

	resp, err := doGet(ctx, client, base, username, password, "")
	if err != nil {
		return Result{Status: StatusError, Message: err.Error()}
	}
	defer resp.Body.Close()

	// A 200 here means the registry is reachable and issued (or accepted) a token.
	// On anonymously readable registries this can succeed with weak/empty creds
	// (an anonymous token is granted), so "ok" attests reachability + token
	// issuance, not that credentials are strictly required. Empty credentials are
	// filtered out by the caller before probing.
	if resp.StatusCode == http.StatusOK {
		return Result{Status: StatusOK, Message: "authenticated"}
	}
	// 403 means the request authenticated but authorization was denied — the
	// endpoint was reached and rejected the caller, which is a credential-level
	// failure rather than an unreachable error.
	if resp.StatusCode == http.StatusForbidden {
		return Result{Status: StatusFailed, Message: statusMessage(resp.StatusCode)}
	}
	if resp.StatusCode == http.StatusUnauthorized {
		challenge := resp.Header.Get("WWW-Authenticate")
		if strings.HasPrefix(strings.ToLower(challenge), "bearer ") {
			token, fr := fetchBearerToken(ctx, client, challenge, username, password)
			if fr != nil {
				return *fr
			}
			resp2, err2 := doGet(ctx, client, base, username, password, token)
			if err2 != nil {
				return Result{Status: StatusError, Message: err2.Error()}
			}
			defer resp2.Body.Close()
			if resp2.StatusCode == http.StatusOK {
				return Result{Status: StatusOK, Message: "authenticated"}
			}
			return Result{Status: StatusFailed, Message: statusMessage(resp2.StatusCode)}
		}
		return Result{Status: StatusFailed, Message: "401 unauthorized"}
	}
	return Result{Status: StatusError, Message: "unexpected " + statusMessage(resp.StatusCode)}
}

func doGet(ctx context.Context, client *http.Client, reqURL, user, pass, bearer string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	} else if user != "" || pass != "" {
		req.SetBasicAuth(user, pass)
	}
	return client.Do(req)
}

func fetchBearerToken(ctx context.Context, client *http.Client, challenge, user, pass string) (string, *Result) {
	params := parseChallenge(challenge)
	realm := params["realm"]
	if realm == "" {
		return "", &Result{Status: StatusError, Message: "no realm in bearer challenge"}
	}
	u, err := url.Parse(realm)
	if err != nil {
		return "", &Result{Status: StatusError, Message: err.Error()}
	}
	q := u.Query()
	if svc := params["service"]; svc != "" {
		q.Set("service", svc)
	}
	if scope := params["scope"]; scope != "" {
		q.Set("scope", scope)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", &Result{Status: StatusError, Message: err.Error()}
	}
	if user != "" || pass != "" {
		req.SetBasicAuth(user, pass)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", &Result{Status: StatusError, Message: err.Error()}
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return "", &Result{Status: StatusFailed, Message: "token endpoint rejected credentials: " + statusMessage(resp.StatusCode)}
	}
	if resp.StatusCode != http.StatusOK {
		return "", &Result{Status: StatusError, Message: "token endpoint returned " + statusMessage(resp.StatusCode)}
	}
	var tok struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return "", &Result{Status: StatusError, Message: err.Error()}
	}
	if tok.Token != "" {
		return tok.Token, nil
	}
	if tok.AccessToken != "" {
		return tok.AccessToken, nil
	}
	return "", &Result{Status: StatusError, Message: "no token in response"}
}

// parseChallenge parses a `Bearer realm="...",service="...",scope="..."` header.
func parseChallenge(h string) map[string]string {
	out := map[string]string{}
	h = strings.TrimSpace(h)
	if i := strings.IndexByte(h, ' '); i >= 0 {
		h = h[i+1:] // strip the "Bearer" scheme
	}
	for _, part := range strings.Split(h, ",") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(kv[0])
		val := strings.Trim(strings.TrimSpace(kv[1]), `"`)
		out[key] = val
	}
	return out
}

func statusMessage(code int) string {
	return fmt.Sprintf("%d %s", code, http.StatusText(code))
}
