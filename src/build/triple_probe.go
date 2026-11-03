package build

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
)

// emitTripleProbeRe matches clang's -Woverride-module warning, which is the
// only place clang prints the exact canonical triple string it is about to
// install on a parsed module. Example:
//
//	warning: overriding the module target triple with
//	      arm64-apple-macosx27.0.0 [-Woverride-module]
//
// We deliberately run clang on a probe IR that has NO `target triple`
// directive: that guarantees the "empty vs non-empty" comparison fails and
// the warning always fires, so we can read the driver-canonical spelling
// straight off stderr without needing to reimplement Triple::normalize in Go.
//
// The line wrap (clang's diagnostic formatter breaks long messages) puts the
// triple on the second line, indented. Both shapes are matched.
var emitTripleProbeRe = regexp.MustCompile(`overriding the module target triple with\s+([^\s\[]+)`)

// emitTripleProbe syncs the once-per-key cache below.
var (
	emitTripleProbeMu    sync.Mutex
	emitTripleProbeCache = map[string]string{}
)

// queryEmitTargetTriple returns the exact string clang will install on the
// parsed module for (cc, target), which is what MIR must write into
// `target triple = "..."` to keep -Woverride-module quiet.
//
// It returns "" for anything the Go-side fallback in mir already knows how to
// canonicalise (linux / windows / wasi — no OS version suffix) and for
// non-native Apple targets whose SDK clang can't reach from this host. That
// way we shell out at most once per build session, only when it's actually
// needed to resolve a Darwin SDK version we can't observe from Go.
//
// The result is cached by (cc, target) key so repeated `no test ./...` runs
// pay the clang spawn exactly once per distinct target.
func queryEmitTargetTriple(cc, target string) string {
	// Non-darwin: mir's Go table already returns the right spelling.
	goos, _ := parseTargetPlatform(target)
	if goos != "darwin" {
		return ""
	}
	// Only native darwin builds (host is darwin) can resolve the SDK version;
	// cross-compiling darwin from Linux has no SDK here to inspect.
	if runtime.GOOS != "darwin" {
		return ""
	}
	key := cc + "\x00" + target
	emitTripleProbeMu.Lock()
	defer emitTripleProbeMu.Unlock()
	if v, ok := emitTripleProbeCache[key]; ok {
		return v
	}
	triple := probeEmitTargetTriple(cc, target)
	emitTripleProbeCache[key] = triple
	return triple
}

// probeEmitTargetTriple writes a triple-less LLVM IR to a temp file, asks
// clang (or zig cc) to compile it with `--target=<target>` while only
// emitting assembly (no link), and scrapes the driver's canonical triple off
// the -Woverride-module warning.
func probeEmitTargetTriple(cc, target string) string {
	dir, err := os.MkdirTemp("", "nolang-triple-probe-")
	if err != nil {
		return ""
	}
	defer os.RemoveAll(dir)
	llPath := filepath.Join(dir, "probe.ll")
	// Any valid IR without a triple works; `define i32 @main` keeps clang
	// happy enough to reach the module-override check before codegen.
	if err := os.WriteFile(llPath, []byte("define i32 @main() { ret i32 0 }\n"), 0o644); err != nil {
		return ""
	}
	exe := "clang"
	if cc == "zig" {
		exe = "zig"
	}
	args := []string{"-x", "ir", "-S"}
	if target != "" {
		args = append(args, "--target="+target)
	}
	if cc == "zig" {
		// zig cc needs the target as a subcommand prefix rather than --target.
		args = append([]string{"cc"}, args...)
	}
	args = append(args, llPath, "-o", filepath.Join(dir, "probe.s"))
	cmd := exec.Command(exe, args...)
	// Merge stderr so the regex sees the whole diagnostic stream.
	out, err := cmd.CombinedOutput()
	if err != nil && len(out) == 0 {
		return ""
	}
	text := strings.ReplaceAll(string(out), "\n", " ")
	m := emitTripleProbeRe.FindStringSubmatch(text)
	if len(m) < 2 {
		return ""
	}
	triple := strings.TrimSpace(m[1])
	// Sanity-check we actually scraped a triple-shaped string.
	if !strings.Contains(triple, "-") {
		return ""
	}
	return triple
}
