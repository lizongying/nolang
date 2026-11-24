package build

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/mmast"
	pkg "github.com/lizongying/nolang/package"
	"github.com/lizongying/nolang/parser"
)

// TestPreloadSignaturesForBarePackageAlias 釘住 `# mymod` 這種裸包別名在
// checker（模組合併之前）就能看到包內函式簽名。
//
// 曾經的缺陷：resolveUse 對裸包名只回傳一個沒有 Statements 的佔位 Program，
// preloadModuleSignatures 因此什麼都沒註冊，checker 直接報
// 'remote_greet' is not defined / 限定名 unknown callee——錯誤直到 MIR 才曝露。
//
// 深層模組路徑（deep/util）按最後一段（util）註冊，與直接 `# mymod/deep/util`
// 的模組短名一致：HIR 的點號呼叫只解析 <module>.<symbol> 兩段。
func TestPreloadSignaturesForBarePackageAlias(t *testing.T) {
	t.Setenv("NOLANG_AST_CACHE_DIR", t.TempDir())

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
	mods := map[string]*parser.Program{
		"remote":    parse("remote.no", astDepRemoteSrc),
		"deep/util": parse("util.no", "deep_greet = (name str) (out str) {\n  out = 'deep ' + name\n}\n"),
	}
	var zbuf bytes.Buffer
	if _, err := mmast.WriteModuleASTZip(&zbuf, mods, nil); err != nil {
		t.Fatalf("WriteModuleASTZip: %v", err)
	}
	zipBytes := zbuf.Bytes()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/mymod-ast.zip") {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(zipBytes)
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")

	tr := NewTranspiler(&pkg.Package{
		Name:         "app",
		Dependencies: map[string]string{"mymod@" + host + "/o/r": "v1.0.0"},
	})
	src := "# mymod\n" +
		"print(mymod.remote.remote_greet('bob'))\n" +
		"print(mymod.util.deep_greet('carol'))\n"
	funcSigs, _, _ := tr.preloadModuleSignatures(src)

	for _, key := range []string{
		"mymod.remote.remote_greet", // 包別名 + 模組短名 + 函式
		"mymod.util.deep_greet",     // 深層路徑取最後一段
		"remote.remote_greet",       // 與 # mymod/remote 的呼叫習慣一致
		"util.deep_greet",
	} {
		if _, ok := funcSigs[key]; !ok {
			t.Errorf("preload signature missing %q; got keys: %v", key, sigKeys(funcSigs))
		}
	}
}

func sigKeys(m map[string][]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
