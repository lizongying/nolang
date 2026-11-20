package checker

import "testing"

// TestOptUnuseInterpolationVsConcat 釘住 option 與字符串的三種口徑：
//   - print/format 家族呼叫點的 `{field}` 插值 → 真消費，不報 optunuse
//   - 普通字符串字面量裡的 `{field}` → 不是插值（MIR 不展開），仍報
//   - `-` / `+` 字符串拼接 → 把 option 底層位模式拼進字符串的沉默泄漏，
//     不算消費，必須報 optunuse
//
// 背景：`s = 'v=' - a`（a 為 ?i64）實測輸出 12884901891（option 的 tag+payload
// 位模式），而 `format('v={a}')` 正確輸出 v=2。
func TestOptUnuseInterpolationVsConcat(t *testing.T) {
	cases := []struct {
		name         string
		src          string
		wantOptUnuse int
	}{
		{
			name: "print_interpolation_is_consumption",
			src: `f = () () {
    a = 1 + 1
    print('v={a}')
}`,
			wantOptUnuse: 0,
		},
		{
			name: "format_interpolation_is_consumption",
			src: `f = () () {
    a = 1 + 1
    s = format('v={a}')
    io.outln(s)
}`,
			wantOptUnuse: 0,
		},
		{
			name: "plain_string_literal_is_not_interpolation",
			src: `f = () () {
    a = 1 + 1
    s = 'v={a}'
    io.outln(s)
}`,
			wantOptUnuse: 1,
		},
		{
			name: "dash_concat_is_not_consumption",
			src: `f = () () {
    a = 1 + 1
    s = 'v=' - a
    io.outln(s)
}`,
			wantOptUnuse: 1,
		},
		{
			name: "plus_concat_is_not_consumption",
			src: `f = () () {
    a = 1 + 1
    s = 'v=' + a
    io.outln(s)
}`,
			wantOptUnuse: 1,
		},
		{
			name: "numeric_minus_is_not_concat",
			src: `f = () () {
    a = 1 + 1
    b = a - 1
    io.outln(b)
}`,
			wantOptUnuse: 0,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			prog := parseProg(t, c.src)
			res := ValidateUnhandledOverflowTHIR(prog, "src/app.no", BuildTHIRView(prog))
			n := 0
			for _, r := range res {
				if r.TraceID == OptUnuseTraceID {
					n++
				}
			}
			if n != c.wantOptUnuse {
				t.Errorf("want %d optunuse, got %d (all results: %+v)", c.wantOptUnuse, n, res)
			}
		})
	}
}
