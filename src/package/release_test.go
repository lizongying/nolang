package pkg

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestParseToolSpec(t *testing.T) {
	cases := []struct {
		in      string
		name    string
		version string
		host    string
		owner   string
		repo    string
	}{
		{"nogit:v0.1.33@github.com/lizongying/notools", "nogit", "v0.1.33", "github.com", "lizongying", "notools"},
		{"nogit@github.com/lizongying/notools", "nogit", "", "github.com", "lizongying", "notools"},
		{"nogit@lizongying/notools", "nogit", "", "github.com", "lizongying", "notools"},
		{"nogit@https://github.com/lizongying/notools/", "nogit", "", "github.com", "lizongying", "notools"},
		{"nogit:latest@github.com/lizongying/notools", "nogit", "", "github.com", "lizongying", "notools"},
		{"nogit:1.2.3@github.com/lizongying/notools", "nogit", "1.2.3", "github.com", "lizongying", "notools"},
	}
	for _, c := range cases {
		got, err := ParseToolSpec(c.in)
		if err != nil {
			t.Fatalf("ParseToolSpec(%q): unexpected error: %v", c.in, err)
		}
		if got.Name != c.name || got.Version != c.version || got.Host != c.host ||
			got.Owner != c.owner || got.Repo != c.repo {
			t.Errorf("ParseToolSpec(%q) = %+v, want {name:%q version:%q host:%q owner:%q repo:%q}",
				c.in, got, c.name, c.version, c.host, c.owner, c.repo)
		}
	}
}

func TestParseToolSpecErrors(t *testing.T) {
	for _, in := range []string{
		"",
		"nogit",
		"@github.com/lizongying/notools",
		"nogit@github.com",
		"no/git@github.com/lizongying/notools",
	} {
		if _, err := ParseToolSpec(in); err == nil {
			t.Errorf("ParseToolSpec(%q): expected error, got nil", in)
		}
	}
}

// TestGithubGETRetriesTransportErrors：連線被掐斷（EOF）屬傳輸層錯誤，應重試。
func TestGithubGETRetriesTransportErrors(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) <= 2 {
			if hj, ok := w.(http.Hijacker); ok {
				if conn, _, err := hj.Hijack(); err == nil {
					conn.Close() // 製造 EOF
					return
				}
			}
		}
		w.Write([]byte("payload"))
	}))
	defer srv.Close()

	data, err := githubGET(srv.URL)
	if err != nil {
		t.Fatalf("githubGET: unexpected error: %v", err)
	}
	if string(data) != "payload" {
		t.Errorf("got %q, want %q", data, "payload")
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Errorf("expected 3 attempts, got %d", got)
	}
}

// TestGithubGETNoRetriesOnHTTPError：404（資產不存在）重試沒有意義，只應呼叫一次。
func TestGithubGETNoRetriesOnHTTPError(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	if _, err := githubGET(srv.URL); err == nil {
		t.Fatal("expected error for 404, got nil")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("expected exactly 1 attempt, got %d", got)
	}
}

func TestToolBinaryName(t *testing.T) {
	cases := []struct{ name, goos, want string }{
		{"nogit", "linux", "nogit"},
		{"nogit", "darwin", "nogit"},
		{"nogit", "windows", "nogit.exe"},
		{"nogit.exe", "windows", "nogit.exe"}, // 不重複加後綴
	}
	for _, c := range cases {
		if got := ToolBinaryName(c.name, c.goos); got != c.want {
			t.Errorf("ToolBinaryName(%q, %q) = %q, want %q", c.name, c.goos, got, c.want)
		}
	}
}

func TestReleaseAssetNameAndURL(t *testing.T) {
	if got, want := ReleaseAssetName("nogit", "linux", "amd64"), "nogit-linux-amd64"; got != want {
		t.Errorf("ReleaseAssetName(linux/amd64) = %q, want %q", got, want)
	}
	if got, want := ReleaseAssetName("nogit", "darwin", "arm64"), "nogit-darwin-arm64"; got != want {
		t.Errorf("ReleaseAssetName(darwin/arm64) = %q, want %q", got, want)
	}
	if got, want := ReleaseAssetName("nogit", "windows", "amd64"), "nogit-windows-amd64.exe"; got != want {
		t.Errorf("ReleaseAssetName(windows/amd64) = %q, want %q", got, want)
	}

	spec := ToolSpec{Name: "nogit", Version: "v0.1.33", Host: "github.com", Owner: "lizongying", Repo: "notools"}
	want := "https://github.com/lizongying/notools/releases/download/v0.1.33/nogit-linux-amd64"
	if got := ReleaseAssetURL(spec, "v0.1.33", "linux", "amd64"); got != want {
		t.Errorf("ReleaseAssetURL = %q, want %q", got, want)
	}
}
