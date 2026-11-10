package parser

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
)

// TestStructLiteralFieldDocAttached 驗證 struct literal 欄位上方的獨立 `;`
// 註解真的被收進該欄位的 Doc（而不是留在 p.comments，被字面量後第一條
// 陳述收走）。compiler/src/h2m.no 的 s99j 釋放清單必須依附 `rls: []`。
func TestStructLiteralFieldDocAttached(t *testing.T) {
	src := `ctx {
    rls []i64
    rlsm []i64
}

c = ctx {
    ops: []
    stftn: []
    ; s99j：scope-exit 釋放清單
    ;   rls = 待釋放值 id
    ;   rlsm = 欄位遮罩
    rls: []
    rlsm: []
}
c.vty.push(TY-I64)
`
	l := lexer.New(src)
	p := New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}

	var lit *StructLiteral
	for _, s := range prog.Statements {
		let, ok := s.(*LetStatement)
		if !ok {
			continue
		}
		if sl, ok := let.Value.(*StructLiteral); ok {
			lit = sl
		}
	}
	if lit == nil {
		t.Fatal("no struct literal parsed")
	}

	var rls, rlsm *StructField
	for _, f := range lit.Fields {
		switch f.Name {
		case "rls":
			rls = f
		case "rlsm":
			rlsm = f
		}
	}
	if rls == nil || rlsm == nil {
		t.Fatalf("missing fields: rls=%v rlsm=%v", rls, rlsm)
	}

	doc := rls.GetDoc()
	if doc == nil || len(doc.List) != 3 {
		t.Fatalf("rls Doc = %v, want 3 comment lines", doc)
	}
	var joined strings.Builder
	for _, c := range doc.List {
		joined.WriteString(c.Text)
		joined.WriteString("\n")
	}
	if !strings.Contains(joined.String(), "s99j") {
		t.Fatalf("rls Doc does not carry the s99j note: %q", joined.String())
	}
	if d := rlsm.GetDoc(); d != nil && len(d.List) > 0 {
		t.Fatalf("rlsm unexpectedly received the previous field's Doc: %+v", d.List)
	}

	// 註解不該再被字面量後的第一條陳述收走。
	for _, s := range prog.Statements {
		d, ok := s.(interface{ GetDoc() *CommentGroup })
		if !ok || d.GetDoc() == nil {
			continue
		}
		for _, c := range d.GetDoc().List {
			if strings.Contains(c.Text, "s99j") {
				t.Fatalf("s99j note leaked into statement %T after the literal", s)
			}
		}
	}
}
