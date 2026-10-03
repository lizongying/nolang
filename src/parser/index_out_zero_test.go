package parser

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
)

// Regression guard for `#{index-out=zero}`: regardless of the element type, the
// default substituted on an out-of-range index is the ZERO VALUE of that element
// type (integers/byte/char → 0, float → 0.0, bool → false, str/txt → '', and
// composite → empty container / zero struct). This differs from an explicit
// literal default (e.g. `#{index-out=zero}`), which must match the element type.
func TestIndexOutZeroDefaultLiteral(t *testing.T) {
	tok := lexer.Token{Type: lexer.INT, Line: 1, Column: 1}
	zero := &AnnotationIdentValue{Token: tok, Value: "zero"}

	// Scalar element types → the corresponding scalar zero literal.
	if lit, errMsg := defaultLiteralFor(tok, "i64", zero); lit == nil || errMsg != "" {
		t.Fatalf("i64 zero: got (%v, %q)", lit, errMsg)
	} else if _, ok := lit.(*IntegerLiteral); !ok {
		t.Fatalf("i64 zero: expected *IntegerLiteral, got %T", lit)
	}

	if lit, errMsg := defaultLiteralFor(tok, "f64", zero); lit == nil || errMsg != "" {
		t.Fatalf("f64 zero: got (%v, %q)", lit, errMsg)
	} else if _, ok := lit.(*FloatLiteral); !ok {
		t.Fatalf("f64 zero: expected *FloatLiteral, got %T", lit)
	}

	if lit, errMsg := defaultLiteralFor(tok, "bool", zero); lit == nil || errMsg != "" {
		t.Fatalf("bool zero: got (%v, %q)", lit, errMsg)
	} else if b, ok := lit.(*BooleanLiteral); !ok || b.Value {
		t.Fatalf("bool zero: expected *BooleanLiteral(false), got %#v", lit)
	}

	if lit, errMsg := defaultLiteralFor(tok, "str", zero); lit == nil || errMsg != "" {
		t.Fatalf("str zero: got (%v, %q)", lit, errMsg)
	} else if s, ok := lit.(*StringLiteral); !ok || s.Value != "" {
		t.Fatalf("str zero: expected *StringLiteral(\"\"), got %#v", lit)
	}

	// Composite element type → the container zero value (empty slice literal).
	lit, errMsg := defaultLiteralFor(tok, "[]i64", zero)
	if lit == nil || errMsg != "" {
		t.Fatalf("[]i64 zero: got (%v, %q)", lit, errMsg)
	}
	if arr, ok := lit.(*ArrayLiteral); !ok || !arr.WasSliceLiteral || len(arr.Elements) != 0 {
		t.Fatalf("[]i64 zero: expected empty slice *ArrayLiteral, got %#v", lit)
	}
}
