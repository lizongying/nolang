package build

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
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
//	生產側  no pack  →  mymod.ast.zip
//	消費側  use mymod →  package.jsonc 的 `mymod@host/owner/repo` 依賴鍵
//	                  →  ReleaseASTZipURL → zip → mm → *parser.Program
//
// 編碼/解碼的正確性由 mmast 與 parser 各自的測試覆蓋；這裡只保證三件事：
//  1. 帶 @ 的依賴鍵能被 MatchDependency 認出來（這是過去匹配不上的地方）；
//  2. 下載 URL 拼對（host/owner/repo/releases/download/ver/<tool>.ast.zip）；
//  3. resolveUse 直接返回 AST，而不是把它當原始碼套件送去下載。
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
	sum := sha256.Sum256([]byte(astDepModuleSrc))
	if err := mmast.WriteASTZip(&zipBuf, want, "mymod", "mymod.no", hex.EncodeToString(sum[:])); err != nil {
		t.Fatalf("WriteASTZip: %v", err)
	}
	zipBytes := zipBuf.Bytes()

	// ── 2. 假裝是 GitHub Releases ──
	var gotPaths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPaths = append(gotPaths, r.URL.Path)
		if !strings.HasSuffix(r.URL.Path, "/mymod.ast.zip") {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(zipBytes)
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://") // 形如 127.0.0.1:PORT

	// ── 3. 消費側：resolveUse ──
	p := &pkg.Package{
		Name:         "app",
		Dependencies: map[string]string{"mymod@" + host + "/o/r": "v1.0.0"},
	}
	tr := NewTranspiler(p)
	got, err := tr.resolveUse(&parser.UseStatement{Path: "mymod"})
	if err != nil {
		t.Fatalf("resolveUse: %v", err)
	}
	if got == nil {
		t.Fatal("resolveUse returned nil program")
	}

	// URL 必須是 GitHub Releases 的形狀；本地 httptest 讓 host 帶埠號，
	// 所以 ReleaseASTZipURL 走 http 分支。
	wantPath := "/o/r/releases/download/v1.0.0/mymod.ast.zip"
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

	// ── 5. 第二種寫法：use <host/owner/repo>/<tool> ──
	got2, err := tr.resolveUse(&parser.UseStatement{Path: host + "/o/r/mymod"})
	if err != nil {
		t.Fatalf("resolveUse (repo-qualified): %v", err)
	}
	if got2 == nil {
		t.Fatal("resolveUse (repo-qualified) returned nil program")
	}
	if len(gotPaths) != 1 {
		t.Fatalf("second resolveUse should hit the in-process registry, but fetched again: %v", gotPaths)
	}
}

// TestResolveUseASTDependencySubpathRejected 一個 zip 就是一個模組：
// 指向更深的模組路徑時要明確報錯，不能靜默退化成「檔案找不到」。
func TestResolveUseASTDependencySubpathRejected(t *testing.T) {
	t.Setenv("NOLANG_AST_CACHE_DIR", t.TempDir())

	p := &pkg.Package{
		Name:         "app",
		Dependencies: map[string]string{"mymod@127.0.0.1:1/o/r": "v1.0.0"},
	}
	tr := NewTranspiler(p)
	_, err := tr.resolveUse(&parser.UseStatement{Path: "127.0.0.1:1/o/r/deep/mod"})
	if err == nil {
		t.Fatal("expected an error for subpath import of a single-module AST dependency")
	}
	if !strings.Contains(err.Error(), "single prebuilt module") {
		t.Fatalf("error = %q, want it to mention \"single prebuilt module\"", err)
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
