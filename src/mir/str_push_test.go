package mir

// str_push_test.go — IR-shape pins for `str.push-str` (the only way to GROW a str).
//
// WHY AN IR TEST AND NOT ONLY AN END-TO-END ONE
// ---------------------------------------------
// The std declaration of `str.push-str` is a `#{buildin}` stub with an EMPTY
// body: the entire implementation is emitBuiltinStrPushBytes. If the builtin
// lookup ever stopped resolving (a renamed registry entry, a dropped ForwardFunc),
// the call would lower to the empty std body and become a SILENT NO-OP — the
// program still compiles, still exits 0, and just does not append anything. An
// end-to-end assertion would catch that, but only for the shapes it happens to
// cover; these assertions name the instructions that make the append real.
//
// The five properties, each of which was a decision in the emitter and each of
// which a plausible refactor can silently drop:
//  1. `slbG.`      — the buffer actually GROWS (ensureStrLongBuffer's grow block).
//  2. `@nolang_free` — the growth releases the previous allocation (no leak).
//  3. `@llvm.memmove` — the byte copy uses memmove, not memcpy: `t.push-str(t)`
//     makes source and destination the same buffer, which memcpy leaves undefined.
//  4. `@str_retain` + `@str_free` — the source is retained before growth and
//     released after; that pair is the only reason a self-append is safe (the
//     grow frees the old buffer, and without the retain the source pointer would
//     dangle).
//  5. `spsvw`      — the borrowed-view COW branch exists (cap == 0 && data != null),
//     checked BEFORE ensureStrLongBuffer, which deliberately skips that case.
//
// Plus one structural pin for the zero-length short-circuit: a `push-str('')`
// must branch straight to the release block. That path cannot be observed in the
// IR text as "an absent block" (all blocks are emitted regardless of reachability,
// so `slbA.` is always present) — it has to be asserted as the branch shape.

import (
	"regexp"
	"strings"
	"testing"
)

const strPushSrc = `
main = () {
    s str = 'ab'
    x str = 'cd'
    s.push-str(x)
}
`

const strPushEmptySrc = `
main = () {
    s str = 'ab'
    s.push-str('')
}
`

func TestStrPushStrEmitsInPlaceGrow(t *testing.T) {
	mod := lowerForTest(t, strPushSrc)
	ir, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed: %v (a non-nil error here usually means the "+
			"str.push-str builtin entry / ForwardFunc no longer resolves, i.e. the "+
			"call fell through to the empty std body)", err)
	}

	if !strings.Contains(ir, "slbG.") {
		t.Errorf("str.push-str did not emit a growth block (slbG): the append has " +
			"nowhere to put the source bytes and would write past the buffer")
	}
	if !strings.Contains(ir, "call void @nolang_free(i8* ") {
		t.Errorf("str.push-str growth does not free the old buffer: every grow " +
			"would leak the previous allocation")
	}
	if !strings.Contains(ir, "@llvm.memmove.p0i8.p0i8.i64") {
		t.Errorf("str.push-str does not copy with llvm.memmove: memcpy is " +
			"undefined for `t.push-str(t)`, where source and destination overlap")
	}
	if !strings.Contains(ir, "call void @str_retain(%str-long ") {
		t.Errorf("str.push-str does not retain the source before growing: a " +
			"self-append would free the buffer the source still points at")
	}
	if !strings.Contains(ir, "call void @str_free(%str-long ") {
		t.Errorf("str.push-str does not release the retained source: the retain " +
			"pins the buffer forever (one leak per call)")
	}
	if !strings.Contains(ir, "spsvw.") {
		t.Errorf("str.push-str lost the borrowed-view COW branch: ensureStrLongBuffer " +
			"skips cap==0 views on purpose, so without this the append would grow " +
			"someone else's buffer (or write into it)")
	}
	// The COW allocation must NOT release the borrowed pointer — it belongs to
	// the view's owner. The only @nolang_free in this emitter is the growth path,
	// so assert the COW block allocates without a free before writing the header
	// back.
	cow, ok := blockByPrefix(ir, "spsC")
	if !ok {
		t.Fatalf("no COW block (spsC.) in the IR:\n%s", ir)
	}
	if strings.Contains(cow, "@nolang_free") {
		t.Errorf("the borrowed-view COW block frees the old pointer: a cap==0 view " +
			"aliases storage this value does not own (double free / freeing a " +
			"string constant's neighbours)")
	}
	if !strings.Contains(cow, "call i8* @nolang_rc_alloc(i64 ") {
		t.Errorf("the borrowed-view COW block does not allocate an owned buffer:\n%s", cow)
	}
}

// nextBlockDefRe matches any block definition line at column 0, which is where
// EmitLLVM writes them (`slbD.62:`, `spsG.31:`, `entry:`).
var nextBlockDefRe = regexp.MustCompile(`(?m)^[A-Za-z_][A-Za-z0-9_.]*:$`)

// blockAt returns the body of the block whose definition line starts at off:
// from the definition line up to (not including) the next block definition.
//
// A plain strings.Index is NOT usable here: the label also appears one line
// earlier as a `label %spsC.5` branch target, and slicing from there yields a
// fragment of the PRECEDING block (which is how an earlier version of this file
// silently asserted against the wrong text).
func blockAt(ir string, off int) string {
	rest := ir[off:]
	// FindAllStringIndex, not FindStringIndex: rest STARTS at this block's own
	// definition line, so the first match is the block itself and the second is
	// its terminator. (Searching rest[1:] instead is a trap — Go's multiline `^`
	// also matches the start of the sliced text, so `psD.8:` matches mid-line and
	// the slice collapses to a single character.)
	locs := nextBlockDefRe.FindAllStringIndex(rest, 2)
	if len(locs) == 2 {
		rest = rest[:locs[1][0]]
	}
	return rest
}

// blockByPrefix returns the body of the first block defined with the label
// prefix (e.g. "spsC" matches `spsC.5:`).
func blockByPrefix(ir, prefix string) (string, bool) {
	re := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(prefix) + `\.\d+:$`)
	loc := re.FindStringIndex(ir)
	if loc == nil {
		return "", false
	}
	return blockAt(ir, loc[0]), true
}

// blockByLabel returns the body of the block defined by exactly `label`
// (e.g. "spsD.34", which already carries its sequence number).
func blockByLabel(ir, label string) (string, bool) {
	re := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(label) + `:$`)
	loc := re.FindStringIndex(ir)
	if loc == nil {
		return "", false
	}
	return blockAt(ir, loc[0]), true
}

func TestStrPushStrEmptySourceShortCircuits(t *testing.T) {
	mod := lowerForTest(t, strPushEmptySrc)
	ir, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed: %v", err)
	}

	// The zero-length guard is `br i1 %spsz.N, label %spsD.X, label %spsG.Y`:
	// TRUE (empty source) goes straight to the release block, skipping every
	// growth/allocation block. Without this a receiver whose data is null would be
	// allocated 1024 bytes by a `push-str('')`.
	re := regexp.MustCompile(`br i1 %spsz\.\d+, label %(spsD\.\d+), label %(spsG\.\d+)`)
	m := re.FindStringSubmatch(ir)
	if m == nil {
		t.Fatalf("no zero-length short-circuit branch found: `push-str('')` would "+
			"fall into the growth path and allocate a buffer for nothing\n%s", ir)
	}
	doneLabel, growLabel := m[1], m[2]

	// The release must live in the SHORT-CIRCUIT target (spsD), not in the growth
	// path: the retain/free pair has to be balanced on both branches, since static
	// aliasing between receiver and source cannot be decided.
	doneBlock, ok := blockByLabel(ir, doneLabel)
	if !ok {
		t.Fatalf("done block %q has no definition in the IR", doneLabel)
	}
	if !strings.Contains(doneBlock, "call void @str_free(%str-long ") {
		t.Errorf("the zero-length target %q does not release the retained source: "+
			"the retain made before the branch leaks on every empty append\n%s",
			doneLabel, doneBlock)
	}
	if growLabel == doneLabel {
		t.Errorf("the growth branch and the short-circuit branch are the same block %q", doneLabel)
	}
}
