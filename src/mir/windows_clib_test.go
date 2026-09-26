package mir

import (
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
