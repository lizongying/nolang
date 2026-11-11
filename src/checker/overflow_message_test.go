package checker

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/i18n/catalog"
)

// 本檔案釘住 ovf-int-default（TraceID xmicjezl）的**單一訊息口徑**。
//
// 2026-11-11 定稿：整數算術在**任何**位置都會溢出 —— 值根（`x = a + b`）、
// 呼叫實參（`f(a + 1)`）、match 臂、索引運算元（`arr[a + b]`）都一樣。
// 早期把訊息切成「值根版三選一（option 接收 / match 解包 / `_ =` 丟棄）」與
// 「索引版只教註解」是錯的：option 的疊加 / 短路 / 解包是另一條軸，
// 不該由這條 hint 教，而且按語法位置判斷「哪幾種補救寫得出來」本身就是誤導。
// 現在所有站點共用**一條**訊息：給擁有它的陳述加 `#{overflow = …}` 註解，
// 指定溢位取值。ovfidxop key 已從所有語系移除。
//
// 另外兩件仍照報的事實（與 option 無關）：
//   - 註解逐語句生效：`#{overflow=wrap}` 寫在上一行 `sb2 = seed[pos + i]`
//     只覆蓋那一條，管不到下一條的 `seed[8 + i]`（mysql.no:743 的形狀）。
//   - option match 主體的下標算術會被 lowering 提升成 `__idx_L_C ?T`、
//     溢位短路進失敗臂；已被消費不代表使用者聲明了溢出行為，失敗臂
//     （常見 `-> _`）會把資料默默丟掉，所以照樣報。
//
// 分類用「內嵌目錄裡該 key 的所有語系譯文」集合比對，而非硬編英文片段：
// 測試二進位的預設語系隨環境而變（en / zh-cn / zh-tw 皆可能），硬編字串會誤紅。

// messageSet 回傳某個 message key 在全部內嵌語系下的譯文集合。
func messageSet(t *testing.T, id string) map[string]bool {
	t.Helper()
	set := map[string]bool{}
	for _, tag := range catalog.Locales() {
		cat, err := catalog.Load(nil, tag)
		if err != nil {
			t.Fatalf("load %s: %v", tag, err)
		}
		if msg, ok := cat[id]; ok && msg != "" {
			set[msg] = true
		}
	}
	if len(set) == 0 {
		t.Fatalf("catalog has no entry for %q", id)
	}
	return set
}

// overflowHints 收集 ovf-int-default 的提示，並保證每一條都來自同一個
// message key（不存在第二套按站點分發的訊息）。
func overflowHints(t *testing.T, src string) []ValidateResult {
	t.Helper()
	set := messageSet(t, "xmicjezl")
	var out []ValidateResult
	for _, r := range ValidateIntOverflow(parseProg(t, src)) {
		if r.TraceID != "xmicjezl" {
			continue
		}
		if !set[r.Message] {
			t.Fatalf("hint at %d:%d is not the xmicjezl message for any locale: %q",
				r.Line, r.Column, r.Message)
		}
		out = append(out, r)
	}
	return out
}

// TestOverflowHintIsAnnotationOnly 逐語系檢查訊息**只**教加註解：
// 六種模式標記必須齊全，option 解包教學的標記一個都不能有。
func TestOverflowHintIsAnnotationOnly(t *testing.T) {
	set := messageSet(t, "xmicjezl")
	want := []string{"#{overflow = wrap}", "clamp0", "min", "max", "saturate", "u8-max"}
	ban := []string{"option<T>", "?T", "ok -> it", "?=", "_ = x + y",
		"三选一", "三選一", "Pick one of three", "receive it as", "two-arm match"}
	for msg := range set {
		for _, s := range want {
			if !strings.Contains(msg, s) {
				t.Errorf("hint is missing %q (not annotation-only): %q", s, msg)
			}
		}
		for _, s := range ban {
			if strings.Contains(msg, s) {
				t.Errorf("hint still teaches option unwrapping (%q): %q", s, msg)
			}
		}
	}
	// ovfidxop 必須已從所有語系移除；若還在，代表站點分支沒拆乾淨。
	for _, tag := range catalog.Locales() {
		cat, err := catalog.Load(nil, tag)
		if err != nil {
			t.Fatalf("load %s: %v", tag, err)
		}
		if _, ok := cat["ovfidxop"]; ok {
			t.Errorf("locale %s still carries the obsolete ovfidxop key", tag)
		}
	}
}

// TestOverflowUnhandledHintIsAnnotationFirst：同一條未標註算術還會被 D2 的
// ovfhndld（8t1jaj02）在陳述層報一次。它也必須以「加註解」為主，不能整段
// 在教 option 兩段式解包；option 的消費由 optunuse 這條 ERROR 負責。
func TestOverflowUnhandledHintIsAnnotationFirst(t *testing.T) {
	set := messageSet(t, "8t1jaj02")
	want := []string{"#{overflow = wrap}", "clamp0", "min", "max", "saturate", "u8-max"}
	ban := []string{"option<T>", "ok -> it", "two-step unwrap", "two-arm match",
		"?= <expr>", "Use the two-step", "两段式", "兩段式", "三选一", "三選一"}
	for msg := range set {
		if got := strings.Count(msg, "%s"); got != 1 {
			t.Errorf("8t1jaj02 must carry exactly one %%s (bound variable), got %d: %q", got, msg)
		}
		for _, s := range want {
			if !strings.Contains(msg, s) {
				t.Errorf("ovfhndld is missing %q (not annotation-first): %q", s, msg)
			}
		}
		for _, s := range ban {
			if strings.Contains(msg, s) {
				t.Errorf("ovfhndld still teaches option unwrapping (%q): %q", s, msg)
			}
		}
	}
}

func TestOverflowHintAtEverySite(t *testing.T) {
	// 索引寫入，原地、既無 match 也無 option 綁定。
	const indexWrite = `seed [16]byte
k = 3
seed[8 + k] = 65
`
	// 普通 let 讀：同樣是下標運算元。
	const plainRead = `seed [16]byte
k = 3
w = seed[8 + k]
w: { ok -> k = it  -> _ }
`
	// 值 match 主體：`{ 5 -> … }` 沒有失敗臂可消費下標選項。
	const valueMatch = `seed [16]byte
k = 3
seed[8 + k]: { 5 -> print(1)  -> _ }
`
	// option match 主體（mysql.no:743 的形狀）。
	const optionMatchWrite = `seed [16]byte
k = 3
seed[8 + k]: { ok -> it = 65  -> _ }
`
	const optionMatchRead = `seed []byte
k = 3
v = seed[8 + k]: {
    ok -> it
    -> 0
}
`
	// 註解寫在**擁有它的那條陳述**上 ⇒ 已聲明，不報。
	const optionMatchAnnotated = `seed [16]byte
k = 3
#{overflow=wrap}
seed[8 + k]: { ok -> it = 65  -> _ }
`
	// 註解寫在**相鄰語句**上（mysql.no:738 → 743）⇒ 逐語句生效，仍然報。
	const neighbourAnnotated = `seed [16]byte
k = 3
#{overflow=wrap}
sb = seed[8 + k]
seed[9 + k]: { ok -> it = sb  -> _ }
`
	// 手寫兩步式：`idx ?i64` 已明確聲明 option，後面的下標是識別字、沒有算術。
	const twoStep = `seed [16]byte
k = 3
idx ?i64 = 8 + k
seed[idx]: { ok -> it = 65  -> _ }
`
	// 值根：這裡舊版會給「三選一」option 教學，現在必須也是同一條註解版訊息。
	const bindRoot = `f = (a i64, b i64) () {
    a + b
}
`
	// 混在同一條陳述：索引運算元 + 值根，兩個站點各一條、但訊息相同。
	const mixed = `f = (arr []i64, i i64) () {
    arr[i + 1] + 2
}
`
	// 呼叫實參：CallExpression.Arguments 以值根口徑走查。
	const callArg = `f = (v i64) (r i64) {
    r = v
}
a i64 = 9223372036854775807
b = f(a + 1)
`
	const callArgAnnotated = `f = (v i64) (r i64) {
    r = v
}
a i64 = 9223372036854775807
#{overflow=wrap}
b = f(a + 1)
`
	// match 臂體內的算術（糖形態下否則不可見）。
	const matchArm = `f = (a i64, b i64) () {
    a: {
        ok -> b = a + b

        -> _
    }
}
`
	cases := []struct {
		name string
		src  string
		want int
	}{
		{"index_write", indexWrite, 1},
		{"plain_read", plainRead, 1},
		{"value_match", valueMatch, 1},
		{"option_match_write", optionMatchWrite, 1},
		{"option_match_read", optionMatchRead, 1},
		{"option_match_annotated", optionMatchAnnotated, 0},
		{"neighbour_annotated", neighbourAnnotated, 1},
		{"two_step", twoStep, 0},
		{"bind_root", bindRoot, 1},
		{"mixed", mixed, 2},
		{"call_arg", callArg, 1},
		{"call_arg_annotated", callArgAnnotated, 0},
		{"match_arm", matchArm, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := len(overflowHints(t, c.src)); got != c.want {
				t.Fatalf("hints=%d, want %d", got, c.want)
			}
		})
	}
}

// TestOverflowHintNotSuppressedByFunctionBodyLet 釘住一個「按綁定名漏報」的缺陷：
// collectTopLevelLets 曾對 FunctionDefinition 也呼叫 collectLetsInStmts，把函式體
// 內的 `c ?T` 登記進**頂層**型別表。`no vet` 會把 std 合併進同一 program，於是使用者
// 頂層的 `c = f(a + 1)` 撞上 std 函式裡的 `c ?= ...`，被 walkStmtForOverflow 的
// 「option 重綁」豁免整條語句 ⇒ 完全不報 xmicjezl（2026-11-11 實測：只有 `c` 不報，
// `b/d/e/i/n/s/t/x` 全報）。函式體型別本來就由 collectFuncDeclared 在各自作用域
// 收集，不該污染頂層表；runIntOverflowCore 現在用 includeFuncBodies=false。
func TestOverflowHintNotSuppressedByFunctionBodyLet(t *testing.T) {
	// 函式體內先宣告一個同名的 option let（模擬 std 合併後的撞名）。
	const shadowed = `mk = (arr [4]i64) () {
    c ?i64 = arr[0]
}
f = (v i64) (r i64) {
    r = v
}
a i64 = 1
c = f(a + 1)
print(c)
`
	// 對照組：函式體沒有同名 option let，同一條算術也必須照報。
	const control = `mk = (arr [4]i64) () {
    print(arr[0])
}
f = (v i64) (r i64) {
    r = v
}
a i64 = 1
c = f(a + 1)
print(c)
`
	for _, c := range []struct{ name, src string }{
		{"shadowed_by_function_body_let", shadowed},
		{"control", control},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := len(overflowHints(t, c.src)); got != 1 {
				t.Fatalf("hints=%d, want 1 (top-level `c = f(a + 1)` must always be reported)", got)
			}
		})
	}
}
