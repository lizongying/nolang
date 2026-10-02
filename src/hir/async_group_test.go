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

// shapeOf renders the statements of the FIRST bare block found inside fn as
// compact tokens: "run:<handle>", "awy:<target>", or the node kind name for
// anything else. Order is the point of the assertion, so a flat ordered list is
// the right observable.
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
	// A bare block is a KBlock whose parent is a KBlock (fn bodies hang off a
	// KSlot), which is exactly the rule the pass uses.
	group := hir.NoID
	for _, c := range pkg.Children(body) {
		if cn := pkg.Node(c); cn != nil && cn.Kind == hir.KBlock {
			group = c
			break
		}
	}
	if group == hir.NoID {
		t.Fatalf("no bare block inside fn %q", fn)
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

const groupWorkSrc = `work-async = (n i64) (r i64) {
    r = n
}
`

func wantShape(t *testing.T, got []string, want ...string) {
	t.Helper()
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("group shape = [%s], want [%s]", strings.Join(got, "|"), strings.Join(want, "|"))
	}
}

// TestGroupIndependentSpawnsThenAwaits: nothing in the group reads another
// statement's result, so both spawns are emitted before either await.
func TestGroupIndependentSpawnsThenAwaits(t *testing.T) {
	src := groupWorkSrc + `main = () {
    r1 i64
    r2 i64
    {
        r1 = work-async(1)
        r2 = work-async(2)
    }
    print(r1)
}
`
	wantShape(t, shapeOf(t, hirOf(t, src), "main"),
		"run:__ag0", "run:__ag1", "awy:r1", "awy:r2")
}

// TestGroupDependencyDegradesToAwait: `r2 = work-async(r1)` reads r1, so r1 is
// awaited before r2 is launched. This is the "退化成 await" case.
func TestGroupDependencyDegradesToAwait(t *testing.T) {
	src := groupWorkSrc + `main = () {
    r1 i64
    r2 i64
    {
        r1 = work-async(1)
        r2 = work-async(r1)
    }
    print(r2)
}
`
	wantShape(t, shapeOf(t, hirOf(t, src), "main"),
		"run:__ag0", "awy:r1", "run:__ag1", "awy:r2")
}

// TestGroupDiscardedResultStillAwaited: a bare `-async` call statement with no
// binding still gets both a spawn and an await — an un-awaited task leaks its
// argument buffer.
func TestGroupDiscardedResultStillAwaited(t *testing.T) {
	src := `side-async = (n i64) {
    print(n)
}
main = () {
    {
        side-async(7)
    }
}
`
	wantShape(t, shapeOf(t, hirOf(t, src), "main"), "run:__ag0", "awy:")
}

// TestGroupWithoutAsyncCallIsLeftAlone: no `-async` callee means no group. The
// pass must report 0 and leave the block as written — this is the property that
// makes the whole feature a no-op for the existing corpus.
func TestGroupWithoutAsyncCallIsLeftAlone(t *testing.T) {
	pkg := hirOf(t, `work = (n i64) (r i64) {
    r = n
}
main = () {
    r1 i64
    {
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
    r1 i64
    {
        r1 = work-async(1)
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

// TestGroupKillSwitch: NOLANG_ASYNC_GROUP=0 restores the untouched HIR. It is
// the A/B lever for validating the pass across the corpus without building a
// second binary.
func TestGroupKillSwitch(t *testing.T) {
	t.Setenv("NOLANG_ASYNC_GROUP", "0")
	src := groupWorkSrc + `main = () {
    r1 i64
    {
        r1 = work-async(1)
    }
}
`
	wantShape(t, shapeOf(t, hirOf(t, src), "main"), "let")
}
