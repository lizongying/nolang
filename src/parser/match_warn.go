package parser

import (
	"reflect"
)

// ---- S4：把「'->' 臂不可達」診斷從降糖迴圈搬到解析期 ------------------------
//
// 這個診斷原本由 buildMatchDesugar 在展開 match 的過程中用 p.saveWarning 發出
// （lowering.go 兩處：列舉變體列滿 / option 三變體列滿）。它是**降糖期診斷**——
// SkipLowering 下那兩行根本不會執行，於是消費方翻到糖形態後這條提示靜默消失
// （實測：tests/match-test.no 19:9 與 79:9 是 532 份語料裡僅有的兩條）。
//
// 設計稿 §17.1 立的規矩是「診斷必須先做成與形態無關，才能翻」。做法：把判定的
// 輸入從「降糖過程中的 arm 狀態」改成「SurfaceMatch 自己攜帶的語法欄位」，放在
// ParseProgram 裡、降糖**之前**執行——兩種形態都跑同一段程式碼，降糖與否不再
// 影響診斷。降糖路徑那兩處 saveWarning 隨之移除。
//
// 走訪複用 lowerer.walk（analysisOnly + warnMatchArms），因此
//   - curFuncName 的追蹤與降糖一致（FuncVarType 的查詢口徑相同）；
//   - 走訪順序與 lowerSurfaceMatch 一致（先 Matched、再各臂條件、再各臂體，
//     最後才判本層），嵌套 match 的診斷先後順序也與原本一致。
//
// 本 pass **不寫回 AST**：列舉載荷析構綁定（`some(v) ->`）的正規化原本是就地改
// 寫 arm.Condition；這裡只在區域變數裡記「該臂等同於列出哪個變體」。

func (p *Parser) warnUnreachableMatchArms(program *Program) {
	if program == nil {
		return
	}
	l := &lowerer{
		p:              p,
		visited:        map[uintptr]bool{},
		declaredLocals: map[string]bool{},
		analysisOnly:   true,
		warnMatchArms:  true,
	}
	l.walk(reflect.ValueOf(program))
}

// warnMatchArmsPass 是 warnMatchArms 模式下遇到 SurfaceMatch 時的完整處理：
// 先按 lowerSurfaceMatch 的順序走訪子節點，再判本層。
func (l *lowerer) warnMatchArmsPass(sm *SurfaceMatch) {
	l.walk(reflect.ValueOf(&sm.Matched))
	for i := range sm.Arms {
		a := &sm.Arms[i]
		l.walk(reflect.ValueOf(&a.Condition))
		for j := range a.MultiValuePatterns {
			l.walk(reflect.ValueOf(&a.MultiValuePatterns[j]))
		}
		if a.Body != nil {
			l.walk(reflect.ValueOf(a.Body))
		}
	}
	l.warnUnreachableArms(sm)
}

// warnUnreachableArms 對一個有主語的 match 判定「裸 `->` 臂是否不可達」。
//
// 與 buildMatchDesugar 的對應關係（逐條）：
//   - 無主語（裸 match）走 buildBareMatchDesugar、union 主語走
//     buildTypeMatchDesugar，兩者都不發這個診斷 ⇒ 這裡同樣直接返回；
//   - `itStmt != nil` 在降糖裡是 buildItBinding(tok, matched) != nil，而該函式
//     只依 matched 的型別開關（Identifier / IndexExpression）⇒ 這裡用同一條件；
//   - 任一臂是 raw-cond（`ok(cond) ->`）⇒ 降糖整段 per-arm 區塊被跳過 ⇒ 返回；
//   - 主體型別來自 sem.FuncVarType（函式作用域），列舉變體來自 sem.EnumVariantsOf。
func (l *lowerer) warnUnreachableArms(sm *SurfaceMatch) {
	if sm == nil || sm.Matched == nil || len(sm.Arms) == 0 || l.p.sem == nil {
		return
	}
	matched := sm.Matched

	// union 主語：lowerSurfaceMatch 會改走 buildTypeMatchDesugar，不經
	// buildMatchDesugar ⇒ 原本就不會發這個診斷。
	if ident, ok := matched.(*Identifier); ok {
		if t, ok := l.p.sem.FuncVarType(l.curFuncName, ident.Value); ok && l.p.isUnionType(t) {
			return
		}
	}
	// 對應 buildItBinding 的型別開關。
	switch matched.(type) {
	case *Identifier, *IndexExpression:
	default:
		return
	}
	// 對應 `if itStmt != nil && !hasRawCond`。
	for i := range sm.Arms {
		if sm.Arms[i].IsRawCond {
			return
		}
	}

	// 主體型別：識別符查函式作用域符號表；索引主體若被歸一化為合成
	// `__match_subj_L_C`（安全索引 + option match），其型別即 `?elem`
	// —— 這裡**不**寫回 sem（那會污染糖形態的符號表），只用來判列舉。
	matchedVarType := ""
	isEnumType := false
	var enumVariants []string
	setType := func(t string) {
		matchedVarType = t
		if vs, ok := l.p.sem.EnumVariantsOf(t); ok {
			isEnumType = true
			enumVariants = vs
		}
	}
	if ident, ok := matched.(*Identifier); ok {
		if t, ok := l.p.sem.FuncVarType(l.curFuncName, ident.Value); ok {
			setType(t)
		}
	} else if idx, ok := matched.(*IndexExpression); ok && l.p.isSafeIndexBase(idx) {
		if matchArmsAreOptionMatch(sm.Arms) {
			if elem := l.p.inferIndexElemType(idx); elem != "" {
				subjType := "?" + elem
				if len(elem) > 0 && elem[0] == '?' {
					subjType = elem
				}
				setType(subjType)
			}
		}
	}

	// 列出各臂聲明的變體（對應 buildMatchDesugar 的 hasExplicit* 掃描 + 列舉
	// 載荷析構綁定正規化）。
	hasOk, hasErr, hasNil := false, false, false
	listedVariants := map[string]bool{}
	for i := range sm.Arms {
		a := &sm.Arms[i]
		cond := a.Condition
		if isEnumType && a.BindingName == "" && len(a.BindingNames) == 0 {
			if norm, ok := enumPayloadVariantOf(l.p, matchedVarType, cond); ok {
				cond = &Identifier{Token: norm.Token, Value: norm.Value}
			}
		}
		switch {
		case len(a.MultiOptionPatterns) > 0:
			for _, pat := range a.MultiOptionPatterns {
				switch pat {
				case "ok":
					hasOk = true
				case "err":
					hasErr = true
				case "nil":
					hasNil = true
				}
			}
		case a.IsDotVal:
			hasOk = true
		case cond != nil:
			if ident, ok := cond.(*Identifier); ok {
				switch ident.Value {
				case "ok":
					hasOk = true
				case "err":
					hasErr = true
				case "nil":
					hasNil = true
				default:
					if isEnumType {
						listedVariants[ident.Value] = true
					}
				}
			} else if _, ok := cond.(*NilLiteral); ok {
				hasNil = true
			}
		}
	}

	// 降糖是從最後一臂往前跑（inside-out），這裡同序，多條診斷的先後才一致。
	for i := len(sm.Arms) - 1; i >= 0; i-- {
		a := &sm.Arms[i]
		if !a.IsWildcard || a.IsDotVal {
			continue
		}
		var msg string
		switch {
		case isEnumType:
			remaining := 0
			for _, v := range enumVariants {
				if !listedVariants[v] {
					remaining++
				}
			}
			if remaining != 0 {
				continue
			}
			msg = "'->' arm is unreachable: all enum variants have been listed"
		case hasOk && hasErr && hasNil:
			msg = "'->' arm is unreachable: all option variants (ok, err, nil) have been listed"
		default:
			continue
		}
		pos := a.Pos
		if pos.Line == 0 && a.Body != nil {
			pos = a.Body.Pos()
		}
		l.p.saveWarningf("u13hzynp", "line %d, column %d: %s", pos.Line, pos.Column, msg)
	}
}

// enumPayloadVariantOf 判斷一個臂是否是「列舉載荷析構綁定」`some(v) ->`，
// 是則回傳它等價的變體識別符。邏輯與 buildMatchDesugar 的正規化一致：
// 括號內全為裸識別符，且數量與該變體的欄位型別數相符。
func enumPayloadVariantOf(p *Parser, enumType string, cond Expression) (*Identifier, bool) {
	call, ok := cond.(*CallExpression)
	if !ok || len(call.Arguments) == 0 {
		return nil, false
	}
	fn, ok := call.Function.(*Identifier)
	if !ok {
		return nil, false
	}
	names := make([]string, 0, len(call.Arguments))
	for _, arg := range call.Arguments {
		id, isID := arg.(*Identifier)
		if !isID {
			return nil, false
		}
		names = append(names, id.Value)
	}
	fieldTypes := p.enumVariantFieldTypes(enumType, fn.Value)
	if len(fieldTypes) == 0 || len(fieldTypes) != len(names) {
		return nil, false
	}
	return fn, true
}

// matchArmsAreOptionMatch 複製 buildMatchDesugar 的 isOptMatch 判定：
// 索引主體只有在「這是 option match」時才會被歸一化。
func matchArmsAreOptionMatch(arms []MatchArm) bool {
	for i := range arms {
		a := &arms[i]
		if a.IsDotVal {
			return true
		}
		switch c := a.Condition.(type) {
		case *Identifier:
			if c.Value == "ok" || c.Value == "nil" || c.Value == "err" {
				return true
			}
		case *NilLiteral:
			return true
		}
	}
	return false
}
