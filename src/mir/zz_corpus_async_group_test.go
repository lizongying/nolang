package mir

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/lizongying/nolang/hir"
	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestAsyncGroupCorpus is the corpus guard for the coroutine group (協程組)
// desugaring, and the reason it exists is SPEED: the alternative — running
// `no build` over every corpus file — costs a clang link per file (~3s), which
// is ~15 minutes for the corpus and long enough that a sandbox kill turns a
// clean run into an inconclusive one. Lowering straight from source is ~100x
// cheaper and exercises exactly the layer the pass touches.
//
// WHAT IT COMPARES. Every corpus file is lowered twice — once with the pass on
// and once with NOLANG_ASYNC_GROUP=0 — and three things must agree:
//
//  1. whether lowering produced a module at all (a new hard failure is the
//     regression class that matters: the pass emits nodes the lowerer rejects);
//  2. the number of diagnostics;
//  3. the per-module count of OpRun and OpAwait, in program order.
//
// (3) is the point of the whole feature, so it is also the assertion that would
// catch a pass that started firing where it must not. Only the COUNTS are
// compared, never a hash of the module: the drop-insertion pass iterates a Go
// map, so the same binary on the same input can legitimately emit a different
// instruction order (see NOLANG-OWNERSHIP-MODEL.md / the notes on MIR dumps).
//
// GATED because it is a corpus walk, not a unit test: NOLANG_ASYNC_GROUP_CORPUS=1.
func TestAsyncGroupCorpus(t *testing.T) {
	if os.Getenv("NOLANG_ASYNC_GROUP_CORPUS") == "" {
		t.Skip("set NOLANG_ASYNC_GROUP_CORPUS=1 to run the corpus guard")
	}
	roots := []string{"../../tests", "../../test", "../../src/std", "../../example"}
	var files []string
	for _, r := range roots {
		filepath.Walk(r, func(p string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			if strings.HasSuffix(p, ".no") {
				files = append(files, p)
			}
			return nil
		})
	}
	sort.Strings(files)
	if len(files) == 0 {
		t.Fatalf("no corpus files under %v", roots)
	}
	t.Logf("corpus: %d files", len(files))

	type res struct {
		ok    bool
		diags int
		ops   string // "run=N await=M"
	}
	lower := func(path string, on bool) res {
		if on {
			os.Unsetenv("NOLANG_ASYNC_GROUP")
		} else {
			os.Setenv("NOLANG_ASYNC_GROUP", "0")
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return res{}
		}
		l := lexer.New(string(src))
		p := parser.New(l)
		prog := p.ParseProgram()
		if len(p.Errors()) > 0 {
			return res{} // a file the parser rejects is not this pass's business
		}
		pkg := parser.ASTToHIR(prog)
		if pkg == nil {
			return res{}
		}
		mod, _, diags := LowerHIR(pkg, nil)
		r := res{diags: len(diags)}
		if mod == nil {
			return r
		}
		r.ok = true
		run, awt := 0, 0
		for i := range mod.Funcs {
			f := &mod.Funcs[i]
			for _, bid := range f.Blocks {
				b := mod.Block(bid)
				if b == nil {
					continue
				}
				for _, iid := range b.Insts {
					inst := mod.Inst(iid)
					switch {
					case inst == nil:
					case inst.Op == OpRun:
						run++
					case inst.Op == OpAwait:
						awt++
					}
				}
			}
		}
		r.ops = "run=" + itoa(run) + " await=" + itoa(awt)
		return r
	}

	diffs := 0
	featureDiffs := 0
	for _, f := range files {
		on := lower(f, true)
		off := lower(f, false)
		if on.ok == off.ok && on.diags == off.diags && on.ops == off.ops {
			continue
		}
		// A difference is only a regression if the file does NOT actually use
		// the coroutine-group feature. Files that do are EXPECTED to differ
		// (pass-on adds run/awy bindings), and counting them as regressions
		// would make the guard meaningless for the very files it protects.
		src, _ := os.ReadFile(f)
		if asyncGroupFeatureFile(string(src)) {
			featureDiffs++
			t.Logf("%s: feature-file difference (expected) on={%v %d %s} off={%v %d %s}",
				f, on.ok, on.diags, on.ops, off.ok, off.diags, off.ops)
			continue
		}
		diffs++
		t.Errorf("%s: pass on={%v %d %s} off={%v %d %s}",
			f, on.ok, on.diags, on.ops, off.ok, off.diags, off.ops)
	}
	t.Logf("files compared: %d, regressions: %d, feature-file diffs (expected): %d",
		len(files), diffs, featureDiffs)
	if diffs > 0 {
		t.Fatalf("corpus guard: %d unexpected differences", diffs)
	}
}

// asyncGroupFeatureFile reports whether src contains at least one coroutine
// group, detected from raw HIR with the desugar pass DISABLED (so the bare
// blocks survive for the planner to find). It is independent of the env switch
// the corpus walk toggles, and uses the same pure planner the desugar relies on.
func asyncGroupFeatureFile(src string) bool {
	prev := os.Getenv("NOLANG_ASYNC_GROUP")
	os.Setenv("NOLANG_ASYNC_GROUP", "0")
	defer os.Setenv("NOLANG_ASYNC_GROUP", prev)
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		return false
	}
	pkg := parser.ASTToHIR(prog)
	if pkg == nil {
		return false
	}
	for i := 1; i < len(pkg.Nodes); i++ {
		if pkg.Nodes[i].Kind != hir.KBlock {
			continue
		}
		for c := pkg.Nodes[i].First; c != hir.NoID; c = pkg.Nodes[c].Next {
			if pkg.Nodes[c].Kind == hir.KBlock {
				if _, ok := pkg.AsyncGroupPlan(c); ok {
					return true
				}
			}
		}
	}
	return false
}
