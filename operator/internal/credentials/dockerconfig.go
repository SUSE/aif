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

package credentials

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

// dockerConfigJSON models the subset of ~/.docker/config.json we consume.
type dockerConfigJSON struct {
	Auths map[string]dockerAuthEntry `json:"auths"`
}

type dockerAuthEntry struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Auth     string `json:"auth"`
}

// normalizeRegistryHost strips any scheme and trailing path/slash so a
// dockerconfigjson key (e.g. "https://index.docker.io/v1/") can be compared to
// a bare registry host (e.g. "index.docker.io").
func normalizeRegistryHost(s string) string {
	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimPrefix(s, "http://")
	if i := strings.IndexByte(s, '/'); i >= 0 {
		s = s[:i]
	}
	return strings.ToLower(s)
}

// dockerConfigCredentials parses a .dockerconfigjson blob and returns the
// username/password for the entry whose host matches host. It never includes
// credential values in its error strings.
func dockerConfigCredentials(blob []byte, host string) (string, string, error) {
	var cfg dockerConfigJSON
	if err := json.Unmarshal(blob, &cfg); err != nil {
		return "", "", fmt.Errorf("malformed dockerconfigjson: %w", err)
	}
	want := normalizeRegistryHost(host)
	for key, entry := range cfg.Auths {
		if normalizeRegistryHost(key) != want {
			continue
		}
		if entry.Username != "" && entry.Password != "" {
			return entry.Username, entry.Password, nil
		}
		if entry.Auth != "" {
			decoded, err := base64.StdEncoding.DecodeString(entry.Auth)
			if err != nil {
				return "", "", fmt.Errorf("invalid base64 auth for host %q", want)
			}
			u, p, ok := strings.Cut(string(decoded), ":")
			if !ok || u == "" || p == "" {
				return "", "", fmt.Errorf("malformed auth pair for host %q", want)
			}
			return u, p, nil
		}
		return "", "", fmt.Errorf("no usable credentials for host %q", want)
	}
	return "", "", fmt.Errorf("no dockerconfigjson entry matching host %q", want)
}
