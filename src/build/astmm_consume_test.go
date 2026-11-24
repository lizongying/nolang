package build

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/mmast"
	pkg "github.com/lizongying/nolang/package"
	"github.com/lizongying/nolang/parser"
	"github.com/lizongying/nolang/parser/dump"
)

// astDepModuleSrc 是一個最小模組：一個具名函式，足以證明 AST 真的被解回來。
// 用 `name = (args) (rets) { }` 的賦值形式是因為 std/tests 都這麼寫。
const astDepModuleSrc = "greet = (name str) (out str) {\n  out = 'hi ' + name\n}\n"

// TestResolveUseASTDependency 端到端驗證消費側閉環：
//
//	生產側  no pack  →  mymod-ast.zip
//	消費側  # mymod.greet → package.noml 的 `mymod@host/owner/repo` 依賴鍵
//	                     →  ReleaseASTZipURL → zip → gob → *parser.Program
//
// `# mymod.greet` 中 mymod 只是包名；按函式名 greet 在包內定位模組。
// 裸 `# mymod` 是包別名，不報錯：正文可用 `mymod.remote(...)` 取用包內模組。
func TestResolveUseASTDependency(t *testing.T) {
	// 快取落在臨時目錄，避免污染 ~/no/cache/ast，也避免跨 run 讀到陳舊 zip。
	t.Setenv("NOLANG_AST_CACHE_DIR", t.TempDir())

	// ── 1. 生產側：解析 + 打包 ──
	pp := parser.New(lexer.NewCached("mymod.no", astDepModuleSrc))
	pp.Filename = "mymod.no"
	want := pp.ParseProgram()
	if want == nil {
		t.Fatalf("parse: nil program: %v", pp.Errors())
	}
	if errs := pp.Errors(); len(errs) > 0 {
		t.Fatalf("parse: %v", errs[0])
	}
	var zipBuf bytes.Buffer
	if _, err := mmast.WriteASTZip(&zipBuf, want, nil); err != nil {
		t.Fatalf("WriteASTZip: %v", err)
	}
	zipBytes := zipBuf.Bytes()

	// ── 2. 假裝是 GitHub Releases ──
	var gotPaths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPaths = append(gotPaths, r.URL.Path)
		if !strings.HasSuffix(r.URL.Path, "/mymod-ast.zip") {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(zipBytes)
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://") // 形如 127.0.0.1:PORT

	// ── 3. 消費側：# mymod.greet ──
	p := &pkg.Package{
		Name:         "app",
		Dependencies: map[string]string{"mymod@" + host + "/o/r": "v1.0.0"},
	}
	tr := NewTranspiler(p)
	got, err := tr.resolveUse(&parser.UseStatement{Path: "mymod", Function: "greet"})
	if err != nil {
		t.Fatalf("resolveUse: %v", err)
	}
	if got == nil {
		t.Fatal("resolveUse returned nil program")
	}

	// URL 必須是 GitHub Releases 的形狀；本地 httptest 讓 host 帶埠號，
	// 所以 ReleaseASTZipURL 走 http 分支。
	wantPath := "/o/r/releases/download/v1.0.0/mymod-ast.zip"
	if len(gotPaths) != 1 || gotPaths[0] != wantPath {
		t.Fatalf("request path = %v, want exactly [%s]", gotPaths, wantPath)
	}

	// ── 4. AST 必須與原始解析逐位元組相同（dump 是 L2 oracle）──
	a := dump.DumpWithOptions(want, dump.Options{Annotations: true})
	b := dump.DumpWithOptions(got, dump.Options{Annotations: true})
	if a != b {
		t.Fatalf("AST mismatch after zip round-trip:\n--- want ---\n%s\n--- got ---\n%s", firstDiff(a, b), "")
	}
	if !strings.Contains(a, "greet") {
		t.Fatalf("decoded AST lost the greet function:\n%s", a[:min(len(a), 400)])
	}

	// ── 5. 第二種寫法：# <host/owner/repo>/mymod.greet ──
	got2, err := tr.resolveUse(&parser.UseStatement{Path: host + "/o/r/mymod", Function: "greet"})
	if err != nil {
		t.Fatalf("resolveUse (repo-qualified): %v", err)
	}
	if got2 == nil {
		t.Fatal("resolveUse (repo-qualified) returned nil program")
	}
	if len(gotPaths) != 1 {
		t.Fatalf("second resolveUse should hit the in-process registry, but fetched again: %v", gotPaths)
	}

	// ── 6. 裸包名是包別名，不報錯也不導入 main ──
	placeholder, err := tr.resolveUse(&parser.UseStatement{Path: "mymod"})
	if err != nil {
		t.Fatalf("bare # mymod should resolve to a package alias, got: %v", err)
	}
	if placeholder == nil || placeholder.ModulePath != astPackageModulePath || placeholder.SourcePath != "mymod" {
		t.Fatalf("bare # mymod placeholder = %+v, want ast-package alias mymod", placeholder)
	}
}

// astDepRemoteSrc 是子模組 "remote"：函式名與 main 不同，便於驗證取到的是正確模組。
const astDepRemoteSrc = "remote_greet = (name str) (out str) {\n  out = 'remote ' + name\n}\n"

// TestResolveUseASTDependencySubmodules 一個 zip = 一個包，包內可含多模組：
//   - `# mymod/remote`        → 直接取子模組 remote
//   - `# mymod/remote.fn`     → 同上，fn 由合併階段過濾
//   - `# mymod.remote_greet`  → 包名 + 函式名，在包內定位到 remote
//   - repo 前綴形式同理
func TestResolveUseASTDependencySubmodules(t *testing.T) {
	t.Setenv("NOLANG_AST_CACHE_DIR", t.TempDir())

	// ── 1. 生產側：main + remote 子模組 ──
	parse := func(name, src string) *parser.Program {
		t.Helper()
		pp := parser.New(lexer.NewCached(name, src))
		pp.Filename = name
		prog := pp.ParseProgram()
		if prog == nil || len(pp.Errors()) > 0 {
			t.Fatalf("parse %s: nil=%v errs=%v", name, prog == nil, pp.Errors())
		}
		return prog
	}
	main := parse("main.no", astDepModuleSrc)
	remote := parse("remote.no", astDepRemoteSrc)
	mods := map[string]*parser.Program{"main": main, "remote": remote}

	var zipBuf bytes.Buffer
	if _, err := mmast.WriteModuleASTZip(&zipBuf, mods, nil); err != nil {
		t.Fatalf("WriteModuleASTZip: %v", err)
	}
	zipBytes := zipBuf.Bytes()

	// ── 2. 假 GitHub Releases ──
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/mymod-ast.zip") {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(zipBytes)
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")

	// ── 3. 消費側：# mymod/remote ──
	p := &pkg.Package{
		Name:         "app",
		Dependencies: map[string]string{"mymod@" + host + "/o/r": "v1.0.0"},
	}
	tr := NewTranspiler(p)
	got, err := tr.resolveUse(&parser.UseStatement{Path: "mymod/remote"})
	if err != nil {
		t.Fatalf("resolveUse (submodule): %v", err)
	}
	if got == nil {
		t.Fatal("resolveUse (submodule) returned nil")
	}
	opts := dump.Options{Annotations: true}
	if w, g := dump.DumpWithOptions(remote, opts), dump.DumpWithOptions(got, opts); w != g {
		t.Fatalf("submodule AST mismatch:\n--- want ---\n%s\n--- got ---\n%s", w, g)
	}
	if w, g := dump.DumpWithOptions(main, opts), dump.DumpWithOptions(got, opts); w == g {
		t.Fatal("submodule resolve returned the main module")
	}

	// ── 4. repo-qualified 子模組 ──
	got2, err := tr.resolveUse(&parser.UseStatement{Path: host + "/o/r/mymod/remote"})
	if err != nil {
		t.Fatalf("resolveUse (repo-qualified submodule): %v", err)
	}
	if w, g := dump.DumpWithOptions(remote, opts), dump.DumpWithOptions(got2, opts); w != g {
		t.Fatal("repo-qualified submodule AST mismatch")
	}

	// ── 5. 包名 + 函式名 → 在包內定位到 remote ──
	got3, err := tr.resolveUse(&parser.UseStatement{Path: "mymod", Function: "remote_greet"})
	if err != nil {
		t.Fatalf("resolveUse (function lookup): %v", err)
	}
	if w, g := dump.DumpWithOptions(remote, opts), dump.DumpWithOptions(got3, opts); w != g {
		t.Fatal("function lookup resolved wrong module")
	}
	got4, err := tr.resolveUse(&parser.UseStatement{Path: "mymod", Function: "greet"})
	if err != nil {
		t.Fatalf("resolveUse (function lookup main): %v", err)
	}
	if w, g := dump.DumpWithOptions(main, opts), dump.DumpWithOptions(got4, opts); w != g {
		t.Fatal("function lookup resolved main module wrongly")
	}

	// ── 6. 不存在的子模組 / 函式：明確報錯 ──
	if _, err := tr.resolveUse(&parser.UseStatement{Path: "mymod/missing"}); err == nil {
		t.Fatal("expected error for missing submodule")
	} else if !strings.Contains(err.Error(), "no module") {
		t.Fatalf("error = %q, want it to mention \"no module\"", err)
	}
	if _, err := tr.resolveUse(&parser.UseStatement{Path: "mymod", Function: "nope"}); err == nil {
		t.Fatal("expected error for unknown function")
	}
}

// TestASTDependencySkippedByEnsureDependencies 預編 AST 依賴沒有原始碼套件可
// 下載，不能進依賴圖——否則 `no sync` / EnsureDependencies 會試著去抓不存在的
// 原始碼而失敗。
func TestASTDependencySkippedByEnsureDependencies(t *testing.T) {
	p := &pkg.Package{
		Name:         "app",
		Dependencies: map[string]string{"mymod@github.com/o/r": "v1.0.0"},
	}
	g, err := p.EnsureDependencies(10)
	if err != nil {
		t.Fatalf("EnsureDependencies: %v", err)
	}
	if n := len(g.Resolved()); n != 0 {
		t.Fatalf("AST dependency entered the dependency graph: %d resolved, want 0", n)
	}
}

// firstDiff 回傳 a/b 第一行不同之處的前後文（避免整棵 AST 刷屏）。
func firstDiff(a, b string) string {
	la, lb := strings.Split(a, "\n"), strings.Split(b, "\n")
	for i := 0; i < len(la) || i < len(lb); i++ {
		var x, y string
		if i < len(la) {
			x = la[i]
		}
		if i < len(lb) {
			y = lb[i]
		}
		if x != y {
			return "line " + itoa(i+1) + ":\n  want: " + x + "\n  got:  " + y
		}
	}
	return "<identical>"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
