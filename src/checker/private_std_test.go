package checker

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestCheckPrivateStdCall pins the compile-time privacy contract for
// underscore-prefixed STDLIB symbols: `_foo` in std is module-internal, and a
// cross-module call is an ERROR (TraceID "raqvyltn"), not a convention to be
// honored by migrating call sites and hoping nobody regresses.
//
// The ownership facts come from the baked std signature tables, so the whole
// check is silent when the tables were never generated (a binary built by bare
// `go build` has StdSigKey == ""). `make test` injects the key; running this
// file with plain `go test` skips instead of failing on an empty table.
func TestCheckPrivateStdCall(t *testing.T) {
	if StdSigKey == "" {
		t.Skip("std signature tables not baked; run via `make test` (StdSigKey is injected there)")
	}
	CollectStdModuleSignatures()

	// mkCall builds `recv.property(...)`; recvIdent is the receiver's SOURCE
	// name, recvType its resolved static type (the two differ for a value
	// receiver, coincide for a module-qualified call).
	mkCall := func(recvIdent, prop string, line, col int) (*parser.CallExpression, *parser.DotExpression) {
		recv := &parser.Identifier{Token: lexer.Token{Line: line, Column: col}, Value: recvIdent}
		dot := &parser.DotExpression{
			Token:    lexer.Token{Line: line, Column: col},
			Receiver: recv,
			Property: prop,
		}
		return &parser.CallExpression{
			Token:    lexer.Token{Line: line, Column: col},
			Function: dot,
		}, dot
	}

	cases := []struct {
		name      string
		recvIdent string
		recvType  string
		prop      string
		curModule string
		wantErr   bool
	}{
		// The case that motivated the rule: str.no reaching into bigint's
		// private small-int fast path.
		{"bigint._mod-i64 from str -> rejected", "tmp", "bigint", "_mod-i64", "str", true},
		{"bigint._mod-i64 from user code -> rejected", "tmp", "bigint", "_mod-i64", "", true},
		{"bigint._mod-i64 inside bigint -> allowed", "tmp", "bigint", "_mod-i64", "bigint", false},
		{"bigint._div-i64 from crypto -> rejected", "tmp", "bigint", "_div-i64", "chacha20-poly1305", true},
		{"bigint._mul-i64 from txt -> rejected", "acc", "bigint", "_mul-i64", "txt", true},

		// The receiver's STATIC TYPE may be spelled module-qualified
		// (`v bigint.bigint`), which is exactly how std wrote the offending
		// call sites before the migration. resolveExprType hands that string
		// over verbatim, so the bare `struct.method` key misses and the guard
		// goes silent on the one shape it exists to catch.
		{"bigint._mod-i64 on bigint.bigint receiver -> rejected", "v", "bigint.bigint", "_mod-i64", "str", true},
		{"bigint._mod-i64 on bigint.bigint receiver inside bigint -> allowed", "v", "bigint.bigint", "_mod-i64", "bigint", false},

		// Module-qualified spelling of the same symbol is caught too.
		{"bigint._mod-i64 (qualified) from str -> rejected", "bigint", "bigint", "_mod-i64", "str", true},
		{"str._str-dig-val (qualified) from bigint -> rejected", "str", "", "_str-dig-val", "bigint", true},
		{"str._str-dig-val inside str -> allowed", "str", "", "_str-dig-val", "str", false},

		// Exemption 1: operator-overload names are the IMPLEMENTATION NAMES of
		// the public `+ - * / %`, and the parser rewrites `a + b` into
		// `a._add(b)` before the checker ever sees it. Flagging them would flag
		// every operator.
		{"bigint._mod from str -> allowed (overload name)", "a", "bigint", "_mod", "str", false},
		{"bigint._add from str -> allowed (overload name)", "a", "bigint", "_add", "str", false},

		// Exemption 2: str/txt private BUILTINS belong to checkPrivateBuiltinMethodCall
		// (privbuiltin); reporting them here too would double-book one position.
		{"str._len from txt -> allowed (privbuiltin owns it)", "s", "str", "_len", "txt", false},

		// Public API is never affected.
		{"bigint.to-i64 from str -> allowed", "b", "bigint", "to-i64", "str", false},
		{"bigint.div-mod from str -> allowed", "b", "bigint", "div-mod", "str", false},

		// Nothing owns the name -> no opinion (user code keeps its own `_` names).
		{"unknown _bogus on bigint -> allowed", "b", "bigint", "_bogus", "str", false},
		{"user-namespace receiver -> allowed", "v", "vec", "_push", "", false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e, dot := mkCall(c.recvIdent, c.prop, 11, 5)
			got := checkPrivateStdCall(e, dot, c.recvType, c.curModule)
			if c.wantErr && len(got) == 0 {
				t.Fatalf("expected a privstd error for %s.%s from module %q, got none",
					c.recvType, c.prop, c.curModule)
			}
			if !c.wantErr && len(got) != 0 {
				t.Fatalf("expected NO error for %s.%s from module %q, got: %q",
					c.recvType, c.prop, c.curModule, got[0].Message)
			}
			if !c.wantErr {
				return
			}
			if got[0].TraceID != "raqvyltn" {
				t.Fatalf("expected TraceID %q, got %q", "raqvyltn", got[0].TraceID)
			}
			if got[0].Line != 11 || got[0].Column != 5 {
				t.Fatalf("expected position 11:5, got %d:%d", got[0].Line, got[0].Column)
			}
			// The message must name the owning module and say what to do; a bare
			// "private" is not actionable.
			if !strings.Contains(got[0].Message, "public API") {
				t.Errorf("message should point at the public API, got %q", got[0].Message)
			}
		})
	}
}

// TestStdPrivateOwnersExist guards the premise of the whole check: the two
// tables really do carry ownership for the private std symbols this rule is
// meant to protect. If a generator change ever drops the `module.struct.method`
// or `module.fn` key shape, stdPrivateOwners silently returns nil and the rule
// stops enforcing anything — a green build hiding a dead guard.
func TestStdPrivateOwnersExist(t *testing.T) {
	if StdSigKey == "" {
		t.Skip("std signature tables not baked; run via `make test`")
	}
	CollectStdModuleSignatures()
	initStdStructMethodMods()
	initStdPrivateFnMods()

	if got := stdStructMethodMods["bigint._mod-i64"]["bigint"]; !got {
		t.Errorf("stdStructMethodMods has no owner for bigint._mod-i64; table keys changed? mods=%v",
			stdStructMethodMods["bigint._mod-i64"])
	}
	if !stdPrivateFnMods["str"]["_str-dig-val"] {
		t.Errorf("stdPrivateFnMods has no entry for str._str-dig-val; `module.fn` keys changed?")
	}
	// And the inverse: a PUBLIC symbol must not show up as a private owner, or
	// the one-dot scan is over-collecting.
	if stdPrivateFnMods["bigint"]["to-i64"] {
		t.Errorf("public bigint.to-i64 landed in the private function table")
	}
}
