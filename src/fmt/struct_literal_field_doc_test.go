package fmt

import (
	"strings"
	"testing"
)

// TestFormatStructLiteralFieldDocStaysOnField 固定使用者回報的反例：
// compiler/src/h2m.no 裡 h2m-new 的 s99j 釋放清單註解必須依附在 `rls: []`
// 上，不能被 no fmt 搬到字面量 `}` 之後、變成 `c.vty.push(...)` 的 Doc。
//
// 從前 parseStructLiteral 不取走註解緩衝，formatStructLiteral 也不輸出
// field.Doc，兩邊合起來造成「欄位說明漂到下一條陳述」。此測試同時守住
// parse（Doc 存在）、fmt（印在欄位正上方）與冪等（二次 fmt 不再漂移）。
func TestFormatStructLiteralFieldDocStaysOnField(t *testing.T) {
	in := "f = () {\n" +
		"    c = h2m-ctx{\n" +
		"        ops: []\n" +
		"        stftn: []\n" +
		"        ; s99j：scope-exit 釋放清單\n" +
		"        ;   rls = 待釋放值 id，rlsm = 欄位遮罩\n" +
		"        rls: []\n" +
		"        rlsm: []\n" +
		"    }\n" +
		"    c.vty.push(TY-I64)\n" +
		"}\n"
	want := strings.TrimSuffix(in, "\n")

	got := Format(in)
	if got != want {
		t.Fatalf("struct literal field doc was moved:\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}

	// 明確的「依附」斷言：註解必須緊貼在 rls 前，而不是出現在字面量之後。
	docAt := strings.Index(got, "; s99j：scope-exit 釋放清單")
	rlsAt := strings.Index(got, "rls: []")
	closeAt := strings.Index(got, "\n    }\n")
	pushAt := strings.Index(got, "c.vty.push")
	if docAt < 0 || rlsAt < 0 || closeAt < 0 || pushAt < 0 {
		t.Fatalf("missing expected markers in output:\n%s", got)
	}
	if !(docAt < rlsAt && rlsAt < closeAt && closeAt < pushAt) {
		t.Fatalf("field doc is not attached to rls: doc=%d rls=%d close=%d push=%d\n%s",
			docAt, rlsAt, closeAt, pushAt, got)
	}
	if strings.Contains(got[closeAt:], "; s99j") {
		t.Fatalf("field doc drifted past the closing brace:\n%s", got)
	}

	if again := Format(got); again != got {
		t.Fatalf("not idempotent:\n--- pass1 ---\n%s\n--- pass2 ---\n%s", got, again)
	}
}

// TestFormatStructLiteralFieldDocBlankLine 守住註解組與欄位之間作者手寫的空行。
func TestFormatStructLiteralFieldDocBlankLine(t *testing.T) {
	in := "f = () {\n" +
		"    c = box{\n" +
		"        ; 計數器說明\n" +
		"\n" +
		"        n: 0\n" +
		"    }\n" +
		"}\n"
	want := strings.TrimSuffix(in, "\n")
	got := Format(in)
	if got != want {
		t.Fatalf("blank line between field doc and field not preserved:\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
	if again := Format(got); again != got {
		t.Fatalf("not idempotent:\n--- pass1 ---\n%s\n--- pass2 ---\n%s", got, again)
	}
}
