//go:build wasip1

package main

// The browser build has no sockets, so remote nohub sites cannot be fetched.
// Local-directory sources (os.ReadFile of <dir>/index.noml) still work when the
// sandbox preopens them.

const hubDefaultSource = "https://lizongying.github.io/nohub"

func hubFetchIndex(base string) ([]byte, error) {
	return nil, errHubNetworkUnavailable
}
