package mir

import (
	"regexp"
	"strings"
	"testing"
)

func rawVFSIRForTest(t *testing.T, goos, goarch, body string) string {
	t.Helper()
	withTarget(t, goos, goarch)
	src := "main = () () {\n" + body + "\n}\n"
	ir, err := lowerSourceForTest(t, src).EmitLLVM()
	if err != nil {
		t.Fatalf("%s/%s raw VFS lowering failed: %v", goos, goarch, err)
	}
	return ir
}

func rawVFSBlock(t *testing.T, ir, label string) string {
	t.Helper()
	start := strings.Index(ir, label+":\n")
	if start < 0 {
		t.Fatalf("missing LLVM block %q", label)
	}
	rest := ir[start+len(label)+2:]
	if end := strings.Index(rest, "\n}"); end >= 0 {
		rest = rest[:end]
	}
	if next := regexp.MustCompile(`(?m)^[A-Za-z][A-Za-z0-9_.]*:\n`).FindStringIndex(rest); next != nil {
		rest = rest[:next[0]]
	}
	return rest
}

func TestRawVFSCapabilitiesFollowTarget(t *testing.T) {
	cases := []struct {
		goos, goarch string
		want         int64
	}{
		{"linux", "amd64", 255},
		{"linux", "arm64", 255},
		{"darwin", "amd64", 255},
		{"darwin", "arm64", 255},
		{"windows", "amd64", 239},
		{"windows", "arm64", 239},
		{"windows", "386", 0},
		{"wasi", "wasm32", 0},
	}
	for _, tc := range cases {
		withTarget(t, tc.goos, tc.goarch)
		if got := rawVFSCapabilitiesForTarget(); got != tc.want {
			t.Errorf("%s/%s capabilities = %d, want %d", tc.goos, tc.goarch, got, tc.want)
		}
	}
}

func TestRawPOSIXSupportUsesTargetConstantsAndLayouts(t *testing.T) {
	cases := []struct {
		goos, goarch        string
		create, excl, trunc int64
		directory           int64
		flockSize           int64
		typeOff, whenceOff  int64
		startOff, lengthOff int64
		setlk, setlkw       int64
		rdlck, wrlck, unlck int64
	}{
		{"linux", "amd64", 64, 128, 512, 0x10000, 32, 0, 2, 8, 16, 6, 7, 0, 1, 2},
		{"linux", "arm64", 64, 128, 512, 0x10000, 32, 0, 2, 8, 16, 6, 7, 0, 1, 2},
		{"darwin", "amd64", 512, 2048, 1024, 0x100000, 24, 20, 22, 0, 8, 8, 9, 1, 3, 2},
		{"darwin", "arm64", 512, 2048, 1024, 0x100000, 24, 20, 22, 0, 8, 8, 9, 1, 3, 2},
	}
	for _, tc := range cases {
		withTarget(t, tc.goos, tc.goarch)
		ir := rawPOSIXSupportIR()
		errnoFn := "__errno_location"
		if tc.goos == "darwin" {
			errnoFn = "__error"
		}
		if !strings.Contains(ir, "call i32* @"+errnoFn+"()") {
			t.Errorf("%s/%s raw POSIX IR must call @%s()", tc.goos, tc.goarch, errnoFn)
		}
		if strings.Contains(ir, "@"+errnoFn+"@()") {
			t.Errorf("%s/%s raw POSIX IR contains a malformed errno symbol", tc.goos, tc.goarch)
		}
		for _, want := range []string{
			"%createflag = select i1 %hascreate, i64 " + itoa(int(tc.create)) + ", i64 0",
			"%exclflag = select i1 %hasexcl, i64 " + itoa(int(tc.excl)) + ", i64 0",
			"%truncflag = select i1 %hastrunc, i64 " + itoa(int(tc.trunc)) + ", i64 0",
			"%flags = or i32 " + itoa(int(tc.directory)) + ", 0",
			"alloca [" + itoa(int(tc.flockSize)) + " x i8], align 8",
			"i64 " + itoa(int(tc.typeOff)),
			"i64 " + itoa(int(tc.whenceOff)),
			"i64 " + itoa(int(tc.startOff)),
			"i64 " + itoa(int(tc.lengthOff)),
			"call i64 @pread(i32 %fd, i8* %buf, i64 %count, i64 %offset)",
			"call i64 @pwrite(i32 %fd, i8* %data, i64 %count, i64 %offset)",
			"%cmd = select i1 %nonblocking, i32 " + itoa(int(tc.setlk)) + ", i32 " + itoa(int(tc.setlkw)),
			"call i32 (i32, i32, ...) @fcntl(i32 %fd, i32 " + itoa(int(tc.setlk)) + ", i8* %p)",
			// Lock types are OS-specific. Darwin F_WRLCK=3 is invalid on
			// Linux (F_WRLCK=1) — reusing it silently broke every exclusive
			// range lock in the Linux CI (fs.raw-range-locks / xproc-locks).
			"%type = select i1 %exclusive, i32 " + itoa(int(tc.wrlck)) + ", i32 " + itoa(int(tc.rdlck)),
			"i32 " + itoa(int(tc.unlck)) + ")\n", // raw_unlock_range -> F_UNLCK
		} {
			if !strings.Contains(ir, want) {
				t.Errorf("%s/%s raw POSIX IR missing %q", tc.goos, tc.goarch, want)
			}
		}
	}
}

func TestRawWindowsTruncateKeepsSuccessfulOpenHandle(t *testing.T) {
	withTarget(t, "windows", "amd64")
	ir := rawWindowsShimsIR
	setend := rawVFSBlock(t, ir, "setend")
	if !strings.Contains(setend, "br i1 %seok, label %done, label %truncerr") {
		t.Fatalf("successful SetEndOfFile must branch to done, got:\n%s", setend)
	}
	done := rawVFSBlock(t, ir, "done")
	if strings.Contains(done, "CloseHandle") {
		t.Fatalf("successful truncation must not close the returned handle, got:\n%s", done)
	}
	if !strings.Contains(done, "ret i64 %h") {
		t.Fatalf("successful truncation must return the live handle, got:\n%s", done)
	}
}

func TestRawWindowsPreludeUsesValidBinaryOperandSyntax(t *testing.T) {
	for _, goarch := range []string{"amd64", "arm64"} {
		withTarget(t, "windows", goarch)
		ir := rawWindowsShimsIR
		if strings.Contains(ir, "or i64 %ar, i64 %aw") {
			t.Fatalf("%s raw_open repeats the type of the second binary operand", goarch)
		}
		for _, want := range []string{
			"%desired = or i64 %ar, %aw",
			"%desired32 = trunc i64 %desired to i32",
			"i32 %desired32, i32 7",
		} {
			if !strings.Contains(ir, want) {
				t.Errorf("%s Windows raw_open IR missing %q", goarch, want)
			}
		}
	}
}

func TestRawWindowsPreludeHasUniqueSSAValues(t *testing.T) {
	for _, goarch := range []string{"amd64", "arm64"} {
		withTarget(t, "windows", goarch)
		ir := rawWindowsShimsIR
		functions := regexp.MustCompile(`(?ms)^define[^\n]*?@[^\s(]+\([^\n]*?\) \{\n(.*?)^\}`).FindAllStringSubmatch(ir, -1)
		if len(functions) == 0 {
			t.Fatalf("%s Windows raw VFS prelude has no function bodies", goarch)
		}
		definitions := regexp.MustCompile(`(?m)^\s*(%[-A-Za-z$._][-A-Za-z$._0-9]*)\s*=`)
		for _, function := range functions {
			seen := make(map[string]bool)
			for _, definition := range definitions.FindAllStringSubmatch(function[1], -1) {
				name := definition[1]
				if seen[name] {
					t.Fatalf("%s Windows raw VFS prelude defines %s more than once in one function", goarch, name)
				}
				seen[name] = true
			}
		}
	}
}

func TestRawWindowsRangeLocksPreserveZeroLengthAndFlags(t *testing.T) {
	withTarget(t, "windows", "arm64")
	ir := rawWindowsShimsIR
	for _, label := range []string{"lock", "unlock"} {
		block := rawVFSBlock(t, ir, label)
		if !strings.Contains(block, "%lo = trunc i64 %length to i32") ||
			!strings.Contains(block, "%hi = trunc i64 %shifted to i32") {
			t.Errorf("%s must split the original length into low/high words", label)
		}
		if strings.Contains(block, "4294967295") {
			t.Errorf("%s must keep zero length as zero (through EOF)", label)
		}
	}
	lock := rawVFSBlock(t, ir, "lock")
	for _, want := range []string{
		"%f0 = select i1 %exclusive, i32 2, i32 0",
		"%nbflag = select i1 %nonblocking, i32 1, i32 0",
		"%f2 = or i32 %f0, %nbflag",
		"call i32 @LockFileEx(i64 %handle, i32 %f2, i32 0, i32 %lo, i32 %hi, i8* %p)",
	} {
		if !strings.Contains(lock, want) {
			t.Errorf("Windows lock block missing %q", want)
		}
	}
}

func TestRawVFSBuiltinLoweringAndDiscardedSideEffects(t *testing.T) {
	ir := rawVFSIRForTest(t, "linux", "amd64", `
fd fs.fd = 3
opened ?fs.fd = raw-open('page.db', 7, 420)
raw-close(fd)
pos ?i64 = raw-seek(fd, 4, 0)
buf []byte = [0, 0, 0, 0]
nr ?i64 = raw-pread(fd, buf, 4, 0)
nw ?i64 = raw-pwrite(fd, buf, 4, 0)
trunc ?bool = raw-ftruncate(fd, 8)
size ?i64 = raw-fstat-size(fd)
synced ?bool = raw-fsync(fd)
data-synced ?bool = raw-fdatasync(fd)
dir-synced ?bool = raw-sync-dir('.')
locked ?bool = raw-lock-range(fd, 0, 4, 2, true)
unlocked ?bool = raw-unlock-range(fd, 0, 4)
caps = raw-capabilities()
print(caps)
`)
	for _, want := range []string{
		"call i64 @nolang.raw_open(i8*",
		"call i32 @nolang.raw_close(i64",
		"call i64 @nolang.raw_seek(i64",
		"call i64 @nolang.raw_pread(i64",
		"call i64 @nolang.raw_pwrite(i64",
		"call i32 @nolang.raw_ftruncate(i64",
		"call i64 @nolang.raw_fstat_size(i64",
		"call i32 @nolang.raw_fsync(i64",
		"call i32 @nolang.raw_fdatasync(i64",
		"call i32 @nolang.raw_sync_dir(i8*",
		"call i32 @nolang.raw_lock_range(i64",
		"call i32 @nolang.raw_unlock_range(i64",
		"zext i1",
	} {
		if !strings.Contains(ir, want) {
			t.Errorf("raw VFS IR missing %q", want)
		}
	}
	if !strings.Contains(ir, "i64 255") {
		t.Errorf("raw-capabilities must lower to the target mask")
	}
}

func TestRawVFSSupportIsLazyAndUnsupportedTargetsReportZero(t *testing.T) {
	ir := rawVFSIRForTest(t, "linux", "amd64", "print(1)")
	if strings.Contains(ir, "@nolang.raw_open") {
		t.Fatal("modules that do not use raw VFS must not emit its helpers")
	}
	ir = rawVFSIRForTest(t, "wasi", "wasm32", `
fd ?fs.fd = raw-open('page.db', 1, 0)
caps = raw-capabilities()
print(caps)
`)
	if !strings.Contains(ir, "store i32 40, i32* %e") || !strings.Contains(ir, "ret i64 -1") {
		t.Errorf("unsupported raw VFS helpers must report ENOSYS and fail")
	}
	if !strings.Contains(ir, "i64 0") {
		t.Errorf("unsupported target capability mask must be zero")
	}
}
