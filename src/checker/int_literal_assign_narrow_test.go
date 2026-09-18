package checker

import (
	"strings"
	"testing"
)

// TestIntLiteralAssignNarrowingToDeclared verifies that an integer literal
// assigned to a variable that ALREADY has a concrete integer type is treated
// as a constant conversion instead of a narrowing error.
//
// 放行：負字面量 → 無號目標（二補數位模式，如 `b byte = -1` == 0xFF）。
//   std/archive/xz.no 的 `unpacked-size = -1`（unpacked-size 為 byte）即此例。
//
// 仍然報錯（真實數值溢位，不能靜默）：
//   - 正值超出目標上界：`b byte = 300`
//   - 負值低於有號目標下界：`n i8 = -200`
//   - 截斷有歧義：`b byte = -200`（byte 只接受 -128..255）
//
// 範圍僅限指派語句；呼叫實參不適用（見 array_slice_compat_test.go：
// `set-u64(-1)` 仍報錯）。
func TestIntLiteralAssignNarrowingToDeclared(t *testing.T) {
	tests := []struct {
		name     string
		src      string
		wantErrs int
	}{
		// --- 負字面量 → 無號目標：放行 ---
		{
			name:     "neg_literal_to_byte_decl",
			src:      `f = () { b byte = -1 }`,
			wantErrs: 0,
		},
		{
			name: "neg_literal_to_byte_reassign",
			src: `f = (d []byte) {
    b = d[0]
    b = -1
}`,
			wantErrs: 0,
		},
		{
			name:     "neg_literal_to_u16",
			src:      `f = () { u u16 = -1 }`,
			wantErrs: 0,
		},
		{
			name:     "neg_literal_to_u32",
			src:      `f = () { u u32 = -2 }`,
			wantErrs: 0,
		},
		{
			name:     "neg_literal_to_u64",
			src:      `f = () { u u64 = -1 }`,
			wantErrs: 0,
		},
		{
			name:     "neg_literal_to_u8_signed_min_bound",
			src:      `f = () { b u8 = -128 }`,
			wantErrs: 0,
		},
		// --- 範圍內字面量：本來就合法 ---
		{
			name:     "in_range_byte",
			src:      `f = () { b byte = 200 }`,
			wantErrs: 0,
		},
		// --- 真實溢位：仍然報錯 ---
		{
			name:     "positive_overflow_byte",
			src:      `f = () { b byte = 300 }`,
			wantErrs: 1,
		},
		{
			name:     "negative_below_signed_min",
			src:      `f = () { n i8 = -200 }`,
			wantErrs: 1,
		},
		{
			name:     "ambiguous_truncation_byte",
			src:      `f = () { b byte = -200 }`,
			wantErrs: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := countTypeErrors(t, tt.src)
			var narrowing []string
			for _, r := range results {
				if strings.Contains(r.Message, "cannot assign") {
					narrowing = append(narrowing, r.Message)
				}
			}
			if len(narrowing) != tt.wantErrs {
				t.Errorf("expected %d 'cannot assign' error(s), got %d: %v",
					tt.wantErrs, len(narrowing), narrowing)
			}
		})
	}
}
