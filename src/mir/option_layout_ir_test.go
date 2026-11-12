package mir

import (
	"regexp"
	"strings"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// The per-payload option layout, as it reaches the IR.
//
// `?T` used to lower to ONE module-wide `%option = { i64 tag, [N x i64] slot }`,
// so every option cost the same 32 bytes and a `?txt` (a 256-byte inline
// buffer) had to be heap-boxed even though the payload would have fitted an
// inline area. The layout is now:
//
//	%option_<key> = type { i8 tag, [K x i64] payload }     8 + 8K bytes
//
//	?i64  16 B   ?str  24 B   ?txt 264 B   ?Huge 16 B (boxed)
//
// Two properties are load-bearing and asserted here:
//
//  1. the tag is ONE BYTE (an option has three variants, and the width follows
//     the variant count), so no read or write of it may be i64;
//  2. the payload area is sized from the ACTUAL payload — that is the whole
//     point of the change, and it is the part a later refactor is most likely
//     to regress back to a single worst-case width.
// ─────────────────────────────────────────────────────────────────────────────

// optionTypeDeclRE matches a type declaration and captures its body, so the
// test can compare the payload array rather than a whole line.
var optionTypeDeclRE = regexp.MustCompile(`(?m)^(%option_[\w]+) = type \{ ([^}]*)\}`)

// optionTypeDecls returns "name -> body" for every option type declared.
func optionTypeDecls(ir string) map[string]string {
	out := map[string]string{}
	for _, m := range optionTypeDeclRE.FindAllStringSubmatch(ir, -1) {
		out[m[1]] = strings.TrimSpace(m[2])
	}
	return out
}

// declareOptions lowers `src` and emits IR, returning the IR plus the option
// type declarations.
func declareOptions(t *testing.T, src string, threshold int) (string, map[string]string) {
	t.Helper()
	mod, diags := lowerSrcWithDiags(t, src)
	if len(diags) != 0 {
		t.Fatalf("unexpected lowering diagnostics: %v", diags)
	}
	if threshold != 0 {
		mod.OptionInlineThreshold = threshold
	}
	ll, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed: %v", err)
	}
	return ll, optionTypeDecls(ll)
}

// Every fixture CALLS the function that mentions the option type: an option
// type is interned by the lowering of a value that has it, so a declared-but-
// never-called parameter list leaves nothing for the layout pass to find.
const optionLayoutSrc = `quad {
    a i64
    b i64
    c i64
    d i64
}

touch-all = (a ?i64, b ?str, c ?txt, d ?quad, e ?[]i64) (r i64) {
    r = 0
}

q quad
p ?i64 = ok(1)
s ?str = ok('x')
t txt
u ?txt = ok(t)
v ?quad = ok(q)
w ?[]i64 = ok([1, 2])
res = touch-all(p, s, u, v, w)
`

func TestOptionLayoutPerPayloadTypes(t *testing.T) {
	ll, decls := declareOptions(t, optionLayoutSrc, 0)

	want := map[string]string{
		// 8-byte payload: one word.
		"%option_i64": "i8, [1 x i64]",
		// %str-long is { i32 len, i32 cap, i8* data } = 16 bytes.
		"%option__x25str_x2dlong": "i8, [2 x i64]",
		// %vec is the same 16 bytes; a DIFFERENT type name because the
		// payload's identity (not only its width) is what a named aggregate
		// type represents.
		"%option__x25vec": "i8, [2 x i64]",
		// %txt is a 256-byte inline buffer — the case the old shared slot
		// could not represent, and the reason the threshold moved to 264.
		"%option__x25txt": "i8, [32 x i64]",
		// a 32-byte struct payload: four words.
		"%option__x25quad": "i8, [4 x i64]",
	}
	for name, body := range want {
		got, ok := decls[name]
		if !ok {
			t.Errorf("missing type declaration %q; declared: %v", name, sortedKeys(decls))
			continue
		}
		if got != body {
			t.Errorf("%s = type { %s }, want type { %s }", name, got, body)
		}
	}

	// The singleton must be gone: with per-payload types there is nothing a
	// bare `%option` could mean, and any surviving reference would be a
	// verifier error rather than a subtle bug — assert it loudly here.
	if regexp.MustCompile(`(?m)^%option = type`).MatchString(ll) {
		t.Error("the module-wide option singleton type is still declared; each ?T must have its own type")
	}
	if code := stripLLVMComments(ll); regexp.MustCompile(`%option($|[^\w_])`).MatchString(code) {
		t.Error("a reference to the removed option singleton type survived")
	}
}

// stripLLVMComments drops everything from the first `;` on each line, so the
// prose in the prelude (which still names the old singleton when explaining the
// change) cannot trip an assertion about the EMITTED types.
func stripLLVMComments(ir string) string {
	var b strings.Builder
	for _, line := range strings.Split(ir, "\n") {
		if i := strings.IndexByte(line, ';'); i >= 0 {
			line = line[:i]
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	// insertion sort keeps the helper dependency-free and the order stable
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}

// TestOptionLayoutTagIsOneByte pins the tag width. Every read and write of the
// discriminant must be i8 at the storage boundary; a leftover `load i64` off a
// `{ i8, … }` field would be an out-of-bounds read of the neighbouring payload
// word, and `icmp eq i64 <i8 value>` is not even valid IR.
func TestOptionLayoutTagIsOneByte(t *testing.T) {
	src := `probe = (o ?i64) (r i64) {
    r = o: {
        ok -> it
        nil -> -1
        err -> -2
    }
}

x ?i64 = ok(1)
y = probe(x)
`
	ll, _ := declareOptions(t, src, 0)
	fn := extractDefineBlock(t, ll, "@probe")
	if fn == "" {
		t.Fatal("@probe not found")
	}
	if regexp.MustCompile(`load i64, i64\* %[\w.]+ *\n *%[\w.]+ = [^)]*%option`).MatchString(ll) {
		t.Error("an option tag is still loaded as i64")
	}
	// The discriminant store: `store %option_i64 { i8 0, ... }`.
	if !regexp.MustCompile(`store %option_i64 \{ i8 \d+, \[1 x i64\] zeroinitializer \}`).MatchString(ll) {
		t.Errorf("no i8 tag store found for %%option_i64\n%s", fn)
	}
	// And the read widening: extractvalue i8 field, then zext to i64.
	if !regexp.MustCompile(`= extractvalue %option_i64 %[\w.]+, 0`).MatchString(fn) {
		t.Errorf("@probe never extracts the tag\n%s", fn)
	}
	if !regexp.MustCompile(`= zext i8 %[\w.]+ to i64`).MatchString(fn) {
		t.Errorf("@probe extracts the tag without widening the i8\n%s", fn)
	}
}

// TestOptionLayoutThresholdIsRespected proves the configured threshold actually
// decides inline-vs-boxed, and that a boxed payload degrades to ONE word.
func TestOptionLayoutThresholdIsRespected(t *testing.T) {
	const src = `touch-str = (b ?str) (r i64) {
    r = 0
}

s ?str = ok('x')
res = touch-str(s)
`
	// Default (264): %str-long is 16 bytes, so it is inline in two words.
	_, decls := declareOptions(t, src, 0)
	if got := decls["%option__x25str_x2dlong"]; got != "i8, [2 x i64]" {
		t.Fatalf("default threshold: ?str = type { %s }, want type { i8, [2 x i64] }", got)
	}
	// 16 = tag area (8) + one word: a 16-byte payload no longer fits.
	_, decls = declareOptions(t, src, 16)
	if got := decls["%option__x25str_x2dlong"]; got != "i8, [1 x i64]" {
		t.Errorf("threshold 16: ?str = type { %s }, want type { i8, [1 x i64] } (boxed)", got)
	}
	// 24 = tag + two words: exactly enough for %str-long again.
	_, decls = declareOptions(t, src, 24)
	if got := decls["%option__x25str_x2dlong"]; got != "i8, [2 x i64]" {
		t.Errorf("threshold 24: ?str = type { %s }, want type { i8, [2 x i64] } (inline)", got)
	}
}

// TestOptionLayoutOversizePayloadIsBoxed covers the > threshold case end to
// end: the type stays one word and the wrap has to malloc.
func TestOptionLayoutOversizePayloadIsBoxed(t *testing.T) {
	src := `huge {
    pad [33]i64
}

mk = () (r ?huge) {
    h huge
    r = ok(h)
}

hv ?huge = mk()
`
	_, decls := declareOptions(t, src, 0)
	got, ok := decls["%option__x25huge"]
	if !ok {
		t.Fatalf("?huge has no option type; declared: %v", sortedKeys(decls))
	}
	if got != "i8, [1 x i64]" {
		t.Errorf("?huge (264-byte payload) = type { %s }, want type { i8, [1 x i64] } (one-word box pointer)", got)
	}
}

// TestOptionLayoutTxtPayloadFitsInline is the headline case: a 256-byte %txt
// payload is stored inside the option, so no malloc is needed to wrap one.
func TestOptionLayoutTxtPayloadFitsInline(t *testing.T) {
	src := `mk = () (r ?txt) {
    t txt
    r = ok(t)
}

tv ?txt = mk()
`
	ll, decls := declareOptions(t, src, 0)
	if got := decls["%option__x25txt"]; got != "i8, [32 x i64]" {
		t.Fatalf("?txt = type { %s }, want type { i8, [32 x i64] }", got)
	}
	fn := extractDefineBlock(t, ll, "@mk")
	if fn == "" {
		t.Fatal("@mk not found")
	}
	// The wrap stores the payload area at byte offset 8 and must NOT box it.
	if !strings.Contains(fn, "getelementptr inbounds i8, i8* ") {
		t.Errorf("@mk does not address the payload area by byte offset\n%s", fn)
	}
	if regexp.MustCompile(`call i8\* @nolang_rc_alloc\(i64 264\)`).MatchString(fn) {
		t.Errorf("?txt payload was heap-boxed even though it fits inline\n%s", fn)
	}
}
