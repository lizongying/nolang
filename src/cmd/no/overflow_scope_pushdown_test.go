package main

import (
	"strings"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// `no fmt --fix=overflow` 的作用域下推（annotation-scope.md §2 的 L1/L2 → L3）
//
// 背景（全部為實測，非推論）：
//
//  1. `#{overflow=...}` 只在**單條陳述**（L3）上進 codegen。寫在函式頭（L1）或
//     塊頭（L2）時，codegen 完全不讀它——`checker.OverflowMode` 在 src/build
//     下沒有任何讀取點（grep 已核）。
//  2. L1/L2 同樣**不會**讓編譯期檢查放行。實測對照（同一份程式，只差頭部注解）：
//     `for` 頭加 `#{overflow=wrap}` 而體內寫 `x i8 = a + a`，報錯與不加注解
//     逐字相同（同為 15w45dqk）。裸塊頭更徹底：`{` 不在
//     parseAnnotationStatement 的可附加目標裡（只認 IDENT / USE / IN / RARROW /
//     陣列型別方法），注解退化成獨立的 AnnotationStatement，對誰都不生效。
//  3. 所以 L1/L2 上的 `#{overflow}` 是**純 lint 遮蔽**：只騙過
//     `stmtOverflowAnnotated` → `effOverflow` 的傳遞，讓 `no vet` 少報 hint。
//
// 下推的意思是：把這層遮蔽摘掉，改由逐條插入按需補 L3。摘掉不會改變任何既有的
// 編譯結果或執行期行為（第 1、2 點的推論），插不插入則由 checker 原樣判定——
// 已被 option 消費的語句（declaredOption 豁免）不會被硬塞注解，所以語意不變。
// ─────────────────────────────────────────────────────────────────────────────

// mustFixOverflow 跑一次 --fix=overflow 的純字串路徑並要求它確有改動。
func mustFixOverflow(t *testing.T, src string) string {
	t.Helper()
	out, ok := fixOverflowSource(src)
	if !ok {
		t.Fatalf("fixOverflowSource reported no change on:\n%s", src)
	}
	return out
}

// TestPushDownL1FunctionHead 函式頭注解必須消失，且其體的缺標運算就地補上 L3。
func TestPushDownL1FunctionHead(t *testing.T) {
	src := "#{overflow=wrap}\n" +
		"f = (n i8) (r i8) {\n" +
		"    r i8 = n + n\n" +
		"}\n"
	got := mustFixOverflow(t, src)
	if strings.Contains(got, "#{overflow=wrap}\nf = ") {
		t.Fatalf("L1 function-head annotation survived:\n%s", got)
	}
	if !strings.Contains(got, "    #{overflow=wrap}\n    r i8 = n + n") {
		t.Fatalf("per-statement annotation not inserted above the body statement:\n%s", got)
	}
}

// TestPushDownL2ForHead for 頭注解必須消失，體內缺標運算補 L3。
func TestPushDownL2ForHead(t *testing.T) {
	src := "f = (n i8) () {\n" +
		"    #{overflow=wrap}\n" +
		"    i <- [0..2): {\n" +
		"        print(n + n)\n" +
		"    }\n" +
		"}\n"
	got := mustFixOverflow(t, src)
	if strings.Contains(got, "#{overflow=wrap}\n    i <- [0..2): {") {
		t.Fatalf("L2 for-head annotation survived:\n%s", got)
	}
	if !strings.Contains(got, "        #{overflow=wrap}\n        print(n + n)") {
		t.Fatalf("per-statement annotation not inserted inside the loop body:\n%s", got)
	}
}

// TestPushDownL2BareBlock 裸塊頭是「漂移形態」：`{` 不是可附加目標，注解自成一條
// AnnotationStatement。這條路徑若不認，注解會永久留在塊首且永遠不生效。
func TestPushDownL2BareBlock(t *testing.T) {
	src := "f = (n i8) () {\n" +
		"    #{overflow=wrap}\n" +
		"    {\n" +
		"        print(n + n)\n" +
		"    }\n" +
		"}\n"
	got := mustFixOverflow(t, src)
	if strings.Contains(got, "#{overflow=wrap}\n    {\n") {
		t.Fatalf("detached bare-block annotation survived:\n%s", got)
	}
	if !strings.Contains(got, "        #{overflow=wrap}\n        print(n + n)") {
		t.Fatalf("per-statement annotation not inserted inside the bare block:\n%s", got)
	}
}

// TestPushDownKeepsTrailingAndMultiKey 尾隨合法、多鍵同行不可整行刪除，兩者都必須
// 原樣保留（刪整行會連帶丟掉別的鍵）。
func TestPushDownKeepsTrailingAndMultiKey(t *testing.T) {
	for _, src := range []string{
		"f = (n i8) (r i8) {\n    r i8 = n + n #{overflow=wrap}\n}\n",
		"#{overflow=wrap, js}\nf = (n i8) (r i8) {\n    r i8 = n + n\n}\n",
	} {
		if out, ok := fixOverflowSource(src); ok {
			t.Fatalf("fixer touched a form it must leave alone:\n--- in ---\n%s\n--- out ---\n%s", src, out)
		}
	}
}

// TestPushDownIsIdempotent 第二次執行必須逐字相同，否則批次套用會來回震盪。
func TestPushDownIsIdempotent(t *testing.T) {
	src := "#{overflow=wrap}\n" +
		"f = (n i8) (r i8) {\n" +
		"    #{overflow=wrap}\n" +
		"    i <- [0..2): {\n" +
		"        print(n + n)\n" +
		"    }\n" +
		"    r i8 = n + n\n" +
		"}\n"
	once := mustFixOverflow(t, src)
	if twice, ok := fixOverflowSource(once); ok {
		t.Fatalf("second pass changed output (not idempotent):\n--- once ---\n%s\n--- twice ---\n%s", once, twice)
	}
}

// TestPushDownLeavesOptionTypedStatementsAlone 目標已是 ?T 的陳述（由 declared 豁免
// 覆蓋）不得被硬塞注解——塞了會把 option 變成 plain，反而破壞消費端的 match。
func TestPushDownLeavesOptionTypedStatementsAlone(t *testing.T) {
	src := "#{overflow=wrap}\n" +
		"f = (n i8) () {\n" +
		"    b = n + n\n" +
		"    v = b: {\n" +
		"        ok -> it\n" +
		"        -> 0\n" +
		"    }\n" +
		"    print(v)\n" +
		"}\n"
	got := mustFixOverflow(t, src)
	if strings.Contains(got, "#{overflow") {
		t.Fatalf("option-typed statement got a plain-mode annotation (semantics changed):\n%s", got)
	}
}
