package mir

import (
	"strings"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// Tests for optPrunePrelude (src/mir/codegen.go).
//
// This pass is TEXTUAL and it DELETES code, which is the one combination where
// a silent mistake becomes a link error far from the cause. So the tests are
// split the same way the pass is:
//
//   - the text surgery (which bytes are a "define block", what happens when the
//     region is not shaped as expected), tested on hand-written IR where the
//     expected answer can be read off by eye;
//   - the liveness rule (what keeps a helper alive), tested with the transitive
//     case, because the interesting failure is not "it kept too little" — that
//     fails loudly at link time — but "it kept too much" and quietly saved
//     nothing.
//
// The end-to-end case at the bottom runs the real EmitLLVM, because a pass that
// is correct on a fixture but never invoked from the emitter saves nothing.
// ─────────────────────────────────────────────────────────────────────────────

// preludeFixture wraps `prelude` and `rest` into one IR string and reports the
// region boundaries the pass is given, so a test never has to count bytes.
func preludeFixture(prelude, rest string) (ir string, start, end int) {
	ir = prelude + rest
	return ir, 0, len(prelude)
}

// optIRSymbols returns every symbol the module defines, declares or defines as a
// global — i.e. everything an operand may legitimately reference.
//
// definedSymbols CANNOT be reused for this. It exists so codegen can reserve the
// names the prelude DEFINES, so it recognises only `define` headers and
// `@x = ...` globals; it has no `declare` case at all. Running a call line
// through it also returns nothing, because a call line begins with `%5 =`.
// Getting this wrong is not a compile error — it makes the resolution check
// below report every C function and every string global as missing, which is how
// the first version of this test failed.
func optIRSymbols(ir string) map[string]bool {
	out := map[string]bool{}
	scan := func(s string, from int) string {
		j := from
		for j < len(s) && isSymChar(s[j]) {
			j++
		}
		if j > from {
			return s[from:j]
		}
		return ""
	}
	for _, line := range strings.Split(ir, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "define"), strings.HasPrefix(trimmed, "declare"):
			at := strings.IndexByte(trimmed, '@')
			if at < 0 {
				continue
			}
			if name := scan(trimmed, at+1); name != "" {
				out[name] = true
			}
		case strings.HasPrefix(trimmed, "@"):
			name := scan(trimmed, 1)
			// Only `@name = ...` defines a global; anything else is a use.
			if name != "" && strings.HasPrefix(strings.TrimSpace(trimmed[1+len(name):]), "=") {
				out[name] = true
			}
		}
	}
	return out
}

func TestOptPrunePreludeDropsOnlyUnreferencedHelpers(t *testing.T) {
	prelude := `; runtime
declare void @llvm.memcpy(i8*, i8*, i64, i1)
@.dot = private constant [2 x i8] c".\00"

define void @dead_helper() {
  ret void
}

define void @live_helper() {
  call void @callee()
  ret void
}

define void @callee() {
  ret void
}

`
	rest := "define void @main() {\n  call void @live_helper()\n  ret void\n}\n"
	ir, start, end := preludeFixture(prelude, rest)

	got, removed := optPrunePrelude(ir, start, end)

	if removed != 1 {
		t.Errorf("removed %d definitions, want 1 (only @dead_helper is unreferenced)", removed)
	}
	for _, want := range []string{"@live_helper", "@callee", "@main"} {
		if !strings.Contains(got, want) {
			t.Errorf("%s was removed but is reachable from @main:\n%s", want, got)
		}
	}
	if strings.Contains(got, "@dead_helper") {
		t.Errorf("@dead_helper survived even though nothing references it:\n%s", got)
	}
	// The non-define text must survive byte for byte: a dropped `declare` or
	// global would break the module in a way no test above would notice.
	for _, want := range []string{"@llvm.memcpy", "@.dot = private constant", "; runtime"} {
		if !strings.Contains(got, want) {
			t.Errorf("non-define prelude text %q was dropped:\n%s", want, got)
		}
	}
	if !strings.HasSuffix(got, rest) {
		t.Errorf("the region after the prelude was modified:\n%s", got)
	}
}

// TestOptPrunePreludeClosesOverThePreludeItself is the "kept too much" half.
// @callee is mentioned NOWHERE outside the prelude — only inside @live_helper's
// body — so a pass that scanned only the user code would drop it and produce a
// module with an undefined symbol. The reverse must also hold: a helper whose
// only caller is itself dead has to go, or a prelude that calls itself in a
// cycle would never shrink.
func TestOptPrunePreludeClosesOverThePreludeItself(t *testing.T) {
	prelude := `define void @live_outer() {
  call void @live_inner()
  ret void
}

define void @live_inner() {
  ret void
}

define void @dead_outer() {
  call void @dead_inner()
  ret void
}

define void @dead_inner() {
  ret void
}

`
	rest := "define void @main() {\n  call void @live_outer()\n  ret void\n}\n"
	ir, start, end := preludeFixture(prelude, rest)

	got, removed := optPrunePrelude(ir, start, end)

	if removed != 2 {
		t.Errorf("removed %d definitions, want 2 (@dead_outer and its callee)", removed)
	}
	for _, want := range []string{"@live_outer", "@live_inner"} {
		if !strings.Contains(got, want) {
			t.Errorf("%s was removed but is reachable from @main:\n%s", want, got)
		}
	}
	for _, bad := range []string{"@dead_outer", "@dead_inner"} {
		if strings.Contains(got, bad) {
			t.Errorf("%s survived a chain that nothing outside reaches:\n%s", bad, got)
		}
	}
}

// A helper named by a global initializer is kept; one named only by a COMMENT is
// not.
//
// The two cases look identical to a naive text scan and are opposite in meaning:
// a global initializer is a real reference the linker must resolve, a comment is
// prose. Treating comments as references is not a harmless over-approximation —
// the prelude documents itself heavily, so it kept the entire async cluster and
// every utf8 helper alive on a one-line program (24 of 33 surviving
// definitions). This test pins the cut.
func TestOptPrunePreludeIgnoresCommentsButKeepsGlobals(t *testing.T) {
	prelude := `define void @only_in_a_comment() {
  ret void
}

define void @only_in_a_global() {
  ret void
}

`
	rest := "@.fnptr = private constant i8* bitcast (void ()* @only_in_a_global to i8*)\n" +
		"; see @only_in_a_comment for the rationale\n" +
		"define void @main() {\n  ret void\n}\n"
	ir, start, end := preludeFixture(prelude, rest)

	got, removed := optPrunePrelude(ir, start, end)

	if removed != 1 {
		t.Errorf("removed %d definitions, want 1 (only the comment-only one)", removed)
	}
	// Assert on the DEFINITION, not on the bare name: the comment that mentions
	// it survives by design, so a substring check for "@only_in_a_comment" would
	// match that prose and report a failure for a pass that did the right thing.
	if !strings.Contains(got, "define void @only_in_a_global(") {
		t.Errorf("@only_in_a_global was removed but a global initializer takes its address:\n%s", got)
	}
	if strings.Contains(got, "define void @only_in_a_comment(") {
		t.Errorf("@only_in_a_comment survived on the strength of a comment:\n%s", got)
	}
	// A comment OUTSIDE the region must survive untouched. This pass drops the
	// doc comment of a definition it removes (see
	// TestOptPrunePreludeDropsADeadHelpersDocComment), but prose elsewhere in
	// the module is none of its business.
	if !strings.Contains(got, "; see @only_in_a_comment for the rationale") {
		t.Errorf("the comment was rewritten:\n%s", got)
	}
}

// A removed definition takes its own doc comment with it, but nothing else.
//
// The prelude documents itself heavily, so an earlier version of the pass left
// 12,434 bytes of prose describing 44 functions it had just deleted — 35.9% on
// top of the 34,651 bytes of code. The rule is "the trailing run of comment and
// blank lines directly above the definition", and the guard that makes it safe
// is that the scan stops at the first non-comment line.
func TestOptPrunePreludeDropsADeadHelpersDocComment(t *testing.T) {
	prelude := `; this comment describes the TYPE below and must survive
%u = type { i64 }

; why @keep exists
define void @keep() {
  ret void
}

; why @dead exists
; second line of the same rationale
define void @dead() {
  ret void
}
`
	rest := "define void @main() {\n  call void @keep()\n  ret void\n}\n"
	ir, start, end := preludeFixture(prelude, rest)

	got, removed := optPrunePrelude(ir, start, end)

	if removed != 1 {
		t.Fatalf("removed %d definitions, want 1", removed)
	}
	if strings.Contains(got, "define void @dead(") {
		t.Errorf("@dead survived:\n%s", got)
	}
	if !strings.Contains(got, "define void @keep(") {
		t.Errorf("@keep was removed but @main calls it:\n%s", got)
	}
	// The dead helper's own prose goes with it.
	if strings.Contains(got, "why @dead exists") || strings.Contains(got, "second line of the same rationale") {
		t.Errorf("the removed helper's doc comment survived it:\n%s", got)
	}
	// The surviving helper's prose stays.
	if !strings.Contains(got, "why @keep exists") {
		t.Errorf("the kept helper lost its doc comment:\n%s", got)
	}
	// A comment documenting a TYPE is protected by the declaration under it:
	// the backward scan stops there. Getting this wrong would delete the
	// explanation of a type that is still in the module.
	if !strings.Contains(got, "this comment describes the TYPE below") {
		t.Errorf("a type's doc comment was deleted:\n%s", got)
	}
	if !strings.Contains(got, "%u = type { i64 }") {
		t.Errorf("the type declaration itself was deleted:\n%s", got)
	}
}

// An unbalanced region must be left alone from the point the shape stops making
// sense. Guessing where an unclosed function ends would delete the rest of the
// module, so the pass gives up instead.
func TestOptPrunePreludeRefusesAMalformedRegion(t *testing.T) {
	prelude := "define void @unterminated() {\n  ret void\n"
	rest := "define void @main() {\n  ret void\n}\n"
	ir, start, end := preludeFixture(prelude, rest)

	got, removed := optPrunePrelude(ir, start, end)

	if got != ir {
		t.Errorf("a region with no closing brace was rewritten:\n got %q\nwant %q", got, ir)
	}
	if removed != 0 {
		t.Errorf("removed %d definitions from a malformed region, want 0", removed)
	}
}

func TestOptPrunePreludeIgnoresAnEmptyOrInvalidRegion(t *testing.T) {
	const ir = "define void @main() {\n  ret void\n}\n"
	cases := []struct {
		name       string
		start, end int
	}{
		{"empty", 0, 0},
		{"inverted", 10, 4},
		{"past the end", 0, len(ir) + 1},
		{"negative", -1, 4},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, removed := optPrunePrelude(ir, c.start, c.end)
			if got != ir || removed != 0 {
				t.Errorf("region [%d,%d) changed the module: got %q removed=%d",
					c.start, c.end, got, removed)
			}
		})
	}
}

// TestOptPrunePreludeRunsThroughEmitLLVM is the non-vacuity guard for the whole
// feature: the pass being correct is worthless if EmitLLVM never calls it. It
// compares the real emitter's output with the switch off and at level 2 and
// requires the level-2 module to be strictly smaller, to still define @main,
// and to drop no helper the level-0 module's own user code calls.
func TestOptPrunePreludeRunsThroughEmitLLVM(t *testing.T) {
	const src = `main = () {
    print('hi')
}
main()
`
	emit := func(lvl string) string {
		t.Setenv("NOLANG_MIR_OPT", lvl)
		m := lowerSrc(t, src)
		ir, err := m.EmitLLVM()
		if err != nil {
			t.Fatalf("level %s: EmitLLVM: %v", lvl, err)
		}
		return ir
	}
	off := emit("0")
	on := emit("2")

	offDefs := strings.Count(off, "\ndefine ")
	onDefs := strings.Count(on, "\ndefine ")
	if offDefs == 0 {
		t.Fatal("the emitter produced no definitions, so this test proves nothing")
	}
	if onDefs >= offDefs {
		t.Errorf("level 2 emitted %d definitions, level 0 emitted %d — the pass did not run",
			onDefs, offDefs)
	}
	if !strings.Contains(on, "@_nolang_main") {
		t.Error("the pruned module lost @_nolang_main, the program entry point")
	}

	// Every symbol the surviving code mentions must still resolve: a definition
	// (@print_str), a declaration (@write, @free, @llvm.memcpy) or a global
	// (@.nl, @.mir.str.1). This is the link-error failure mode, checked here
	// rather than at link time.
	resolvable := optIRSymbols(on)
	// Scan call lines for `@name` directly. definedSymbols cannot be reused
	// here: it only recognises a line that BEGINS with `define` or `@`, and a
	// call line is `%5 = call void @f(...)`, so feeding it one returns nothing
	// and the loop below would pass vacuously while checking nothing at all.
	callLines := 0
	for _, line := range strings.Split(on, "\n") {
		at := strings.Index(line, "call ")
		if at < 0 {
			continue
		}
		callLines++
		rest := line[at:]
		for i := 0; i < len(rest); i++ {
			if rest[i] != '@' {
				continue
			}
			j := i + 1
			for j < len(rest) && isSymChar(rest[j]) {
				j++
			}
			sym := rest[i+1 : j]
			if sym == "" || sym == "_nolang_main" || resolvable[sym] {
				continue
			}
			t.Errorf("surviving code references @%s, which the pruned module does not define, declare or hold as a global", sym)
		}
	}
	// Non-vacuity: a program that prints a string necessarily calls something,
	// so zero call lines means the scan above never ran.
	if callLines == 0 {
		t.Fatal("no call survived in the pruned module, so the resolution check above proved nothing")
	}
}
