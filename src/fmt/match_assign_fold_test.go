package fmt

// 「全臂同一目標賦值」的 match 折疊回歸（見 match_assign_fold.go）。
//
// 每條用例都同時驗三件事：形態（折疊 / 不折疊）、再解析（輸出必須仍能 parse）、
// 冪等（二次格式化不變）。折疊是**常規 fmt 規則**，沒有任何 --fix 旗標，所以測試
// 直接走 FormatFileWithErrors。

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

func foldFmt(t *testing.T, src string) string {
	t.Helper()
	out, errs := FormatFileWithErrors(src)
	if len(errs) > 0 {
		t.Fatalf("format errors: %v\nsource:\n%s", errs, src)
	}
	p := parser.New(lexer.New(out))
	p.SkipLowering = true
	p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("formatted output no longer parses: %v\noutput:\n%s", p.Errors(), out)
	}
	out2, errs2 := FormatFileWithErrors(out)
	if len(errs2) > 0 {
		t.Fatalf("second-pass errors: %v", errs2)
	}
	if out2 != out {
		t.Fatalf("not idempotent:\n--- pass1 ---\n%s\n--- pass2 ---\n%s", out, out2)
	}
	return out
}

func TestFoldMatchAssign(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		want    []string // 输出必须包含的片段
		notWant []string // 输出不得包含的片段
	}{
		{
			name: "enum_two_arms_with_catch_all",
			src: "f = (m mode) (flags i64) {\n" +
				"    m: {\n" +
				"        read -> flags = 1\n\n" +
				"        -> flags = 0\n" +
				"    }\n" +
				"}\n",
			want: []string{"flags = m: {", "read -> 1", "-> 0"},
		},
		{
			name: "index_subject_option",
			src: "f = (buf []byte, offset i64) (t i8) {\n" +
				"    buf[offset]: {\n" +
				"        ok -> t = it\n\n" +
				"        -> t = 0\n" +
				"    }\n" +
				"}\n",
			want: []string{"t = buf[offset]: {", "ok -> it", "-> 0"},
		},
		{
			// 多臂 + 通配 `_ ->`：同樣折疊，臂數不限。
			name: "multi_arm_wildcard",
			src: "f = (m mode) (flags i64) {\n" +
				"    m: {\n" +
				"        read -> flags = 1\n\n" +
				"        write -> flags = 2\n\n" +
				"        _ -> flags = 0\n" +
				"    }\n" +
				"}\n",
			want: []string{"flags = m: {", "read -> 1", "write -> 2", "-> 0"},
		},
		{
			// 無 catch-all：折疊會把「沒命中就不賦值」變成「賦零值」，行為改變。
			name: "no_catch_all_kept",
			src: "f = (m mode) (flags i64) {\n" +
				"    m: {\n" +
				"        read -> flags = 1\n\n" +
				"        write -> flags = 2\n" +
				"    }\n" +
				"}\n",
			want:    []string{"m: {", "read -> flags = 1"},
			notWant: []string{"flags = m: {"},
		},
		{
			// 各臂目標不同：不是「同一目標」，保持原樣。
			name: "different_targets_kept",
			src: "f = (m mode) (a i64) {\n" +
				"    m: {\n" +
				"        read -> a = 1\n\n" +
				"        -> b = 0\n" +
				"    }\n" +
				"}\n",
			want:    []string{"m: {", "read -> a = 1"},
			notWant: []string{"= m: {"},
		},
		{
			// 臂體是區塊形態：折疊與否屬排版決定，保守跳過。
			name: "block_arms_kept",
			src: "f = (m mode) (flags i64) {\n" +
				"    m: {\n" +
				"        read -> {\n" +
				"            flags = 1\n" +
				"        }\n\n" +
				"        -> {\n" +
				"            flags = 0\n" +
				"        }\n" +
				"    }\n" +
				"}\n",
			want:    []string{"m: {", "flags = 1"},
			notWant: []string{"flags = m: {"},
		},
		{
			// 臂前的 doc 註釋綁在臂內陳述上：折疊會把註釋挪到 match 外或丟掉，
			// 兩者都改變使用者寫下的位置 ⇒ 保守跳過（註釋隨原樣保留）。
			name: "arm_doc_comment_kept",
			src: "f = (m mode) (flags i64) {\n" +
				"    m: {\n" +
				"        ; note\n" +
				"        read -> flags = 1\n\n" +
				"        -> flags = 0\n" +
				"    }\n" +
				"}\n",
			want:    []string{"m: {", "; note", "read -> flags = 1"},
			notWant: []string{"flags = m: {"},
		},
		{
			// 目標帶顯式型別標註：折疊要搬移標註，各臂可能不一致，跳過。
			name: "explicit_type_kept",
			src: "f = (m mode) (flags i64) {\n" +
				"    m: {\n" +
				"        read -> flags i64 = 1\n\n" +
				"        -> flags i64 = 0\n" +
				"    }\n" +
				"}\n",
			want:    []string{"m: {"},
			notWant: []string{"flags i64 = m: {"},
		},
		{
			// 隱含 self 主體：fmt 會把 self 印成 `.`，`v = .: { … }` 不可解析。
			name: "self_subject_kept",
			src: "code.to-name = () (s str) {\n" +
				"    .: {\n" +
				"        ok -> s = 'ok'\n\n" +
				"        -> s = 'other'\n" +
				"    }\n" +
				"}\n",
			want:    []string{".: {", "ok -> s = 'ok'"},
			notWant: []string{"s = .: {"},
		},
		{
			// 已經是目標形態：不得再折一次（冪等）。
			name: "already_folded_kept",
			src: "f = (m mode) (flags i64) {\n" +
				"    flags = m: {\n" +
				"        read -> 1\n\n" +
				"        -> 0\n" +
				"    }\n" +
				"}\n",
			want: []string{"flags = m: {"},
		},
		{
			// 值語境的 match（本身是賦值右值）不受影響。
			name: "value_context_kept",
			src: "f = (m mode) (flags i64) {\n" +
				"    out = m: {\n" +
				"        read -> 1\n\n" +
				"        -> 0\n" +
				"    }\n" +
				"    out\n" +
				"}\n",
			want:    []string{"out = m: {"},
			notWant: []string{"out = out ="},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := foldFmt(t, tc.src)
			for _, w := range tc.want {
				if !strings.Contains(out, w) {
					t.Fatalf("missing %q in output:\n%s", w, out)
				}
			}
			for _, nw := range tc.notWant {
				if strings.Contains(out, nw) {
					t.Fatalf("unexpected %q in output:\n%s", nw, out)
				}
			}
		})
	}
}
