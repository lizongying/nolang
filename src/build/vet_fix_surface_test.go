//go:build !wasm

package build

import (
	"bytes"
	"compress/gzip"
	"encoding/gob"
	"os"
	"path/filepath"
	"testing"

	"github.com/lizongying/nolang/checker"
	"github.com/lizongying/nolang/checker/stdastembed"
)

// bakeStdSigKeyForTest replicates checker.useBakedStdSigKey without importing its
// unexported helper: it decodes the embedded std AST blob and sets
// checker.StdSigKey to the blob's StdKey, so the transpiler can load the std
// tables without relying on -ldflags injection. This keeps the test runnable via
// a bare `go test ./build/` as well as `make test`.
func bakeStdSigKeyForTest(t *testing.T) {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(stdastembed.Blob))
	if err != nil {
		t.Fatalf("gunzip baked blob: %v", err)
	}
	defer zr.Close()
	var blob checker.StdASTBlob
	if err := gob.NewDecoder(zr).Decode(&blob); err != nil {
		t.Fatalf("decode baked blob: %v", err)
	}
	if blob.StdKey == "" {
		t.Fatal("baked blob carries no StdKey; regenerate with `make gen`")
	}
	orig := checker.StdSigKey
	checker.StdSigKey = blob.StdKey
	t.Cleanup(func() { checker.StdSigKey = orig })
}

// TestVetFileWithLints_BuildLayerFixes 回歸保障：build 層編譯期診斷
// （sm7rjfcm / sjuueqqo / 5jsd564o / uqby2pvs / gyuohhum / xo6e53hm /
// xgs17cqx / 2243l8if）在 no vet 路徑下必須攜帶 DiagnosticFix（Fixes 非空），
// 否則 CLI 文本 / JSON 報告 / LSP 診斷三處表面都無法展示「建議修復」。
//
// 這些診斷原本以 i18n.Errorf 字串返回，經 parseCompileErrorToLints 正則反解析後
// 會丟失 Fixes；本測試確保它們改走結構化 *DiagnosticError 路徑，Fixes 被原樣透傳。
func TestVetFileWithLints_BuildLayerFixes(t *testing.T) {
	bakeStdSigKeyForTest(t)
	checker.SetStdASTBlob(stdastembed.Blob)

	cases := []struct {
		name    string
		source  string
		wantTID string
	}{
		{"dup_var", "x i64 = 1\nx i64 = 1\n", "sm7rjfcm"},
		{"loop_uninit", "g = () {\n  for i = 0, i < 10, i = i + 1 {\n    x i64 = 0\n  }\n  r i64 = x\n}\n", "sjuueqqo"},
		{"str_assign", "s str = 42\n", "5jsd564o"},
		{"oob", "a [3]i64 = [1, 2, 3]\nb i64 = a[5]\n", "uqby2pvs"},
		{"neg_str_idx", "s str = 'abc'\nb i64 = s[-1]\n", "gyuohhum"},
		{"len_ro", "a [3]i64 = [1, 2, 3]\na.len = 1\n", "xo6e53hm"},
		{"main_reserved", "main = () {\n}\n", "xgs17cqx"},
		{"any_user", "f = (args ..any) {\n}\n", "2243l8if"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "m.no")
			if err := os.WriteFile(path, []byte(tc.source), 0o644); err != nil {
				t.Fatalf("write temp: %v", err)
			}
			lints, err := VetFileWithLints(path, BuildOptions{})
			if err != nil {
				t.Fatalf("VetFileWithLints returned error: %v", err)
			}
			var found *checker.LintResult
			for i := range lints {
				if lints[i].TraceID == tc.wantTID {
					found = &lints[i]
					break
				}
			}
			if found == nil {
				t.Fatalf("expected lint with TraceID %s, got: %+v", tc.wantTID, lints)
			}
			if len(found.Fixes) == 0 {
				t.Errorf("lint %s has no Fixes; no 'fix:' will surface in no vet", tc.wantTID)
			}
			if found.Fixes[0].Description == "" && found.Fixes[0].Snippet == "" {
				t.Errorf("lint %s Fix[0] is empty (no Description and no Snippet)", tc.wantTID)
			}
		})
	}
}
