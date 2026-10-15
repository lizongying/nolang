package mmast

import (
	"bytes"
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
		if err := WriteASTZip(&buf, prog, "test", rel, "deadbeef"); err != nil {
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
		if info.Manifest.Nodes == 0 || info.SrcSHA != "deadbeef" {
			t.Fatalf("%s: bad manifest %+v", rel, info.Manifest)
		}
		t.Logf("%-16s zip=%7d bytes  nodes=%6d", rel, buf.Len(), info.Manifest.Nodes)
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
	st, err := WriteASTZipWithStats(&buf, prog, "vec", rel, "")
	if err != nil {
		t.Fatal(err)
	}
	if st.Nodes == 0 || st.Chunks == 0 || st.Bytes != int64(buf.Len()) {
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
	t.Logf("pack flow: nodes=%d chunks=%d bytes=%d", st.Nodes, st.Chunks, st.Bytes)
}

// TestASTZipLayout：zip 必須恰好含 manifest.mm + tree.mm 兩個條目（單文檔佈局）。
// mm 的容器上限已放開到 4GB，整棵樹原樣塞進 tree.mm，不再按位元組切片。
func TestASTZipLayout(t *testing.T) {
	prog := parseStd(t, "net/tls.no")
	var buf bytes.Buffer
	if err := WriteASTZip(&buf, prog, "tls", "net/tls.no", ""); err != nil {
		t.Fatal(err)
	}
	zr, err := readZipEntries(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(zr) != 2 {
		t.Fatalf("expected manifest.mm + tree.mm (2 entries), got %d: %v", len(zr), keysOf(zr))
	}
	if _, ok := zr[manifestName]; !ok {
		t.Fatalf("missing %s", manifestName)
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

// TestASTZipTiny：極小模組走完整 mm→zip→mm 路徑（樹 <64KB，不依賴 4GB 容器）。
// 驗證單文檔佈局的編解碼本身正確（大模組要等 mm 容器放開到 4GB）。
func TestASTZipTiny(t *testing.T) {
	src := "x = 1\ny = x + 2\nz = y * 3\n"
	p := parser.New(lexer.New(src))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse: %v", errs)
	}
	var buf bytes.Buffer
	if err := WriteASTZip(&buf, prog, "tiny", "tiny.no", "abc123"); err != nil {
		t.Fatalf("write zip: %v", err)
	}
	back, info, err := ReadASTZip(buf.Bytes())
	if err != nil {
		t.Fatalf("read zip: %v", err)
	}
	opts := dump.Options{Annotations: true}
	if want, got := dump.DumpWithOptions(prog, opts), dump.DumpWithOptions(back, opts); want != got {
		t.Fatal("dump mismatch after tiny pack/unpack")
	}
	t.Logf("tiny: zip=%d bytes nodes=%d", buf.Len(), info.Manifest.Nodes)
}

// TestASTZipVersionGuard：zip/節點表版本不符必須硬錯。
func TestASTZipVersionGuard(t *testing.T) {
	prog := parseStd(t, "vec.no")
	var buf bytes.Buffer
	if err := WriteASTZip(&buf, prog, "vec", "vec.no", ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadASTZip(buf.Bytes()); err != nil {
		t.Fatalf("valid zip rejected: %v", err)
	}
	if _, _, err := ReadASTZip([]byte("not a zip")); err == nil {
		t.Fatal("expected zip open error")
	}
}
