package build

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/lizongying/nolang/build/astaudit"
	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// repoRoot walks up from the working directory until it finds go.mod (src/).
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod not found above %s", dir)
		}
		dir = parent
	}
}

// astWriteCorpus is the curated corpus for the AST-write gate. Each entry
// reaches at least one assembly pass that §13.5 lists: module merge
// (# std/...), cross-file imports (# path), generic-struct monomorphisation,
// union monomorphisation, slice-struct monomorphisation, embeds, multi-assign
// conversion, top-level statements, interface devirtualisation, match
// lowering. Keep it broad: a narrower corpus makes the pinned inventory
// flaky rather than smaller.
//
// Paths point at the 37-category merge (15618c3c, "tests: merge similar cases
// into single files"). Eight entries this gate was pinned against — 1.no,
// 2.no, simple-match.no, number-generic.no, string-union.no, std-net-ext.no,
// heap.no, sha3.no — were folded into category files and deleted, so the
// originals no longer exist. Each replacement carries its original member
// verbatim (the `merged from tests/<name>` marker in the file names it), so
// the assembly-pass coverage below is unchanged; only the containing file
// moved. Run the pinned test with -v after touching this: it logs the census
// verbatim, and a non-empty `--- got ---` means a write site reached a parse
// output (a §1.4 violation), not that the corpus needs relaxing.
var astWriteCorpus = []string{
	"tests/misc1.no",        // was 1.no + 2.no
	"tests/match.no",        // was simple-match.no (match lowering)
	"tests/embed.no",        // embeds
	"tests/set.no",          // slice-struct monomorphisation
	"tests/map-generics.no", // generic-struct monomorphisation
	"tests/generics.no",     // generic-struct monomorphisation
	"tests/number.no",       // was number-generic.no (union: num = int | float)
	"tests/str.no",          // was string-union.no (union: string = str | txt)
	"tests/database-sql.no", // module merge (# std/...)
	"tests/net.no",          // was std-net-ext.no (cross-file imports)
	"tests/json.no",         // multi-assign conversion
	"tests/memory.no",       // was heap.no
	"tests/sha.no",          // was sha3.no
}

// runASTAudit compiles one file in vet mode with the AST-write gate enabled
// and returns the merged delta across every compiled Program root.
func runASTAudit(t *testing.T, path string) astaudit.Delta {
	t.Helper()
	t.Setenv("NOLANG_AUDIT_AST_WRITES", "1")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	tr := NewTranspiler(nil)
	tr.sourcePath = path
	tr.SetVetMode(true)
	// Vet mode deliberately stops before HIR/MIR emission, which keeps the
	// gate fast enough to run over the corpus in CI while still covering every
	// assembly pass (all of them run before the vet-mode return).
	if _, err := tr.Compile(string(src)); err != nil {
		// A compile failure is fine: assembly has already run (and already
		// written) by the time most of these errors are produced, and the
		// audit is taken on the way out via defer.
		t.Logf("%s: compile error: %v", path, err)
	}
	return tr.ASTAuditDelta()
}

// collectASTWriteCensus runs the corpus once and returns the distinct
// (node type, field) write keys with their counts.
func collectASTWriteCensus(t *testing.T, corpus []string) astaudit.Census {
	t.Helper()
	root := repoRoot(t)
	census := astaudit.Census{}
	for _, rel := range corpus {
		for _, w := range runASTAudit(t, filepath.Join(root, "..", rel)).Writes {
			census.Add(w)
		}
	}
	return census
}

// TestASTWriteInventoryPinned is the S5 gate. It measures which AST node
// fields assembly writes after ParseProgram (design doc §1.4) and pins the
// KEY SET. `go test -v` prints the inventory verbatim so the pin can be
// regenerated; a drift fails with want/got side by side.
func TestASTWriteInventoryPinned(t *testing.T) {
	census := collectASTWriteCensus(t, astWriteCorpus)
	got := census.Keys()
	for _, k := range got {
		t.Logf("%s\t%d", k, census[k])
	}
	want := pinnedASTWrites()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		var b strings.Builder
		b.WriteString("AST write inventory drifted.\n")
		b.WriteString("A key DISAPPEARED: a write site was removed (S5 progress) — update astWriteInventorySeed and §13.5.\n")
		b.WriteString("A key APPEARED: either a new AST write (a §1.4 violation, fix it) or the corpus must be extended.\n\n")
		b.WriteString("--- want ---\n")
		b.WriteString(strings.Join(want, "\n"))
		b.WriteString("\n--- got ---\n")
		b.WriteString(strings.Join(got, "\n"))
		b.WriteString("\n--- counts ---\n")
		b.WriteString(census.Report())
		t.Fatalf("%s", b.String())
	}
}

// pinnedASTWrites returns the frozen inventory in sorted order.
func pinnedASTWrites() []string {
	out := append([]string(nil), astWriteInventorySeed...)
	sort.Strings(out)
	return out
}

// TestASTAuditGateIsWired guards against a vacuous census: if the gate stops
// observing (broken env var, unregistered roots), the pinned test would see
// zero keys and could be "fixed" by deleting the pin. This test pins the
// *observation* itself, with both a positive and a negative control.
func TestASTAuditGateIsWired(t *testing.T) {
	root := repoRoot(t)
	prog := filepath.Join(root, "..", "tests/set.no")

	// Positive control (S5 stage 6 revision): assembly now runs on working
	// copies (parser.CloneProgram), so a real compile is expected to observe
	// ZERO writes on parse outputs — that is the pin itself. The control here
	// must prove the OBSERVATION machinery still works: register a parsed
	// program as a root, mutate one field directly, and require the census to
	// see exactly that write. Without this, a broken gate (unregistered roots,
	// dead snapshots) would let the zero-key pin pass vacuously.
	t.Setenv("NOLANG_AUDIT_AST_WRITES", "1")
	tr := NewTranspiler(nil)
	l := lexer.New("f = () () {\n    x = 1\n}\n")
	synth := parser.New(l).ParseProgram()
	tr.auditRegister("synthetic", synth)
	fd, ok := synth.Statements[0].(*parser.FunctionDefinition)
	if !ok {
		t.Fatalf("synthetic Statements[0] = %T, want *parser.FunctionDefinition", synth.Statements[0])
	}
	fd.Name = "f_mangled"
	tr.collectASTAudit()
	d := tr.ASTAuditDelta()
	found := false
	for _, w := range d.Writes {
		if astaudit.Key(w) == "*parser.FunctionDefinition.Name" {
			found = true
		}
	}
	if !found {
		t.Fatalf("gate failed to observe a direct field write on a registered root: %+v", d.Writes)
	}

	// Negative control: with the gate off, nothing is collected at all. This
	// is both the zero-cost guarantee and proof that a passing pin test cannot
	// be produced by a silently disabled gate.
	t.Setenv("NOLANG_AUDIT_AST_WRITES", "")
	src, err := os.ReadFile(prog)
	if err != nil {
		t.Fatalf("read %s: %v", prog, err)
	}
	tr = NewTranspiler(nil)
	tr.sourcePath = prog
	tr.SetVetMode(true)
	if _, err := tr.Compile(string(src)); err != nil {
		t.Logf("compile error: %v", err)
	}
	if got := tr.ASTAuditDelta(); !got.Empty() {
		t.Fatalf("gate collected %+v while disabled", got)
	}
	if got := tr.ASTAuditByRoot(); len(got) != 0 {
		t.Fatalf("gate registered %d roots while disabled", len(got))
	}
}
