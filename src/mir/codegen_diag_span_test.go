package mir

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestEmitLLVMDiagnosticsCarrySourceSpanAndAggregate pins the P0 diagnostic
// contract: a backend error must point at the HIR source span (file:line:col)
// and independent bad instructions must all be reported in one build.
// Previously vet was green while build printed a single internal MIR message
// with no location, forcing a peel-the-onion debugging session.
func TestEmitLLVMDiagnosticsCarrySourceSpanAndAggregate(t *testing.T) {
	src := `main = () () {
  x ?str = nil
  eprint(x)
  eprint(x)
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	// The build pipeline tags statements with their source file before
	// ASTToHIR; do the same here so HIR KCall nodes carry the file name.
	for _, st := range prog.Statements {
		prog.Sem.SetSourceFileDeep(st, "probe.no")
	}
	pkg := parser.ASTToHIR(prog)
	mod, _, _ := LowerHIR(pkg, nil)
	if mod == nil {
		t.Fatal("LowerHIR returned nil module")
	}
	_, diags := mod.EmitLLVMDiagnostics()
	if len(diags) < 2 {
		t.Fatalf("expected both eprint errors to be aggregated, got %d: %v", len(diags), diags)
	}
	wantLines := map[int32]bool{3: true, 4: true}
	for _, d := range diags {
		if !strings.Contains(d.Message, "eprint expects") {
			continue
		}
		if d.File != "probe.no" {
			t.Errorf("diagnostic file = %q, want probe.no", d.File)
		}
		if !wantLines[d.Line] {
			t.Errorf("diagnostic line = %d, want 3 or 4", d.Line)
		}
		if d.Col <= 0 {
			t.Errorf("diagnostic column = %d, want > 0", d.Col)
		}
		if !strings.Contains(d.Error(), "probe.no:") {
			t.Errorf("diagnostic does not render source location: %s", d.Error())
		}
		delete(wantLines, d.Line)
	}
	if len(wantLines) != 0 {
		t.Errorf("missing diagnostics on lines %v; got %v", wantLines, diags)
	}
}

// TestEmitLLVMShortCircuitDiagnosticIsSingleAndLocated covers the fatal-path
// half of the same contract. Before the fix these sites recorded a span-less
// diagnostic and returned a real error; EmitLLVM then appended err.Error() a
// second time, so one bad instruction produced two messages. Now the site
// uses failAbort (explicit short-circuit) and EmitLLVM does not append again
// — one located diagnostic, and emission still stops (IR empty).
func TestEmitLLVMShortCircuitDiagnosticIsSingleAndLocated(t *testing.T) {
	src := `point = { x i64 }
main = () () {
  s point = { x: 1 }
  n = s.len()
  print(n)
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	for _, st := range prog.Statements {
		prog.Sem.SetSourceFileDeep(st, "short.no")
	}
	pkg := parser.ASTToHIR(prog)
	mod, _, _ := LowerHIR(pkg, nil)
	if mod == nil {
		t.Fatal("LowerHIR returned nil module")
	}
	ir, diags := mod.EmitLLVMDiagnostics()
	if ir != "" {
		t.Fatalf("fatal path must stop emission, got IR of %d bytes", len(ir))
	}
	if len(diags) != 1 {
		t.Fatalf("expected exactly one diagnostic (no duplicate), got %d: %v", len(diags), diags)
	}
	d := diags[0]
	if d.Line <= 0 || d.Col <= 0 {
		t.Errorf("diagnostic has no source span: %+v", d)
	}
	if !strings.Contains(d.Message, "setfield") {
		t.Errorf("unexpected diagnostic message: %q", d.Message)
	}
	if d.File != "short.no" {
		t.Errorf("diagnostic file = %q, want short.no (non-call nodes must carry a file too)", d.File)
	}
	if !strings.Contains(d.Error(), "short.no:3:") {
		t.Errorf("diagnostic does not render file:line: %s", d.Error())
	}
}
