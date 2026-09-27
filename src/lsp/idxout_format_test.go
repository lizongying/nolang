package lsp

import (
	"strings"
	"testing"

	nolangfmt "github.com/lizongying/nolang/fmt"
)

// REGRESSION GUARD for the fmt-on-save __idx_out corruption.
//
// Same class of bug as TestFormatOnSaveDoesNotEmitUnwrapBlocks, but for the
// safe-index desugar. formatNolangCode (server.go) formats doc.AST directly and
// writes the result back to the user's file on save. ParseDocument must keep the
// surface AST so a `#{index-out = DEF} x = v[i]` renders back verbatim; if safe
// index lowering runs (parser.SkipSafeIndexLowering=false), doc.AST contains the
// non-reparseable `__idx_out_L_C: { ok -> ...; -> x = DEF }` block, and format-on-save
// mangles the source (and drops the DEF literal, producing `x = =`).
func TestFormatOnSaveDoesNotEmitIdxOutBlocks(t *testing.T) {
	dm := NewDocumentManager()
	uri := "file:///test/idxout.no"
	text := "combined [40]byte\nseed [20]byte\ni = 0\n(i < 20) {\n    #{index-out=0}\n    combined[i] = seed[i]\n    i = i + 1\n}\n"

	if _, err := dm.OpenDocument(uri, text); err != nil {
		t.Fatalf("OpenDocument failed: %v", err)
	}
	if _, _, err := dm.ParseDocument(uri); err != nil {
		t.Fatalf("ParseDocument failed: err=%v", err)
	}

	doc, err := dm.GetDocument(uri)
	if err != nil {
		t.Fatalf("GetDocument failed: %v", err)
	}
	if doc.AST == nil {
		t.Fatal("doc.AST is nil after ParseDocument")
	}

	formatted := nolangfmt.FormatProgram(doc.AST, doc.Text)
	if strings.Contains(formatted, "__idx_out_") {
		t.Errorf("formatted output contains lowered __idx_out blocks:\n%s", formatted)
	}
	if !strings.Contains(formatted, "combined[i] = seed[i]") {
		t.Errorf("safe-index statement not preserved verbatim:\n%s", formatted)
	}
	if !strings.Contains(formatted, "#{index-out=0}") {
		t.Errorf("#{index-out} annotation not preserved:\n%s", formatted)
	}
}
