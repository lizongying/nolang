//go:build !wasip1

package main

// Network half of the nohub index reader. Kept behind !wasip1 for the same
// reason as package/download_net.go: pulling net/http into a wasip1 binary
// drags in sock_accept / sock_shutdown wasm imports that the browser sandbox
// does not provide.
//
// A nohub source is the gh-pages base URL of a nohub site, e.g.
// https://lizongying.github.io/nohub. We fetch its root index.noml — the exact
// file web/app.js loads for its first screen — and parse the `packages` array.

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// hubDefaultSource is the built-in public nohub site (gh-pages).
const hubDefaultSource = "https://lizongying.github.io/nohub"

var hubHTTPClient = &http.Client{Timeout: 30 * time.Second}

// hubIndexURL returns the index.noml URL for a source base. A source may be a
// gh-pages URL (https://host/path) or a local directory; local directories are
// handled by the caller before this function is reached.
func hubIndexURL(base string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if strings.Contains(base, "/index.noml") {
		return base
	}
	return base + "/index.noml"
}

// hubFetchIndex downloads a nohub site's root index.noml.
func hubFetchIndex(base string) ([]byte, error) {
	url := hubIndexURL(base)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("nohub index request: %w", err)
	}
	req.Header.Set("User-Agent", "nolang")
	resp, err := hubHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("nohub index fetch %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("nohub index fetch %s: status %d", url, resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("nohub index read %s: %w", url, err)
	}
	return data, nil
}
