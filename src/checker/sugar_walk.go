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
	if sm.Matched != nil && onExpr != nil {
		onExpr(sm.Matched)
	}
	walkSurfaceMatchArms(sm, varTypes, onExpr, onStmt)
}

// walkSurfaceMatchArms 只枚舉 arm（條件／多值模式／臂體），不碰主體。
//
// 主體需要被特判時（例如 option match 的安全索引主體：其下標算術已被 lowering
// 提升成 option、由本 match 的失敗臂消費，該站點不該再被報告）由呼叫方自己走
// 主體，其餘枚舉邏輯仍集中在這個檔案 —— 新增 arm 欄位時只改這裡。
func walkSurfaceMatchArms(
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

// ---- 值 match 的臂型別統一（2026-11-20 規定） -------------------------------
//
// 規定：match 作為值時，各臂末值型別必須能統一成一個型別：
//
//	全 t  → t
//	t|?t  → ?t（共存即加寬）
//	t|never = t、?t|never = ?t —— return / break / continue / `_` 丟棄臂
//	不產值，是統一的么元；「可以忽略 _，以有型別的臂為準」由此形式化。
//	t|nil = ?t —— nil 臂只貢獻 option 標記。
//	基型不一致（i64 vs str 等）→ 衝突，報錯。
//
// `n = ARR[0] + 1: { ok -> it; -> ARR[0] }` 與 `… { ok -> it; -> cnt }`
// （cnt = ARR[0]）因此都統一成 `?i64`：ok 臂的 it 是解包後的 i64，兜底臂是
// safe-index 的 ?i64。若 n 已被標成 plain t，則 ?t 臂使統一結果變 ?t，
// 交由既有 15w45dqk 在該宣告行報「?i64 不能賦值給 i64」。

// matchArmTail 是一個臂的「末值分類」。
type matchArmTail struct {
	t      string // 末值型別（"" = 推斷不出，統一時忽略）
	never  bool   // return / break / continue / `_` 丟棄尾：不產值（never）
	nilLit bool   // 末值是 nil 字面量：只貢獻 option 標記
	lit    bool   // 末值是數值字面量：可被另一個同族具體型別吸收（u8|0 → u8）
	litInt int64  // lit 為整數字面量時的值（範圍檢查用；`-> 300` 進 u8 不得吸收）
}

// matchArmTailOf 分類一個臂的末值。
//
// 形狀升級與 let 路徑的 option 升格同口徑：safe-index 臂（`-> ARR[0]`）記成
// `?elem`（str/txt 下標是哨兵 char，不升格）；option 算術臂（`-> it + 1`）記成
// `?payload`；option 回傳呼叫臂記成其 option 型別。inferExprType 對 index 一律
// 回 plain elem，所以這裡必須按形狀補回 `?`，否則 `-> ARR[0]` 會被當成 i64，
// 統一結果錯成 plain t。
func matchArmTailOf(arm *parser.MatchArm, varTypes map[string]string, funcTypes map[string]string, selfType string) matchArmTail {
	if arm == nil || arm.Body == nil || len(arm.Body.Statements) == 0 {
		return matchArmTail{}
	}
	last := arm.Body.Statements[len(arm.Body.Statements)-1]
	switch st := last.(type) {
	case *parser.ReturnStatement, *parser.BreakStatement, *parser.ContinueStatement:
		// never 臂：控制流離開 match，不產值（t|never = t）。
		return matchArmTail{never: true}
	case *parser.LetStatement:
		// 臂體以 `_ = expr` 收尾（塊形臂）：顯式丟棄，never。
		// 其他 let 尾在值語境被 parser 拒收（9q9raufb），到不了這裡。
		if st.Name != nil && st.Name.Value == "_" {
			return matchArmTail{never: true}
		}
		return matchArmTail{}
	case *parser.ExpressionStatement:
		e := st.Expression
		if e == nil {
			// `-> _`（裸底線臂）：parser 產出空 ExpressionStatement。
			return matchArmTail{never: true}
		}
		if id, ok := e.(*parser.Identifier); ok && id.Value == "_" {
			return matchArmTail{never: true}
		}
		if as, ok := e.(*parser.AssignExpression); ok {
			if id, ok := as.Left.(*parser.Identifier); ok && id.Value == "_" {
				return matchArmTail{never: true}
			}
		}
		t := inferExprType(e, varTypes, funcTypes, selfType)
		// `err` 是變體標記不是真型別：與 let 路徑（validateStmtTypes）同口徑
		// 正規化成 err 載荷型別，統一時才不會和 ok 臂的具體型別假性衝突。
		if t == "err" {
			t = errVariantPayloadType
		}
		tail := matchArmTail{t: t}
		// 形狀升級：剝括號後按末值形狀補回 option / 字面量資訊。
		core := e
		for {
			g, ok := core.(*parser.GroupedExpression)
			if !ok {
				break
			}
			core = g.Expression
		}
		switch c := core.(type) {
		case *parser.IntegerLiteral, *parser.FloatLiteral:
			tail.lit = true
			if v, ok := integerLiteralValue(core); ok {
				tail.litInt = v
			}
		case *parser.NilLiteral:
			tail.nilLit = true
			tail.t = ""
		case *parser.IndexExpression:
			if c.Left != nil {
				lt := inferExprType(c.Left, varTypes, funcTypes, selfType)
				if lt != "str" && lt != "txt" && strings.HasPrefix(lt, "[") {
					if elem := extractArrayElemType(lt); elem != "" {
						tail.t = "?" + elem
					}
				}
			}
		case *parser.InfixExpression:
			if p := ovfOptionArithPayloadType(core, varTypes, selfType); p != "" && !isPureLiteralArith(core) {
				tail.t = "?" + p
				tail.lit = false
			}
		case *parser.CallExpression:
			if opt := optionResultOfCallTarget(core, varTypes, selfType); opt != "" {
				tail.t = opt
				tail.lit = false
			}
		}
		return tail
	}
	return matchArmTail{}
}

// matchTypeFamily 把型別歸族：整數一族（寬度不同的整數字面量臂可被吸收）、
// 浮點一族；其餘型別各自一族。
func matchTypeFamily(t string) string {
	if isIntType(t) {
		return "int"
	}
	if t == "f32" || t == "f64" {
		return "float"
	}
	return t
}

// unifyMatchArms 統一各臂末值型別。
//
// 回 (統一型別, 衝突型別A, 衝突型別B, 是否衝突)。規則（2026-11-20 規定）：
//   - t|t = t；t|?t = ?t；t|never = t（return/break/continue/`_` 不產值）
//   - t|nil = ?t
//   - 同族字面量吸收：`u8|0 → u8`，但僅在字面量值可表示於該型別時成立；
//     `-> 300` 進 u8 不可表示，保持 i64，由賦值點的 15w45dqk 報窄化
//   - 同族具體整數取較寬者（u8|i64 → i64），避免窄化被統一悄悄吸收
//   - 不同族（如 char vs str）→ 衝突，回 ("", base, t, true)
//
// ""（推斷不出）與 never 臂一律忽略，以有型別的臂為準。
func unifyMatchArms(arms []matchArmTail) (string, string, string, bool) {
	base := ""
	opt := false
	seen := false
	baseIsLit := false
	for _, a := range arms {
		if a.never {
			continue // t|never = t
		}
		if a.nilLit {
			opt = true // t|nil = ?t
			continue
		}
		t := a.t
		if t == "" {
			continue // 無型別臂忽略，以有型別的臂為準
		}
		o := ty.IsOptionRaw(t)
		if o {
			t = ty.StripOptionRaw(t)
		}
		if !seen {
			base, opt, seen, baseIsLit = t, o, true, a.lit
			continue
		}
		if t == base {
			opt = opt || o
			baseIsLit = baseIsLit && a.lit
			continue
		}
		if matchTypeFamily(t) != matchTypeFamily(base) {
			return "", base, t, true
		}
		switch {
		case a.lit && !baseIsLit:
			// 字面量讓步的條件是值可表示於 base（`-> 0` 進 u8 合法，
			// `-> 300` 進 u8 是真實數值溢位，仍交由 15w45dqk 拒絕）。
			if matchLiteralFitsType(a, base) {
				opt = opt || o
				continue
			}
			base, opt, baseIsLit = t, opt || o, false
		case !a.lit && baseIsLit:
			base, opt, baseIsLit = t, opt || o, false
		default:
			// 兩個具體同族型別：取較寬者，窄化留給賦值點的既有判定把關。
			if integerBitWidth(t) > integerBitWidth(base) {
				base = t
			}
			opt = opt || o
			baseIsLit = false
		}
	}
	if !seen {
		// 全 never / 全 nil / 全無型別：統一不出具體型別，回 ""（寬鬆方向）。
		return "", "", "", false
	}
	if opt {
		return "?" + base, "", "", false
	}
	return base, "", "", false
}

// matchLiteralFitsType 報告一個數值字面量臂的值是否可表示於 t（同族時才呼叫）。
// 整數看範圍；浮點字面量對 f32/f64 一律視為常量轉換（與 isFloatLiteralToDeclared
// 同口徑）。非整數型別（浮點等）回 true，交由既有賦值判定處理。
func matchLiteralFitsType(a matchArmTail, t string) bool {
	min, max, ok := intTypeRange(t)
	if !ok {
		return true
	}
	return a.litInt >= min && a.litInt <= max
}

// surfaceMatchSubjectType 推斷被匹配值的靜態型別。舊實作只認 Identifier 主語
// （varTypes 查表），`ARR[0] + 1: { ok -> it … }` 這類表達式主語拿不到 mt，
// ok 臂的 it 便綁不上載荷型別。這裡與 lowering 的主語正規化（resolveSubjectMatch
// → matchSubjectTypeRaw）同口徑：safe-index 主語是 `?elem`、option 算術主語是
// `?payload`、option 回傳呼叫主語取其 option 型別。
func surfaceMatchSubjectType(e parser.Expression, varTypes map[string]string, funcTypes map[string]string, selfType string) string {
	for {
		g, ok := e.(*parser.GroupedExpression)
		if !ok {
			break
		}
		e = g.Expression
	}
	switch x := e.(type) {
	case *parser.Identifier:
		return varTypes[x.Value]
	case *parser.IndexExpression:
		if x.Left != nil {
			lt := inferExprType(x.Left, varTypes, funcTypes, selfType)
			if lt == "str" || lt == "txt" {
				return "char"
			}
			if strings.HasPrefix(lt, "[") {
				if elem := extractArrayElemType(lt); elem != "" {
					return "?" + elem
				}
			}
		}
		return ""
	case *parser.InfixExpression:
		if p := ovfOptionArithPayloadType(x, varTypes, selfType); p != "" {
			return "?" + p
		}
		return inferExprType(x, varTypes, funcTypes, selfType)
	case *parser.CallExpression:
		if opt := optionResultOfCallTarget(x, varTypes, selfType); opt != "" {
			return opt
		}
		return inferExprType(x, varTypes, funcTypes, selfType)
	default:
		return inferExprType(e, varTypes, funcTypes, selfType)
	}
}

// surfaceMatchValueType 計算值 match 的統一結果型別。
//
// 副作用：把各臂的 it / 析構綁定登記進 varTypes（與舊 inferExprType SurfaceMatch
// 分支一致——臂體型別推斷需要看見本臂綁定）。validateStmtTypes 的臂一致性檢查
// 會在 inferExprType 之後再呼叫一次，重複登記是冪等的。
// 回 (統一型別, 衝突型別A, 衝突型別B, 是否衝突)。
func surfaceMatchValueType(sm *parser.SurfaceMatch, varTypes map[string]string, funcTypes map[string]string, selfType string) (string, string, string, bool) {
	mt := surfaceMatchSubjectType(sm.Matched, varTypes, funcTypes, selfType)
	// 類型解構 match（union / type-set 的 `w i64 -> …` 臂）本来就異質：各臂綁定
	// 不同型別，統一規定不適用。沿用舊行為（第一個非空臂末值型別），也不報衝突。
	for i := range sm.Arms {
		if sm.Arms[i].IsTypeArm {
			return surfaceMatchLegacyType(sm, mt, varTypes, funcTypes, selfType)
		}
	}
	var tails []matchArmTail
	for i := range sm.Arms {
		arm := &sm.Arms[i]
		for _, b := range matchArmBindings(sm, arm, mt) {
			if b.Name != "" {
				varTypes[b.Name] = b.Type
			}
		}
		tails = append(tails, matchArmTailOf(arm, varTypes, funcTypes, selfType))
	}
	return unifyMatchArms(tails)
}

// surfaceMatchLegacyType 是類型解構 match 的舊行為：第一個非空臂末值型別即整個
// match 的型別（对应旧 inferExprType SurfaceMatch 分支）。
func surfaceMatchLegacyType(sm *parser.SurfaceMatch, mt string, varTypes map[string]string, funcTypes map[string]string, selfType string) (string, string, string, bool) {
	for i := range sm.Arms {
		arm := &sm.Arms[i]
		for _, b := range matchArmBindings(sm, arm, mt) {
			if b.Name != "" {
				varTypes[b.Name] = b.Type
			}
		}
		if arm.Body != nil {
			if t := blockTrailingExprType(arm.Body, varTypes, funcTypes, selfType); t != "" {
				return t, "", "", false
			}
		}
	}
	return "", "", "", false
}

// optionIndexCaptureType 報告右值是否為 safe-index 捕獲（`x = ARR[i]`），是則回
// `?elem`。lowering 的 capture 早已把這類右值合成 `?T`（synthesizeNullableType →
// publishInferredType），redundant lint 也認得（producesOptionIndex）；唯有
// validateStmtTypes 的型別登記一直把它記成 plain elem，使 `m i64 = cnt`（cnt =
// ARR[0]）被當成 plain→plain 放行、option 的 tag 被靜默丟掉。str/txt 下標回哨兵
// char（越界 -1），不產生 option，故不升格。
func optionIndexCaptureType(v parser.Expression, varTypes map[string]string, funcTypes map[string]string, selfType string) string {
	for {
		g, ok := v.(*parser.GroupedExpression)
		if !ok {
			break
		}
		v = g.Expression
	}
	idx, ok := v.(*parser.IndexExpression)
	if !ok || idx.Left == nil {
		return ""
	}
	lt := inferExprType(idx.Left, varTypes, funcTypes, selfType)
	if lt == "str" || lt == "txt" {
		return ""
	}
	if strings.HasPrefix(lt, "[") {
		if elem := extractArrayElemType(lt); elem != "" {
			return "?" + elem
		}
	}
	return ""
}
