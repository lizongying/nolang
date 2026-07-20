package build

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// countTypeErrors parses src and returns the number of ValidateTypes errors.
func countTypeErrors(t *testing.T, src string) []ValidateResult {
	t.Helper()
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("unexpected parse errors: %v", errs)
	}
	return ValidateTypes(prog)
}

// TestSafeBitwiseNarrowing verifies that assignments whose right-hand side is
// a bitwise expression (mask &, shift >>, or | etc.) are allowed to narrow to
// a smaller unsigned integer type without a type error.
func TestSafeBitwiseNarrowing(t *testing.T) {
	tests := []struct {
		name      string
		src       string
		wantErrs  int
		wantError bool
	}{
		// --- 安全窄化：應放行（0 errors） ---
		{
			name:     "mask_u64_to_u32_literal",
			src:      `poly = (d0 u64) { h0 u32 = d0 & 67108863 }`,
			wantErrs: 0,
		},
		{
			name:     "mask_u64_to_u32_paren",
			src:      `poly = (d0 u64) { h0 u32 = (d0 & 67108863) }`,
			wantErrs: 0,
		},
		{
			name:     "mask_u64_to_u32_full_range",
			src:      `poly = (d0 u64) { h0 u32 = d0 & 4294967295 }`,
			wantErrs: 0,
		},
		{
			name:     "shift_right_u64_to_u32",
			src:      `poly = (f u64) { hi u32 = f >> 32 }`,
			wantErrs: 0,
		},
		{
			name:     "or_of_masked_bytes_to_u32",
			src:      `poly = (key []byte) { s u32 = (key[0] & 255) | ((key[1] & 255) << 8) | ((key[2] & 255) << 16) | ((key[3] & 255) << 24) }`,
			wantErrs: 0,
		},
		{
			name:     "xor_u64_to_u32",
			src:      `poly = (a u64, b u64) { c u32 = a ^ b }`,
			wantErrs: 0,
		},
		{
			name:     "mask_to_byte",
			src:      `poly = (v u64) { b byte = v & 255 }`,
			wantErrs: 0,
		},
		{
			name:     "shift_left_to_u64",
			src:      `poly = (v u32) { r u64 = v << 32 }`,
			wantErrs: 0,
		},

		// --- 不安全：應報錯 ---
		{
			name:      "plain_u64_to_u32_no_mask",
			src:       `poly = (d0 u64) { h0 u32 = d0 }`,
			wantError: true,
		},
		{
			name:      "addition_u64_to_u32",
			src:       `poly = (a u64, b u64) { c u32 = a + b }`,
			wantError: true,
		},
		{
			name:      "subtraction_u64_to_u32",
			src:       `poly = (a u64, b u64) { c u32 = a - b }`,
			wantError: true,
		},
		{
			name:      "call_result_u64_to_u32",
			src:       `foo = () (r u64) { r = 0 } poly = () { x u32 = foo() }`,
			wantError: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := countTypeErrors(t, tt.src)
			if tt.wantError {
				if len(errs) == 0 {
					t.Errorf("expected at least one type error, got none")
				}
			} else {
				for _, e := range errs {
					t.Errorf("unexpected type error L%d:C%d: %s", e.Line, e.Column, e.Message)
				}
			}
		})
	}
}

// TestNarrowingHintMessage verifies that the error message for unsafe
// narrowing includes an actionable hint with the correct mask value and
// shift amount for the target type.
func TestNarrowingHintMessage(t *testing.T) {
	tests := []struct {
		name       string
		src        string
		wantMask   string // expected mask value in the hint
		wantShift  string // expected shift amount in the hint
		wantSigned bool   // true if the hint should mention signed-target caveat
	}{
		{
			name:      "u64_to_u32_hint",
			src:       `poly = (d0 u64) { h0 u32 = d0 }`,
			wantMask:  "4294967295", // 2^32 - 1
			wantShift: "32",
		},
		{
			name:      "u64_to_u16_hint",
			src:       `poly = (d0 u64) { h0 u16 = d0 }`,
			wantMask:  "65535", // 2^16 - 1
			wantShift: "48",
		},
		{
			name:      "u64_to_u8_hint",
			src:       `poly = (d0 u64) { h0 u8 = d0 }`,
			wantMask:  "255", // 2^8 - 1
			wantShift: "56",
		},
		{
			name:      "u64_to_byte_hint",
			src:       `poly = (d0 u64) { h0 byte = d0 }`,
			wantMask:  "255",
			wantShift: "56",
		},
		{
			name:      "u32_to_u16_hint",
			src:       `poly = (d0 u32) { h0 u16 = d0 }`,
			wantMask:  "65535",
			wantShift: "16",
		},
		{
			name:       "u64_to_i32_signed_hint",
			src:        `poly = (d0 u64) { h0 i32 = d0 & 4294967295 }`,
			wantSigned: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := countTypeErrors(t, tt.src)
			if len(errs) == 0 {
				t.Fatal("expected at least one type error, got none")
			}
			msg := errs[0].Message
			if tt.wantSigned {
				if !strings.Contains(msg, "signed target") || !strings.Contains(msg, "ambiguous") {
					t.Errorf("error message should mention signed-target caveat, got: %s", msg)
				}
				return
			}
			if !strings.Contains(msg, "hint:") {
				t.Errorf("error message should contain a hint, got: %s", msg)
			}
			if !strings.Contains(msg, tt.wantMask) {
				t.Errorf("error message should contain mask %s, got: %s", tt.wantMask, msg)
			}
			if !strings.Contains(msg, ">> "+tt.wantShift) {
				t.Errorf("error message should contain shift >> %s, got: %s", tt.wantShift, msg)
			}
			t.Logf("hint message: %s", msg)
		})
	}
}

// TestNarrowingToSignedRejected verifies that narrowing to a SIGNED integer
// type is NOT allowed even with a bitwise expression, because sign-bit
// truncation semantics are ambiguous.
func TestNarrowingToSignedRejected(t *testing.T) {
	src := `poly = (d0 u64) { h0 i32 = d0 & 4294967295 }`
	errs := countTypeErrors(t, src)
	if len(errs) == 0 {
		t.Errorf("expected type error for narrowing u64 & mask to i32, got none")
	}
}

// TestGenericNotAffected verifies that generic-typed variables (where
// inferExprType falls back to i64) are not falsely flagged.
func TestGenericNotAffected(t *testing.T) {
	// key has generic type k; idx is i64. The & expression infers to i64,
	// which is compatible with i64 — no error.
	src := `hashmap-tmpl.hash = (key k) (idx i64) {
    idx = key & (.cap - 1)
}`
	errs := countTypeErrors(t, src)
	for _, e := range errs {
		t.Errorf("unexpected type error L%d:C%d: %s", e.Line, e.Column, e.Message)
	}
}

// TestCharArrayLiteralToByteArray verifies that char literals (double-quoted
// single characters like "a") can be assigned to []byte / [N]byte arrays when
// their Unicode code point fits within the byte range (0–255).
func TestCharArrayLiteralToByteArray(t *testing.T) {
	tests := []struct {
		name      string
		src       string
		wantErrs  int
		wantError bool
	}{
		// --- ASCII chars in byte range: should pass ---
		{
			name:     "ascii_chars_to_byte_slice",
			src:      `input []byte = ["a", "b", "c"]`,
			wantErrs: 0,
		},
		{
			name:     "ascii_chars_to_fixed_byte_array",
			src:      `data [3]byte = ["a", "b", "c"]`,
			wantErrs: 0,
		},
		{
			name:     "digit_chars_to_byte_slice",
			src:      `digits []byte = ["0", "1", "2", "3"]`,
			wantErrs: 0,
		},
		{
			name:     "high_ascii_char_to_byte",
			src:      "data []byte = [\"ÿ\"]",
			wantErrs: 0,
		},
		// --- Non-ASCII chars outside byte range: should fail ---
		{
			name:      "unicode_char_outside_byte_range",
			src:       `data []byte = ["€"]`,
			wantError: true,
		},
		{
			name:      "cjk_char_outside_byte_range",
			src:       `data []byte = ["a", "中", "c"]`,
			wantError: true,
		},
		// --- Mixed valid array literals still work ---
		{
			name:     "int_literals_to_byte_slice",
			src:      `data []byte = [0x61, 0x62, 0x63]`,
			wantErrs: 0,
		},
		{
			name:     "int_literals_to_i64_array",
			src:      `nums [4] = [1, 2, 3, 4]`,
			wantErrs: 0,
		},
		// --- Out-of-range integer literal should still fail ---
		{
			name:      "int_too_large_for_byte",
			src:       `data []byte = [256]`,
			wantError: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := countTypeErrors(t, tt.src)
			if tt.wantError {
				if len(errs) == 0 {
					t.Errorf("expected at least one type error, got none")
				}
			} else {
				for _, e := range errs {
					t.Errorf("unexpected type error L%d:C%d: %s", e.Line, e.Column, e.Message)
				}
			}
		})
	}
}
