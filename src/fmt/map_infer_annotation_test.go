package fmt

import (
	"strings"
	"testing"
)

// TestInferredMapFromCallNotRenderedAsAnnotation guards the formatter against
// rendering a parser-INFERRED map type as a source annotation.
//
// `m2 = make-map()` carries no type annotation in the source, but parseLetStatement
// synthesizes a MapType for m2 from make-map's return type. When MapType had no
// IsInferred flag, formatLetStatement's map branch printed it unconditionally, so
// the formatter turned `m2 = make-map()` into `m2 [str]i64 = make-map()` — an
// annotation that was never written (and, combined with the redundant-annotation
// pre-pass, led to deleting the variable name entirely on the file path).
//
// The rendered annotation must stay absent, and formatting must be idempotent.
func TestInferredMapFromCallNotRenderedAsAnnotation(t *testing.T) {
	src := "make-map = () (out [str]i64) {\n    out [str]i64 = { 'x': 10 }\n}\nm2 = make-map()\nv2 = m2.get('x')\n"
	got := Format(src)
	if strings.Contains(got, "m2 [str]i64 = make-map()") {
		t.Fatalf("formatter injected an inferred map annotation onto m2:\n%s", got)
	}
	if !strings.Contains(got, "m2 = make-map()") {
		t.Fatalf("expected the m2 assignment name to be preserved, got:\n%s", got)
	}
	if Format(got) != got {
		t.Fatalf("Format is not idempotent for inferred map type:\nfirst:\n%s\nsecond:\n%s", got, Format(got))
	}
}
