package mir

import (
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/lizongying/nolang/builtin"
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
	// kernel32 — subprocess pipeline + system info (win_create_pipe / *_pipe /
	// std-handle / wait / exit-code / create-process shims, and win_sysconf).
	// These declares predate this list; re-added so the gate is green again.
	"CreatePipe":           true,
	"ReadFile":             true,
	"WriteFile":            true,
	"GetStdHandle":         true,
	"WaitForSingleObject":  true,
	"GetExitCodeProcess":   true,
	"CreateProcessA":       true,
	"GetSystemInfo":        true,
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

// ---------------------------------------------------------------------------
// Class-level guard for the OTHER half of the Windows link story.
//
// TestWindowsShimDeclaresAreRealExports only validates the `declare` block INSIDE
// the shim prelude. It does not look at the symbols the generic CLibCall path
// emits at the CALL SITE. `os.sysconf` is exactly that gap: it registered a bare
// CLibCall{FuncName:"sysconf"} with no windows variant, so a `*-windows-gnu`
// cross build emitted `call @sysconf` and died at lld-link with
//
//	undefined symbol: sysconf
//
// because msvcrt has no sysconf(3). The per-symbol tests (set-env, truncate) each
// covered one regression but nothing swept the WHOLE registry, so a builtin that
// was simply never exercised on Windows slipped through.
//
// This test closes that: for the windows target it resolves EVERY CLibCall
// builtin the way codegen does (clibResolve) and asserts the chosen symbol is
// linkable — either a nolang.win_* shim that is actually DEFINED in the prelude,
// a runtime/intrinsic symbol (nolang.*/llvm.*), or a bare name on the verified
// CRT/WinSDK list. Adding a POSIX-only builtin without an AltFuncs/shim now fails
// here, on any host, without needing to run the Windows linker.
// ---------------------------------------------------------------------------

// windowsCLibCRT are the bare C symbols the CLibCall path legitimately resolves
// to on Windows: the `_`-prefixed UCRT i/o + process entries, the ISO C names
// UCRT exports (exit/getenv/rename/system/signal), and gethostname (ws2_32, which
// the builder links for every windows target). Kept as a list rather than
// wildcarded so a NEW bare POSIX name is rejected until someone checks it.
var windowsCLibCRT = map[string]bool{
	"_chdir": true, "_chmod": true, "_close": true, "_dup2": true,
	"_getcwd": true, "_getpid": true, "_mkdir": true, "_open": true,
	"_putenv_s": true, "_read": true, "_rmdir": true, "_unlink": true,
	"_write": true,
	"exit": true, "getenv": true, "gethostname": true, "rename": true,
	"signal": true, "system": true,
}

// definedWinShims returns the set of nolang.win_* entry points the prelude
// actually DEFINES (not merely declares), so an AltFuncs pointing at a shim that
// was never written is caught rather than silently left undefined.
func definedWinShims() map[string]bool {
	re := regexp.MustCompile(`(?m)^define\s+[^@]*@(nolang\.win_[A-Za-z0-9_]+)\s*\(`)
	defs := map[string]bool{}
	for _, m := range re.FindAllStringSubmatch(windowsShimsIR(), -1) {
		defs[m[1]] = true
	}
	return defs
}

func TestWindowsCLibSymbolsAreResolvable(t *testing.T) {
	withTarget(t, "windows", "amd64")
	defs := definedWinShims()
	for _, m := range builtin.BuiltinMethodList {
		if m.CLibCall == nil || m.CLibCall.FuncName == "" {
			continue
		}
		fn, _, _ := clibResolve(m.CLibCall)
		switch {
		case strings.HasPrefix(fn, "nolang.win_"):
			if !defs[fn] {
				t.Errorf("windows: %s routes to shim %q which is not defined in windowsShimsIR", m.MethodName, fn)
			}
		case strings.HasPrefix(fn, "nolang."), strings.HasPrefix(fn, "llvm."):
			// runtime-provided / intrinsic, always resolvable.
		case windowsCLibCRT[fn]:
			// verified Windows CRT/WinSDK export.
		default:
			t.Errorf("windows: builtin %q emits bare %q, which the Windows CRT does not "+
				"export — add AltFuncs{windows: ...} (a CRT spelling or a nolang.win_* "+
				"shim). This is the sysconf-class bug: it only surfaces at lld-link on a "+
				"windows target, so it must be caught statically here.", m.MethodName, fn)
		}
	}
}

// TestSysconfWindowsShim: the specific regression. os.sysconf must lower to the
// nolang.win_sysconf shim on Windows and never reference bare @sysconf; the POSIX
// targets keep the real sysconf(3).
func TestSysconfWindowsShim(t *testing.T) {
	src := `main = () () {
  n = os.sysconf(84)
  print(n)
}
`
	withTarget(t, "windows", "amd64")
	ir, err := lowerSourceForTest(t, src).EmitLLVM()
	if err != nil {
		t.Fatalf("windows sysconf must lower: %v", err)
	}
	if !strings.Contains(ir, "@nolang.win_sysconf") {
		t.Errorf("windows IR must call @nolang.win_sysconf, got:\n%s", ir)
	}
	if strings.Contains(ir, "@sysconf(") {
		t.Errorf("windows IR must not reference @sysconf (msvcrt has no sysconf(3))")
	}
	// The shim body comes from the prelude; require a define of the symbol, in
	// either the typed- or opaque-pointer spelling.
	if !regexp.MustCompile(`define [^@]*@nolang\.win_sysconf`).MatchString(ir) {
		t.Errorf("windows IR must define @nolang.win_sysconf")
	}

	withTarget(t, "linux", "amd64")
	ir, err = lowerSourceForTest(t, src).EmitLLVM()
	if err != nil {
		t.Fatalf("linux sysconf must lower: %v", err)
	}
	if !strings.Contains(ir, "@sysconf(") {
		t.Errorf("linux IR must keep calling @sysconf, got:\n%s", ir)
	}
	if strings.Contains(ir, "nolang.win_") {
		t.Errorf("linux IR must not reference the Windows shims")
	}
}
