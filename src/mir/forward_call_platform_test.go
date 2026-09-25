package mir

import "testing"

// ---------------------------------------------------------------------------
// The C-call spec table (forward_call.go) is TARGET-dependent, and used not to
// be: it was a package-level `var`, so statLayoutFor() and sysconfNProc() ran at
// package-init time — before the driver parsed `-target` — and every
// cross-compile baked the HOST's `struct stat` offsets and _SC_NPROCESSORS_ONLN
// value into the target binary. The second half of the bug is that the table
// only ever held POSIX names, so a Windows cross-compile referenced
// realpath/utimensat/sync, none of which exist in msvcrt:
//
//	lld-link: error: undefined symbol: sync
//	lld-link: error: undefined symbol: utimensat
//	lld-link: error: undefined symbol: realpath
//
// These tests pin both halves. They set the target explicitly (and restore it),
// so they assert the same expected values on every host — which is the point:
// on a Linux runner the darwin entry below used to return the Linux offsets.
// ---------------------------------------------------------------------------

// withTarget sets the -target platform for the duration of the test.
func withTarget(t *testing.T, goos, goarch string) {
	t.Helper()
	oldGoos, oldGoarch := mirTargetPlatform()
	SetTargetPlatform(goos, goarch)
	t.Cleanup(func() { SetTargetPlatform(oldGoos, oldGoarch) })
}

func specOf(t *testing.T, ff string) cCallSpec {
	t.Helper()
	sp, ok := forwardCSpecTable()[ff]
	if !ok {
		t.Fatalf("no C-call spec for ForwardFunc %q", ff)
	}
	return sp
}

// TestForwardCSpecsFollowTargetPlatform: the stat family and num-cpu must be
// laid out for the TARGET, not for the machine running the compiler.
func TestForwardCSpecsFollowTargetPlatform(t *testing.T) {
	cases := []struct {
		goos, goarch string
		sizeOff      int64 // stat-size / fstat-size
		modeOff      int64 // stat-file / stat-dir / stat-mode
		uidOff       int64
		uidWidth     int
		nproc        string // sysconf(_SC_NPROCESSORS_ONLN)
	}{
		{"linux", "amd64", 48, 24, 28, 32, "84"},
		{"linux", "arm64", 48, 16, 24, 32, "84"},
		{"darwin", "amd64", 96, 4, 16, 32, "58"},
		{"darwin", "arm64", 96, 4, 16, 32, "58"},
	}
	for _, tc := range cases {
		withTarget(t, tc.goos, tc.goarch)
		if got := specOf(t, "stat-size").Ret.Offset; got != tc.sizeOff {
			t.Errorf("%s/%s stat-size offset = %d, want %d", tc.goos, tc.goarch, got, tc.sizeOff)
		}
		if got := specOf(t, "fstat-size").Ret.Offset; got != tc.sizeOff {
			t.Errorf("%s/%s fstat-size offset = %d, want %d", tc.goos, tc.goarch, got, tc.sizeOff)
		}
		if got := specOf(t, "stat-file").Ret.Offset; got != tc.modeOff {
			t.Errorf("%s/%s stat-file offset = %d, want %d", tc.goos, tc.goarch, got, tc.modeOff)
		}
		if got := specOf(t, "stat-uid").Ret.Offset; got != tc.uidOff {
			t.Errorf("%s/%s stat-uid offset = %d, want %d", tc.goos, tc.goarch, got, tc.uidOff)
		}
		if got := specOf(t, "stat-uid").Ret.Width; got != tc.uidWidth {
			t.Errorf("%s/%s stat-uid width = %d, want %d", tc.goos, tc.goarch, got, tc.uidWidth)
		}
		nc := specOf(t, "num-cpu")
		if nc.Func != "sysconf" {
			t.Fatalf("%s/%s num-cpu func = %q, want sysconf", tc.goos, tc.goarch, nc.Func)
		}
		if got := nc.Args[0].Fixed; got != tc.nproc {
			t.Errorf("%s/%s _SC_NPROCESSORS_ONLN = %s, want %s", tc.goos, tc.goarch, got, tc.nproc)
		}
		// The stat family keeps its POSIX spelling off Windows.
		if got := specOf(t, "stat-file").Func; got != "stat" {
			t.Errorf("%s/%s stat-file func = %q, want stat", tc.goos, tc.goarch, got)
		}
		if got := specOf(t, "lstat").Func; got != "lstat" {
			t.Errorf("%s/%s lstat func = %q, want lstat", tc.goos, tc.goarch, got)
		}
		if got := specOf(t, "realpath").Func; got != "realpath" {
			t.Errorf("%s/%s realpath func = %q, want realpath", tc.goos, tc.goarch, got)
		}
		if got := specOf(t, "touch-file").Func; got != "utimensat" {
			t.Errorf("%s/%s touch-file func = %q, want utimensat", tc.goos, tc.goarch, got)
		}
		if got := specOf(t, "sync").Func; got != "sync" {
			t.Errorf("%s/%s sync func = %q, want sync", tc.goos, tc.goarch, got)
		}
	}
}

// TestForwardCSpecsWindowsSubstitutions: the three symbols that broke
// `no build -cc zig -target x86_64-windows-gnu`, plus the rest of the
// POSIX-only family, must lower to something msvcrt actually exports
// (checked against the mingw-w64 headers bundled with zig's libc: no realpath,
// no utimensat, no sync, no plain `stat`, no lstat at all).
func TestForwardCSpecsWindowsSubstitutions(t *testing.T) {
	withTarget(t, "windows", "amd64")

	want := map[string]string{
		"realpath":    "_fullpath",           // not realpath(3)
		"sync":        "_flushall",           // not sync(2) — best effort
		"touch-file":  "_utime64",            // not utimensat(2); _utime is a header-only inline
		"stat-file":   "_stat64",             // no plain `stat` in the import library
		"stat-dir":    "_stat64",             //
		"stat-exists": "_stat64",             //
		"stat-size":   "_stat64",             //
		"stat-mode":   "_stat64",             //
		"stat-uid":    "_stat64",             //
		"stat-gid":    "_stat64",             //
		"stat-mtime":  "_stat64",             //
		"lstat":       "_stat64",             // no lstat(2) on Windows
		"fstat-size":  "_fstat64",            //
		"num-cpu":     "GetNativeSystemInfo", // no sysconf(3)
	}
	for ff, fn := range want {
		if got := specOf(t, ff).Func; got != fn {
			t.Errorf("windows %s func = %q, want %q", ff, got, fn)
		}
	}

	// msvcrt `struct _stat64`: st_size @24, st_mode @6, st_uid/st_gid @10/@12
	// and they are 16-bit `short`s there, not 32-bit uid_t/gid_t.
	if got := specOf(t, "stat-size").Ret.Offset; got != 24 {
		t.Errorf("windows stat-size offset = %d, want 24", got)
	}
	if got := specOf(t, "stat-file").Ret.Offset; got != 6 {
		t.Errorf("windows stat-file offset = %d, want 6", got)
	}
	if got := specOf(t, "stat-uid").Ret.Width; got != 16 {
		t.Errorf("windows stat-uid width = %d, want 16 (msvcrt st_uid is short)", got)
	}
	if got := statLayoutFor().Size; got != 56 {
		t.Errorf("windows sizeof(struct _stat64) = %d, want 56", got)
	}
	// _fullpath takes the destination buffer FIRST (POSIX realpath takes the
	// path first) — a silent swap would write the answer over the input.
	rp := specOf(t, "realpath")
	if len(rp.Args) != 3 || rp.Args[0].Kind != cArgBufPtr || rp.Args[1].Kind != cArgCStr {
		t.Errorf("windows realpath args = %+v, want (buf, path, size)", rp.Args)
	}
	// num-cpu reads SYSTEM_INFO.dwNumberOfProcessors out of the scratch buffer
	// (the call returns void, so there is no return register to convert).
	nc := specOf(t, "num-cpu")
	if nc.Ret.Kind != cRetField || nc.Ret.LLVM != "void" || nc.Ret.Offset != 32 || nc.Ret.Width != 32 {
		t.Errorf("windows num-cpu ret = %+v, want cRetField/void@32 width 32", nc.Ret)
	}
}

// TestForwardCSpecsWindowsDropsUnavailable: builtins with no Windows C
// counterpart must be ABSENT from the table (so forwardCSpecOf misses) and must
// report why, instead of emitting a symbol lld-link rejects with a bare
// "undefined symbol".
func TestForwardCSpecsWindowsDropsUnavailable(t *testing.T) {
	for ff := range windowsUnavailable {
		withTarget(t, "windows", "amd64")
		if _, ok := forwardCSpecTable()[ff]; ok {
			t.Errorf("windows: %q must not have a C-call spec", ff)
		}
		if got := forwardCSpecUnavailable(ff); got == "" {
			t.Errorf("windows: forwardCSpecUnavailable(%q) = \"\", want a reason", ff)
		}
	}
	// ...and they must come back on a POSIX target.
	withTarget(t, "linux", "amd64")
	for _, ff := range []string{"readlink", "mkdtemp", "ttyname", "getdomainname", "process-fork"} {
		if _, ok := forwardCSpecTable()[ff]; !ok {
			t.Errorf("linux: %q lost its C-call spec", ff)
		}
		if got := forwardCSpecUnavailable(ff); got != "" {
			t.Errorf("linux: forwardCSpecUnavailable(%q) = %q, want \"\"", ff, got)
		}
	}
}
