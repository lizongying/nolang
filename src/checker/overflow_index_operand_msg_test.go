package checker

import (
	"testing"

	"github.com/lizongying/nolang/i18n/catalog"
)

// 本檔案釘住 ovf-int-default（TraceID xmicjezl）的「按位置選訊息」行為。
//
// 背景：`seed[8 + i]: { ok -> it = sb2  -> _ }`（example/mysql-driver/src/mysql.no:743）
// 曾被報成「`a OP b` 的結果是 `?T`，用 `?T` 接收 / match 解包 / `_ =` 丟棄」，
// 但那三種補救在索引運算元裡一個都寫不出來——索引必須是純整數，值由外層
// `[...]` 直接消費。那是「值根」的話題（overflowSiteBound），與索引運算元無關。
//
// 索引運算元只剩一件事可講：**給這條陳述加 `#{overflow = …}` 註解聲明溢出行為**。
// 註解是逐語句生效的——`#{overflow=wrap}` 寫在上一條 `sb = seed[pos + i]` 上
// 只覆蓋那一條，管不到下一條的 `seed[8 + i]`，所以 mysql.no:743 這種形狀
// **必須照報**（ovfidxop，只教註解）。
//
// 另一條獨立事實（parser/index_operand_promotion_test.go）：option match 主體的
// 下標算術會被 lowering 提升成 `__idx_L_C ?T`，下標溢位短路進同一個失敗臂
// ——那是「已經被消費、不會算錯」，但失敗臂（常見 `-> _`）會把這筆資料默默丟掉，
// 使用者從未聲明要這個行為。故本 lint 照樣報，只是報的是「請加註解聲明」。
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

// overflowMsgSites 把 ValidateIntOverflow 的結果依「用的是哪一版訊息」分類。
func overflowMsgSites(t *testing.T, src string) (bound, operand int) {
	t.Helper()
	boundSet := messageSet(t, "xmicjezl")
	operandSet := messageSet(t, "ovfidxop")
	for m := range operandSet {
		if boundSet[m] {
			t.Fatalf("both sites share the message %q: the two variants must differ", m)
		}
	}
	for _, r := range ValidateIntOverflow(parseProg(t, src)) {
		if r.TraceID != "xmicjezl" {
			continue
		}
		switch {
		case boundSet[r.Message]:
			bound++
		case operandSet[r.Message]:
			operand++
		default:
			t.Fatalf("unexpected message for %d:%d: %q", r.Line, r.Column, r.Message)
		}
	}
	return bound, operand
}

func TestOverflowIndexOperandGetsAnnotationOnlyMessage(t *testing.T) {
	// 索引寫入，原地、既無 match 也無 option 綁定：值的型別必須是純整數，
	// 故只該拿到「加註解」那一版訊息。
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
	// 值 match 主體：`{ 5 -> … }` 沒有失敗臂可消費下標選項 ⇒ 不提升，照報。
	const valueMatch = `seed [16]byte
k = 3
seed[8 + k]: { 5 -> print(1)  -> _ }
`
	// option match 主體（mysql.no:743 的形狀）：下標算術被提升成 `__idx ?T`，
	// 溢位短路進 `-> _`。已被消費不代表使用者聲明了行為 ⇒ 照報註解版訊息。
	const optionMatchWrite = `seed [16]byte
k = 3
seed[8 + k]: { ok -> it = 65  -> _ }
`
	// 同一個形狀的純讀取（`it` 當值用）同理。
	const optionMatchRead = `seed []byte
k = 3
v = seed[8 + k]: {
    ok -> it
    -> 0
}
`
	// 註解寫在**擁有它的那條陳述**上 ⇒ 使用者已聲明，不報。
	const optionMatchAnnotated = `seed [16]byte
k = 3
#{overflow=wrap}
seed[8 + k]: { ok -> it = 65  -> _ }
`
	// 註解寫在**相鄰語句**上（mysql.no:738 → 743 的真實形狀）⇒ 逐語句生效，
	// 管不到下一條 ⇒ 仍然報。
	const neighbourAnnotated = `seed [16]byte
k = 3
#{overflow=wrap}
sb = seed[8 + k]
seed[9 + k]: { ok -> it = sb  -> _ }
`
	// 手寫的兩步式：`idx ?i64` 已明確聲明 option（合法方案），後面的
	// `seed[idx]` 下標是識別字、沒有算術 ⇒ 全篇無站點可報。
	const twoStep = `seed [16]byte
k = 3
idx ?i64 = 8 + k
seed[idx]: { ok -> it = 65  -> _ }
`
	// 對照組：綁定值根 —— 這裡的三種補救全部成立，必須仍是完整訊息。
	// （用裸表達式陳述當載體：`x = a + b` 會先被 capture pass 判成 `?T`，
	// 目標已是 option ⇒ 這條 lint 依既有規則豁免，見
	// TestCaptureOptionalTargetSuppressesIntOverflow。）
	const bindRoot = `f = (a i64, b i64) () {
    a + b
}
`
	// 對照組二：索引運算元與綁定值根混在同一條陳述裡，兩版訊息各出一條，
	// 證明分類是「逐個站點」而不是「逐條陳述」。
	const mixed = `f = (arr []i64, i i64) () {
    arr[i + 1] + 2
}
`
	cases := []struct {
		name           string
		src            string
		bound, operand int
	}{
		{"index_write", indexWrite, 0, 1},
		{"plain_read", plainRead, 0, 1},
		{"value_match", valueMatch, 0, 1},
		{"option_match_write", optionMatchWrite, 0, 1},
		{"option_match_read", optionMatchRead, 0, 1},
		{"option_match_annotated", optionMatchAnnotated, 0, 0},
		{"neighbour_annotated", neighbourAnnotated, 0, 1},
		{"two_step", twoStep, 0, 0},
		{"bind_root", bindRoot, 1, 0},
		{"mixed", mixed, 1, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			bound, operand := overflowMsgSites(t, c.src)
			if bound != c.bound || operand != c.operand {
				t.Fatalf("bound=%d operand=%d, want bound=%d operand=%d",
					bound, operand, c.bound, c.operand)
			}
		})
	}
}
