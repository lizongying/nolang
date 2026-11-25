package fmt

import (
	"math/rand"
	"regexp"
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// hirPos is the `@line:col` suffix the HIR dumper appends to each node. Formatting
// shifts those numbers legitimately (it inserts/reorders layout), so the semantic
// fingerprint drops them. Everything else in the dump — node kinds, edges,
// desugared sugar, inferred facts — is what the round trip must preserve.
var hirPos = regexp.MustCompile(`@\d+:\d+`)

// hirTrailingWS strips the padding left behind where a `@line:col` was removed.
// The dumper emits `@N:M` as a trailing token, so dropping it can leave trailing
// spaces that differ only because one source had one more layout token than the
// other. That padding is a dump artifact, not part of the program.
var hirTrailingWS = regexp.MustCompile(`(?m)[ \t]+$`)

// roundTripSemantics parses src and lowers the resulting AST to HIR. HIR is the
// oracle here rather than the source text: a formatter may legitimately rewrite
// surface spelling (loop style, annotation layout, blank lines) as long as the
// program it describes is unchanged. The dump is deterministic and covers the
// parser's semantic side-tables (sugar desugaring, inferred facts, annotations),
// so two sources with equal dumps denote the same program.
//
// The bool is false when src does not parse cleanly — such inputs carry no
// semantics to preserve and the property is vacuous for them.
func roundTripSemantics(src string) (string, bool) {
	p := parser.New(lexer.New(src))
	prog := p.ParseProgram()
	if prog == nil || len(p.Errors()) > 0 {
		return "", false
	}
	dump := hirPos.ReplaceAllString(parser.ASTToHIR(prog).Dump(), "")
	dump = hirTrailingWS.ReplaceAllString(dump, "")
	// The dumper ends a program with a blank line per trailing source newline.
	// FormatFile guarantees exactly one trailing newline, so an unformatted
	// source with several produces extra empty lines that say nothing about the
	// program. Trim the tail on both sides.
	return strings.TrimRight(dump, "\n"), true
}

// roundTripStatements are individually valid Nolang statements spanning the
// syntax families the formatter rewrites: declarations, calls, match/option,
// range loops, arrays, annotations and doc comments. The property test assembles
// random programs from them so the fuzz space is always well-formed (arbitrary
// byte fuzzing instead lands on malformed inputs whose "semantics" the formatter
// never promised to preserve — see the note at the bottom).
var roundTripStatements = []string{
	"x i64 = 1",
	"y str = 'hello'",
	"print('hi')",
	"print(x)",
	"v: {\n    ok -> print('yes')\n    err -> print('bad')\n    nil -> print('no')\n}",
	"a [3]i64 = [1, 2, 3]",
	"i <- [0..3): {\n    print(a[i])\n}",
	"{ x > 0 -> print('big')\n  -> print('small')\n}",
	"#{overflow=wrap}\nz u8 = 255",
	"; doc comment\n; [f](#f)\nf = () (out i64) {\n    out = x\n}",
}

// TestFormatRoundTripProperty states the parser/fmt round-trip contract over a
// deterministically generated corpus of valid programs:
//
//  1. formatting never breaks a parseable program;
//  2. the HIR (hence the program, not just its text) is unchanged;
//  3. formatting is a fixed point.
//
// It complements the spelling-pinned idempotence tests in formatter_test.go:
// those check specific regressions, this one checks the general contract across
// combinations nobody enumerated by hand. The generator is seeded, so a failure
// is reproducible from the test name alone.
func TestFormatRoundTripProperty(t *testing.T) {
	const programs = 240
	rng := rand.New(rand.NewSource(20261125))

	checked := 0
	for n := 0; n < programs; n++ {
		var b strings.Builder
		count := 1 + rng.Intn(6)
		for i := 0; i < count; i++ {
			b.WriteString(roundTripStatements[rng.Intn(len(roundTripStatements))])
			b.WriteByte('\n')
			// Vary leading blank lines: the formatter normalises them, and that
			// normalisation must not touch the program's semantics.
			if rng.Intn(3) == 0 {
				b.WriteByte('\n')
			}
		}
		src := b.String()

		before, ok := roundTripSemantics(src)
		if !ok {
			t.Fatalf("generated program #%d did not parse:\n%s", n, src)
		}

		out := FormatFile(src)
		after, ok := roundTripSemantics(out)
		if !ok {
			t.Fatalf("formatting broke generated program #%d\n--- src ---\n%s\n--- out ---\n%s", n, src, out)
		}
		if after != before {
			t.Errorf("formatting changed semantics of program #%d\n--- src ---\n%s\n--- out ---\n%s\n--- HIR before ---\n%s\n--- HIR after ---\n%s",
				n, src, out, before, after)
		}
		if again := FormatFile(out); again != out {
			t.Errorf("fmt is not a fixed point for program #%d\n--- once ---\n%s\n--- twice ---\n%s", n, out, again)
		}
		checked++
	}

	// Anti-vacuity: prove the oracle can actually see a semantic change. If the
	// fingerprint were constant, all the checks above would be tautologies.
	base, ok := roundTripSemantics("x i64 = 1\nprint(x)\n")
	if !ok {
		t.Fatal("anti-vacuity fixture did not parse")
	}
	changed, ok := roundTripSemantics("x i64 = 2\nprint(x)\n")
	if !ok {
		t.Fatal("anti-vacuity control fixture did not parse")
	}
	if base == changed {
		t.Fatal("HIR fingerprint ignores a changed literal; the property test is vacuous")
	}

	t.Logf("round-trip property held for %d generated programs", checked)
}

// TestFormatRejectsPrefixOperatorInput pins the end-to-end effect of the
// parser-side fix (m0db1nfx): a leading binary operator is a parse error, so
// `no fmt` reports it and leaves the file untouched instead of printing a
// phantom empty statement as a blank line that the next pass would drop.
func TestFormatRejectsPrefixOperatorInput(t *testing.T) {
	for _, src := range []string{"%0", "%0\n", "%%0\n", "x i64 = 1\nprint(1)\n%0\n"} {
		out, errs := FormatFileWithErrors(src)
		if out != src {
			t.Errorf("FormatFileWithErrors(%q) = %q; want source unchanged", src, out)
		}
		if len(errs) == 0 {
			t.Errorf("%q: expected a parser error, got none", src)
		}
		if again := FormatFile(src); again != src {
			t.Errorf("FormatFile(%q) = %q; want unchanged (idempotence)", src, again)
		}
	}
}

// Deliberately NOT a byte-level fuzz target: arbitrary bytes still find valid
// concerns such as trailing blank lines shifting in the HIR dump, and the
// generator's job here is to pin the contract for well-formed programs — what
// `no fmt` promises its users. Malformed prefix-operator input is now rejected
// by the parser (see TestFormatRejectsPrefixOperatorInput), not silently
// rewritten.
