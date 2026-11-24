package mmast

import (
	"archive/zip"
	"bytes"
	"encoding/gob"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	pkgspec "github.com/lizongying/nolang/package"
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
		set, err := ReadASTZip(buf.Bytes())
		if err != nil {
			t.Fatalf("%s: read zip: %v", rel, err)
		}
		back := set.Modules["main"]
		if back == nil {
			t.Fatalf("%s: no main module after read", rel)
		}
		opts := dump.Options{Annotations: true}
		want := dump.DumpWithOptions(prog, opts)
		got := dump.DumpWithOptions(back, opts)
		if want != got {
			t.Fatalf("%s: dump mismatch (%d vs %d bytes)", rel, len(want), len(got))
		}
		if st.Nodes == 0 || set == nil {
			t.Fatalf("%s: bad stats/set: %+v %+v", rel, st, set)
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
	set, err := ReadASTZip(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	opts := dump.Options{Annotations: true}
	if want, got := dump.DumpWithOptions(prog, opts), dump.DumpWithOptions(set.Modules["main"], opts); want != got {
		t.Fatal("dump mismatch after pack/unpack")
	}
	t.Logf("pack flow: nodes=%d bytes=%d", st.Nodes, st.Bytes)
}

// TestASTZipLayout：zip 恰好每個模組一個 <path>.gob 條目，
// 不再有 ast.gob / mods.gob，也沒有「入口」條目。
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
	if len(zr) != 1 || zr["main.gob"] == nil {
		t.Fatalf("expected only main.gob, got %d entries: %v", len(zr), keysOf(zr))
	}
	t.Logf("tls.no: %d bytes -> %v", buf.Len(), keysOf(zr))
}

// TestMultiModuleZipRoundTrip：一個包多模組打包 → 解包 → 逐模組 dump 逐位元組比對。
func TestMultiModuleZipRoundTrip(t *testing.T) {
	main := parseStd(t, "vec.no")
	remote := parseStd(t, "json.no")
	deep := parseStd(t, "str.no")
	mods := map[string]*parser.Program{
		"main":      main,
		"remote":    remote,
		"deep/util": deep,
	}
	var buf bytes.Buffer
	st, err := WriteModuleASTZip(&buf, mods, nil)
	if err != nil {
		t.Fatal(err)
	}
	if st.Modules != 3 {
		t.Fatalf("stats.Modules = %d, want 3", st.Modules)
	}
	set, err := ReadModuleASTZip(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Modules) != 3 {
		t.Fatalf("decoded %d modules, want 3", len(set.Modules))
	}
	entries, err := readZipEntries(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"main.gob", "remote.gob", "deep/util.gob"} {
		if entries[name] == nil {
			t.Fatalf("missing zip entry %s (got %v)", name, keysOf(entries))
		}
	}
	opts := dump.Options{Annotations: true}
	for name, want := range mods {
		got := set.Modules[name]
		if got == nil {
			t.Fatalf("module %q missing after round-trip", name)
		}
		if w, g := dump.DumpWithOptions(want, opts), dump.DumpWithOptions(got, opts); w != g {
			t.Fatalf("module %q dump mismatch after round-trip", name)
		}
	}
	t.Logf("multi-module: %d bytes, %d modules, %d nodes", buf.Len(), st.Modules, st.Nodes)
}

// TestLegacyContainerRejected：舊容器（ast.gob / mods.gob）不再支援——
// 版本鍵控已讓舊快取天然 miss，讀端不該靜默接受失效佈局。
func TestLegacyContainerRejected(t *testing.T) {
	prog := parseStd(t, "vec.no")
	for _, name := range []string{"ast.gob", "mods.gob"} {
		var tb bytes.Buffer
		if err := gob.NewEncoder(&tb).Encode(&parser.CachedProgram{Prog: prog}); err != nil {
			t.Fatal(err)
		}
		var zb bytes.Buffer
		zw := zip.NewWriter(&zb)
		tw, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(tb.Bytes()); err != nil {
			t.Fatal(err)
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadModuleASTZip(zb.Bytes()); err == nil {
			t.Fatalf("legacy container %s should be rejected", name)
		}
	}
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
	set, err := ReadASTZip(buf.Bytes())
	if err != nil {
		t.Fatalf("read zip: %v", err)
	}
	opts := dump.Options{Annotations: true}
	if want, got := dump.DumpWithOptions(prog, opts), dump.DumpWithOptions(set.Modules["main"], opts); want != got {
		t.Fatal("dump mismatch after tiny pack/unpack")
	}
	t.Logf("tiny: zip=%d bytes nodes=%d", buf.Len(), st.Nodes)
}

// TestASTZipCachePathVersioned：版本鍵控从流内改成路徑後，ASTZipCachePath 必须
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

// TestASTZipForeignBlobRejected： foreign gob 型別（模擬上一代 astDoc 流）硬錯。
func TestASTZipForeignBlobRejected(t *testing.T) {
	prog := parseStd(t, "vec.no")
	var buf bytes.Buffer
	if _, err := WriteASTZip(&buf, prog, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadASTZip(buf.Bytes()); err != nil {
		t.Fatalf("valid zip rejected: %v", err)
	}
	if _, err := ReadASTZip([]byte("not a zip")); err == nil {
		t.Fatal("expected zip open error")
	}

	type legacyAstDoc struct {
		ZipVer string
		AstVer string
		Prog   *parser.Program
		Anns   map[uint32][]*parser.AnnotationEntry
	}
	var tb bytes.Buffer
	if err := gob.NewEncoder(&tb).Encode(&legacyAstDoc{ZipVer: "old", AstVer: parser.ASTVersion, Prog: prog}); err != nil {
		t.Fatal(err)
	}
	var zb bytes.Buffer
	zw := zip.NewWriter(&zb)
	tw, err := zw.Create("main.gob")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(tb.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadASTZip(zb.Bytes()); err == nil {
		t.Fatal("expected hard error on foreign blob type")
	} else {
		t.Logf("legacy astDoc blob rejected: %v", err)
	}
}

// TestASTZipWithPackageNoml：附 package.noml 打包時 zip 应多一個條目，
// 且 ReadASTZip 回傳的清單位元組與寫入完全一致（原樣分發，不做任何改寫）。
func TestASTZipWithPackageNoml(t *testing.T) {
	prog := parseStd(t, "vec.no")
	pj := []byte("{\n  name: 'vec'\n  version: 'v1.2.3'\n  dependencies: ['nogit:v1.0.0@github.com/lizongying/notools']\n}\n")
	var buf bytes.Buffer
	if _, err := WriteASTZip(&buf, prog, pj); err != nil {
		t.Fatal(err)
	}
	entries, err := readZipEntries(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries["main.gob"] == nil || entries[packageName] == nil {
		t.Fatalf("expected main.gob + package.noml, got %v", keysOf(entries))
	}
	info, err := ReadASTZip(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(info.PackageNoml, pj) {
		t.Fatalf("package.noml round-trip mismatch: %q", info.PackageNoml)
	}
	// 不附清單時只有一個模組條目，且 PackageNoml 為 nil。
	var plain bytes.Buffer
	if _, err := WriteASTZip(&plain, prog, nil); err != nil {
		t.Fatal(err)
	}
	info, err = ReadASTZip(plain.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if info.PackageNoml != nil {
		t.Fatalf("plain zip should carry no package.noml, got %q", info.PackageNoml)
	}
	t.Logf("with package.noml: zip=%d bytes, round-trip OK", buf.Len())
}

// TestModulePathsTravelWithGobPayload 釘住「路徑隨 AST 走」：每個條目的
// gob 載荷自帶 ModulePath / SourcePath，讀端交叉校驗條目名與註解。
func TestModulePathsTravelWithGobPayload(t *testing.T) {
	entry := parseStd(t, "vec.no")
	entry.SourcePath = "entry.no"
	remote := parseStd(t, "json.no")
	remote.SourcePath = "remote.no"
	var buf bytes.Buffer
	if _, err := WriteModuleASTZip(&buf, map[string]*parser.Program{
		"main":   entry,
		"remote": remote,
	}, nil); err != nil {
		t.Fatal(err)
	}
	entries, err := readZipEntries(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, name := range []string{"main.gob", "remote.gob"} {
		var cp parser.CachedProgram
		if err := gob.NewDecoder(bytes.NewReader(entries[name])).Decode(&cp); err != nil {
			t.Fatal(err)
		}
		if cp.Prog == nil {
			t.Fatal("payload carries no program")
		}
		got[cp.Prog.ModulePath] = cp.Prog.SourcePath
	}
	if got["main"] != "entry.no" || got["remote"] != "remote.no" {
		t.Fatalf("path annotations did not survive gob round-trip: %v", got)
	}
	set, err := ReadModuleASTZip(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Modules) != 2 || set.Modules["main"] == nil || set.Modules["remote"] == nil {
		t.Fatalf("reader did not rebuild module index from payloads: %v", set.ModulePaths())
	}
}

// TestAnnotationMismatchRejected：條目名與載荷 ModulePath 不一致時硬錯，
// 避免分發件被篡改後靜默用錯模組索引。
func TestAnnotationMismatchRejected(t *testing.T) {
	prog := parseStd(t, "vec.no")
	prog.ModulePath = "wrong"
	var payload bytes.Buffer
	if err := gob.NewEncoder(&payload).Encode(&parser.CachedProgram{Prog: prog}); err != nil {
		t.Fatal(err)
	}
	var zb bytes.Buffer
	zw := zip.NewWriter(&zb)
	tw, err := zw.Create("main.gob")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(payload.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadModuleASTZip(zb.Bytes()); err == nil {
		t.Fatal("expected mismatch error")
	}
}

// TestFindModuleByFunction 對應 `# <pkg>.<fn>`：按頂層函式名在包內定位模組。
func TestFindModuleByFunction(t *testing.T) {
	src := "greet = () (out str) { out = 'hi' }\n"
	p := parser.New(lexer.New(src))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse: %v", errs)
	}
	other := parseStd(t, "vec.no")
	set := &ASTZip{Modules: map[string]*parser.Program{"main": other, "remote": prog}}
	got, err := set.FindModuleByFunction("greet")
	if err != nil {
		t.Fatal(err)
	}
	if got != prog {
		t.Fatalf("located wrong module")
	}
	if _, err := set.FindModuleByFunction("nope"); err == nil {
		t.Fatal("expected error for unknown function")
	}
	// 同名定義在兩個模組 → 明確報錯，不靜默選一個。
	set.Modules["main"] = prog
	if _, err := set.FindModuleByFunction("greet"); err == nil {
		t.Fatal("expected error for ambiguous function")
	}
}
