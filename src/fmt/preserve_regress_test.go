package fmt

import (
	"strings"
	"testing"
)

// REGRESSION GUARD (in-as-identifier statement boundary).
//
// `in` is a lexer keyword but is commonly used as a parameter / variable name
// (e.g. `aes-128-cbc-enc = (in []byte, …)`). When a statement is followed by a
// line whose first token is `in`, skipToStatementEnd must treat `in` as a
// statement boundary. Previously parser/stmt.go isStatementBoundary lacked
// lexer.IN, so the preceding statement's skipToStatementEnd swallowed the
// leading `in` of the NEXT statement and the source was corrupted:
//
//	in.len() == 0 -> out = 1     ⇒     .len() == 0 -> out = 1
//
// (also made the file non-idempotent). See parser/annotation.go's attach
// condition, which likewise had to accept lexer.IN.
func TestFormatInIdentifierNotSwallowed(t *testing.T) {
	in := "f = (in []byte) (out i64) {\n    #{overflow=wrap}\n    in.len() == 0 -> out = 1\n    out = 0\n}\n"
	// The annotation line is a node head, so it is separated from `{` by one
	// blank line; the `in` identifier itself must stay intact.
	want := "f = (in []byte) (out i64) {\n\n    #{overflow=wrap}\n    in.len() == 0 -> out = 1\n    out = 0\n}\n"
	out, errs := FormatFileWithErrors(in)
	if len(errs) > 0 {
		t.Fatalf("format errors: %v", errs)
	}
	if !strings.Contains(out, "in.len() == 0") {
		t.Fatalf("`in` identifier corrupted by formatter:\n--- want ---\n%q\n--- got ---\n%q", want, out)
	}
	if out != want {
		t.Fatalf("output not stable:\n--- want ---\n%q\n--- got ---\n%q", want, out)
	}
	out2, _ := FormatFileWithErrors(out)
	if out2 != out {
		t.Fatalf("not idempotent:\n--- pass1 ---\n%q\n--- pass2 ---\n%q", out, out2)
	}
}

// REGRESSION GUARD (a plain index assignment must stay `=`).
//
// `x = v[i]` in an option-returning function used to be promoted by
// parser/lowering.go maybeAutoPropagateIndex into an
// UnwrapAssignStatement{IsAutoPropagated: true} even in format-only mode
// (SkipUnwrapLowering gates the *expansion*, not the promotion). fmt/stmt.go
// rendered every UnwrapAssignStatement as `?=`, so `no fmt` silently rewrote
// `=` into `?=` on every save.
//
// That promotion is gone (the capture pass replaced it, and it is fully gated
// by SkipUnwrapLowering), so the formatter must never see a synthetic `?=`
// here: `x = v[0]` has to round-trip verbatim.
func TestFormatAutoPropagatedIndexKeepsAssign(t *testing.T) {
	in := "f = (v []i64) (result ?i64) {\n    result = nil\n    x = v[0]\n    result = x\n}\n"
	out, errs := FormatFileWithErrors(in)
	if len(errs) > 0 {
		t.Fatalf("format errors: %v", errs)
	}
	if strings.Contains(out, "?=") {
		t.Fatalf("auto-propagated index assignment rewritten to `?=`:\n%q", out)
	}
	if out != in {
		t.Fatalf("output not stable:\n--- want ---\n%q\n--- got ---\n%q", in, out)
	}
}

// REGRESSION GUARD (doc comment above a plain index assignment).
//
// While `x = v[i]` was auto-promoted to `?=` the promotion dropped the
// statement's CommentedNode, so a doc comment sitting directly above it vanished
// on format. The comment must survive, and a blank line must be inserted between
// the preceding statement and the comment (the block-inner doc-comment blank-line
// rule).
func TestFormatDocCommentBeforeAutoPromotedIndex(t *testing.T) {
	in := "f = (v []i64) (result ?i64) {\n    result = nil\n    ; read the first element\n    x = v[0]\n    result = x\n}\n"
	want := "f = (v []i64) (result ?i64) {\n    result = nil\n\n    ; read the first element\n    x = v[0]\n    result = x\n}\n"
	out, errs := FormatFileWithErrors(in)
	if len(errs) > 0 {
		t.Fatalf("format errors: %v", errs)
	}
	if !strings.Contains(out, "; read the first element") {
		t.Fatalf("doc comment before auto-promoted index assignment was dropped:\n%q", out)
	}
	if out != want {
		t.Fatalf("doc-comment blank line not inserted:\n--- want ---\n%q\n--- got ---\n%q", want, out)
	}
}

// REGRESSION GUARD (standalone comment + line overflow annotation before a bare
// `{` match block must not be merged or dropped).
//
// A bare `{` match block may be preceded by an ordinary standalone comment line
// and a line-scoped `#{overflow=wrap}` annotation:
//
//	pos = pos + 1
//	; 名稱長度（varint，高位=0 表示 Huffman 關閉）
//	#{overflow=wrap}
//	{
//
// Previously an (older) formatter corrupted this shape in two ways:
//  1. it merged the standalone comment onto the preceding statement as an
//     inline trailing comment — `pos = pos + 1; 名稱長度…` — destroying the
//     comment's association with the `{` block;
//  2. it silently dropped the `#{overflow=wrap}` annotation, leaving integer
//     arithmetic to fall back to option<int>.
//
// The corrected formatter keeps the comment on its own line (separated from the
// previous statement by a blank line, since it is the `{` block's doc comment),
// preserves the `#{overflow=wrap}` line verbatim, and is idempotent.
func TestFormatStandaloneCommentPlusOverflowBeforeBareMatch(t *testing.T) {
	in := "encode-header = (name str, value str) (buf []byte, n i64) {\n" +
		"    pos = 0\n" +
		"    pos = pos + 1\n" +
		"    ; 名稱長度（varint，高位=0 表示 Huffman 關閉）\n" +
		"    #{overflow=wrap}\n" +
		"    {\n" +
		"        x < 64 -> {\n" +
		"            pos = pos + 1\n" +
		"        }\n" +
		"\n" +
		"        -> {\n" +
		"            pos = pos + 2\n" +
		"        }\n" +
		"    }\n" +
		"    return\n" +
		"}\n"
	want := "encode-header = (name str, value str) (buf []byte, n i64) {\n" +
		"    pos = 0\n" +
		"    pos = pos + 1\n" +
		"\n" +
		"    ; 名稱長度（varint，高位=0 表示 Huffman 關閉）\n" +
		"    #{overflow=wrap}\n" +
		"    {\n" +
		"        x < 64 -> {\n" +
		"            pos = pos + 1\n" +
		"        }\n" +
		"\n" +
		"        -> {\n" +
		"            pos = pos + 2\n" +
		"        }\n" +
		"    }\n" +
		"    return\n" +
		"}\n"
	out, errs := FormatFileWithErrors(in)
	if len(errs) > 0 {
		t.Fatalf("format errors: %v", errs)
	}
	// The standalone comment must NOT be merged into the previous statement.
	if strings.Contains(out, "pos = pos + 1; 名稱長度") {
		t.Fatalf("standalone comment merged into preceding statement:\n%q", out)
	}
	// The line-scoped overflow annotation must survive, exactly once.
	if got := strings.Count(out, "#{overflow"); got != 1 {
		t.Fatalf("expected exactly 1 overflow annotation, got %d:\n%q", got, out)
	}
	if !strings.Contains(out, "; 名稱長度（varint，高位=0 表示 Huffman 關閉）") {
		t.Fatalf("standalone comment was dropped:\n%q", out)
	}
	if out != want {
		t.Fatalf("output not stable:\n--- want ---\n%q\n--- got ---\n%q", want, out)
	}
	// Idempotency: a second pass must reproduce the same output.
	out2, _ := FormatFileWithErrors(out)
	if out2 != out {
		t.Fatalf("not idempotent:\n--- pass1 ---\n%q\n--- pass2 ---\n%q", out, out2)
	}
}

// REGRESSION GUARD (line-scoped overflow annotation preserved in place).
//
// `#{overflow=wrap}` is a *line* annotation: it applies only to the single
// statement on the line immediately below it (see parser
// applyLineOverflowAnnotations), and is no longer hoisted to the top of a
// block. The formatter must therefore emit each occurrence verbatim, on its own
// line, without dropping or collapsing it. This prevents the prior silent
// loss: reformatting used to fold away block-scoped overflow annotations, which
// left integer arithmetic to silently fall back to option<int> (LLVM then
// rejected the value after truncation to a narrow type).
//
// With line-scoped annotations, the source keeps one `#{overflow=wrap}` directly
// above every statement that needs wrap mode, and `no fmt` reproduces it
// byte-for-byte (idempotent) — each annotated statement being its own node, it is
// separated from the code above by one blank line.
func TestFormatLineScopedOverflowPreservedInPlace(t *testing.T) {
	in := "f = (spec []i64, n i64) (out i64) {\n" +
		"    last-wi = 0\n" +
		"    wi <- [0..n): {\n" +
		"        c = spec[wi]\n" +
		"        c < 48 || c > 57 -> break\n" +
		"        #{overflow=wrap}\n" +
		"        width-v = width-v * 10 + c - 48\n" +
		"        #{overflow=wrap}\n" +
		"        last-wi = wi + 1\n" +
		"    }\n" +
		"    out = last-wi\n" +
		"}\n"
	want := "f = (spec []i64, n i64) (out i64) {\n" +
		"    last-wi = 0\n" +
		"    wi <- [0..n): {\n" +
		"        c = spec[wi]\n" +
		"        c < 48 || c > 57 -> break\n" +
		"\n" +
		"        #{overflow=wrap}\n" +
		"        width-v = width-v * 10 + c - 48\n" +
		"\n" +
		"        #{overflow=wrap}\n" +
		"        last-wi = wi + 1\n" +
		"    }\n" +
		"    out = last-wi\n" +
		"}\n"
	out, errs := FormatFileWithErrors(in)
	if len(errs) > 0 {
		t.Fatalf("format errors: %v", errs)
	}
	if strings.Count(out, "#{overflow") != 2 {
		t.Fatalf("expected exactly 2 line-scoped overflow annotations, got:\n%q", out)
	}
	if out != want {
		t.Fatalf("line-scoped overflow annotation not preserved in place:\n--- want ---\n%q\n--- got ---\n%q", want, out)
	}
	// Idempotency: a second pass must reproduce the same output.
	out2, _ := FormatFileWithErrors(out)
	if out2 != out {
		t.Fatalf("not idempotent:\n--- pass1 ---\n%q\n--- pass2 ---\n%q", out, out2)
	}
}

// REGRESSION GUARD (a short multi-line infix collapses to one line).
//
// A multi-line infix whose operands fit comfortably on one line was an
// incidental source break, not the long string-concat idiom. Previously
// formatInfixExpression always preserved the source split, so
//
//	d = 1 +
//	2
//
// was reformatted to `d = 1 +\n    2` (the continuation aligned to the RHS
// column). It must instead collapse to `d = 1 + 2`. Long concatenation chains
// (covered by the str1/INVSBOX case) stay split.
func TestFormatShortMultilineInfixCollapses(t *testing.T) {
	in := "d = 1 +\n2\n"
	want := "d = 1 + 2\n"
	out, errs := FormatFileWithErrors(in)
	if len(errs) > 0 {
		t.Fatalf("format errors: %v", errs)
	}
	if out != want {
		t.Fatalf("short multi-line infix not collapsed:\n--- want ---\n%q\n--- got ---\n%q", want, out)
	}
	out2, _ := FormatFileWithErrors(out)
	if out2 != out {
		t.Fatalf("not idempotent:\n--- pass1 ---\n%q\n--- pass2 ---\n%q", out, out2)
	}
}

// REGRESSION GUARD (struct field trailing `;` comments must stay in place).
//
// Struct fields are not Statements, so the parser never ran attachInlineComment
// on them and the formatter never emitted field.GetComment(). The comments were
// left in the parser's buffered comment queue, then drained by the enclosing
// block as *trailing* comments of the struct body and re-emitted AFTER the
// closing brace:
//
//	ast {
//	    str-k []str; interning table
//	}
//
// became
//
//	ast {
//	    str-k []str
//	}
//
//	; interning table
//
// i.e. the comment silently drifted out of the field it documents. Fields must
// keep their own trailing comment, exactly as `compiler/src/ast.no` relies on.
func TestFormatStructFieldTrailingCommentsPreserved(t *testing.T) {
	in := "ast {\n" +
		"    nodes []nd\n" +
		"    str-k []str; string-interning hash table (open addressing; grows on demand)\n" +
		"    str-v []i64; maps interned string -> index in strs\n" +
		"    str-n i64; live entry count in str-k/str-v (drives load-factor growth)\n" +
		"}\n"
	out, errs := FormatFileWithErrors(in)
	if len(errs) > 0 {
		t.Fatalf("format errors: %v", errs)
	}
	if out != in {
		t.Fatalf("struct field trailing comments not preserved:\n--- want ---\n%q\n--- got ---\n%q", in, out)
	}
	// The comments must not be relocated below the struct body.
	if strings.Contains(out, "}\n\n;") {
		t.Fatalf("field comments drifted past the closing brace:\n%q", out)
	}
	out2, _ := FormatFileWithErrors(out)
	if out2 != out {
		t.Fatalf("not idempotent:\n--- pass1 ---\n%q\n--- pass2 ---\n%q", out, out2)
	}
}

// REGRESSION GUARD (doc comment on a line above a struct field survives).
//
// The same comment-queue plumbing that dropped trailing field comments also
// mis-attached a field's standalone `;` doc line: it stayed buffered and was
// emitted as a trailing comment after the struct instead of above the field.
func TestFormatStructFieldDocCommentPreserved(t *testing.T) {
	in := "ast {\n" +
		"    ; doc line for a\n" +
		"    a i64\n" +
		"    b i64; trailing b\n" +
		"}\n"
	out, errs := FormatFileWithErrors(in)
	if len(errs) > 0 {
		t.Fatalf("format errors: %v", errs)
	}
	if out != in {
		t.Fatalf("struct field doc comment not preserved:\n--- want ---\n%q\n--- got ---\n%q", in, out)
	}
	out2, _ := FormatFileWithErrors(out)
	if out2 != out {
		t.Fatalf("not idempotent:\n--- pass1 ---\n%q\n--- pass2 ---\n%q", out, out2)
	}
}

// REGRESSION GUARD (an if-group inside an arm body must keep its own braces).
//
// `cond -> { { inner -> ...  -> ... } }` 的內層 `{}` 是**獨立的 short-circuit
// chain**（if 組），不是可以省略的多餘區塊。曾有一版 fmt 把內層 `{}` 當成
// wrapper 一併印成無大括號形態，內層的臂於是被外層 chain 吸收 —— 語意改變：
// 以 f64.round 為例，x = -3.7 時 `f <= -0.5` 臂不再屬於 `. >= 0.0` 的 else 分支，
// 結果從 -4.0 變成 -3.0。wrapper 節點在 parser 側由 IsWrapper 標記（人工造、
// 無對應源碼大括號），fmt 必須跳過「wrapper 自己」的那一層，而不能連內層 chain
// 的大括號一起吞掉。
func TestFormatNestedChainArmKeepsBraceGroup(t *testing.T) {
	in := "f = (x f64) (res f64) {\n" +
		"    {\n" +
		"        x >= 0.0 -> {\n" +
		"            {\n" +
		"                x >= 0.5 -> {\n" +
		"                    res = 1.0\n" +
		"                }\n" +
		"\n" +
		"                -> {\n" +
		"                    res = 0.0\n" +
		"                }\n" +
		"            }\n" +
		"        }\n" +
		"\n" +
		"        -> {\n" +
		"            res = -1.0\n" +
		"        }\n" +
		"    }\n" +
		"}\n"
	out, errs := FormatFileWithErrors(in)
	if len(errs) > 0 {
		t.Fatalf("format errors: %v", errs)
	}
	if out != in {
		t.Fatalf("if-group braces inside an arm body were rewritten:\n--- want ---\n%s\n--- got ---\n%s", in, out)
	}
	out2, _ := FormatFileWithErrors(out)
	if out2 != out {
		t.Fatalf("not idempotent:\n--- pass1 ---\n%s\n--- pass2 ---\n%s", out, out2)
	}
}

// REGRESSION GUARD (`v ?= expr ; comment` must keep its inline comment).
//
// parser.setComment had a case for every CommentedNode-bearing statement except
// UnwrapAssignStatement. Because setDoc *did* have that case, only the
// same-line form was affected: `; 說明` written above the `?=` line survived,
// while `v ?= expr ; 說明` had its comment attached to nothing and was then
// silently dropped by the formatter — user-written content turned into data
// loss. Same class as the TypeAlias / EmptyStatement cases already guarded
// there.
func TestFormatUnwrapAssignKeepsInlineComment(t *testing.T) {
	in := "f = (p str) (result ?str) {\n    v ?= open(p) ; 說明\n    result = v\n}\n"
	// Inline comments are printed flush against the statement (`a = 1; c`),
	// which is the formatter's long-standing canonical spelling.
	want := "f = (p str) (result ?str) {\n    v ?= open(p); 說明\n    result = v\n}\n"
	out, errs := FormatFileWithErrors(in)
	if len(errs) > 0 {
		t.Fatalf("format errors: %v", errs)
	}
	if !strings.Contains(out, "說明") {
		t.Fatalf("inline comment on `?=` was dropped:\n--- want ---\n%q\n--- got ---\n%q", want, out)
	}
	if out != want {
		t.Fatalf("unexpected output:\n--- want ---\n%q\n--- got ---\n%q", want, out)
	}
	out2, _ := FormatFileWithErrors(out)
	if out2 != out {
		t.Fatalf("not idempotent:\n--- pass1 ---\n%q\n--- pass2 ---\n%q", out, out2)
	}
}

// REGRESSION GUARD (an arm body wrapping a **matched** match keeps its braces).
//
// `cond -> { x: { ... } }` 的外層 `{}` 是使用者明確寫下的區塊。writeArm 只在
// 兩種情況可以內聯：body 原本就是 inline（IsInline），或唯一陳述是「裸 match」
// （`{ ... }`，印出來自帶大括號，內聯不會多一層）。S4 糖化時 isBareMatchBody
// 從降糖期的 RTBareMatch 旗標放寬成「任意 *SurfaceMatch」，於是帶主語的
// match 也被當成裸 match：
//
//	cond -> { rle-idx[...]: { ... } }  =>  cond -> rle-idx[...]: { ... }
//
// 丟掉一層大括號，臂主體也從 block 變成 inline expression。真實案例：
// noimg/src/bmp.no 的 RLE4/RLE8 解碼。第二則案例反向釘住「裸 match 仍要內聯」，
// 避免把 isBareMatchBody 整個拿掉而多包一層 `{}`。
func TestFormatMatchedMatchInArmBodyKeepsBlock(t *testing.T) {
	cases := map[string]string{
		"matchedMatch": "f = (n i64, v ?i64) (r i64) {\n" +
			"    {\n" +
			"        n == 1 -> {\n" +
			"            n > 0 -> {\n" +
			"                v: {\n" +
			"                    ok -> r = it\n" +
			"\n" +
			"                    -> _\n" +
			"                }\n" +
			"            }\n" +
			"        }\n" +
			"\n" +
			"        -> r = 0\n" +
			"    }\n" +
			"}\n",
		"bareMatch": "f = (n i64) (r i64) {\n" +
			"    {\n" +
			"        n == 1 -> {\n" +
			"            n > 0 -> r = 1\n" +
			"\n" +
			"            -> r = 2\n" +
			"        }\n" +
			"\n" +
			"        -> r = 0\n" +
			"    }\n" +
			"}\n",
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			out, errs := FormatFileWithErrors(in)
			if len(errs) > 0 {
				t.Fatalf("format errors: %v", errs)
			}
			if out != in {
				t.Fatalf("arm body braces were rewritten:\n--- want ---\n%s\n--- got ---\n%s", in, out)
			}
			out2, _ := FormatFileWithErrors(out)
			if out2 != out {
				t.Fatalf("not idempotent:\n--- pass1 ---\n%s\n--- pass2 ---\n%s", out, out2)
			}
		})
	}
}
