package mir

import (
	"fmt"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/lizongying/nolang/hir"
	"github.com/lizongying/nolang/lexer"
	nopkg "github.com/lizongying/nolang/package"
	"github.com/lizongying/nolang/parser"
)

// ---------------------------------------------------------------------------
// Platform-variant filtering (src/mir/platform.go, nodeMatchesPlatform)
//
// This is the area that produced the §13.3.13 ① script-mode leak: the platform
// filter was consulted by the top-level registration loop but NOT by
// synthesizeMainForTopLevel. Before this file existed the MIR package had no
// test at all for `#{mac-arm64}` / `#{win-amd64}` / ... handling — the whole
// mechanism was exercised only indirectly by the std sources, whose mac-amd64
// and mac-arm64 constants happen to hold equal values (std/fs.no O-CREAT
// 512/512, O-TRUNC 1024/1024), which made a wrong pick invisible.
//
// Every test derives the expected platform from the HOST (mirTargetPlatform)
// instead of hard-coding darwin/arm64, so the suite stays meaningful on any
// machine that has a key in package.PlatformKeys. The -target override
// (SetTargetPlatform) is covered by TestSetTargetPlatformOverridesHostFiltering
// at the bottom of this file.
// ---------------------------------------------------------------------------

// hostPlatformKey returns the annotation key denoting the platform the MIR
// backend lowers for (it lowers for the host; see mirTargetPlatform).
func hostPlatformKey(t *testing.T) string {
	t.Helper()
	k := nopkg.PlatformKeyFor(runtime.GOOS, runtime.GOARCH)
	if k == "" {
		t.Skipf("host %s/%s has no entry in package.PlatformKeys; nothing to assert here", runtime.GOOS, runtime.GOARCH)
	}
	return k
}

// otherPlatformKey returns a key that is definitely NOT the host's. A real
// desktop platform is preferred over `js` so the generated program stays
// representable.
func otherPlatformKey(t *testing.T, host string) string {
	t.Helper()
	var others []string
	for k := range nopkg.PlatformKeys {
		if k != host {
			others = append(others, k)
		}
	}
	sort.Strings(others)
	for _, k := range others {
		if nopkg.PlatformKeys[k].GOOS != "js" {
			return k
		}
	}
	if len(others) == 0 {
		t.Skip("package.PlatformKeys defines only the host key; no mismatching variant to test")
	}
	return others[0]
}

// parseHIR parses src and lowers it to HIR, mirroring the front-end sequence in
// build/transpiler.go (ASTToHIRWithMap + PopulateInferredTypes). Both steps are
// required: lowering top-level statements into the synthetic `main` reads the
// inferred-type side table, and without it the script path produces an empty
// module.
func parseHIR(t *testing.T, src string) *hir.Package {
	t.Helper()
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v\n--- src ---\n%s", errs, src)
	}
	pkg, idMap := parser.ASTToHIRWithMap(prog)
	if pkg == nil {
		t.Fatal("ASTToHIRWithMap returned nil")
	}
	parser.PopulateInferredTypes(pkg, idMap)
	return pkg
}

func lowerHIR(t *testing.T, src string) *Module {
	t.Helper()
	mod, _, _ := LowerHIR(parseHIR(t, src), nil)
	if mod == nil {
		t.Fatal("LowerHIR returned nil")
	}
	return mod
}

func globalNames(mod *Module) []string {
	out := make([]string, 0, len(mod.Globals))
	for _, g := range mod.Globals {
		out = append(out, fmt.Sprintf("%s=%q", g.Name, g.ConstText))
	}
	return out
}

// hasConstInt reports whether the module materialised an integer constant with
// value v (OpConst carries the literal payload in Inst.Int; string constants
// leave Str non-empty).
func hasConstInt(mod *Module, v int64) bool {
	for i := range mod.Insts {
		in := &mod.Insts[i]
		if in.Op == OpConst && in.Str == "" && in.Int == v {
			return true
		}
	}
	return false
}

func sortedPlatformKeys() []string {
	keys := make([]string, 0, len(nopkg.PlatformKeys))
	for k := range nopkg.PlatformKeys {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// --- the key table itself ---------------------------------------------------

func TestPlatformKeyForRoundTrip(t *testing.T) {
	for _, key := range sortedPlatformKeys() {
		m := nopkg.PlatformKeys[key]
		if got := nopkg.PlatformKeyFor(m.GOOS, m.GOARCH); got != key {
			t.Errorf("PlatformKeyFor(%q, %q) = %q, want %q", m.GOOS, m.GOARCH, got, key)
		}
	}
	// A platform outside the table must not resolve to some other key.
	if got := nopkg.PlatformKeyFor("freebsd", "arm64"); got != "" {
		t.Errorf("PlatformKeyFor(freebsd, arm64) = %q, want \"\" (unsupported platform)", got)
	}
}

// TestNodeMatchesPlatformForEveryKey pins the whole decision table: exactly the
// host's key is kept, every other key is dropped.
func TestNodeMatchesPlatformForEveryKey(t *testing.T) {
	host := hostPlatformKey(t)
	for _, key := range sortedPlatformKeys() {
		src := fmt.Sprintf("#{%s}\nV = 1\n", key)
		pkg := parseHIR(t, src)
		if len(pkg.Top) == 0 {
			t.Fatalf("#{%s}: no top-level node was recorded", key)
		}
		want := key == host
		if got := nodeMatchesPlatform(pkg, pkg.Top[0]); got != want {
			t.Errorf("nodeMatchesPlatform(#{%s}) = %v, want %v (host key is %q)", key, got, want, host)
		}
	}
}

// TestNodeMatchesPlatformKeepsUnannotatedNodes: an absent annotation, or an
// annotation that is NOT a platform key, must never filter a node out. The
// valued form matters because `#{overflow = wrap}` shares the annotation syntax
// with the bare platform flags — mistaking one for the other would silently
// drop half the program.
func TestNodeMatchesPlatformKeepsUnannotatedNodes(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"no annotation", "V = 1\n"},
		{"valued annotation", "#{overflow = wrap}\nV = 1\n"},
		{"valued annotation, other key", "#{overflow = clamp0}\nV = 1\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pkg := parseHIR(t, tc.src)
			if len(pkg.Top) == 0 {
				t.Fatal("no top-level node recorded")
			}
			if !nodeMatchesPlatform(pkg, pkg.Top[0]) {
				t.Errorf("node with %s must be kept, but was filtered out", tc.name)
			}
		})
	}
}

// --- script mode (no `fn main`) — the §13.3.13 ① regression ---------------

// TestScriptModePlatformVariantKeepsHostValue is the regression for the leak
// fixed in round 39: with no explicit `fn main`, a filtered-out variant was
// still inlined as a script local by synthesizeMainForTopLevel and its
// wrong-platform value overwrote the global registered by the matching variant.
//
// Note on the oracle: the legacy backend does NOT filter here (measured on the
// pre-deletion binary at 47b6cad: it prints the LAST variant, 222/600). So this
// is a deliberate semantic fix, not a restoration of legacy behaviour —
// filtering is what the annotation is documented to mean (docs/docs/lang/
// syntax.md). With an explicit `fn main` both backends filter identically; see
// TestTopLevelVariantWithExplicitMainKeepsHostValue.
func TestScriptModePlatformVariantKeepsHostValue(t *testing.T) {
	host := hostPlatformKey(t)
	other := otherPlatformKey(t, host)
	src := fmt.Sprintf("#{%s}\nPV = 111\n\n#{%s}\nPV = 222\n\nprint(PV)\n", host, other)

	mod := lowerHIR(t, src)
	if len(mod.Globals) != 1 {
		t.Fatalf("expected exactly 1 global (the host variant), got %d: %v", len(mod.Globals), globalNames(mod))
	}
	if g := mod.Globals[0]; !strings.Contains(g.ConstText, "111") {
		t.Errorf("global %q holds %q, want the HOST variant value 111 (was the wrong-platform variant inlined?)", g.Name, g.ConstText)
	}
	if hasConstInt(mod, 222) {
		t.Errorf("the filtered-out variant (222) still materialised a value; MIR dump:\n%s", mod.String())
	}
}

// TestScriptModeLoneWrongPlatformVariantIsDropped: a variant for some other
// platform must not contribute a value at all when it is the only definition.
func TestScriptModeLoneWrongPlatformVariantIsDropped(t *testing.T) {
	host := hostPlatformKey(t)
	other := otherPlatformKey(t, host)
	src := fmt.Sprintf("#{%s}\nPV = 222\n\nprint(PV)\n", other)

	mod := lowerHIR(t, src)
	if len(mod.Globals) != 0 {
		t.Errorf("a lone non-host variant must not register a global, got %v", globalNames(mod))
	}
	if hasConstInt(mod, 222) {
		t.Errorf("a lone non-host variant must not materialise a value; MIR dump:\n%s", mod.String())
	}
	_ = host
}

// --- explicit `fn main`: both backends agree here --------------------------

func TestTopLevelVariantWithExplicitMainKeepsHostValue(t *testing.T) {
	host := hostPlatformKey(t)
	other := otherPlatformKey(t, host)
	src := fmt.Sprintf(
		"#{%s}\nPV = 111\n\n#{%s}\nPV = 222\n\nmain = () () {\n  print(PV)\n  return\n}\nmain()\n",
		host, other)

	mod := lowerHIR(t, src)
	if len(mod.Globals) != 1 {
		t.Fatalf("expected exactly 1 global (the host variant), got %d: %v", len(mod.Globals), globalNames(mod))
	}
	if g := mod.Globals[0]; !strings.Contains(g.ConstText, "111") {
		t.Errorf("global %q holds %q, want the host variant 111", g.Name, g.ConstText)
	}
	if hasConstInt(mod, 222) {
		t.Errorf("the filtered-out variant (222) still materialised a value; MIR dump:\n%s", mod.String())
	}
}

// --- known limitation, shared with the (removed) legacy backend ------------

// TestFunctionBodyPlatformVariantIsNotFiltered documents a LIMITATION rather
// than a regression.
//
// Platform annotations on statements INSIDE a function body are not filtered by
// either backend. Verified against the pre-deletion legacy binary (47b6cad) on
// arm64 macOS: the two sources below print 222 (last variant wins) under BOTH
// backends, regardless of host.
//
//	main = () () { #{mac-arm64} PV = 111  #{linux-amd64} PV = 222  print(PV) }
//
// The statement-level filter is only wired at top level (registration loop +
// synthesizeMainForTopLevel). Both variants therefore survive, and the later
// assignment wins. This test exists so the limitation is visible in the test
// suite; fixing it means deleting this expectation and asserting the host value
// instead.
func TestFunctionBodyPlatformVariantIsNotFiltered(t *testing.T) {
	host := hostPlatformKey(t)
	other := otherPlatformKey(t, host)
	src := fmt.Sprintf(
		"main = () () {\n  #{%s}\n  PV = 111\n  #{%s}\n  PV = 222\n  print(PV)\n  return\n}\nmain()\n",
		host, other)

	mod := lowerHIR(t, src)
	// KNOWN LIMITATION: both variants are lowered (legacy behaves identically).
	if !hasConstInt(mod, 111) || !hasConstInt(mod, 222) {
		t.Fatalf("expected BOTH in-body variants to be lowered (known limitation: no in-body filtering); "+
			"111 present=%v 222 present=%v\nMIR dump:\n%s",
			hasConstInt(mod, 111), hasConstInt(mod, 222), mod.String())
	}
	if len(mod.Globals) != 0 {
		t.Errorf("in-body lets must not be materialised as globals, got %v", globalNames(mod))
	}
}

// --- the -target override (SetTargetPlatform) ------------------------------

// TestSetTargetPlatformOverridesHostFiltering is the cross-compilation
// regression: a darwin binary built on a Linux runner used to keep the LINUX
// variants of #{linux-*}/#{mac-*} pairs (the host always won) and the C shims
// emitted glibc symbols (__errno_location, stdin) into the Mach-O module —
// linking failed with "undefined symbol: ___errno_location" and
// "undefined symbol: _stdin". After the fix, nodeMatchesPlatform (and the
// targetGOOS/targetGOARCH consumers built on the same mirTargetPlatform) follow
// the declared target instead of the host.
func TestSetTargetPlatformOverridesHostFiltering(t *testing.T) {
	host := hostPlatformKey(t)
	other := otherPlatformKey(t, host)
	m := nopkg.PlatformKeys[other]
	defer SetTargetPlatform("", "") // restore the host fallback for later tests
	SetTargetPlatform(m.GOOS, m.GOARCH)

	// The filter decision must flip relative to the host default.
	pkg := parseHIR(t, fmt.Sprintf("#{%s}\nV = 1\n", host))
	if nodeMatchesPlatform(pkg, pkg.Top[0]) {
		t.Errorf("#{%s} must be filtered out when the target is %s/%s", host, m.GOOS, m.GOARCH)
	}
	pkg = parseHIR(t, fmt.Sprintf("#{%s}\nV = 2\n", other))
	if !nodeMatchesPlatform(pkg, pkg.Top[0]) {
		t.Errorf("#{%s} must be kept when the target is %s/%s", other, m.GOOS, m.GOARCH)
	}

	// End to end through LowerHIR: the TARGET variant's value survives, the
	// host variant's does not — the exact opposite of
	// TestScriptModePlatformVariantKeepsHostValue on the same machine.
	src := fmt.Sprintf("#{%s}\nPV = 111\n\n#{%s}\nPV = 222\n\nprint(PV)\n", host, other)
	mod := lowerHIR(t, src)
	if len(mod.Globals) != 1 {
		t.Fatalf("expected exactly 1 global (the target variant), got %d: %v", len(mod.Globals), globalNames(mod))
	}
	if g := mod.Globals[0]; !strings.Contains(g.ConstText, "222") {
		t.Errorf("global %q holds %q, want the TARGET variant value 222", g.Name, g.ConstText)
	}
	if hasConstInt(mod, 111) {
		t.Errorf("the host variant (111) leaked into a %s/%s build; MIR dump:\n%s", m.GOOS, m.GOARCH, mod.String())
	}
}

// TestSetTargetPlatformEmptyFallsBackToHost: an unset or partially-resolved
// target (native builds, unparseable triples) must keep the pre-fix host
// behavior — exactly the host variant survives.
func TestSetTargetPlatformEmptyFallsBackToHost(t *testing.T) {
	host := hostPlatformKey(t)
	defer SetTargetPlatform("", "")
	SetTargetPlatform("", "")
	goos, goarch := mirTargetPlatform()
	if goos != runtime.GOOS || goarch != runtime.GOARCH {
		t.Errorf("mirTargetPlatform() = %s/%s after reset, want host %s/%s", goos, goarch, runtime.GOOS, runtime.GOARCH)
	}
	// A partial target must fall back too (empty component is never a real GOOS/GOARCH).
	SetTargetPlatform("", "amd64")
	if goos, _ := mirTargetPlatform(); goos != runtime.GOOS {
		t.Errorf("partial target kept goarch but goos = %q, want host %q", goos, runtime.GOOS)
	}
	src := fmt.Sprintf("#{%s}\nPV = 111\n\nprint(PV)\n", host)
	mod := lowerHIR(t, src)
	if len(mod.Globals) != 1 || !strings.Contains(mod.Globals[0].ConstText, "111") {
		t.Errorf("host fallback lost the host variant: %v", globalNames(mod))
	}
}
