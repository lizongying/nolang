package main

import (
	"errors"
	"reflect"
	"testing"
)

// TestDefaultJSRuntime pins the user-facing default: `no run --js` must use
// Node.js unless --js-runtime says otherwise.
func TestDefaultJSRuntime(t *testing.T) {
	if defaultJSRuntime != "node" {
		t.Fatalf("defaultJSRuntime = %q, want %q", defaultJSRuntime, "node")
	}
	if exe, extra := jsRuntimeSpec(""); exe != defaultJSRuntime || len(extra) != 0 {
		t.Fatalf("jsRuntimeSpec(\"\") = (%q, %v), want (%q, nil)", exe, extra, defaultJSRuntime)
	}
}

// fakeLookPath builds a lookPath that only knows the listed executables.
func fakeLookPath(known ...string) func(string) (string, error) {
	return func(name string) (string, error) {
		for _, k := range known {
			if k == name {
				return "/usr/bin/" + name, nil
			}
		}
		return "", errors.New("executable file not found in $PATH")
	}
}

// resolveJSRuntime adds the Debian/Ubuntu `nodejs` fallback on top of the spec
// parser. The fallback applies only to an untouched default: an explicit
// runtime (bun, an absolute path) or an explicit "node <flags>" spec must fail
// loudly rather than run something the user did not ask for.
func TestResolveJSRuntime(t *testing.T) {
	cases := []struct {
		name   string
		spec   string
		known  []string
		wantEx string
		wantOK bool
	}{
		{"node present", "", []string{"node"}, "node", true},
		{"default falls back to nodejs", "", []string{"nodejs"}, "nodejs", true},
		{"neither node nor nodejs", "", []string{"bun"}, "node", false},
		{"explicit bun", "bun", []string{"bun", "nodejs"}, "bun", true},
		{"explicit bun missing", "bun", []string{"node", "nodejs"}, "bun", false},
		{"explicit node args are not overridden", "node --enable-source-maps", []string{"nodejs"}, "node", false},
		{"explicit path missing", "/opt/js/bin/node", []string{"node", "nodejs"}, "/opt/js/bin/node", false},
		{"deno run", "deno run", []string{"deno"}, "deno", true},
	}
	for _, c := range cases {
		exe, _, ok := resolveJSRuntime(c.spec, fakeLookPath(c.known...))
		if ok != c.wantOK || exe != c.wantEx {
			t.Errorf("%s: resolveJSRuntime(%q) = (%q, ok=%v), want (%q, ok=%v)",
				c.name, c.spec, exe, ok, c.wantEx, c.wantOK)
		}
	}
}

// jsRuntimeSpec drives which runtime `no run --js` executes the bundle with:
// the default must stay exactly `node` (byte-compatible historical behavior),
// single-token runtimes (bun, deno) carry no extra args, and space-separated
// specs keep the leading tokens as runtime args so subcommand-style runtimes
// ("deno run --allow-read") work.
func TestJSRuntimeSpec(t *testing.T) {
	cases := []struct {
		spec  string
		exe   string
		extra []string
	}{
		{"", "node", nil},
		{"node", "node", nil},
		{"bun", "bun", nil},
		{"deno", "deno", nil},
		{"/usr/local/bin/node", "/usr/local/bin/node", nil},
		{"  bun  ", "bun", nil}, // surrounding space tolerated
		{"deno run", "deno", []string{"run"}},
		{"deno run --allow-read", "deno", []string{"run", "--allow-read"}},
		{"node --enable-source-maps", "node", []string{"--enable-source-maps"}},
	}
	for _, c := range cases {
		exe, extra := jsRuntimeSpec(c.spec)
		if exe != c.exe {
			t.Errorf("jsRuntimeSpec(%q) exe = %q, want %q", c.spec, exe, c.exe)
		}
		if len(extra) == 0 && len(c.extra) == 0 {
			continue
		}
		if !reflect.DeepEqual(extra, c.extra) {
			t.Errorf("jsRuntimeSpec(%q) extra = %v, want %v", c.spec, extra, c.extra)
		}
	}
}
