package lsp

import (
	"strings"
	"testing"

	nolangfmt "github.com/lizongying/nolang/fmt"
)

// REGRESSION GUARD for the fmt-on-save __unwrap corruption.
//
// The document AST is formatted directly by formatNolangCode (server.go →
// fmt.FormatProgram(doc.AST, doc.Text)). If the document were parsed WITH
// unwrap lowering enabled, `v ?= expr` would already be expanded into
// __unwrap_N blocks in the AST, and formatting would write those blocks back
// into the user's source — where they no longer parse ("expected expression,
// got nil instead"). ParseDocument must keep the surface AST
// (parser.SkipUnwrapLowering=true), so the formatter renders the original
// `?=` form.
func TestFormatOnSaveDoesNotEmitUnwrapBlocks(t *testing.T) {
	dm := NewDocumentManager()
	uri := "file:///test/unwrap.no"
	text := "main = () (content ?i64) {\n    size ?= fstat-size(.fd)\n    content = size\n}\n"

	if _, err := dm.OpenDocument(uri, text); err != nil {
		t.Fatalf("OpenDocument failed: %v", err)
	}
	if _, errs, err := dm.ParseDocument(uri); err != nil || len(errs) > 0 {
		t.Fatalf("ParseDocument failed: err=%v errs=%v", err, errs)
	}

	doc, err := dm.GetDocument(uri)
	if err != nil {
		t.Fatalf("GetDocument failed: %v", err)
	}
	if doc.AST == nil {
		t.Fatal("doc.AST is nil after ParseDocument")
	}

	formatted := nolangfmt.FormatProgram(doc.AST, doc.Text)
	if strings.Contains(formatted, "__unwrap_") {
		t.Errorf("formatted output contains lowered __unwrap blocks:\n%s", formatted)
	}
	if !strings.Contains(formatted, "size ?= fstat-size(.fd)") {
		t.Errorf("?= statement not preserved verbatim:\n%s", formatted)
	}
}
