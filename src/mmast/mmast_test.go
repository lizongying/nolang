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
		t.Logf("%-16s zip=%7d bytes  nodes=%6d  chunks=%3d",
			rel, buf.Len(), info.Manifest.Nodes, info.Manifest.Chunks)
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

// TestASTZipChunkLimit：每個分片編碼後都必須 ≤ mm 的 65535 位元組上限。
func TestASTZipChunkLimit(t *testing.T) {
	prog := parseStd(t, "net/tls.no")
	var buf bytes.Buffer
	if err := WriteASTZip(&buf, prog, "tls", "net/tls.no", ""); err != nil {
		t.Fatal(err)
	}
	zr, err := readZipEntries(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(zr) < 2 {
		t.Fatalf("expected manifest + at least one chunk, got %d entries", len(zr))
	}
	mmChunkMax := 65535
	for name, data := range zr {
		if name == "manifest.mm" {
			continue
		}
		if len(data) > mmChunkMax {
			t.Fatalf("%s is %d bytes, over mm's %d-byte container limit", name, len(data), mmChunkMax)
		}
	}
	t.Logf("tls.no: %d zip entries, all chunks within the mm limit", len(zr))
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
