package llvm

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestPlatformVariantResolution 驗證 FilterByPlatform 依目標平台過濾宣告，
// 以及 PlatformKeyFor 的反向查找行為。
func TestPlatformVariantResolution(t *testing.T) {
	// 測試 FilterByPlatform 依目標平台過濾
	t.Run("filter_by_target_platform", func(t *testing.T) {
		src := `#{mac-arm64}
O-EXCL = 2048
#{win-arm64}
O-EXCL = 1024
`
		l := lexer.New(src)
		p := parser.New(l)
		prog := p.ParseProgram()
		if len(p.Errors()) > 0 {
			t.Fatalf("parse errors: %v", p.Errors())
		}

		// 過濾到 windows/arm64 — 應保留 win-arm64 變體
		filtered := FilterByPlatform(prog.Sem, prog.Statements, "windows", "arm64")
		if len(filtered) != 1 {
			t.Fatalf("expected 1 statement for windows/arm64, got %d", len(filtered))
		}
		letStmt, ok := filtered[0].(*parser.LetStatement)
		if !ok {
			t.Fatalf("expected *LetStatement, got %T", filtered[0])
		}
		// 驗證保留的是 win-arm64 變體（值為 1024）
		if intLit, ok := letStmt.Value.(*parser.IntegerLiteral); !ok || intLit.Value != 1024 {
			t.Errorf("expected win-arm64 variant with value 1024, got %v", letStmt.Value)
		}

		// 過濾到 darwin/arm64 — 應保留 mac-arm64 變體
		filtered = FilterByPlatform(prog.Sem, prog.Statements, "darwin", "arm64")
		if len(filtered) != 1 {
			t.Fatalf("expected 1 statement for darwin/arm64, got %d", len(filtered))
		}
		letStmt, ok = filtered[0].(*parser.LetStatement)
		if !ok {
			t.Fatalf("expected *LetStatement, got %T", filtered[0])
		}
		if intLit, ok := letStmt.Value.(*parser.IntegerLiteral); !ok || intLit.Value != 2048 {
			t.Errorf("expected mac-arm64 variant with value 2048, got %v", letStmt.Value)
		}
	})

	// 測試 PlatformKeyFor 反向查找
	t.Run("platform_key_for", func(t *testing.T) {
		cases := []struct {
			goos, goarch, expected string
		}{
			{"darwin", "arm64", "mac-arm64"},
			{"darwin", "amd64", "mac-amd64"},
			{"linux", "arm64", "linux-arm64"},
			{"linux", "amd64", "linux-amd64"},
			{"windows", "arm64", "win-arm64"},
			{"windows", "amd64", "win-amd64"},
			{"freebsd", "arm64", ""}, // 不支援的平台
		}
		for _, c := range cases {
			got := PlatformKeyFor(c.goos, c.goarch)
			if got != c.expected {
				t.Errorf("PlatformKeyFor(%q, %q) = %q, want %q", c.goos, c.goarch, got, c.expected)
			}
		}
	})

	// 測試無平台註解的宣告在所有平台下都保留
	t.Run("generic_declaration_kept", func(t *testing.T) {
		src := `O-RDONLY = 0
`
		l := lexer.New(src)
		p := parser.New(l)
		prog := p.ParseProgram()
		if len(p.Errors()) > 0 {
			t.Fatalf("parse errors: %v", p.Errors())
		}

		// 在所有平台下都應保留
		for _, tc := range []struct{ goos, goarch string }{
			{"darwin", "arm64"},
			{"linux", "amd64"},
			{"windows", "arm64"},
		} {
			filtered := FilterByPlatform(prog.Sem, prog.Statements, tc.goos, tc.goarch)
			if len(filtered) != 1 {
				t.Errorf("expected 1 statement for %s/%s, got %d", tc.goos, tc.goarch, len(filtered))
			}
		}
	})
}

// TestTargetDatalayoutAndTriple 驗證 targetDatalayoutAndTriple 對 6 個原生平台
// + wasi/wasm32 回傳正確的 datalayout 與 triple，以及空字串回退到歷史預設。
func TestTargetDatalayoutAndTriple(t *testing.T) {
	cases := []struct {
		name           string
		goos, goarch   string
		wantLayout     string
		wantTriple     string
	}{
		{
			name:       "wasi/wasm32",
			goos:       "wasi",
			goarch:     "wasm32",
			wantLayout: "e-m:e-p:32:32-i64:64-n32:64-S128",
			wantTriple: "wasm32-wasi",
		},
		{
			name:       "darwin/arm64",
			goos:       "darwin",
			goarch:     "arm64",
			wantLayout: "e-m:o-i64:64-i128:128-n32:64-S128",
			wantTriple: "arm64-apple-macosx15.0.0",
		},
		{
			name:       "darwin/amd64",
			goos:       "darwin",
			goarch:     "amd64",
			wantLayout: "e-m:o-p270:32:32-p271:32:32-p272:64:64-i64:64-i128:128-f80:128-n8:16:32:64-S128",
			wantTriple: "x86_64-apple-macosx15.0.0",
		},
		{
			name:       "linux/amd64",
			goos:       "linux",
			goarch:     "amd64",
			wantLayout: "e-m:e-p270:32:32-p271:32:32-p272:64:64-i64:64-i128:128-f80:128-n8:16:32:64-S128",
			wantTriple: "x86_64-unknown-linux-gnu",
		},
		{
			name:       "linux/arm64",
			goos:       "linux",
			goarch:     "arm64",
			wantLayout: "e-m:e-i8:8:32-i16:16:32-i64:64-i128:128-n32:64-S128-Fn32",
			wantTriple: "aarch64-unknown-linux-gnu",
		},
		{
			name:       "windows/amd64",
			goos:       "windows",
			goarch:     "amd64",
			wantLayout: "e-m:w-p270:32:32-p271:32:32-p272:64:64-i64:64-i128:128-f80:128-n8:16:32:64-S128",
			wantTriple: "x86_64-pc-windows-gnu",
		},
		{
			name:       "windows/arm64",
			goos:       "windows",
			goarch:     "arm64",
			wantLayout: "e-m:e-p270:32:32-p271:32:32-p272:64:64-i64:64-i128:128-f80:128-n8:16:32:64-S128",
			wantTriple: "aarch64-pc-windows-gnu",
		},
		{
			name:       "empty/empty (historical default)",
			goos:       "",
			goarch:     "",
			wantLayout: "e-m:o-i64:64-i128:128-n32:64-S128",
			wantTriple: "arm64-apple-macosx15.0.0",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotLayout, gotTriple := targetDatalayoutAndTriple(c.goos, c.goarch)
			if gotLayout != c.wantLayout {
				t.Errorf("targetDatalayoutAndTriple(%q, %q) layout = %q, want %q",
					c.goos, c.goarch, gotLayout, c.wantLayout)
			}
			if gotTriple != c.wantTriple {
				t.Errorf("targetDatalayoutAndTriple(%q, %q) triple = %q, want %q",
					c.goos, c.goarch, gotTriple, c.wantTriple)
			}
		})
	}
}

// TestSetTargetPlatformPopulatesFields 驗證 SetTargetPlatform 會透過
// targetDatalayoutAndTriple 填充 Generator 的 targetDatalayout / targetTriple 欄位。
func TestSetTargetPlatformPopulatesFields(t *testing.T) {
	g := NewGenerator()

	// 呼叫前欄位應為零值
	if g.targetDatalayout != "" {
		t.Errorf("expected empty targetDatalayout before SetTargetPlatform, got %q", g.targetDatalayout)
	}
	if g.targetTriple != "" {
		t.Errorf("expected empty targetTriple before SetTargetPlatform, got %q", g.targetTriple)
	}

	g.SetTargetPlatform("wasi", "wasm32")

	if g.targetGoos != "wasi" {
		t.Errorf("expected targetGoos=%q, got %q", "wasi", g.targetGoos)
	}
	if g.targetGoarch != "wasm32" {
		t.Errorf("expected targetGoarch=%q, got %q", "wasm32", g.targetGoarch)
	}
	if g.targetDatalayout != "e-m:e-p:32:32-i64:64-n32:64-S128" {
		t.Errorf("expected wasi datalayout, got %q", g.targetDatalayout)
	}
	if g.targetTriple != "wasm32-wasi" {
		t.Errorf("expected wasi triple, got %q", g.targetTriple)
	}
}

// TestGenerateUsesTargetTriple 驗證 Generator.Generate 發射的 LLVM IR 包含
// 正確的 target datalayout / target triple。包含 wasi/wasm32 與預設（未呼叫
// SetTargetPlatform）兩種情境。
func TestGenerateUsesTargetTriple(t *testing.T) {
	// 最小 Nolang 程式：頂層整數賦值（與既有測試一致的寫法）
	src := `x = 1
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}

	t.Run("wasi_wasm32", func(t *testing.T) {
		g := NewGenerator()
		g.SetTargetPlatform("wasi", "wasm32")
		ir := g.Generate(prog)

		wantLayout := `target datalayout = "e-m:e-p:32:32-i64:64-n32:64-S128"`
		wantTriple := `target triple = "wasm32-wasi"`
		if !strings.Contains(ir, wantLayout) {
			t.Errorf("IR should contain %q, got:\n%s", wantLayout, ir)
		}
		if !strings.Contains(ir, wantTriple) {
			t.Errorf("IR should contain %q, got:\n%s", wantTriple, ir)
		}
	})

	t.Run("default_no_set_target", func(t *testing.T) {
		// 未呼叫 SetTargetPlatform — 應回退到歷史預設 arm64-apple-macosx15.0.0
		g := NewGenerator()
		ir := g.Generate(prog)

		wantLayout := `target datalayout = "e-m:o-i64:64-i128:128-n32:64-S128"`
		wantTriple := `target triple = "arm64-apple-macosx15.0.0"`
		if !strings.Contains(ir, wantLayout) {
			t.Errorf("IR should contain %q (backward compat), got:\n%s", wantLayout, ir)
		}
		if !strings.Contains(ir, wantTriple) {
			t.Errorf("IR should contain %q (backward compat), got:\n%s", wantTriple, ir)
		}
	})
}

// TestDeclarationsForWasiTarget 驗證 wasi/wasm32 目標下發射的 libc 宣告符合
// wasi-libc 支援的 POSIX 子集：包含 open/malloc/__errno_location 等，
// 不包含 fork/execlp/pipe/_getcwd/_mkdir/FindFirstFileA 等。
func TestDeclarationsForWasiTarget(t *testing.T) {
	src := `x = 1
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}

	g := NewGenerator()
	g.SetTargetPlatform("wasi", "wasm32")
	ir := g.Generate(prog)

	// WASI 應發射的 POSIX 子集宣告
	wantContains := []string{
		"declare i32 @open(i8*, i32, ...)\n", // WASI has open
		// WASI/wasm32 平台 size_t 為 i32，wasi-libc 的 malloc 簽名為 (i32)。
		// Nolang 透過 @nolang.malloc(i64) wrapper 隔離平台差異。
		"declare i8* @malloc(i32)\n",
		"declare void @free(i8*)\n",
		"declare i8* @getcwd(i8*, i64)\n", // WASI has getcwd
		"declare i32 @chdir(i8*)\n",       // WASI: declared external (linker fails if called)
		"declare i32 @mkdir(i8*, i32)\n",
		"declare i32 @unlink(i8*)\n",
		"declare i32 @stat(i8*, i8*)\n",
		"declare i8* @opendir(i8*)\n", // WASI wraps fd_readdir
		"declare i32* @__errno_location()\n", // WASI uses __errno_location (like Linux)
		// WASI/wasm32 平台 ssize_t=size_t=i32，read/write 簽名為 (i32, i8*, i32) -> i32。
		"declare i32 @read(i32, i8*, i32)\n",
		"declare i32 @write(i32, i8*, i32)\n",
		"declare i32 @close(i32)\n",
		// nolang.malloc wrapper 必須存在並做 i64 → i32 trunc
		"define internal i8* @nolang.malloc(i64 %sz) {\n",
		"\t%sz32 = trunc i64 %sz to i32\n",
		"\t%p = call i8* @malloc(i32 %sz32)\n",
		// nolang.read / nolang.write wrapper 必須存在並做 i64 → i32 截斷 + i32 → i64 符號擴展
		"define internal i64 @nolang.read(i32 %fd, i8* %buf, i64 %count) {\n",
		"define internal i64 @nolang.write(i32 %fd, i8* %buf, i64 %count) {\n",
	}
	for _, want := range wantContains {
		if !strings.Contains(ir, want) {
			t.Errorf("WASI IR should contain %q", want)
		}
	}

	// WASI 不支援的 API — 不應出現在 IR 中
	// 註：fork/execlp/pipe/waitpid/dup2/kill/getppid 仍宣告為 external
	// （讓 std 函式庫中的 call site 能通過 IR parser），但實際呼叫時
	// 連結階段會失敗。chdir 同理。
	wantNotContains := []string{
		"declare i32 @getuid()",            // WASI: single-user
		"declare i32 @getgid()",            // WASI: single-user
		"declare i32 @chown(",              // WASI: single-user
		"declare i32 @symlink(",            // WASI: limited, skipped
		"declare i32 @link(",               // WASI: limited, skipped
		"declare i32 @utimensat(",          // WASI: limited, skipped
		// Windows 專屬變體 — 不應出現
		"@_getcwd",            // Windows variant
		"@_mkdir",             // Windows variant
		"@_chmod",             // Windows variant
		"@_unlink",            // Windows variant
		"@_stat64",            // Windows variant
		"@_open",              // Windows variant
		"@_read",              // Windows variant
		"@_write",             // Windows variant
		"@_close",             // Windows variant
		"@_dup2",              // Windows variant
		"@_execlp",            // Windows variant
		"@_pipe",              // Windows variant
		"@_cwait",             // Windows variant
		"@_errno",             // Windows errno
		"@FindFirstFileA",     // Windows directory ops
		"@FindNextFileA",      // Windows directory ops
		"@FindClose",          // Windows directory ops
		"@__error",            // macOS errno
		"declare i32 @WSAStartup", // Windows winsock
		"declare i32 @WSACleanup", // Windows winsock
		"nolang.win_getuid",   // Windows stubs
		"nolang.win_getgid",   // Windows stubs
		"nolang.win_waitpid",  // Windows stubs
	}
	for _, notWant := range wantNotContains {
		if strings.Contains(ir, notWant) {
			t.Errorf("WASI IR should NOT contain %q", notWant)
		}
	}
}

// TestDeclarationsForDarwinTarget 驗證 darwin 目標下發射的 libc 宣告保持既有行為：
// 使用 __error()（macOS errno）、__stdinp/__stderrp（macOS stdin/stderr），
// 不包含 Windows 變體或 Linux 的 __errno_location。
func TestDeclarationsForDarwinTarget(t *testing.T) {
	src := `x = 1
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}

	g := NewGenerator()
	g.SetTargetPlatform("darwin", "arm64")
	ir := g.Generate(prog)

	wantContains := []string{
		"declare i32* @__error()\n", // macOS errno
		"@__stdinp = external global i8*\n", // macOS stdin
		"@__stderrp = external global i8*\n", // macOS stderr
		"declare i32 @fork()\n",           // macOS has fork
		"declare i32 @execlp(i8*, ...)\n", // macOS has execlp
		"declare i32 @pipe(i32*)\n",       // macOS has pipe
		"declare i8* @getcwd(i8*, i64)\n", // macOS POSIX getcwd
		"declare i32 @chdir(i8*)\n",       // macOS has chdir
		"declare i32 @getuid()\n",         // macOS has getuid
		"declare i32 @symlink(i8*, i8*)\n", // macOS has symlink
		"declare i32 @utimensat(i32, i8*, i8*, i32)\n", // macOS has utimensat
	}
	for _, want := range wantContains {
		if !strings.Contains(ir, want) {
			t.Errorf("darwin IR should contain %q", want)
		}
	}

	wantNotContains := []string{
		"declare i32* @_errno()\n",          // Windows errno
		"declare i32* @__errno_location()\n", // Linux errno
		"@_getcwd",                          // Windows variant
		"@_mkdir",                           // Windows variant
		"@_chmod",                           // Windows variant
		"@_unlink",                          // Windows variant
		"@_stat64",                          // Windows variant
		"@_open",                            // Windows variant
		"@_read",                            // Windows variant
		"@_write",                           // Windows variant
		"@_close",                           // Windows variant
		"@_fork",                            // Windows variant (does not exist)
		"@_execlp",                          // Windows variant
		"@_pipe",                            // Windows variant
		"@_cwait",                           // Windows variant
		"@FindFirstFileA",                   // Windows directory ops
		"@FindNextFileA",                    // Windows directory ops
		"@FindClose",                        // Windows directory ops
		"declare i32 @WSAStartup",           // Windows winsock
		"nolang.win_getuid",                 // Windows stubs
	}
	for _, notWant := range wantNotContains {
		if strings.Contains(ir, notWant) {
			t.Errorf("darwin IR should NOT contain %q", notWant)
		}
	}
}

// TestDeclarationsForWindowsTarget 驗證 windows 目標下發射的 libc 宣告使用
// Windows 變體：_getcwd/_mkdir/FindFirstFileA 等，不包含 POSIX 的 fork/opendir 等。
func TestDeclarationsForWindowsTarget(t *testing.T) {
	src := `x = 1
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}

	g := NewGenerator()
	g.SetTargetPlatform("windows", "amd64")
	ir := g.Generate(prog)

	wantContains := []string{
		"declare i8* @_getcwd(i8*, i64)\n", // Windows getcwd
		"declare i32 @_mkdir(i8*, i32)\n",  // Windows mkdir
		"declare i32 @_chmod(i8*, i32)\n",  // Windows chmod
		"declare i32 @_unlink(i8*)\n",      // Windows unlink
		"declare i32 @_stat64(i8*, i8*)\n", // Windows stat
		"declare i32 @_open(i8*, i32, ...)\n", // Windows open
		"declare i64 @_read(i32, i8*, i64)\n", // Windows read
		"declare i64 @_write(i32, i8*, i64)\n", // Windows write
		"declare i32 @_close(i32)\n",          // Windows close
		"declare i8* @FindFirstFileA(i8*, i8*)\n", // Windows directory ops
		"declare i32 @FindNextFileA(i8*, i8*)\n",
		"declare i32 @FindClose(i8*)\n",
		"declare i32* @_errno()\n", // Windows errno
		"declare i32 @WSAStartup(i16, i8*)\n", // Windows winsock
		"declare i32 @WSACleanup()\n",
	}
	for _, want := range wantContains {
		if !strings.Contains(ir, want) {
			t.Errorf("windows IR should contain %q", want)
		}
	}

	wantNotContains := []string{
		"declare i32 @fork()\n",          // POSIX only — not on Windows
		"declare i32 @execlp(i8*, ...)\n", // POSIX only (Windows has _execlp)
		"declare i32 @pipe(i32*)\n",       // POSIX only (Windows has _pipe)
		"declare i8* @opendir(i8*)\n",     // POSIX directory ops — not on Windows
		"declare i8* @readdir(i8*)\n",
		"declare i32 @closedir(i8*)\n",
		"declare i8* @getcwd(i8*, i64)\n", // POSIX getcwd (Windows has _getcwd)
		"declare i32 @mkdir(i8*, i32)\n",  // POSIX mkdir (Windows has _mkdir)
		"declare i32 @getuid()\n",         // Not on Windows
		"declare i32 @symlink(i8*, i8*)\n", // Not on Windows
		"declare i32 @utimensat(i32, i8*, i8*, i32)\n", // Routed to stub
		"declare i32* @__error()\n",       // macOS errno
		"declare i32* @__errno_location()\n", // Linux errno
	}
	for _, notWant := range wantNotContains {
		if strings.Contains(ir, notWant) {
			t.Errorf("windows IR should NOT contain %q", notWant)
		}
	}
}
