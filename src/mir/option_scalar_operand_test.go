package mir

import (
	"strings"
	"testing"
)

// These tests pin the rule that a SCALAR consumer of a `?T` value must peel the
// option payload before feeding LLVM an i64-typed operand.
//
// Background (D6 unified option rule, 15c72ccd): signed-int arithmetic `+ - * /
// %` always yields `?T` — even when the statement carries `#{overflow=...}`,
// which only decides how the FAILURE is resolved (wrap/clamp/...) and never
// downgrades the result type to a plain int (see lowerer.inferOptionArith, D1
// note). Every site that previously assumed "int expression == i64" therefore
// started receiving `%option` values. Two of them broke the corpus gate
// (`.github/workflows/test.yml`, "Compile corpus"):
//
//   - emitBuiltinAlloc (`s str = with-cap(sz)` with sz from arithmetic) emitted
//     `call i8* @nolang_rc_alloc(i64 %lvN)` with %lvN defined as `%option`;
//     opt-verify rejected the module ("defined with type '%option' but expected
//     'i64'") — tests/http3.no, http-rest, https-server, net-http, sse, tls,
//     tls13-crypto.
//   - emitBuiltinCLib (a `?i64` argument reaching a C builtin's i64 parameter)
//     failed with "cannot coerce arg 0 from %option to i64" — tests/time.no via
//     std's sleep-ms (`#{overflow=wrap} us = ms * 1000` then `sleep-us(us)`).
//
// Both paths now unwrap through unwrapOptionOperand, mirroring emitArith's
// established operand unwrap.

// assertOperandIsNotOptionLoad checks EVERY call starting at callPrefix that
// its first operand register is NOT defined by a `load %option` — the exact
// shape opt-verify rejected before the unwrap landed.
func assertOperandIsNotOptionLoad(t *testing.T, ll, callPrefix string) {
	t.Helper()
	found := false
	for off := 0; ; {
		i := strings.Index(ll[off:], callPrefix)
		if i < 0 {
			break
		}
		i += off
		found = true
		reg := strings.Fields(ll[i+len(callPrefix):])[0]
		reg = strings.TrimSuffix(reg, ")")
		if strings.Contains(ll, reg+" = load %option") {
			t.Fatalf("operand %s of %q is a raw %%option load; expected a peeled scalar payload", reg, callPrefix)
		}
		off = i + len(callPrefix)
	}
	if !found {
		t.Fatalf("call %q not found in IR:\n%s", callPrefix, ll)
	}
}

func TestBuiltinCLibAcceptsOptionScalarArg(t *testing.T) {
	const src = `f = (ms i64) {
  #{overflow=wrap}
  us = ms * 1000
  sleep-us(us)
}
f(50)
`
	ll, err := lowerSourceForTest(t, src).EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM rejected a ?i64 argument for an i64 builtin parameter: %v", err)
	}
	assertOperandIsNotOptionLoad(t, ll, "call i32 @nolang.sleep_us(i64 ")
}

func TestBuiltinAllocAcceptsOptionCap(t *testing.T) {
	const src = `g = (n i64) (s str) {
  #{overflow=wrap}
  sz = n * 8
  s str = with-cap(sz)
}
print(g(2).len())
`
	ll, err := lowerSourceForTest(t, src).EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM rejected an option-typed with-cap size operand: %v", err)
	}
	if !strings.Contains(ll, "call i8* @nolang_rc_alloc(i64 ") {
		t.Fatalf("expected an rc_alloc for the with-cap backing store, IR was:\n%s", ll)
	}
	assertOperandIsNotOptionLoad(t, ll, "call i8* @nolang_rc_alloc(i64 ")
}

// TestBuiltinAllocZeroCapacityUsesNullBuffer pins the empty-container descriptor
// invariant. nolang_rc_alloc(0) returns a non-null allocator-header block, but a
// cap==0 descriptor denotes a borrowed view or an unallocated container. Giving
// that block to a fresh with-cap(0) vector makes ensureVecBuffer skip allocation
// and the first typed element load/write runs past the 16-byte header.
func TestBuiltinAllocZeroCapacityUsesNullBuffer(t *testing.T) {
	cases := []struct {
		name string
		src  string
		fn   string
	}{
		{
			name: "vec-literal",
			fn:   "_nolang_main",
			src: `main = () {
    values []i64 = with-cap(0)
}
`,
		},
		{
			name: "vec-dynamic",
			fn:   "make_values",
			src: `make-values = (n i64) (values []i64) {
    values []i64 = with-cap(n)
}
make-values(0)
`,
		},
		{
			name: "str-literal",
			fn:   "_nolang_main",
			src: `main = () {
    value str = with-cap(0)
}
`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ll, err := lowerSourceForTest(t, tc.src).EmitLLVM()
			if err != nil {
				t.Fatalf("EmitLLVM failed: %v", err)
			}
			body := irFunc(ll, tc.fn)
			if body == "" {
				t.Fatalf("function %q not found in IR", tc.fn)
			}
			if !strings.Contains(body, "phi i8* [ null, %") {
				t.Fatalf("zero-capacity allocation must select a null buffer; IR was:\n%s", body)
			}
			if !strings.Contains(body, "icmp eq i64") || !strings.Contains(body, ", 0\n") {
				t.Fatalf("zero-capacity allocation lost its empty-size guard; IR was:\n%s", body)
			}
			if strings.Contains(body, "@nolang_rc_alloc(i64 0)") {
				t.Fatalf("zero-capacity allocation still calls nolang_rc_alloc(0); IR was:\n%s", body)
			}
			seen := make(map[string]bool)
			for _, line := range strings.Split(body, "\n") {
				fields := strings.Fields(line)
				if len(fields) < 3 || !strings.HasPrefix(fields[0], "%ba.") || fields[1] != "=" {
					continue
				}
				if seen[fields[0]] {
					t.Fatalf("duplicate allocation register %s; IR was:\n%s", fields[0], body)
				}
				seen[fields[0]] = true
			}
		})
	}
}
