package mir

import (
	"regexp"
	"sort"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Windows cross-compile regressed on two C symbols the MIR backend baked in for
// POSIX hosts only (seen building a `-cc zig -target *-windows-gnu` binary):
//
//	lld-link: error: undefined symbol: setenv
//	lld-link: error: undefined symbol: __stdinp
//
//   - `os.set-env` lowers through the generic CLibCall path (src/builtin/os.go
//     registers FuncName "setenv" with AltFuncs{"windows": _putenv_s} plus
//     AltArgTypes/AltFixedArgs dropping the POSIX overwrite flag). The Windows
//     CRT has no setenv(3); UCRT's _putenv_s(key, value) is 0 on success, so
//     the CmpRet (icmp eq 0 -> bool) conversion still applies. Verified against
//     zig's bundled mingw lib-common/api-ms-win-crt-environment def: it exports
//     _putenv_s (and plain getenv), never setenv.
//   - `fs.get-line` reads stdin via fgets. Darwin/glibc expose stdin as a data
//     symbol (__stdinp / stdin); UCRT does not — its FILE* slots are only
//     reachable by calling __acrt_iob_func(0) (exported by
//     api-ms-win-crt-stdio). Referencing __stdinp there dies at link time.
//
// Both symbols must follow the COMPILATION TARGET, like every other
// platform-dependent decision (see platform.go).
// ---------------------------------------------------------------------------

// TestSetEnvCLibWindowsSubstitution: on windows the call must route directly to
// @_putenv_s (two args, no leftover overwrite flag), and never reference bare
// @setenv; elsewhere the POSIX setenv stays.
func TestSetEnvCLibWindowsSubstitution(t *testing.T) {
	src := `main = () () {
  ok = set-env('A', 'B')
  print(ok)
}
`
	withTarget(t, "windows", "amd64")
	ir, err := lowerSourceForTest(t, src).EmitLLVM()
	if err != nil {
		t.Fatalf("windows set-env must lower: %v", err)
	}
	if !strings.Contains(ir, "@_putenv_s") {
		t.Errorf("windows IR must call @_putenv_s, got:\n%s", ir)
	}
	if strings.Contains(ir, "@setenv(") {
		t.Errorf("windows IR must not reference @setenv (not exported by the Windows CRT)")
	}
	// _putenv_s takes exactly (char*, char*): the POSIX overwrite flag must not
	// leak into its declaration.
	if strings.Contains(ir, "declare i32 @_putenv_s(i8*, i8*, i32)") {
		t.Errorf("windows _putenv_s declared with the leftover POSIX overwrite arg")
	}

	withTarget(t, "linux", "amd64")
	ir, err = lowerSourceForTest(t, src).EmitLLVM()
	if err != nil {
		t.Fatalf("linux set-env must lower: %v", err)
	}
	if !strings.Contains(ir, "@setenv(") {
		t.Errorf("linux IR must keep calling @setenv, got:\n%s", ir)
	}
	if strings.Contains(ir, "_putenv_s") {
		t.Errorf("linux IR must not reference _putenv_s")
	}
}

// TestGetLineStdinSymbolFollowsTarget: the stdin handle fed to fgets must be
// acquired the way the TARGET's CRT exposes it.
func TestGetLineStdinSymbolFollowsTarget(t *testing.T) {
	src := `main = () () {
  line, ok = get-line()
  print(ok)
}
`
	cases := []struct {
		goos   string
		want   string // must appear
		notWan string // must NOT appear
	}{
		{"windows", "@__acrt_iob_func", "__stdinp"},
		{"linux", "@stdin = external global", "__acrt_iob_func"},
		{"darwin", "@__stdinp", "__acrt_iob_func"},
	}
	for _, tc := range cases {
		withTarget(t, tc.goos, "amd64")
		ir, err := lowerSourceForTest(t, src).EmitLLVM()
		if err != nil {
			t.Fatalf("%s get-line must lower: %v", tc.goos, err)
		}
		if !strings.Contains(ir, tc.want) {
			t.Errorf("%s IR must contain %q, got:\n%s", tc.goos, tc.want, ir)
		}
		if strings.Contains(ir, tc.notWan) {
			t.Errorf("%s IR must not contain %q", tc.goos, tc.notWan)
		}
	}
}

// ---------------------------------------------------------------------------
// os.truncate on Windows called @_truncate, which does not exist:
//
//	lld-link: error: undefined symbol: _truncate
//	>>> referenced by notools.obj:(nolang.win_truncate)
//	>>> referenced by notools.obj:(truncate_run)
//
// `_truncate` is nowhere in the Windows CRT: not in UCRT's io.h, not in
// mingw-w64's unistd.h, and not in a single one of the ~600 import-library
// .def files zig bundles. mingw-w64 only offers `truncate`/`truncate64` as CRT
// extensions (libmingwex, and `truncate` is 32-bit _off_t), so the shim now
// goes through kernel32, which is linked on every Windows target.
// ---------------------------------------------------------------------------

// TestTruncateWindowsShimUsesKernel32: on Windows the shim must be built from
// kernel32 entry points and must never reference the nonexistent @_truncate;
// the POSIX path must be untouched.
func TestTruncateWindowsShimUsesKernel32(t *testing.T) {
	src := `main = () () {
  ok = os.truncate('f', 3)
  print(ok)
}
`
	withTarget(t, "windows", "amd64")
	ir, err := lowerSourceForTest(t, src).EmitLLVM()
	if err != nil {
		t.Fatalf("windows truncate must lower: %v", err)
	}
	for _, want := range []string{
		"@nolang.win_truncate",
		"@CreateFileA",
		"@SetFilePointerEx",
		"@SetEndOfFile",
	} {
		if !strings.Contains(ir, want) {
			t.Errorf("windows IR must contain %q, got:\n%s", want, ir)
		}
	}
	// @_truncate is undefined in every Windows CRT. Match the call target, not
	// the substring, so @nolang.win_truncate does not trip this.
	if strings.Contains(ir, "@_truncate(") || strings.Contains(ir, "@_truncate ") {
		t.Errorf("windows IR must not reference @_truncate (not a Windows CRT symbol)")
	}
	if strings.Contains(ir, "@truncate(") {
		t.Errorf("windows IR must not call the POSIX @truncate")
	}

	withTarget(t, "linux", "amd64")
	ir, err = lowerSourceForTest(t, src).EmitLLVM()
	if err != nil {
		t.Fatalf("linux truncate must lower: %v", err)
	}
	if !strings.Contains(ir, "@truncate(") {
		t.Errorf("linux IR must keep calling @truncate, got:\n%s", ir)
	}
	if strings.Contains(ir, "nolang.win_") {
		t.Errorf("linux IR must not reference the Windows shims")
	}
}

// windowsVerifiedExports is the allowlist for windowsShimsIR's `declare` block.
//
// Every name here was checked against zig's bundled mingw import libraries
// (libc/mingw/lib-common/*.def*); the ones starting with `_` live in
// ucrtbase / api-ms-win-crt-*, the rest in kernel32. Adding a shim that
// declares a new external symbol therefore means adding it here — and the
// point of the gate is that you must check it first, because a fabricated CRT
// name is invisible until lld-link runs (which cannot happen on macOS).
var windowsVerifiedExports = map[string]bool{
	// UCRT
	"_errno":         true,
	"_get_osfhandle": true,
	"_isatty":        true,
	// kernel32 — process / thread
	"GetCurrentProcess":        true,
	"GetCurrentProcessId":      true,
	"OpenProcess":              true,
	"TerminateProcess":         true,
	"SetPriorityClass":         true,
	"GetPriorityClass":         true,
	"CreateToolhelp32Snapshot": true,
	"Process32FirstW":          true,
	"Process32NextW":           true,
	// kernel32 — handles / files
	"CloseHandle":             true,
	"CreateFileA":             true,
	"SetFilePointerEx":        true,
	"SetEndOfFile":            true,
	"CreateSymbolicLinkA":     true,
	"CreateHardLinkA":         true,
	"LockFileEx":              true,
	"UnlockFileEx":            true,
	"GetEnvironmentVariableA": true,
	// kernel32 — error handling
	"GetLastError": true,
}

// TestWindowsShimDeclaresAreRealExports is the class-level guard: it parses the
// `declare` block of the Windows shim prelude and rejects any symbol that is
// not on the verified list. A symbol that no import library exports produces
// an `undefined symbol` link error, and only for Windows targets — i.e. it
// escapes every test that runs on the build host.
func TestWindowsShimDeclaresAreRealExports(t *testing.T) {
	re := regexp.MustCompile(`(?m)^declare\s+[^@]*@([A-Za-z0-9_.]+)\s*\(`)
	seen := map[string]bool{}
	for _, m := range re.FindAllStringSubmatch(windowsShimsIR(), -1) {
		name := m[1]
		// The shim defines its own nolang.win_* entry points.
		if strings.HasPrefix(name, "nolang.") {
			continue
		}
		seen[name] = true
		if !windowsVerifiedExports[name] {
			t.Errorf("windowsShimsIR declares @%s, which is not on the verified "+
				"export list — check it against libc/mingw/lib-common/*.def* first", name)
		}
	}
	// Keep the allowlist honest: a stale entry means a symbol was removed
	// without pruning the list (or a typo made the gate vacuous).
	var stale []string
	for name := range windowsVerifiedExports {
		if !seen[name] {
			stale = append(stale, name)
		}
	}
	sort.Strings(stale)
	for _, name := range stale {
		t.Errorf("windowsVerifiedExports lists %q but windowsShimsIR no longer declares it", name)
	}
}
