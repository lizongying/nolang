package mmast

import (
	"archive/zip"
	"bytes"
	"encoding/gob"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pkgspec "github.com/lizongying/nolang/package"
	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
	"github.com/lizongying/nolang/parser/dump"
)

func parseStd(t *testing.T, rel string) *parser.Program {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("..", "parser", "..", "std", rel))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	p := parser.New(lexer.New(string(src)))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse: %v", errs)
	}
	return prog
}

// TestASTZipRoundTrip：幾個代表性模組（含最大的 tls.no）打包 → 解包 →
// 與原始 AST 的 dump 文本逐位元組比對。
func TestASTZipRoundTrip(t *testing.T) {
	for _, rel := range []string{"vec.no", "json.no", "net/tls.no", "crypto/x509.no", "str.no"} {
		prog := parseStd(t, rel)
		var buf bytes.Buffer
		st, err := WriteASTZip(&buf, prog, nil)
		if err != nil {
			t.Fatalf("%s: write zip: %v", rel, err)
		}
		back, info, err := ReadASTZip(buf.Bytes())
		if err != nil {
			t.Fatalf("%s: read zip: %v", rel, err)
		}
		opts := dump.Options{Annotations: true}
		want := dump.DumpWithOptions(prog, opts)
		got := dump.DumpWithOptions(back, opts)
		if want != got {
			t.Fatalf("%s: dump mismatch (%d vs %d bytes)", rel, len(want), len(got))
		}
		if st.Nodes == 0 || info == nil {
			t.Fatalf("%s: bad stats/info: %+v %+v", rel, st, info)
		}
		t.Logf("%-16s zip=%7d bytes  nodes=%6d", rel, buf.Len(), st.Nodes)
	}
}

// TestPackFlowRoundTrip：複現 `no pack` 的實際流程（lexer.NewCached +
// p.Filename，與 build/builder.go 的解析入口一致）→ 打包 → 解包 → dump 比對。
func TestPackFlowRoundTrip(t *testing.T) {
	rel := "vec.no"
	src, err := os.ReadFile(filepath.Join("..", "std", rel))
	if err != nil {
		t.Fatal(err)
	}
	pp := parser.New(lexer.NewCached(rel, string(src)))
	pp.Filename = filepath.Base(rel)
	prog := pp.ParseProgram()
	if errs := pp.Errors(); len(errs) > 0 {
		t.Fatalf("parse: %v", errs)
	}
	var buf bytes.Buffer
	st, err := WriteASTZip(&buf, prog, nil)
	if err != nil {
		t.Fatal(err)
	}
	if st.Nodes == 0 || st.Bytes != int64(buf.Len()) {
		t.Fatalf("bad stats: %+v (buffer %d)", st, buf.Len())
	}
	back, _, err := ReadASTZip(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	opts := dump.Options{Annotations: true}
	if want, got := dump.DumpWithOptions(prog, opts), dump.DumpWithOptions(back, opts); want != got {
		t.Fatal("dump mismatch after pack/unpack")
	}
	t.Logf("pack flow: nodes=%d bytes=%d", st.Nodes, st.Bytes)
}

// TestASTZipLayout：zip 必須恰好只含 ast.gob 一個條目（版本前置欄在流内，
// 沒有獨立清單；gob 流没有容器上限，整棵樹原樣塞進 ast.gob）。
func TestASTZipLayout(t *testing.T) {
	prog := parseStd(t, "net/tls.no")
	var buf bytes.Buffer
	if _, err := WriteASTZip(&buf, prog, nil); err != nil {
		t.Fatal(err)
	}
	zr, err := readZipEntries(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(zr) != 1 {
		t.Fatalf("expected ast.gob only (1 entry), got %d: %v", len(zr), keysOf(zr))
	}
	if _, ok := zr[treeName]; !ok {
		t.Fatalf("missing %s", treeName)
	}
	t.Logf("tls.no: %d bytes -> %v", buf.Len(), keysOf(zr))
}

func keysOf(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestASTZipTiny：極小模組走完整 pack→zip→read 路徑，驗證編解碼本身正確。
func TestASTZipTiny(t *testing.T) {
	src := "x = 1\ny = x + 2\nz = y * 3\n"
	p := parser.New(lexer.New(src))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse: %v", errs)
	}
	var buf bytes.Buffer
	st, err := WriteASTZip(&buf, prog, nil)
	if err != nil {
		t.Fatalf("write zip: %v", err)
	}
	back, _, err := ReadASTZip(buf.Bytes())
	if err != nil {
		t.Fatalf("read zip: %v", err)
	}
	opts := dump.Options{Annotations: true}
	if want, got := dump.DumpWithOptions(prog, opts), dump.DumpWithOptions(back, opts); want != got {
		t.Fatal("dump mismatch after tiny pack/unpack")
	}
	t.Logf("tiny: zip=%d bytes nodes=%d", buf.Len(), st.Nodes)
}

// TestASTZipCachePathVersioned：版本键控从流内改成路徑後，ASTZipCachePath 必须
// 同时含 ASTVersion 与 ZipVer——版本 bump 即路徑變，本機陈旧快取自动失效。
func TestASTZipCachePathVersioned(t *testing.T) {
	spec := pkgspec.ToolSpec{Host: "nolang-lang.org", Owner: "nolang-lang", Repo: "std", Name: "nolang-std"}
	p := ASTZipCachePath(spec, "0.1.0")
	base := filepath.Base(p)
	if !strings.Contains(base, parser.ASTVersion) || !strings.Contains(base, ZipVer) {
		t.Fatalf("cache name %q must embed ASTVersion+ZipVer", base)
	}
	// host:port 形式的本機 registry 路径在 Windows 上不能带 ":"
	spec2 := pkgspec.ToolSpec{Host: "127.0.0.1:8080", Owner: "me", Repo: "tool", Name: "tool"}
	if p2 := ASTZipCachePath(spec2, "v1"); strings.Contains(filepath.Dir(p2), ":") {
		t.Fatalf("sanitized dir must not contain ':': %s", p2)
	}
}

// TestASTZipForeignBlobRejected：旧代格式（如 astDoc 时代的 blob）不是
// parser.CachedProgram，gob 型别名不匹配会硬错——这就是版本不入流后的
// 兑底防线（静默错解只可能发生在同型别内的加字段漂移，而那由路徑键控拦截）。
func TestASTZipForeignBlobRejected(t *testing.T) {
	prog := parseStd(t, "vec.no")
	var buf bytes.Buffer
	if _, err := WriteASTZip(&buf, prog, nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadASTZip(buf.Bytes()); err != nil {
		t.Fatalf("valid zip rejected: %v", err)
	}
	if _, _, err := ReadASTZip([]byte("not a zip")); err == nil {
		t.Fatal("expected zip open error")
	}

	// 用另一个 gob 型别（模拟上一代 astDoc 流）重建 ast.gob
	type legacyAstDoc struct {
		ZipVer string
		AstVer string
		Prog   *parser.Program
		// Stage 7 起 `parser.NodeSemantics` 已不存在；用今天三張 node-keyed 副表
		// 之一占位，保住「上一代流 = Prog + 一張 occurrence 編號副表」的形狀。
		// 該字段在下面的字面量裡恆為 nil，出現與否都不影響本測試的判據 ——
		// 判據是 gob **型別名**不匹配（legacyAstDoc vs parser.CachedProgram）。
		Anns map[uint32][]*parser.AnnotationEntry
	}
	var tb bytes.Buffer
	if err := gob.NewEncoder(&tb).Encode(&legacyAstDoc{ZipVer: "old", AstVer: parser.ASTVersion, Prog: prog}); err != nil {
		t.Fatal(err)
	}
	var zb bytes.Buffer
	zw := zip.NewWriter(&zb)
	tw, err := zw.Create(treeName)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(tb.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadASTZip(zb.Bytes()); err == nil {
		t.Fatal("expected hard error on foreign blob type")
	} else {
		t.Logf("legacy astDoc blob rejected: %v", err)
	}
}

// TestASTZipWithPackageNoml：附 package.noml 打包時 zip 应多一個條目，
// 且 ReadASTZip 回傳的位元組與寫入完全一致（原樣分發，不做任何改寫）。
func TestASTZipWithPackageNoml(t *testing.T) {
	prog := parseStd(t, "vec.no")
	pj := []byte("{\n  name: 'vec'\n  version: 'v1.2.3'\n  dependencies: ['nogit:v1.0.0@github.com/lizongying/notools',]\n}\n")
	var buf bytes.Buffer
	if _, err := WriteASTZip(&buf, prog, pj); err != nil {
		t.Fatal(err)
	}
	entries, err := readZipEntries(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected ast.gob + package.noml, got %v", keysOf(entries))
	}
	back, info, err := ReadASTZip(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if back == nil {
		t.Fatal("nil program after read")
	}
	if !bytes.Equal(info.PackageNoml, pj) {
		t.Fatalf("package.noml round-trip mismatch: %q", info.PackageNoml)
	}
	// 不附清單的入口維持單條目，且 PackageNoml 為 nil。
	var plain bytes.Buffer
	if _, err := WriteASTZip(&plain, prog, nil); err != nil {
		t.Fatal(err)
	}
	_, info, err = ReadASTZip(plain.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if info.PackageNoml != nil {
		t.Fatalf("plain zip should carry no package.noml, got %q", info.PackageNoml)
	}
	t.Logf("with package.noml: zip=%d bytes, round-trip OK", buf.Len())
}
