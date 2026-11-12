package mir

import (
	"regexp"
	"strings"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// An inline option payload must own its heap.
//
// `optStoreInlinePayload` is one `store <T>` of the struct bytes, so the option
// and its source end up sharing every `str` buffer / `[]T` block the payload
// points at. Both sides are dropped (the option when its scope ends, the source
// when its own does), and whichever drops first pulls the memory out from under
// the other. The boxed path never had this problem: `optStoreBoxedPayload`
// memcpy's into a fresh box and then deep-copies it, so the box is self-owned.
//
// The fix is the inline counterpart, `optCloneInlinePayloadInto`, called at the
// end of emitOptionWrap / emitMove's inline branches. These tests pin the two
// halves of that contract at the IR level:
//
//  1. wrapping a struct payload into an INLINE option emits a clone of its
//     owned contents (`str_clone` for the leaf);
//  2. a payload with no heap-owned parts (`?i64`) emits NO clone — the fix must
//     not turn every option store into an allocation.
//
// The end-to-end counterpart is tests/option-inline-clone.no (and tests/yaml.no,
// where the missing clone showed up as heap-use-after-free in str_clone).
// ─────────────────────────────────────────────────────────────────────────────

const optionInlineCloneSrc = `inner {
    name str
    keys []i64
}

outer {
    p inner
    tag i64
}

mk = (t i64) (o ?outer) {
    x outer
    x.tag = t
    x.p.name = 'abc'
    x.p.keys = [1, 2, 3]
    o = ok(x)
}

peek = (o ?outer) (r i64) {
    r = 0
    o: {
        ok -> r = it.tag

        -> r = -1
    }
}

mk-i64 = (t i64) (o ?i64) {
    o = ok(t)
}

peek-i64 = (o ?i64) (r i64) {
    r = 0
    o: {
        ok -> r = it

        -> r = -1
    }
}

a = peek(mk(1))
b = peek-i64(mk-i64(2))
`

// definedBody returns the IR text of one defined function. It anchors on the
// `define` line: a call site (`call void @mk(...)`) appears earlier in the file
// and would otherwise be matched first.
func definedBody(t *testing.T, ir, name string) string {
	t.Helper()
	re := regexp.MustCompile(`(?m)^define [^\n]*@` + regexp.QuoteMeta(name) + `\(`)
	loc := re.FindStringIndex(ir)
	if loc == nil {
		t.Fatalf("function %s not found in IR", name)
	}
	end := strings.Index(ir[loc[0]:], "\n}\n")
	if end < 0 {
		t.Fatalf("function %s is unterminated", name)
	}
	return ir[loc[0] : loc[0]+end+3]
}

// TestOptionInlinePayloadIsDeepCopied: wrapping a struct into an inline option
// must give the payload its own `str` buffer(s).
func TestOptionInlinePayloadIsDeepCopied(t *testing.T) {
	ll, decls := declareOptions(t, optionInlineCloneSrc, 0)

	// Guard against the fixture going vacuous: if `?outer` ever stops inlining
	// (a lower default threshold, a bigger fixture) this test would pass for
	// the wrong reason — the box path clones on its own.
	optType := ""
	for name, body := range decls {
		if strings.Contains(body, "[5 x i64]") && strings.Contains(name, "outer") {
			optType = name
		}
	}
	if optType == "" {
		t.Fatalf("?outer is not a 48-byte inline option any more; declarations: %v", decls)
	}

	body := definedBody(t, ll, "mk")
	if !strings.Contains(body, "oic.") {
		t.Errorf("mk stores an inline %s payload but emits no optCloneInlinePayloadInto (no %%oic.<n>)\n%s", optType, body)
	}
	if !strings.Contains(body, "@str_clone") {
		t.Errorf("mk stores an inline %s payload but never clones its str field — the option and its source would drop the same buffer\n%s", optType, body)
	}
}

// TestOptionInlineScalarPayloadIsNotCloned: the clone is limited to payloads
// that actually own heap. `?i64` has nothing to clone.
func TestOptionInlineScalarPayloadIsNotCloned(t *testing.T) {
	ll, _ := declareOptions(t, optionInlineCloneSrc, 0)

	body := definedBody(t, ll, "mk_i64")
	if strings.Contains(body, "oic.") || strings.Contains(body, "@str_clone") {
		t.Errorf("a `?i64` payload is a plain word — it must not be cloned\n%s", body)
	}
}
