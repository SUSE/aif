package labs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"time"

	"github.com/SUSE/aif-operator/internal/infra/safehttp"
)

const (
	fetchTimeout    = 10 * time.Second
	maxCatalogBytes = 1 << 20
)

var dnsLabel = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

// Fetcher downloads a catalog document.
type Fetcher func(ctx context.Context, url string) ([]byte, error)

var safeClient = safehttp.NewClient(fetchTimeout)

// SafeFetch downloads url with the public-URL-only safehttp client (10s, 1 MiB cap).
func SafeFetch(ctx context.Context, url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := safeClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %s", resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxCatalogBytes))
}

// MaxEntries matches the MaxItems bound on LabCatalog status.entries.
const MaxEntries = 100

// ParseExternal parses an external catalog document. It drops entries with an
// invalid or duplicate name or failing ValidateNames, strips auth/tls from the
// rest, and returns the number skipped and a message naming the first reason.
// At most MaxEntries entries are kept.
func ParseExternal(doc []byte) (entries []Entry, skipped int, msg string, err error) {
	var raw []Entry
	if err := json.Unmarshal(doc, &raw); err != nil {
		return nil, 0, "", fmt.Errorf("invalid catalog JSON: %w", err)
	}
	seen := map[string]bool{}
	skip := func(format string, a ...any) {
		skipped++
		if msg == "" {
			msg = fmt.Sprintf(format, a...)
		}
	}
	for _, e := range raw {
		switch {
		case !dnsLabel.MatchString(e.Name):
			skip("entry %q skipped: invalid name", e.Name)
			continue
		case seen[e.Name]:
			skip("entry %q skipped: duplicate name", e.Name)
			continue
		}
		e = sanitizeExternal(e)
		if verr := ValidateNames(e.Source, e.Extension); verr != nil {
			skip("entry %q skipped: %v", e.Name, verr)
			continue
		}
		if len(entries) >= MaxEntries {
			skip("entry %q skipped: catalog has more than %d entries", e.Name, MaxEntries)
			continue
		}
		seen[e.Name] = true
		entries = append(entries, e)
	}
	return entries, skipped, msg, nil
}
