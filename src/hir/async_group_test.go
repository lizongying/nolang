// Package hir_test is an external test package because these tests have to
// import parser, which depends on hir (see golden_test.go).
package hir_test

import (
	"os"
	"strings"
	"testing"

	"github.com/lizongying/nolang/hir"
	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// These tests pin the coroutine group (協程組) desugaring at the HIR level —
// the layer the pass actually rewrites. src/mir/async_group_test.go covers what
// the rewrite means in MIR; this file covers the rewrite itself, plus the two
// properties only visible here: that a group with nothing to spawn is left
// alone, and that a synthesized handle binding never reuses a name already in
// the package.

// hirOf parses src and lowers it to HIR with the group pass enabled.
func hirOf(t *testing.T, src string) *hir.Package {
	t.Helper()
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
	return pkg
}

// shapeOf renders the statements of the FIRST `awy { ... }` group found inside
// fn as compact tokens: "run:<handle>", "awy:<target>", or the node kind name
// for anything else. Order is the point of the assertion, so a flat ordered
// list is the right observable.
func shapeOf(t *testing.T, pkg *hir.Package, fn string) []string {
	t.Helper()
	var body int32 = hir.NoID
	for _, id := range pkg.Top {
		n := pkg.Node(id)
		if n == nil || n.Kind != hir.KFuncDef || pkg.Str(n.S) != fn {
			continue
		}
		for _, c := range pkg.Children(id) {
			if cn := pkg.Node(c); cn != nil && cn.Kind == hir.KSlot && pkg.Str(cn.S) == "body" {
				body = cn.First
			}
		}
	}
	if body == hir.NoID {
		t.Fatalf("no body found for fn %q", fn)
	}
	// A coroutine group is a KBlock carrying FlagAwaitGroup (emitted by
	// `awy { ... }`), which is exactly the rule the pass uses.
	group := hir.NoID
	for _, c := range pkg.Children(body) {
		if cn := pkg.Node(c); cn != nil && cn.Kind == hir.KBlock &&
			cn.Flags&hir.FlagAwaitGroup != 0 {
			group = c
			break
		}
	}
	if group == hir.NoID {
		t.Fatalf("no awy group inside fn %q", fn)
	}
	var out []string
	for _, s := range pkg.Children(group) {
		st := pkg.Node(s)
		if st == nil {
			continue
		}
		switch {
		case st.Kind == hir.KLet && st.First != hir.NoID && pkg.Node(st.First).Kind == hir.KRun:
			out = append(out, "run:"+pkg.Str(st.S))
		case st.Kind == hir.KLet && st.First != hir.NoID && pkg.Node(st.First).Kind == hir.KAwait:
			out = append(out, "awy:"+pkg.Str(st.S))
		case st.Kind == hir.KExprStmt && st.First != hir.NoID && pkg.Node(st.First).Kind == hir.KAwait:
			out = append(out, "awy:")
		default:
			out = append(out, st.Kind.String())
		}
	}
	return out
}

// `co` only spawns a PROCEDURE (no declared result); results travel through a
// channel, so the group statements below never bind a spawn result.
const groupWorkSrc = `work = (n i64) {
    print(n)
}
`

// normalizeShape strips the synthesized handle suffix from "run:__agN" /
// "awy:rN" tokens, keeping only the run/awy pattern. Exact handle names are
// checked separately by TestGroupHandleNameAvoidsACollision; here the property
// under test is the SPAWN/AWAIT ORDER, and handle numbering shifts whenever
// MonomorphizeGo synthesizes an extra -async variant.
func normalizeShape(shape []string) []string {
	out := make([]string, len(shape))
	for i, s := range shape {
		if j := strings.IndexByte(s, ':'); j >= 0 {
			out[i] = s[:j]
		} else {
			out[i] = s
		}
	}
	return out
}

func wantShape(t *testing.T, got []string, want ...string) {
	t.Helper()
	gotNorm := normalizeShape(got)
	wantNorm := normalizeShape(want)
	if strings.Join(gotNorm, "|") != strings.Join(wantNorm, "|") {
		t.Errorf("group shape = [%s], want [%s]", strings.Join(got, "|"), strings.Join(want, "|"))
	}
}

// TestGroupIndependentSpawnsThenAwaits: nothing in the group reads another
// statement's result, so both spawns are emitted before either await.
func TestGroupIndependentSpawnsThenAwaits(t *testing.T) {
	src := groupWorkSrc + `main = () {
    awy {
        co work(1)
        co work(2)
    }
}
`
	wantShape(t, shapeOf(t, hirOf(t, src), "main"),
		"run:__ag0", "run:__ag1", "awy:", "awy:")
}

// TestGroupBarrierBetweenSpawns: any statement that is not an `-async` call is
// a barrier, so the spawn before it lands before it runs and the spawn after it
// cannot start earlier. `print` is the cheapest such statement.
//
// HISTORY: this slot used to hold the "退化成 await" case (`r2 = co work(r1)`,
// where r2's argument reads r1's spawn result). `co` no longer produces a value,
// so that shape is not expressible any more; a dependency is now written as a
// channel read, which is a barrier for the same reason and lowers the same way
// (pinned in src/mir/async_group_test.go, where only run/await are counted and
// a receive loop does not disturb the shape list).
func TestGroupBarrierBetweenSpawns(t *testing.T) {
	src := groupWorkSrc + `main = () {
    awy {
        co work(1)
        print(9)
        co work(2)
    }
}
`
	wantShape(t, shapeOf(t, hirOf(t, src), "main"),
		"run:__ag0", "awy:", "expr-stmt", "run:__ag1", "awy:")
}

// TestGroupDiscardedResultStillAwaited: a `co` call statement with no binding
// still gets both a spawn and an await — an un-awaited task leaks its argument
// buffer.
func TestGroupDiscardedResultStillAwaited(t *testing.T) {
	src := `side = (n i64) {
    print(n)
}
main = () {
    awy {
        co side(7)
    }
}
`
	wantShape(t, shapeOf(t, hirOf(t, src), "main"), "run:__ag0", "awy:")
}

// TestGroupWithoutAsyncCallIsLeftAlone: an `awy` block with no `co` spawn
// means nothing to desugar. The pass must report 0 and leave the block as
// written — this is the property that keeps the pass a no-op for such groups.
func TestGroupWithoutAsyncCallIsLeftAlone(t *testing.T) {
	pkg := hirOf(t, `work = (n i64) (r i64) {
    r = n
}
main = () {
    r1 i64
    awy {
        r1 = work(1)
    }
    print(r1)
}
`)
	if n := hir.DesugarAsyncGroups(pkg); n != 0 {
		t.Errorf("DesugarAsyncGroups rewrote %d group(s), want 0", n)
	}
	wantShape(t, shapeOf(t, pkg, "main"), "let")
}

// TestGroupHandleNameAvoidsACollision: the pass must not name a handle after a
// binding that already exists. `__ag0` cannot be written in Nolang source
// (the lexer rejects a leading `__`), so the decoy is injected straight into
// the arena — which is also how parser/lowering.go's own synthetic names
// (`__cap_1`, `__opt_1`) arrive.
func TestGroupHandleNameAvoidsACollision(t *testing.T) {
	// Built with the pass OFF, so the group is still in its source form when
	// the decoy goes in. (Running the pass twice would be a weaker test: it is
	// idempotent, so the second call would find no `-async` call and rewrite
	// nothing — which is a property worth having, but not the one under test.)
	t.Setenv("NOLANG_ASYNC_GROUP", "0")
	pkg := hirOf(t, groupWorkSrc+`main = () {
    awy {
        co work(1)
    }
}
`)
	// Inject a detached binding named __ag0 so collectNames has to avoid it.
	sid := int32(len(pkg.Strings))
	pkg.Strings = append(pkg.Strings, "__ag0")
	nid := int32(len(pkg.Nodes))
	pkg.Nodes = append(pkg.Nodes, hir.Node{Kind: hir.KLet, Id: nid, S: sid})

	os.Unsetenv("NOLANG_ASYNC_GROUP")
	hir.DesugarAsyncGroups(pkg)
	shape := shapeOf(t, pkg, "main")
	if len(shape) == 0 || !strings.HasPrefix(shape[0], "run:") {
		t.Fatalf("group was not rewritten: shape=%v", shape)
	}
	if shape[0] == "run:__ag0" {
		t.Errorf("the handle reused an existing name %q; shape=%v", "__ag0", shape)
	}
}

// TestFlowArmCoUsesJoinSet: a `co` inside a loop/if arm cannot use a static
// group-tail slot (the arm may run zero or many times). It must be rewritten to
// `run` + `__join-push`, with `__join-enter`/`__join-leave` around the group and
// NO `awy` at all — awaiting inline would restore the old serial #108 fallback.
func TestFlowArmCoUsesJoinSet(t *testing.T) {
	src := `work = (n i64) {
    print(n)
}
main = () {
    i i64 = 0
    awy {
        for i < 2 {
            i == 0 -> {
                co work(1)
            }
            i = i + 1
        }
    }
}
`
	pkg := hirOf(t, src)
	var runs, awaits int
	calls := map[string]int{}
	var walk func(id int32)
	seen := map[int32]bool{}
	walk = func(id int32) {
		if id == hir.NoID || seen[id] {
			return
		}
		seen[id] = true
		n := pkg.Node(id)
		if n == nil {
			return
		}
		switch n.Kind {
		case hir.KRun:
			runs++
		case hir.KAwait:
			awaits++
		case hir.KCall:
			for _, c := range pkg.Children(id) {
				slot := pkg.Node(c)
				if slot == nil || slot.Kind != hir.KSlot || pkg.Str(slot.S) != "fn" {
					continue
				}
				if fn := pkg.Node(slot.First); fn != nil && fn.Kind == hir.KIdent {
					calls[pkg.Str(fn.S)]++
				}
			}
		}
		for _, c := range pkg.Children(id) {
			walk(c)
		}
	}
	for _, top := range pkg.Top {
		if n := pkg.Node(top); n != nil && n.Kind == hir.KFuncDef && pkg.Str(n.S) == "main" {
			walk(top)
		}
	}
	if runs != 1 {
		t.Errorf("run count = %d, want 1", runs)
	}
	if awaits != 0 {
		t.Errorf("await count = %d, want 0 (inline await is the serial fallback)", awaits)
	}
	for _, name := range []string{"__join-enter", "__join-push", "__join-leave"} {
		if calls[name] != 1 {
			t.Errorf("%s count = %d, want 1 (calls=%v)", name, calls[name], calls)
		}
	}
}

// TestFlowArmCoJoinSetAllControlFlowKinds: every construct that can execute an
// arm zero or many times must route its `co` through the join-set, not just
// `for`. KTypeMatch and KChanIter desugar differently from KIf, so a bug in
// controlFlowKind would otherwise only show up for one of them at runtime.
func TestFlowArmCoJoinSetAllControlFlowKinds(t *testing.T) {
	const prelude = `work = (n i64) {
    print(n)
}
main = () {
    i i64 = 0
    k ?i64 = 1
    ch chan[i64] = with-cap(1)
    ch <- 9
    ch.close()
    awy {
`
	const postlude = `    }
}
`
	cases := map[string]string{
		"for": `        for i < 2 {
            co work(1)
        }
`,
		"if": `        i == 0 -> {
            co work(1)
        }
`,
		"match": `        k: {
            ok -> { co work(1) }
            -> { }
        }
`,
		"chan-iter": `        <- ch: {
            ok -> { co work(1) }
            -> { }
        }
`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			pkg := hirOf(t, prelude+body+postlude)
			var runs, awaits, pushes int
			var walk func(int32)
			seen := map[int32]bool{}
			walk = func(id int32) {
				if id == hir.NoID || seen[id] {
					return
				}
				seen[id] = true
				n := pkg.Node(id)
				if n == nil {
					return
				}
				switch n.Kind {
				case hir.KRun:
					runs++
				case hir.KAwait:
					awaits++
				case hir.KCall:
					if n := pkg.Node(n.First); n != nil && n.Kind == hir.KSlot &&
						pkg.Str(n.S) == "fn" {
						if fn := pkg.Node(n.First); fn != nil && fn.Kind == hir.KIdent &&
							pkg.Str(fn.S) == "__join-push" {
							pushes++
						}
					}
				}
				for _, c := range pkg.Children(id) {
					walk(c)
				}
			}
			for _, top := range pkg.Top {
				if n := pkg.Node(top); n != nil && n.Kind == hir.KFuncDef && pkg.Str(n.S) == "main" {
					walk(top)
				}
			}
			// `match` / chan-iter desugar the arm into BOTH a `then` and a
			// `dot-val` slot; only one runs, but both must be rewritten or a
			// taken path could fall back to #108. So the invariant is
			// "every run has a push, and no run awaits inline".
			if runs < 1 || awaits != 0 || pushes != runs {
				t.Errorf("run/await/push = %d/%d/%d, want >=1/0/runs", runs, awaits, pushes)
			}
		})
	}
}

// TestGroupKillSwitch: NOLANG_ASYNC_GROUP=0 restores the untouched HIR. It is
// the A/B lever for validating the pass across the corpus without building a
// second binary.
func TestGroupKillSwitch(t *testing.T) {
	t.Setenv("NOLANG_ASYNC_GROUP", "0")
	src := groupWorkSrc + `main = () {
    awy {
        co work(1)
    }
}
`
	wantShape(t, shapeOf(t, hirOf(t, src), "main"), "expr-stmt")
}
