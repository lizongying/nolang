package checker

import (
	"strings"

	"github.com/lizongying/nolang/parser"
)

// 陳述層級塊 match `{ body }: { arms }` 只以糖形態 *parser.BlockMatchStatement
// 存在於 checker 面前（H0 展開發生在 parser/tohir.go，checker 看不到降糖後的
// 節點）。它的兩個容器都不是一般 walker 認得的形狀：
//
//   - Body 是 *parser.BlockStatement，但包在一個自訂 statement node 裡；
//   - Match 是「裸」 *parser.SurfaceMatch（Matched == nil），`no fmt` 正是靠
//     Matched == nil 才印得出 `}: {` 而不是 `subject: {`。
//
// 偏偏 match_exhaustive.go 的窮盡性規則以 `Matched != nil` 當作「這是一條
// option match」的判據（裸 guard 區塊不適用）。因此 checker 需要一個**帶主體的
// 檢視**，但又不能改寫糖 AST（改了 `no fmt` 就印不出 `}: {`）。本檔兩個助手就是
// 這個檢視：一律 shallow copy，絕不 mutate 原節點。

// blockMatchSubjectMatch 回傳一條「帶主體」的 SurfaceMatch 淺拷貝：把塊 match
// 的合成 subject（`__bm_N`）綁到 Matched，於是標準三態 ?T 窮盡性規則、
// `sugarMatchHasCatchAll`、`sugarMatchSubjectName` 全部原樣套用，不需要在
// checker 裡為塊 match 開特例。
//
// 主體恆為 `?i64`（見 parser/block_match.go 的 `let __bm_N ?i64 = nil`），
// 執行期只可能是 nil / err，但依使用者定案仍走標準三態規則：沒有兜底臂時
// 連不可達的 `ok ->` 也照樣要求。
//
// ⚠️ 回傳的是**新分配的指標**：collectSugarMatchRoots 用它當 map key 沒問題
// （同一次收集內唯一），但 `no fix` 只讀 NonExhaustiveMatch.Pos / .End，不會
// 拿 Sugar 指標回寫原始 AST，所以淺拷貝不會讓修復漏掉。
func blockMatchSubjectMatch(bm *parser.BlockMatchStatement) *parser.SurfaceMatch {
	if bm == nil || bm.Match == nil {
		return nil
	}
	cp := *bm.Match
	subject := bm.Subject
	if subject == "" {
		subject = "__bm_0"
	}
	cp.Matched = &parser.Identifier{Token: cp.Token, Value: subject}
	return &cp
}

// blockMatchChildStatements 回傳「單純下鑽陳述」的 walker 該拜訪的所有子陳述：
// 主體區塊的陳述，接著每條臂體的陳述。
//
// 為什麼要攤平臂體：checker 的規則大多是手寫 `switch stmt.(type)`，switch 裡
// 沒有的型別會被靜默跳過（見 parser/sugar_walk.go 的註解）。攤平後每條臂體
// 陳述都是一般 statement，walker 自己的遞迴會再撞上內層的 SurfaceMatch /
// 巢狀 BlockMatchStatement，不需要額外管線。
//
// 需要「臂作用域 / it 綁定型別」的 walker 不要用本助手，改用
// blockMatchSubjectMatch + walkSurfaceMatchChildren(... varTypes ...)。
func blockMatchChildStatements(bm *parser.BlockMatchStatement) []parser.Statement {
	if bm == nil {
		return nil
	}
	var out []parser.Statement
	if bm.Body != nil {
		out = append(out, bm.Body.Statements...)
	}
	if bm.Match != nil {
		parser.WalkSurfaceMatchArmBodies(bm.Match, func(stmts []parser.Statement) {
			out = append(out, stmts...)
		})
	}
	return out
}

// blockMatchSubjectType 是合成 subject 的靜態型別，與 parser 展開時寫下的
// `let __bm_N ?i64 = nil` 一致。checker 的 varTypes 必須先登記它，臂綁定
// （matchArmBindings）才知道 err 臂的 `it` 是 err 載荷、catch-all 臂不是
// 未定型別。
const blockMatchSubjectType = "?i64"

// blockMatchSubjectLabel 是診斷文案裡給使用者看的「主體名」。
//
// `__bm_1` 是 parse 期生成、原始碼裡根本不存在的合成變數；直接印它會讓
// 「non-exhaustive match on option '__bm_1'」變成無從查起的錯誤。塊 match
// 的臂其實在匹配 `?=` 的結果，故對外一律稱之為 `block match`。
// 這只是**顯示層**的替換：窮盡性/可達性規則本身仍走標準三態 ?T 口徑，
// 沒有為塊 match 開任何規則特例。
const blockMatchSubjectLabel = "block match"

// isBlockMatchSubject 報告 name 是否為塊 match 的合成 subject。
// parser 端以 fmt.Sprintf("__bm_%d", seq) 命名（parser/stmt.go），兩端一致。
func isBlockMatchSubject(name string) bool {
	return strings.HasPrefix(name, "__bm_")
}
