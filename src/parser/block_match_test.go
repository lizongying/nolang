package parser

import (
	"reflect"
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
)

// parseSugar parses source into the sugar AST (no lowering), which is the shape
// fmt / LSP / checker consume and the shape the block-match node lives in.
func parseSugar(t *testing.T, src string) (*Program, []string) {
	t.Helper()
	l := lexer.New(src)
	p := New(l)
	p.SkipLowering = true
	prog := p.ParseProgram()
	if prog == nil {
		t.Fatalf("ParseProgram returned nil")
	}
	return prog, p.Errors()
}

func bodyOf(t *testing.T, prog *Program) []Statement {
	t.Helper()
	if len(prog.Statements) != 1 {
		t.Fatalf("want 1 top-level statement, got %d", len(prog.Statements))
	}
	fn, ok := prog.Statements[0].(*FunctionDefinition)
	if !ok {
		t.Fatalf("top-level statement is %T, want *FunctionDefinition", prog.Statements[0])
	}
	if fn.Body == nil {
		t.Fatal("function body is nil")
	}
	return fn.Body.Statements
}

// Regression: `}: {` used to parse silently into THREE unrelated statements
// (bare BlockStatement, stray COLON ExpressionStatement with a nil Expression,
// bare SurfaceMatch). It must now be exactly one BlockMatchStatement.
func TestBlockMatchParsesAsSingleStatement(t *testing.T) {
	src := `f = () {
    state = [1, 2, 3]
    {
        state[0] ?= state[2]
    }: {
        err ->
        -> _
    }
}
`
	prog, errs := parseSugar(t, src)
	if len(errs) != 0 {
		t.Fatalf("unexpected parse errors: %v", errs)
	}
	stmts := bodyOf(t, prog)
	if len(stmts) != 2 {
		t.Fatalf("want 2 body statements (let + block match), got %d", len(stmts))
	}
	bm, ok := stmts[1].(*BlockMatchStatement)
	if !ok {
		t.Fatalf("body[1] is %T, want *BlockMatchStatement", stmts[1])
	}
	if bm.Body == nil || len(bm.Body.Statements) != 1 {
		t.Fatalf("block-match body missing or wrong size: %#v", bm.Body)
	}
	if bm.Match == nil {
		t.Fatal("block-match Match is nil")
	}
	if bm.Match.Matched != nil {
		t.Errorf("sugar Match.Matched must stay nil for `no fmt`, got %T", bm.Match.Matched)
	}
	if bm.Match.RBracePos.Line == 0 {
		t.Error("Match.RBracePos.Line == 0: checker would skip exhaustiveness")
	}
	if len(bm.Match.Arms) != 2 {
		t.Fatalf("want 2 arms, got %d", len(bm.Match.Arms))
	}
	if bm.Match.Arms[0].Condition == nil {
		t.Error("first arm has no condition (want `err ->`)")
	}
	if !bm.Match.Arms[1].IsWildcard {
		t.Error("second arm is not the `-> _` catch-all")
	}
	if bm.Subject == "" || !strings.HasPrefix(bm.Subject, "__bm_") {
		t.Errorf("bad synthetic subject %q", bm.Subject)
	}
	if bm.RBrace.Line == 0 {
		t.Error("BlockMatchStatement.RBrace not recorded")
	}
	if bm.EndPos().Line == 0 {
		t.Error("EndPos() returned a zero position")
	}
}

// A plain bare block with no postfix colon must keep its old shape.
func TestBlockMatchTailRequiresColonAndBrace(t *testing.T) {
	src := `f = () {
    {
        print('hi')
    }
    print('after')
}
`
	prog, errs := parseSugar(t, src)
	if len(errs) != 0 {
		t.Fatalf("unexpected parse errors: %v", errs)
	}
	stmts := bodyOf(t, prog)
	if len(stmts) != 2 {
		t.Fatalf("want 2 body statements, got %d", len(stmts))
	}
	if _, ok := stmts[0].(*BlockStatement); !ok {
		t.Errorf("body[0] is %T, want *BlockStatement", stmts[0])
	}
}

// Each block match gets a stable, distinct parse-time subject name. Two `?=`
// on the same line would collide with the old `__unwrap_<line>` scheme.
func TestBlockMatchSubjectsAreUnique(t *testing.T) {
	src := `f = () {
    {
        print('a')
    }: { -> _ }
    {
        print('b')
    }: { -> _ }
}
`
	prog, errs := parseSugar(t, src)
	if len(errs) != 0 {
		t.Fatalf("unexpected parse errors: %v", errs)
	}
	stmts := bodyOf(t, prog)
	if len(stmts) != 2 {
		t.Fatalf("want 2 block matches, got %d statements", len(stmts))
	}
	seen := map[string]bool{}
	for i, s := range stmts {
		bm, ok := s.(*BlockMatchStatement)
		if !ok {
			t.Fatalf("body[%d] is %T, want *BlockMatchStatement", i, s)
		}
		if seen[bm.Subject] {
			t.Errorf("duplicate subject %q", bm.Subject)
		}
		seen[bm.Subject] = true
	}
}

// H0 展開必須是 copy-on-write：同一份糖 Program 被 ASTToHIR 展開多次後，
// BlockMatchStatement.Body 的陳述條數與節點指標都不能變（no fmt 與
// HIR-equivalence seam 都依賴這一點）。
func TestBlockMatchExpansionDoesNotMutateSugar(t *testing.T) {
	src := `f = (d i64) {
    {
        v ?= 6 / d
        print(v.to-str())
    }: {
        err -> print(it.msg())

        -> _
    }
}
`
	p := New(lexer.New(src))
	p.SkipLowering = true
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	fn := prog.Statements[0].(*FunctionDefinition)
	bm := fn.Body.Statements[0].(*BlockMatchStatement)
	before := append([]Statement(nil), bm.Body.Statements...)
	for i := 0; i < 2; i++ {
		pkg := ASTToHIR(prog)
		if pkg == nil {
			t.Fatalf("ASTToHIR #%d returned nil", i)
		}
		if len(bm.Body.Statements) != len(before) {
			t.Fatalf("ASTToHIR #%d mutated the sugar body: %d statements, want %d",
				i, len(bm.Body.Statements), len(before))
		}
		for j := range before {
			if bm.Body.Statements[j] != before[j] {
				t.Fatalf("ASTToHIR #%d replaced sugar statement %d", i, j)
			}
		}
	}
}

// js / wasm 走 DesugarSurfaceMatchesPlain：塊 match 必須在原地換成核心 AST，
// 且展開後不再留下任何 BlockMatchStatement。
func TestBlockMatchDesugarPlainRemovesNode(t *testing.T) {
	src := `f = (d i64) {
    {
        v ?= 6 / d
        print(v.to-str())
    }: {
        err -> print(it.msg())

        -> _
    }
}
`
	p := New(lexer.New(src))
	p.SkipLowering = true
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	p.DesugarSurfaceMatchesPlain(prog)
	fn := prog.Statements[0].(*FunctionDefinition)
	if _, ok := fn.Body.Statements[0].(*BlockMatchStatement); ok {
		t.Fatal("DesugarSurfaceMatchesPlain left a BlockMatchStatement in place")
	}
	if count := countBlockMatchNodes(reflect.ValueOf(prog), map[uintptr]bool{}); count != 0 {
		t.Fatalf("DesugarSurfaceMatchesPlain left %d block match node(s)", count)
	}
}

func countBlockMatchNodes(v reflect.Value, seen map[uintptr]bool) int {
	switch v.Kind() {
	case reflect.Interface:
		if v.IsNil() {
			return 0
		}
		return countBlockMatchNodes(v.Elem(), seen)
	case reflect.Ptr:
		if v.IsNil() {
			return 0
		}
		if _, ok := v.Interface().(*BlockMatchStatement); ok {
			return 1
		}
		if seen[v.Pointer()] {
			return 0
		}
		seen[v.Pointer()] = true
		return countBlockMatchNodes(v.Elem(), seen)
	case reflect.Slice, reflect.Array:
		n := 0
		for i := 0; i < v.Len(); i++ {
			n += countBlockMatchNodes(v.Index(i), seen)
		}
		return n
	case reflect.Struct:
		n := 0
		for _, i := range exportedFieldIndices(v.Type()) {
			n += countBlockMatchNodes(v.Field(i), seen)
		}
		return n
	}
	return 0
}
