package checker

import (
	"strings"

	"github.com/lizongying/nolang/parser"
	"github.com/lizongying/nolang/ty"
)

// ---- S1：糖節點遍歷助手 ----------------------------------------------------
//
// checker 今天跑在兩種 AST 形態上，S1 會加上第三種：
//
//	lowered  ParseProgram 默認路徑：所有糖已展開。`no build` / `no vet`。
//	surface  SkipUnwrapLowering + SkipSafeIndexLowering。LSP / `no fmt` 今天就用。
//	sugar    SkipLowering：連 SurfaceMatch 都存活。S1 的目標形態。
//
// 每條規則都是手寫的 `switch node.(type)`。**switch 裡沒有的型別會被靜默跳過**，
// 於是整棵子樹對該規則不可見——不是報錯，是沉默。S1 之前存活的糖是
// GroupedExpression / UnwrapAssignStatement / MultiAssignStatement，各規則
// 各自長了一個 case（見 checker/index_write_test.go:14 立的規矩：
// 「surface AST 與降級 AST 兩端口徑一致」）。
//
// SurfaceMatch 不一樣：它是**容器**（Matched + N 個 arm，每個 arm 帶
// Condition / MultiValuePatterns / Body）。把它的枚舉內聯進每個 walker，等於把
// 維護面乘上 walker 的數量。故集中在這裡：**新增一個糖節點、或 MatchArm 新增
// 一個欄位，都只改這一個檔案。**
//
// 回調只收到**直接**子節點；遞迴是 walker 自己的事——正因如此，嵌套的糖無需
// 額外管線：walker 遞進 arm 體後又會撞上內層的 *SurfaceMatch，再次呼叫本助手。
//
// 為什麼 arm 的 Condition 也算子節點：糖形態下條件尚未被合併進 `matched == X`
// 的比較運算式（那是 buildMatchDesugar 的產物），條件裡引用的變數只能從這裡看見。
//
// 注意本助手**不**枚舉 MultiOptionPatterns：那是 `[]string`（`nil || err` 這種
// 模式名），不是運算式，不承載變數引用。
//
// S1 原生處理：本助手在遍歷每個 arm 的臂體「之前」，把該臂在其臂體內引入的綁定
// （類型臂 `v i32 ->` 的 v、析構綁定 `ok(v) ->` 的 v、`rect(w, h) ->` 的 w/h、
// 隐式 `it` 等）宣告進傳入的 varTypes——與降糖後「臂體開頭合成 `v = __unwrap_N`」
// 讓 walker 看得見綁定的行為逐位元一致。這是 checker 原生消費 SurfaceMatch 的
// 關鍵：不依賴 parser 的 buildMatchDesugar，直接從 MatchArm 元資料把綁定名/型別
// 登記進 checker 自己的型別表。

// armBinding 是某個 match arm 在其臂體作用域內引入的一個 (名, 型別) 綁定。
type armBinding struct {
	Name string
	Type string
}

// elemTypeOf 從一個 option / union / 普通型別名抽出「載荷 / 元素」型別，
// 供 `ok(v) ->` 的 v 與隐式 `it` 綁定型別推斷：
//
//	"?i64"        -> "i64"
//	"i64 | err"   -> "i64"（union 第一變體即 payload）
//	"num"         -> "num"（未知 union 時退回原式）
//	""            -> ""
func elemTypeOf(t string) string {
	if t == "" {
		return ""
	}
	if ty.IsOptionRaw(t) {
		return ty.StripOptionRaw(t)
	}
	if i := strings.Index(t, "|"); i >= 0 {
		return strings.TrimSpace(t[:i])
	}
	return t
}

// matchArmBindings 回報 arm 在其臂體內引入的全部 (名, 型別) 綁定。
//
// 解析期 MatchArm 元資料已經把「綁定是誰、型別是什麼」編碼好了：
//
//   - 類型臂 `v i32 ->`：TypeBindingName="v", TypePattern="i32"。
//   - 析構綁定 `ok(v) ->`：BindingName="v"；型別取被匹配值的元素型別。
//   - 多欄位 `rect(w, h) ->`：BindingNames=[w,h], BindingFields=[wt,ht]。
//   - 隐式 `it`：ok / err / nil / 值 / catch-all 臂在臂體內可見 `it`，
//     型別依臂別取（ok→元素型別、err→"err"、catch-all→被匹配值整型）。
//
// matchedType 是被匹配值的靜態型別（由 walker 從 varTypes 解析 sm.Matched
// 得到）；型別臂不依賴它（直接用 TypePattern）。
func matchArmBindings(sm *parser.SurfaceMatch, arm *parser.MatchArm, matchedType string) []armBinding {
	if arm == nil {
		return nil
	}
	var out []armBinding

	// 顯式析構綁定 `ok(v) ->` / `Some(v) ->`：BindingVariant 標記是 ok 還是枚舉變體，
	// 但綁定「名」一律在 BindingName；型別取被匹配值的元素型別（ok）或變體載荷（枚舉）。
	if arm.BindingName != "" {
		t := elemTypeOf(matchedType)
		if arm.BindingVariant != "" && !isOptionVariant(arm.BindingVariant) {
			// 枚舉變體析構 `Some(v) ->`：載荷型別需要語義表，checker 這層拿不到
			// 精確型別時退回變體名，不崩潰（型別精確度由後續 lowering 補全）。
			if pt := enumVariantPayloadOf(matchedType, arm.BindingVariant); pt != "" {
				t = pt
			}
		}
		out = append(out, armBinding{Name: arm.BindingName, Type: t})
	}
	for i, n := range arm.BindingNames {
		ft := ""
		if i < len(arm.BindingFields) {
			ft = arm.BindingFields[i]
		}
		out = append(out, armBinding{Name: n, Type: ft})
	}
	if arm.TypeBindingName != "" {
		out = append(out, armBinding{Name: arm.TypeBindingName, Type: arm.TypePattern})
	}

	// 枚舉變體析構綁定 `full(e) ->` / `Some(a, b) ->`：綁定名在 Condition 的
	// CallExpression 參數裡。parser 在枚舉上下文（desugar 路徑）才把 arm.Condition
	// 規範化成 Identifier{X} 並回填 BindingName；純語法期 / S1 保留路徑下
	// arm.Condition 仍是 CallExpression，故這裡直接從參數抽取綁定名。
	// 型別須語義表（變體載荷型別），checker 這層只拿到 matchedType 字串時退回 ""
	// （變體名由 BindingVariant 提供；後續 lowering 補全精確型別）。
	if ce, ok := arm.Condition.(*parser.CallExpression); ok {
		fn := ""
		if id, ok := ce.Function.(*parser.Identifier); ok {
			fn = id.Value
		}
		if fn != "ok" && fn != "err" && fn != "nil" {
			for _, arg := range ce.Arguments {
				if id, ok := arg.(*parser.Identifier); ok && id.Value != "" {
					t := ""
					if pt := enumVariantPayloadOf(matchedType, fn); pt != "" {
						t = pt
					}
					out = append(out, armBinding{Name: id.Value, Type: t})
				}
			}
		}
	}

	// 隐式 `it` 綁定（ok / err / nil / 值 / catch-all 臂）。
	if !arm.SkipItBinding {
		switch {
		case arm.Condition != nil:
			switch c := arm.Condition.(type) {
			case *parser.Identifier:
				switch c.Value {
				case "ok":
					out = append(out, armBinding{Name: "it", Type: elemTypeOf(matchedType)})
				case "err":
					out = append(out, armBinding{Name: "it", Type: "err"})
				case "nil":
					// nil 臂無 it（值為 nil，無載荷可綁）。
				default:
					// 枚舉變體臂 `Some ->`：it 綁定變體載荷（拿不到時退回變體名）。
					if pt := enumVariantPayloadOf(matchedType, c.Value); pt != "" {
						out = append(out, armBinding{Name: "it", Type: pt})
					} else {
						out = append(out, armBinding{Name: "it", Type: c.Value})
					}
				}
			case *parser.NilLiteral:
				// 裸 nil 條件臂：無 it。
			default:
				if arm.IsDotVal {
					out = append(out, armBinding{Name: "it", Type: elemTypeOf(matchedType)})
				}
			}
		case arm.IsWildcard && !arm.IsDotVal:
			// catch-all `->` 臂：it 是被匹配值整型（ok/nil/err 之外的兜底）。
			out = append(out, armBinding{Name: "it", Type: matchedType})
		case arm.IsWildcard && arm.IsDotVal && arm.BindingName == "":
			// `ok ->` / `.->` 值臂：parser 對這兩種形態只設
			// IsWildcard+IsDotVal 而把 Condition 留空（parser/expr.go:1046-1050
			// 的 `.->`、1100-1102 與 1107-1113 的 `ok ->` / 換行體 `ok`），
			// 因此上面兩個分支都蓋不到它——翻轉前這沒關係：buildMatchDesugar
			// 會合成帶型別的 `let it`（lowering.go:2998-3001 令 armType="ok"，
			// 3949-3950 取 typeStr=elemType），checker 從合成 let 看見它。
			// 翻轉後 SurfaceMatch 活到校驗期，這裡就必須等價登記，否則有兩個
			// 後果：
			//   1) err 臂註冊的 it="err" 會沿扁平 varTypes 洩漏到本臂
			//      （walker 不按臂恢復，見 walkSurfaceMatchChildren 註釋）；
			//      err 又在 LetStatement 路徑被正規化成 "str"
			//      （checker.go:8796-8805），於是 `ok -> v = it` 誤報
			//      "cannot assign str value to i64 variable 'v'"
			//      ——tests/heap.no 的回歸案即此。
			//   2) 單獨一條 `ok -> a = it`（前面沒有 err 臂）時 it 完全未
			//      登記，推斷落空、型別檢查被靜默跳過。
			// `ok(v) ->` 析構臂不走此分支：BindingName 已由上方登記 v，
			// 而降糖路徑的 bindName 也改成 v、不再產生 it（lowering.go:3018-3021）。
			out = append(out, armBinding{Name: "it", Type: elemTypeOf(matchedType)})
		}
	}
	return out
}

// isOptionVariant 判斷 BindingVariant 是否為 option 變體（ok / err / nil），
// 與枚舉變體區分（枚舉變體需要語義表才能拿到載荷型別）。
func isOptionVariant(v string) bool {
	return v == "ok" || v == "err" || v == "nil"
}

// enumVariantPayloadOf 嘗試從 matchedType 解析枚舉變體 variant 的載荷型別。
// checker 這層沒有 parser 的語義表，故盡量保守：matchedType 形如
// "variantA|variantB|..." 且 variant 在其中時，無法確知載荷，回 "".
// （型別精確度由後續 lowering 的 buildMatchDesugar 補全；此處只求不崩潰。）
// 保留函式簽章以便未來接入語義表。
func enumVariantPayloadOf(matchedType, variant string) string {
	if matchedType == "" || variant == "" {
		return ""
	}
	for _, part := range ty.UnionMembersOrSelfRaw(matchedType) {
		if strings.TrimSpace(part) == variant {
			return "" // 無法從 union 字面推斷載荷型別。
		}
	}
	return ""
}

// walkSurfaceMatchChildren 以直接子節點回調遍歷 SurfaceMatch（只讀），並在遍歷
// 每個 arm 的臂體「之前」把該臂引入的綁定宣告進 varTypes（原生消費 SurfaceMatch：
// 把綁定名/型別登記進 checker 自己的型別表，等價於降糖後合成 `let` 的效果）。
//
// onExpr / onStmt 可為 nil，nil 回調即跳過該類別。varTypes 為 nil 時跳過綁定宣告
// （純收集型 walker 不需要名稱解析，例如只收集語句列表的 onBody）。
//
// 遍歷順序與 parser.WalkSurfaceMatchChildrenRW 逐位元一致：先 Matched，再逐臂
// Condition / MultiValuePatterns / Body。
func walkSurfaceMatchChildren(
	sm *parser.SurfaceMatch,
	varTypes map[string]string,
	onExpr func(parser.Expression),
	onStmt func(parser.Statement),
) {
	if sm == nil {
		return
	}
	matchedType := ""
	if id, ok := sm.Matched.(*parser.Identifier); ok && varTypes != nil {
		matchedType = varTypes[id.Value]
	}
	if sm.Matched != nil && onExpr != nil {
		onExpr(sm.Matched)
	}
	for i := range sm.Arms {
		arm := &sm.Arms[i]
		// 宣告本臂在臂體內引入的綁定（類型臂 v、析構綁定 v、隐式 it），供隨後的
		// arm 體 walk 看見。flat varTypes 模型下不恢復——與降糖後合成 let 累積进
		// varTypes 的行為一致。
		if varTypes != nil {
			for _, b := range matchArmBindings(sm, arm, matchedType) {
				if b.Name != "" {
					varTypes[b.Name] = b.Type
				}
			}
		}
		if onExpr != nil {
			if arm.Condition != nil {
				onExpr(arm.Condition)
			}
			for j := range arm.MultiValuePatterns {
				if arm.MultiValuePatterns[j] != nil {
					onExpr(arm.MultiValuePatterns[j])
				}
			}
		}
		if arm.Body != nil && onStmt != nil {
			for _, inner := range arm.Body.Statements {
				onStmt(inner)
			}
		}
	}
}

// walkSurfaceMatchArmBodies 以「臂體語句列表」為單位回調，而不是逐條語句。
// 在回調每個臂體「之前」把該臂的綁定宣告進 varTypes。
//
// 為什麼要有這個版本：有些規則的判定依賴**列表上下文**——最典型的是
// OverflowAnnotationRelevance 的「這條 #{overflow} 註解管轄它後面的第一條陳述」。
// 逐條回調（walkSurfaceMatchChildren 的 onStmt）會把「後面是誰」這個資訊切碎，
// 使註解查不到被管轄者，`no fmt` 便把有效的 overflow 註解誤刪。
func walkSurfaceMatchArmBodies(sm *parser.SurfaceMatch, varTypes map[string]string, onBody func([]parser.Statement)) {
	if sm == nil || onBody == nil {
		return
	}
	matchedType := ""
	if id, ok := sm.Matched.(*parser.Identifier); ok && varTypes != nil {
		matchedType = varTypes[id.Value]
	}
	for i := range sm.Arms {
		if sm.Arms[i].Body != nil {
			if varTypes != nil {
				for _, b := range matchArmBindings(sm, &sm.Arms[i], matchedType) {
					if b.Name != "" {
						varTypes[b.Name] = b.Type
					}
				}
			}
			onBody(sm.Arms[i].Body.Statements)
		}
	}
}
