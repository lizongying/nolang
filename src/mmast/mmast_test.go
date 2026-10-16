package mmast

import (
	"archive/zip"
	"bytes"
	"encoding/gob"
	"os"
	"path/filepath"
	"testing"

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
		if st.Nodes == 0 || info.ZipVer != ZipVer || info.AstVer != parser.ASTVersion {
			t.Fatalf("%s: bad stats/manifest: %+v %+v", rel, st, info)
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

// TestASTZipVersionGuard：zip/ast 版本不符必須硬錯。gob 對 schema 漂移是静默
// 跳過，所以閘得我們自己把：篡改 ast.gob 流内的 ZipVer/AstVer 要能被 ReadASTZip
// 拦下（重编一個只改版本前置欄的 ast.gob，AST 本體原樣保留）。
func TestASTZipVersionGuard(t *testing.T) {
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

	tamper := func(t *testing.T, field string, mutate func(*astDoc)) {
		t.Helper()
		entries, err := readZipEntries(buf.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		var doc astDoc
		if err := gob.NewDecoder(bytes.NewReader(entries[treeName])).Decode(&doc); err != nil {
			t.Fatal(err)
		}
		mutate(&doc)
		var tb bytes.Buffer
		if err := gob.NewEncoder(&tb).Encode(&doc); err != nil {
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
			t.Fatalf("expected hard error on tampered %s", field)
		} else {
			t.Logf("tampered %s rejected: %v", field, err)
		}
	}
	tamper(t, "ZipVer", func(d *astDoc) { d.ZipVer = ZipVer + ".tampered" })
	tamper(t, "AstVer", func(d *astDoc) { d.AstVer = parser.ASTVersion + ".tampered" })
}

// TestASTZipWithPackageJSONC：附 package.jsonc 打包時 zip 应多一個條目，
// 且 ReadASTZip 回傳的位元組與寫入完全一致（原樣分發，不做任何改寫）。
func TestASTZipWithPackageJSONC(t *testing.T) {
	prog := parseStd(t, "vec.no")
	pj := []byte("{\n  name: \"vec\",\n  version: \"v1.2.3\",\n  dependencies: { nogit: \"nogit@github.com/lizongying/notools\" },\n}\n")
	var buf bytes.Buffer
	if _, err := WriteASTZip(&buf, prog, pj); err != nil {
		t.Fatal(err)
	}
	entries, err := readZipEntries(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected ast.gob + package.jsonc, got %v", keysOf(entries))
	}
	back, info, err := ReadASTZip(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if back == nil {
		t.Fatal("nil program after read")
	}
	if !bytes.Equal(info.PackageJSONC, pj) {
		t.Fatalf("package.jsonc round-trip mismatch: %q", info.PackageJSONC)
	}
	// 不附清單的入口維持單條目，且 PackageJSONC 為 nil。
	var plain bytes.Buffer
	if _, err := WriteASTZip(&plain, prog, nil); err != nil {
		t.Fatal(err)
	}
	_, info, err = ReadASTZip(plain.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if info.PackageJSONC != nil {
		t.Fatalf("plain zip should carry no package.jsonc, got %q", info.PackageJSONC)
	}
	t.Logf("with package.jsonc: zip=%d bytes, round-trip OK", buf.Len())
}
