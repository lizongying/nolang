package llvm

import (
	"reflect"
	"strings"
	"testing"

	"github.com/lizongying/nolang/hir"
	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestGenerateHIRMatchesGenerate is the golden identity test for the HIR codegen
// seam: lowering an AST to HIR and reconstructing it back must produce exactly
// the same LLVM IR as consuming the AST directly. It exercises the bridge path
// (AST -> HIR -> AST -> emit) end to end, across a diverse set of constructs so
// that any reconstruction-fidelity regression (e.g. dropped nil children,
// mishandled function-type aliases, wrong slice inclusivity) fails loudly.
func TestGenerateHIRMatchesGenerate(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			name: "func-def-call-ifmatch",
			src: `add = (a i64, b i64) (result i64) {
  result = a + b
}
print('hi')
x = 1 + 2
x > 1 -> print('big')
-> print('small')
print(add(3, 4))
`,
		},
		{
			name: "arrays-and-for",
			src: `greet = (name str) (out str) {
  out = 'hello, ' + name
}
main = () () {
  print(greet('nolang'))
  nums = [1, 2, 3, 4]
  sum = 0
  for i in nums {
    sum = sum + i
  }
  print(sum)
  x = 10
  x > 5 -> print('big')
  -> print('small')
}
main()
`,
		},
		{
			name: "func-type-alias",
			src: `ret-cb = () (i64)
get-42 = () (r i64) {
  r = 42
}
apply-ret = (cb ret-cb) (out i64) {
  out = cb()
}
result i64 = 0
result = apply-ret(get-42)
{
  result == 42 -> print('ok: fn-type-return')
  -> print('fail: fn-type-return')
}
`,
		},
		{
			name: "slice-ranges",
			src: `s = 'HelloWorld'
print(s.len)
a = s[..]
print(a.len)
b = s[5..]
print(b.len)
c = s[..5]
print(c.len)
d = s[1..4]
print(d.len)
e = s[1..4)
print(e.len)
f = s(1..4]
print(f.len)
g = s(1..4)
print(g.len)
`,
		},
		{
			name: "optionals-and-match",
			src: `maybe = (v i64) (out ?i64) {
  out = v
}
main = () () {
  x = maybe(7)
  x != nil -> print('has value')
  -> print('none')
}
main()
`,
		},
		{
			// Multi-value return + destructuring assignment exercises the
			// KMultiAssign / multi-result call reconstruction path.
			name: "multi-return-swap",
			src: `swap = (a i64, b i64) (x i64, y i64) {
  x = b
  y = a
}
main = () () {
  m, n = swap(3, 4)
  print(m)
  print(n)
}
main()
`,
		},
		{
			// String indexing, .len, and half-open slice range cover the
			// KIndex / KSlice / struct-field access reconstruction.
			name: "string-slice-index",
			src: `s = 'Hello, World'
print(s[0])
print(s.len)
sub = s[7..]
print(sub)
`,
		},
		{
			// Module-level global named "b" routed through std @out (whose
			// internal scratch var is also "b") — the exact collision the
			// varAddr local-preference fix resolved. Locks the fix at HIR level.
			name: "global-b-array-slice",
			src: `b = [10, 20, 30]
print(b[0])
print(b.len)
g = b[1..2]
print(g[0])
`,
		},
		{
			// Struct definition + literal + field access (KStructDef /
			// KStructLit / field-read reconstruction).
			name: "struct-def-and-access",
			src: `user {
  name str
  age i64
}
make-user = () (u user) {
  u = user { name: 'abc', age: 20 }
}
main = () () {
  u = make-user()
  print(u.name)
}
main()
`,
		},
		{
			// Multiple match arms (several KIf nodes in sequence) plus a
			// trailing default arm.
			name: "multi-arm-match",
			src: `main = () () {
  n = 2
  n == 1 -> print('one')
  n == 2 -> print('two')
  n == 3 -> print('three')
  -> print('many')
}
main()
`,
		},
		{
			// Nested match blocks (KBlock containing KIf arms) reconstruct
			// block scoping correctly.
			name: "nested-if-block",
			src: `main = () () {
  a = 5
  b = 3
  a > b -> {
    a == 5 -> print('a is five')
    -> print('a bigger')
  }
  -> print('b not smaller')
}
main()
`,
		},
		{
			// Recursion (self KCall) exercises nested call emission and the
			// KInfix guard inside a function body.
			name: "recursion",
			src: `fib = (n i64) (r i64) {
  n < 2 -> r = n
  n >= 2 -> r = fib(n - 1) + fib(n - 2)
}
main = () () {
  print(fib(10))
}
main()
`,
		},
		{
			// String concatenation (KInfix with str operands), .len, and
			// character indexing (KIndex).
			name: "string-concat-index",
			src: `main = () () {
  s = 'Hello' + ', ' + 'World'
  print(s)
  print(s.len)
  print(s[0])
}
main()
`,
		},
		{
			// Loop carrying an accumulator inside a function body (KFor with
			// KInfix reassign over a result param).
			name: "loop-in-function",
			src: `sum-to = (n i64) (r i64) {
  r = 0
  for i in [0, 1, 2, 3, 4] {
    r = r + i
  }
}
main = () () {
  print(sum-to(5))
}
main()
`,
		},
		{
			// Nested for loops (KFor inside KFor) reconstruct the two range
			// variables and their scoping correctly.
			name: "nested-for",
			src: `main = () () {
  outer = [1, 2]
  for i in outer {
    for j in [10, 20] {
      print(i + j)
    }
  }
}
main()
`,
		},
		{
			// Platform annotations (#{mac-arm64} / #{win-arm64}) exercise the
			// HIR-id-keyed annotation lookup (Generator.annotationsFor ->
			// pkg.AnnotationsOf via hirAnnotationEntries). Both codegen paths
			// apply the same target-platform filter, so the emitted IR must be
			// byte-identical — any divergence in the HIR annotation resolution
			// (wrong Owner lookup, dropped entries) fails the equality check.
			name: "platform-annotation-filter",
			src: `#{mac-arm64}
get-os = () (out str) { out = 'mac' }
#{win-arm64}
get-os = () (out str) { out = 'win' }
print(get-os())
`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parse := func() *parser.Program {
				l := lexer.New(tc.src)
				p := parser.New(l)
				prog := p.ParseProgram()
				if errs := p.Errors(); len(errs) > 0 {
					t.Fatalf("parse errors: %v", errs)
				}
				return prog
			}

			astIR := NewGenerator().Generate(parse())

			pkg := parser.ASTToHIR(parse())
			if pkg == nil {
				t.Fatal("ASTToHIR returned nil")
			}
			hirIR := NewGenerator().GenerateHIR(pkg)

			if astIR != hirIR {
				t.Errorf("GenerateHIR output differs from Generate\n--- AST path (%d bytes) ---\n%s\n--- HIR path (%d bytes) ---\n%s",
					len(astIR), astIR, len(hirIR), hirIR)
			}
		})
	}
}

// TestGenerateHIRFuncTypeAlias exercises a named function-type alias used as a
// parameter type (e.g. "cb = ()(i64)" then "apply = (f cb) (...) { f() }").
// The reconstruction must rebuild the alias as a *parser.FunctionType (not a
// NamedType wrapping the "fn(...)" string), otherwise the function-typed
// parameter's @cb definition is dropped and opt fails with "undefined @cb".
func TestGenerateHIRFuncTypeAlias(t *testing.T) {
	src := `ret-cb = () (i64)
get-42 = () (r i64) {
  r = 42
}
apply-ret = (cb ret-cb) (out i64) {
  out = cb()
}
result i64 = 0
result = apply-ret(get-42)
{
  result == 42 -> print('ok: fn-type-return')
  -> print('fail: fn-type-return')
}
`
	parse := func() *parser.Program {
		l := lexer.New(src)
		p := parser.New(l)
		prog := p.ParseProgram()
		if errs := p.Errors(); len(errs) > 0 {
			t.Fatalf("parse errors: %v", errs)
		}
		return prog
	}

	astIR := NewGenerator().Generate(parse())
	pkg := parser.ASTToHIR(parse())
	if pkg == nil {
		t.Fatal("ASTToHIR returned nil")
	}
	hirIR := NewGenerator().GenerateHIR(pkg)

	if astIR != hirIR {
		t.Errorf("GenerateHIR func-type-alias output differs from Generate\n--- AST path (%d bytes) ---\n%s\n--- HIR path (%d bytes) ---\n%s",
			len(astIR), astIR, len(hirIR), hirIR)
	}
}

// TestGenerateHIRSanity asserts the HIR path produces a plausible module even
// when inspected in isolation.
func TestGenerateHIRSanity(t *testing.T) {
	src := `print('hi')
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	ir := NewGenerator().GenerateHIR(parser.ASTToHIR(prog))
	if ir == "" {
		t.Fatal("GenerateHIR produced empty IR")
	}
	for _, want := range []string{"define i32 @main", "@out", "str-long"} {
		if !strings.Contains(ir, want) {
			t.Errorf("HIR IR should contain %q, got:\n%s", want, ir)
		}
	}
}

// TestGenerateHIRReportsUnsupported confirms an unsupported kind surfaces as an
// explicit build-error comment rather than a silent wrong module.
func TestGenerateHIRReportsUnsupported(t *testing.T) {
	pkg := &hir.Package{
		Nodes: []hir.Node{
			{Kind: hir.KUnknown, S: hir.NoID + 1}, // unused; real unknown below
			{Id: 1, Kind: hir.KUnknown, S: 0},
		},
		Top: []int32{1},
	}
	// KUnknown is not a valid top-level statement; hirToAST should error.
	ir := NewGenerator().GenerateHIR(pkg)
	if !strings.Contains(ir, "HIR codegen unsupported") {
		t.Errorf("expected unsupported-kind error comment, got:\n%s", ir)
	}
}

// TestGenerateHIRReadsEmbedByHIRID proves the HIR codegen's embed lookup is
// driven by HIR node id (pkg.EmbedDataOf) rather than the AST-pointer-keyed
// semantic side-table. We attach embed bytes to a let-statement, lower the AST
// to HIR (which re-keys the bytes onto the lowered node id), and require that
// GenerateHIR emits the same @.embed.* global as the surface-AST path. If the
// keying regressed to the reconstructed-AST side-table and lost the bytes, the
// HIR path would emit a plain string var and the two IRs would diverge.
func TestGenerateHIRReadsEmbedByHIRID(t *testing.T) {
	src := `assets = 'PLACEHOLDER'
print(assets)
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	ls, ok := prog.Statements[0].(*parser.LetStatement)
	if !ok {
		t.Fatalf("first statement is not a LetStatement: %T", prog.Statements[0])
	}
	data := []byte("hello-embed-bytes")
	prog.Sem.SetEmbedData(ls, data)

	// AST path: semantic side-table keyed by AST pointer identity.
	astIR := NewGenerator().Generate(prog)

	// HIR path: embed bytes re-keyed onto the lowered node id.
	pkg := parser.ASTToHIR(prog)
	if pkg == nil {
		t.Fatal("ASTToHIR returned nil")
	}
	// Sanity: the bytes really travelled into the HIR package.Embeds.
	carried := false
	for _, em := range pkg.Embeds {
		if string(em.Data) == "hello-embed-bytes" {
			carried = true
		}
	}
	if !carried {
		t.Fatal("embed bytes not carried into HIR package.Embeds")
	}
	hirIR := NewGenerator().GenerateHIR(pkg)

	if astIR != hirIR {
		t.Errorf("HIR embed emission differs from AST path\n--- AST ---\n%s\n--- HIR ---\n%s", astIR, hirIR)
	}
	if !strings.Contains(astIR, "@.embed.assets") {
		t.Errorf("AST path did not emit @.embed.assets; IR:\n%s", astIR)
	}
	if !strings.Contains(hirIR, "@.embed.assets") {
		t.Errorf("HIR path did not emit @.embed.assets; IR:\n%s", hirIR)
	}
}

// TestGenerateHIRExprLiterals drives the new expression-emission seam
// (Generator.generateHIRExpr): every leaf literal kind must emit byte-identical
// LLVM value references when read natively from HIR versus reconstructed from
// the AST. This guards the first native slice of the expression engine so that
// later cuts (infix, call, index, …) can grow native coverage without silently
// diverging from the proven AST emitter.
func TestGenerateHIRExprLiterals(t *testing.T) {
	src := `main = () () {
  i = 42
  f = 3.5
  c = 'z'
  b = true
  n = nil
  x = 0x41
  ignored = i + f
}
main()
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	pkg := parser.ASTToHIR(prog)
	if pkg == nil {
		t.Fatal("ASTToHIR returned nil")
	}
	_, astOf, err := hirToAST(pkg)
	if err != nil {
		t.Fatalf("hirToAST failed: %v", err)
	}

	g := NewGenerator()
	g.hirPkg = pkg
	g.hirAstOf = astOf

	// Walk every reconstructed node; for those that are literal expressions,
	// compare native HIR emission against the AST emitter.
	literalKinds := map[reflect.Type]bool{
		reflect.TypeOf((*parser.IntegerLiteral)(nil)): true,
		reflect.TypeOf((*parser.FloatLiteral)(nil)):   true,
		reflect.TypeOf((*parser.ByteLiteral)(nil)):    true,
		reflect.TypeOf((*parser.CharLiteral)(nil)):    true,
		reflect.TypeOf((*parser.BooleanLiteral)(nil)): true,
		reflect.TypeOf((*parser.NilLiteral)(nil)):     true,
	}
	found := 0
	for id, node := range astOf {
		e, ok := node.(parser.Expression)
		if !ok {
			continue
		}
		if !literalKinds[reflect.TypeOf(e)] {
			continue
		}
		found++
		native := g.generateHIRExpr(nil, id)
		ast := g.generateExprWithSB(nil, e)
		if native != ast {
			t.Errorf("literal %T (HIR id %d): native %q != ast %q", e, id, native, ast)
		}
	}
	if found == 0 {
		t.Fatal("no literal expression nodes found in reconstructed program; test fixture is ineffective")
	}
}

// TestGenerateHIRExprIdent drives the native KIdent emitter (Generator.generateHIRIdent)
// through the generateHIRExpr seam: every identifier expression node must emit
// byte-identical LLVM value references and load instructions when read natively
// from HIR versus reconstructed from the AST. The common load path is emitted
// natively; enum-variant / %option / slice-view identifiers fall back to the
// reconstructed AST node, which this test also exercises for equality.
func TestGenerateHIRExprIdent(t *testing.T) {
	src := `main = () () {
  x = 5
  y = x
  print(y)
  s = 'hi'
  p = s
  print(p)
  ok = true
  print(ok)
}
main()
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	pkg := parser.ASTToHIR(prog)
	if pkg == nil {
		t.Fatal("ASTToHIR returned nil")
	}
	_, astOf, err := hirToAST(pkg)
	if err != nil {
		t.Fatalf("hirToAST failed: %v", err)
	}
	g := NewGenerator()
	g.hirPkg = pkg
	g.hirAstOf = astOf

	found := 0
	for id, node := range astOf {
		ident, ok := node.(*parser.Identifier)
		if !ok {
			continue
		}
		var sbA, sbB strings.Builder
		// Reset the temp-reg counter between the two calls so the emitted SSA
		// register names are comparable; both paths do the same number of
		// increments, so identical starting state yields identical output.
		g.tmpIdx = 0
		regA := g.generateHIRExpr(&sbA, id)
		g.tmpIdx = 0
		regB := g.generateExprWithSB(&sbB, ident)
		if regA != regB || sbA.String() != sbB.String() {
			t.Errorf("identifier %q (HIR id %d): native reg=%q ast reg=%q\nnative IR:\n%s\nast IR:\n%s",
				ident.Value, id, regA, regB, sbA.String(), sbB.String())
		}
		found++
	}
	if found == 0 {
		t.Fatal("no identifier expression nodes found in reconstructed program; test fixture is ineffective")
	}
}

// TestGenerateHIRExprInfix drives the native KInfix emitter (Generator.generateHIRInfix)
// through the generateHIRExpr seam: every infix expression node must emit
// byte-identical LLVM IR when read natively from HIR versus the surface-AST
// generateInfix. Operands are emitted through the seam (so nested identifiers /
// literals / infix go native), and the operator emission reuses generateInfixCore
// — identical between the two paths.
func TestGenerateHIRExprInfix(t *testing.T) {
	src := `main = () () {
  a = 3
  b = 4
  c = a + b
  d = a * b - 2
  e = c / b
  f = c % b
  g = a == b
  h = a != b
  i = a < b
  j = a > b
  k = a <= b
  l = a >= b
  m = a & b
  n = a | b
  o = a ^ b
  p = a << 1
  q = a >> 1
  print(c)
  print(d)
  print(e)
  print(f)
  print(g)
  print(h)
  print(i)
  print(j)
  print(k)
  print(l)
  print(m)
  print(n)
  print(o)
  print(p)
  print(q)
}
main()
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	pkg := parser.ASTToHIR(prog)
	if pkg == nil {
		t.Fatal("ASTToHIR returned nil")
	}
	_, astOf, err := hirToAST(pkg)
	if err != nil {
		t.Fatalf("hirToAST failed: %v", err)
	}
	g := NewGenerator()
	g.hirPkg = pkg
	g.hirAstOf = astOf

	found := 0
	for id, node := range astOf {
		infix, ok := node.(*parser.InfixExpression)
		if !ok {
			continue
		}
		var sbA, sbB strings.Builder
		// Reset the temp-reg counter between the two calls so emitted SSA names
		// are comparable; both paths do the same number of increments.
		g.tmpIdx = 0
		regA := g.generateHIRExpr(&sbA, id)
		g.tmpIdx = 0
		regB := g.generateInfix(&sbB, infix)
		if regA != regB || sbA.String() != sbB.String() {
			t.Errorf("infix %q (HIR id %d): native reg=%q ast reg=%q\nnative IR:\n%s\nast IR:\n%s",
				infix.Operator, id, regA, regB, sbA.String(), sbB.String())
		}
		found++
	}
	if found == 0 {
		t.Fatal("no infix expression nodes found in reconstructed program; test fixture is ineffective")
	}
}

func TestGenerateHIRExprPrefix(t *testing.T) {
	src := `main = () () {
  a = 3
  b = -a
  c = !a
  d = ~a
  e = -(a + 1)
  f = !(-a)
  g = -(a * 2)
  h = ~(-a)
  i = !0
  j = ~0
  k = -5
  print(b)
  print(c)
  print(d)
  print(e)
  print(f)
  print(g)
  print(h)
  print(i)
  print(j)
  print(k)
}
main()
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	pkg := parser.ASTToHIR(prog)
	if pkg == nil {
		t.Fatal("ASTToHIR returned nil")
	}
	_, astOf, err := hirToAST(pkg)
	if err != nil {
		t.Fatalf("hirToAST failed: %v", err)
	}
	g := NewGenerator()
	g.hirPkg = pkg
	g.hirAstOf = astOf

	found := 0
	for id, node := range astOf {
		prefix, ok := node.(*parser.PrefixExpression)
		if !ok {
			continue
		}
		var sbA, sbB strings.Builder
		// Reset the temp-reg counter between the two calls so emitted SSA names
		// are comparable; both paths do the same number of increments.
		g.tmpIdx = 0
		regA := g.generateHIRExpr(&sbA, id)
		g.tmpIdx = 0
		regB := g.generatePrefix(&sbB, prefix)
		if regA != regB || sbA.String() != sbB.String() {
			t.Errorf("prefix %q (HIR id %d): native reg=%q ast reg=%q\nnative IR:\n%s\nast IR:\n%s",
				prefix.Operator, id, regA, regB, sbA.String(), sbB.String())
		}
		found++
	}
	if found == 0 {
		t.Fatal("no prefix expression nodes found in reconstructed program; test fixture is ineffective")
	}
}

func TestGenerateHIRExprGrouped(t *testing.T) {
	src := `main = () () {
  a = 7
  b = (a)
  c = ((a + 1))
  d = ((a)) + 1
  print(b)
  print(c)
  print(d)
}
main()
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	pkg := parser.ASTToHIR(prog)
	if pkg == nil {
		t.Fatal("ASTToHIR returned nil")
	}
	_, astOf, err := hirToAST(pkg)
	if err != nil {
		t.Fatalf("hirToAST failed: %v", err)
	}
	g := NewGenerator()
	g.hirPkg = pkg
	g.hirAstOf = astOf

	found := 0
	for id, node := range astOf {
		grp, ok := node.(*parser.GroupedExpression)
		if !ok {
			continue
		}
		var sbA, sbB strings.Builder
		g.tmpIdx = 0
		regA := g.generateHIRExpr(&sbA, id)
		g.tmpIdx = 0
		regB := g.generateExprWithSB(&sbB, grp)
		if regA != regB || sbA.String() != sbB.String() {
			t.Errorf("grouped (HIR id %d): native reg=%q ast reg=%q\nnative IR:\n%s\nast IR:\n%s",
				id, regA, regB, sbA.String(), sbB.String())
		}
		found++
	}
	if found == 0 {
		t.Fatal("no grouped expression nodes found in reconstructed program; test fixture is ineffective")
	}
}

func TestGenerateHIRExprCall(t *testing.T) {
	src := `add = (a i64, b i64) (r i64) {
  r = a + b
}
sub = (a i64, b i64) (r i64) {
  r = a - b
}
main = () () {
  x = add(3, 4)
  y = sub(x, 2)
  z = add(sub(1, 1), add(2, 2))
  w = add(z, y)
  print(x)
  print(y)
  print(z)
  print(w)
}
main()
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	pkg := parser.ASTToHIR(prog)
	if pkg == nil {
		t.Fatal("ASTToHIR returned nil")
	}
	_, astOf, err := hirToAST(pkg)
	if err != nil {
		t.Fatalf("hirToAST failed: %v", err)
	}
	g := NewGenerator()
	g.hirPkg = pkg
	g.hirAstOf = astOf

	found := 0
	for id, node := range astOf {
		call, ok := node.(*parser.CallExpression)
		if !ok {
			continue
		}
		// Only exercise the user-function common subset; verify native == AST.
		// Reset both SSA temp-reg and string-literal counters between the two
		// runs so emitted @%N register names and @.str.N constant indices are
		// comparable (both paths increment the same counters, so identical
		// starting state yields byte-identical output).
		var sbA, sbB strings.Builder
		g.tmpIdx = 0
		g.stringIdx = 0
		regA := g.generateHIRExpr(&sbA, id)
		g.tmpIdx = 0
		g.stringIdx = 0
		regB := g.generateExprWithSB(&sbB, call)
		if regA != regB || sbA.String() != sbB.String() {
			t.Errorf("call %q (HIR id %d): native reg=%q ast reg=%q\nnative IR:\n%s\nast IR:\n%s",
				callFnName(call), id, regA, regB, sbA.String(), sbB.String())
		}
		found++
	}
	if found == 0 {
		t.Fatal("no call expression nodes found in reconstructed program; test fixture is ineffective")
	}
}

func callFnName(c *parser.CallExpression) string {
	if id, ok := c.Function.(*parser.Identifier); ok {
		return id.Value
	}
	return "<non-ident>"
}
