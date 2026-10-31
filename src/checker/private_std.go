package checker

// private_std.go — 跨模組私有符號守衛（privstd）。
//
// 語言約定（見 src/std/bigint.no:34-35 的「私有約定」註釋）：底線開頭的 std
// 符號是**所屬模組的內部實作**，其他模組一律走公開 API。在此之前這條約定只靠
// 「把呼叫點全部遷移乾淨」來維持，編譯器不設防——於是 std 內部隨手就能越界：
// `str.str-to-f64` 直接打了 bigint 的私有快速路徑 `_mod-i64`，把本该由公開
// `_mod` 承擔的 `?bigint` 語義繞了過去。本檔把它變成編譯期 ERROR。
//
// 判定需要的「哪個模組定義了這個符號」來自 bake 進二進位的 std 簽名表
// （stdsig_gen.go），兩張表都已帶模組前綴，不需要額外掃源碼：
//
//   - `module.struct.method` 鍵 → stdStructMethodMods（funcargs.go 既有），
//     解值接收者呼叫 `x._m(...)`（recvType 就是 struct 名）；
//   - `module.fn` 鍵 → 本檔的 stdPrivateFnMods，解模組限定呼叫 `mod._fn(...)`。
//
// 三條刻意畫出來的邊界（誤報比漏報貴）：
//
//  1. **運算符重載名豁免**：`_add/_sub/_mul/_div/_mod` 是一個閉集，是公開運算符
//     `+ - * / %` 的實現名字；而且 parser 會把 `a + b` 改寫成 `a._add(b)` 後才交
//     給 checker，不豁免等於把每一處 `+` 判成錯。
//  2. **Go builtin 私有件交給 privbuiltin**：str/txt 的 `_len/_cap/_data/...` 已由
//     checkPrivateBuiltinMethodCall 報錯，這裡再報一次是同位置的重複診斷。
//  3. **只在查得到歸屬模組時報錯**：查不到（使用者自己碼裡的 `_foo`）一律放行，
//     因為本守衛的資料來源只有 std 簽名表，對非 std 符號沒有任何事實可依。

import (
	"github.com/lizongying/nolang/i18n"
	"sort"
	"strings"
	"sync"

	"github.com/lizongying/nolang/builtin"
	"github.com/lizongying/nolang/parser"
)

// stdPrivateFnModsOnce 護住 stdPrivateFnMods 的延遲建立。
var (
	stdPrivateFnModsOnce sync.Once
	stdPrivateFnMods     map[string]map[string]bool
)

// initStdPrivateFnMods 從 `module.name` 形狀的鍵（恰好一個點號、且前綴真的是 std
// 模組）收集「哪個模組定義了哪個私有符號」。兩張表都要掃：
//
//   - embeddedStdFuncSigs（stdSigsCache）：`str._str-dig-val` 這類頂層函數；
//   - embeddedStdMethodSigs（stdMethodSigsCache）：`bigint._mod-i64` 這類「結構體
//     名與模組名同字」的方法（同時還有 `bigint.bigint._mod-i64` 的兩點號鍵）。
//
// `module.struct.method`（兩個點號）不屬於這一類，留給 stdStructMethodMods；裸名
// 鍵（無點號，global.fn）也沒有歸屬模組可談。前綴必須命中 knownStdModules，否則
// `poly._mod-i64` 這種「struct 名剛好一個點號」的鍵會被誤當成模組名。
func initStdPrivateFnMods() {
	stdPrivateFnModsOnce.Do(func() {
		stdPrivateFnMods = make(map[string]map[string]bool)
		CollectStdModuleSignatures() // warm the caches (sync.Once)
		mods := knownStdModules()
		for _, table := range []map[string]StdSigEntry{stdSigsCache, stdMethodSigsCache} {
			for key := range table {
				if strings.Count(key, ".") != 1 {
					continue
				}
				mod, name, found := strings.Cut(key, ".")
				if !found || !strings.HasPrefix(name, "_") {
					continue
				}
				if _, ok := mods[mod]; !ok {
					continue
				}
				if stdPrivateFnMods[mod] == nil {
					stdPrivateFnMods[mod] = make(map[string]bool)
				}
				stdPrivateFnMods[mod][name] = true
			}
		}
	})
}

// stdPrivateOwners 回傳「在 std 裡定義了 `_<method>` 的模組集合」，查不到回 nil。
//
// 兩種呼叫形狀分別解析：
//   - 模組限定 `bigint._fn(...)`：接收者標識符就是模組名，歸屬直接由它定；
//   - 值接收者 `x._m(...)`：只能靠接收者的靜態型別去 `struct.method` 表裡找。
//
// 值接收者的兩條型別路都試：裸 `Struct` 與模組限定 `mod.Struct`（見下方註釋）。
//
// 值接收者這條路若型別解不出來（recvType 為空）就回 nil：寧可漏報，也不猜一個
// 模組名去報錯。
func stdPrivateOwners(recvType, method string, recvExpr parser.Expression) map[string]bool {
	if id, ok := recvExpr.(*parser.Identifier); ok {
		if _, isMod := knownStdModules()[id.Value]; isMod {
			initStdPrivateFnMods()
			if stdPrivateFnMods[id.Value][method] {
				return map[string]bool{id.Value: true}
			}
			// 前綴雖是模組名，但這張表裡查不到該名字的定義：可能是與模組同名的
			// 局部變數，也可能是接收者型別剛好推得出來的限定寫法。往下走型別
			// 那條路，解不出來才放行（不在此早退，否則 `bigint._mod-i64(...)`
			// 這種「struct 名＝模組名」的正解會被當成查無此事）。
		}
	}
	if recvType == "" {
		return nil
	}
	initStdStructMethodMods()
	keys := []string{recvType + "." + method}
	// 同一個值接收者，但型別寫成**模組限定**形 `x: bigint.bigint`（std 碼裡到處是
	// 這種標註）：recvType 是 `mod.Struct`，直接拼鍵會變成 `bigint.bigint._mod-i64`
	// 而查不到——守衛恰好在它唯一要攔的形狀上失聲。故再試一次「剝掉已知模組前綴」
	// 的裸 `Struct.method` 鍵；只在剝掉的前綴真的是 std 模組時才試。
	if mod, rest, found := strings.Cut(recvType, "."); found && !strings.Contains(rest, ".") {
		if _, isMod := knownStdModules()[mod]; isMod {
			keys = append(keys, rest+"."+method)
		}
	}
	owners := make(map[string]bool)
	for _, key := range keys {
		for m := range stdStructMethodMods[key] {
			owners[m] = true
		}
	}
	if len(owners) == 0 {
		return nil
	}
	return owners
}

// checkPrivateStdCall 擋跨模組呼叫 std 私有（`_` 前綴）符號，TraceID "raqvyltn"。
//
// 允許：curModule 正好是定義模組（同模組內部使用）——例如 bigint.no 自己的
// `._mod-i64(v)` 委派。其餘一律 ERROR，包括使用者碼（curModule 為空）。
func checkPrivateStdCall(e *parser.CallExpression, dot *parser.DotExpression, recvType, curModule string) []ValidateResult {
	if dot == nil || !strings.HasPrefix(dot.Property, "_") {
		return nil
	}
	// 豁免 1：運算符重載名是公開運算符的實現名，不是「私有」。
	if parser.IsOperatorOverloadName(dot.Property) {
		return nil
	}
	// 簽名表未暖（裸 `go build`/`go test` 沒注入 StdSigKey）時完全安靜：本守衛的
	// 全部事實都來自那兩張表，表不在就沒有可判定的歸屬關係。
	if StdSigKey == "" {
		return nil
	}
	// 豁免 2：str/txt 已註冊的私有 builtin 由 privbuiltin 報，避免同位重複。
	if recvType == "str" || recvType == "txt" {
		if builtin.FindBuiltinMethod(recvType+"."+dot.Property) != nil {
			return nil
		}
	}
	owners := stdPrivateOwners(recvType, dot.Property, dot.Receiver)
	if len(owners) == 0 {
		return nil
	}
	if owners[curModule] {
		return nil // 同模組內部使用，正是約定的合法範圍
	}
	line, col := e.Token.Line, e.Token.Column
	if p := dot.Pos(); p.Line > 0 {
		line, col = p.Line, p.Column
	}
	where := curModule
	if where == "" {
		where = "user code"
	}
	from := make([]string, 0, len(owners))
	for m := range owners {
		from = append(from, m)
	}
	// owners 是 map，遍歷順序不穩定；排序後訊息才可對拍（診斷多重集比對的是字串）。
	sort.Strings(from)
	return []ValidateResult{{
		TraceID: "raqvyltn",
		Line:    line,
		Column:  col,
		Message: i18n.T("raqvyltn", "private std symbol '%s' is owned by module %s and may only be used inside it, not from %s; use the public API instead",
			dot.Property, strings.Join(from, "/"), where),
	}}
}
