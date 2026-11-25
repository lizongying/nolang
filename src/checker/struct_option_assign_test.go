package checker

import "testing"

// struct option 的 `?T → T` 隐式解包门禁（15w45dqk）此前只覆盖内建标量：
// `isConcreteType("node")` 對自訂 struct 回 false，讓 11249 行整條判定
// 短路，於是 `z ?node = n; v node = z` 假綠。 value-match 的 join 規則
// （t|t=t, t|?t=?t）要求 `z: {ok->it; ->z}` 的結果是 `?node`，故
// `v node = ...` 必須被拒，而 `v ?node = ...` / `v = ...` 必須放行。
//
// 語義來源：使用者 2026-11-25 給出的 join 表。

const structOptionAssignSrc = `node {
    name str
    deps []node
}
n node
n.name = 'x'
n.deps = []
`

func TestStructOptionAssignToPlainRejected(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			name: "direct_assign",
			src: structOptionAssignSrc + `z ?node = n
v node = z
`,
		},
		{
			name: "value_match_join",
			src: structOptionAssignSrc + `z ?node = n
v node = z: { ok -> it
    -> z }
`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			results := validateSrc(t, c.src)
			if n := countTrace(results, "15w45dqk"); n == 0 {
				t.Fatalf("expected 15w45dqk for ?node → node, got none: %v", results)
			}
		})
	}
}

func TestStructOptionAssignToOptionAllowed(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			name: "option_target_match",
			src: structOptionAssignSrc + `z ?node = n
v ?node = z: { ok -> it
    -> z }
`,
		},
		{
			name: "inferred_target_match",
			src: structOptionAssignSrc + `z ?node = n
v = z: { ok -> it
    -> z }
`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			results := validateSrc(t, c.src)
			if n := countTrace(results, "15w45dqk"); n != 0 {
				t.Fatalf("unexpected 15w45dqk for ?node join into option/inferred target: %v", results)
			}
		})
	}
}
