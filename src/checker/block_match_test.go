package checker

import (
	"strings"
	"testing"
)

// 塊 match 的糖 Match 是裸的（Matched == nil，讓 `no fmt` 印得出 `}: {`），
// 但窮盡性仍走標準三態 ?T 規則：沒有兜底臂時連不可達的 `ok ->` 也要補。
func TestBlockMatchExhaustiveness(t *testing.T) {
	cases := []struct {
		name  string
		arms  string
		count int
	}{
		{
			name: "missing_ok_and_catchall",
			arms: "        err -> _\n\n        nil -> _\n",
			count: 1,
		},
		{
			name: "only_err",
			arms: "        err -> _\n",
			count: 1,
		},
		{
			name:  "all_three_variants",
			arms:  "        nil -> _\n\n        err -> _\n\n        ok -> _\n",
			count: 0,
		},
		{
			name:  "catchall_discard",
			arms:  "        err -> _\n\n        -> _\n",
			count: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := "f = (d i64) {\n    {\n        v ?= 6 / d\n        print(v.to-str())\n    }: {\n" + tc.arms + "    }\n}\n"
			results := ValidateNonExhaustiveMatch(parseProg(t, src))
			if got := countMatchNonex(results); got != tc.count {
				t.Fatalf("want %d nonexhaustive diagnostics, got %d: %v", tc.count, got, results)
			}
			for _, r := range results {
				if strings.Contains(r.Message, "__bm_") {
					t.Errorf("diagnostic exposes the synthetic subject: %s", r.Message)
				}
			}
		})
	}
}

// 合成 subject 不得被當成未定義變數（checker 看的是糖 AST，沒有 let __bm_N）。
func TestBlockMatchSubjectIsNotUndefined(t *testing.T) {
	src := `f = (d i64) {
    {
        v ?= 6 / d
        print(v.to-str())
    }: {
        err -> print(it.msg())

        -> _
    }
}
`
	results := ValidateUndefinedVars(parseProg(t, src), "")
	for _, r := range results {
		if strings.Contains(r.Message, "__bm_") {
			t.Fatalf("synthetic subject reported undefined: %v", r)
		}
	}
}
