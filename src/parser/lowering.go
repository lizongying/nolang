// lowering.go — lowering pass：表層 AST（SurfaceMatch）→ 核心 AST（IfExpression 鏈）。
//
// parser 在解析 match 語法（裸 match `{ cond -> body }`、matched match `x: { ... }`、
// deprecated switch/~match）時只產出表層節點 SurfaceMatch（記錄 arms 與解析期語義
// 快照），不立即 desugar。ParseProgram 結束時呼叫 lowerProgram，由 lowering pass
// 自底向上將所有 SurfaceMatch 展開為 if/elif/else 鏈（核心 AST）。
// 下游（transpiler/LSP/formatter）看到的 AST 與舊「解析期立即 desugar」實作一致。
package parser

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/lizongying/nolang/lexer"
)

// ---- 表層 AST 節點 ----

// SurfaceMatch — match 語法的表層 AST 節點。
// parser 只負責收集 arms 與解析期語義快照；desugar 由 lowering pass 延後執行。
type SurfaceMatch struct {
	Token   lexer.Token // LBRACE（裸 match）或 matched match 的起始 token
	Matched Expression  // 被匹配的表達式；nil 表示裸 match（條件直接使用）
	Arms    []matchArm

	// OpeningBraceComment 保留裸 match `{` 同行註釋，lowering 時轉移到 IfExpression。
	OpeningBraceComment *CommentGroup
}

func (sm *SurfaceMatch) expressionNode()     {}
func (sm *SurfaceMatch) Pos() lexer.Position { return posFromToken(sm.Token) }
func (sm *SurfaceMatch) EndPos() lexer.Position {
	if n := len(sm.Arms); n > 0 && sm.Arms[n-1].body != nil {
		return sm.Arms[n-1].body.EndPos()
	}
	return posFromToken(sm.Token)
}

// newSurfaceMatch 建立表層 match 節點（不捕獲型別快照；類型推斷由 Resolver pass
// 寫入語義副表，lowering 時自 p.sem 讀取）。
func (p *Parser) newSurfaceMatch(tok lexer.Token, matched Expression, arms []matchArm) *SurfaceMatch {
	return &SurfaceMatch{Token: tok, Matched: matched, Arms: arms}
}

// ---- lowering pass 驅動 ----

// lowerProgram 對整棵 AST 執行 lowering pass：以反射遍歷所有節點，
// 將 *SurfaceMatch 就地替換為 desugar 後的 IfExpression 鏈。
// 由 ParseProgram 在解析結束、拷貝 Warnings 之前呼叫。
func (p *Parser) lowerProgram(prog *Program) {
	if prog == nil {
		return
	}
	l := &lowerer{p: p, visited: map[uintptr]bool{}}
	l.walk(reflect.ValueOf(prog))
}

type lowerer struct {
	p       *Parser
	visited map[uintptr]bool // 指標去重：AST 有共享節點（如 MatchedExpr），避免重複遍歷
}

var surfaceMatchPtrType = reflect.TypeOf((*SurfaceMatch)(nil))

// walk 遞迴遍歷任意 AST 值。替換點是「介面欄位/介面切片元素中裝的 *SurfaceMatch」——
// AST 中 Expression/Statement 均以介面持有，且所有節點實作皆為指標，
// 因此經由指標 Elem() 抵達的介面欄位一定可設置（CanSet）。
func (l *lowerer) walk(v reflect.Value) {
	switch v.Kind() {
	case reflect.Ptr:
		if v.IsNil() {
			return
		}
		ptr := v.Pointer()
		if l.visited[ptr] {
			return
		}
		l.visited[ptr] = true
		l.walk(v.Elem())
	case reflect.Interface:
		if v.IsNil() {
			return
		}
		if v.Elem().Type() == surfaceMatchPtrType && v.CanSet() {
			sm := v.Interface().(*SurfaceMatch)
			if lowered := l.lowerSurfaceMatch(sm); lowered != nil {
				v.Set(reflect.ValueOf(lowered))
			} else {
				v.Set(reflect.Zero(v.Type()))
			}
			return
		}
		l.walk(v.Elem())
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			l.walk(v.Index(i))
		}
	case reflect.Struct:
		t := v.Type()
		for i := 0; i < v.NumField(); i++ {
			if t.Field(i).PkgPath != "" {
				continue // 非導出欄位（AST 子節點欄位均導出；matchArm 由 lowerSurfaceMatch 顯式處理）
			}
			l.walk(v.Field(i))
		}
		// 標籤條件迴圈包裝（`#N cond: { body }`）：解析期只掛上 SurfaceMatch，
		// lowering 後補齊 Body = IfExpression.Consequence（與舊解析期行為一致）。
		if v.CanAddr() {
			if fs, ok := v.Addr().Interface().(*ForStatement); ok {
				if fs.IsCondWrapper && fs.Body == nil {
					if ife, ok := fs.Condition.(*IfExpression); ok {
						fs.Body = ife.Consequence
					}
				}
			}
		}
	}
}

// lowerSurfaceMatch 將單個表層 match 節點展開為核心 AST。
// 先自底向上處理 matched 與各 arm 內部（嵌套 match），再建 if 鏈。
func (l *lowerer) lowerSurfaceMatch(sm *SurfaceMatch) Expression {
	l.walk(reflect.ValueOf(&sm.Matched))
	for i := range sm.Arms {
		a := &sm.Arms[i]
		l.walk(reflect.ValueOf(&a.condition))
		for j := range a.multiValuePatterns {
			l.walk(reflect.ValueOf(&a.multiValuePatterns[j]))
		}
		if a.body != nil {
			l.walk(reflect.ValueOf(a.body))
		}
	}

	if sm.Matched == nil {
		result := l.p.buildBareMatchDesugar(sm.Token, sm.Arms)
		if ifExpr, ok := result.(*IfExpression); ok && ifExpr != nil {
			ifExpr.OpeningBraceComment = sm.OpeningBraceComment
		}
		return result
	}
	return l.p.buildMatchDesugar(sm)
}

// ---- desugar 構建器（原 desugar.go，現屬 lowering 層） ----

// buildBareMatchDesugar 建立 if/elif/else 鏈（無 matched expression，條件直接使用）
func (p *Parser) buildBareMatchDesugar(tok lexer.Token, arms []matchArm) Expression {
	if len(arms) == 0 {
		return nil
	}

	var ifExpr *IfExpression
	for i := len(arms) - 1; i >= 0; i-- {
		arm := arms[i]
		if arm.isWildcard {
			if ifExpr == nil {
				ifExpr = &IfExpression{
					Token:           tok,
					Condition:       &IntegerLiteral{Token: tok, Value: 1},
					Consequence:     arm.body,
					IsBareMatch:     true,
					IsMatchWildcard: true,
				}
			} else {
				if ifExpr.Alternative == nil {
					ifExpr.Alternative = arm.body
				}
			}
		} else {
			var equalityPattern Expression
			var rangePattern *RangeExpression
			if rng, isRange := arm.condition.(*RangeExpression); isRange {
				rangePattern = rng
			} else {
				equalityPattern = arm.condition
			}
			newIf := &IfExpression{
				Token:           tok,
				Condition:       arm.condition,
				Consequence:     arm.body,
				Alternative:     nil,
				IsBareMatch:     true,
				EqualityPattern: equalityPattern,
				RangePattern:    rangePattern,
			}
			if ifExpr != nil {
				newIf.Alternative = &BlockStatement{
					Token:      tok,
					Statements: []Statement{&ExpressionStatement{Token: tok, Expression: ifExpr}},
				}
			}
			ifExpr = newIf
		}
	}

	return ifExpr
}

// buildMatchDesugar 建立 if/elif/else 鏈
//
// 對 option match（含 err/nil arm），直接使用 `matched == err` / `matched == nil`
// 比較，由 transpiler 的 generateInfixI1 識別 %option 變數並生成 tag 比較的 LLVM IR。
// wildcard arm（含 ok/val/->）作為 else 分支。
//
// 型別資訊（matched 變數型別、枚舉變體）一律取自 sm 的解析期快照，
// 不讀 parser 的活動符號表（lowering 延後執行時符號表已是整檔終態）。
func (p *Parser) buildMatchDesugar(sm *SurfaceMatch) Expression {
	tok, matched, arms := sm.Token, sm.Matched, sm.Arms
	if len(arms) == 0 {
		return nil
	}

	// 類型推斷結果取自語義副表（Resolver pass 寫入 p.sem），不再依賴解析期快照。
	matchedVarType := ""
	isEnumType := false
	var enumVariants []string
	if ident, ok := matched.(*Identifier); ok {
		if t, ok := p.sem.VarType(ident.Value); ok {
			matchedVarType = t
			if vs, ok := p.sem.EnumVariantsOf(t); ok {
				isEnumType = true
				enumVariants = vs
			}
		}
	}

	// Determine element type from option type for per-arm `it` type inference.
	// For ?i64, elemType = "i64"; for result unions (i64 | err), extract the payload type.
	elemType := ""
	if matchedVarType != "" {
		if strings.HasPrefix(matchedVarType, "?") {
			elemType = strings.TrimPrefix(matchedVarType, "?")
		} else if strings.Contains(matchedVarType, "|") {
			// 聯合型別（如 i64 | err）：提取 payload 型別（i64）
			elemType = unionElemType(matchedVarType)
		} else if isEnumType {
			// For enum match, set elemType to trigger per-arm it binding path
			elemType = matchedVarType
		}
	}

	// Shared it binding (used for hasRawCond or fallback)
	itStmt := p.buildItBinding(tok, matched)

	// For hasRawCond, set the element type on the shared binding for LSP inference
	if itStmt != nil && elemType != "" {
		itStmt.Type = &NamedType{Value: elemType}
	}

	// Check if any arm uses ok(cond) — if so, `it` must be bound BEFORE the if-chain
	// (not inside arm bodies) because the condition references `it`.
	hasRawCond := false
	for _, arm := range arms {
		if arm.isRawCond {
			hasRawCond = true
			break
		}
	}

	// Build from last to first (inside-out)
	var ifExpr *IfExpression
	var defaultBody *BlockStatement       // 最內層 wildcard body（直接作為 else，避免 if 1 {} 包裝）
	var defaultDotValBody *BlockStatement // track val branch body separately

	// Check which variants are explicitly listed (for computing else arm complement)
	hasExplicitOk, hasExplicitErr, hasExplicitNil := false, false, false
	// For enum types, track which enum variant identifiers are listed
	enumListedVariants := make(map[string]bool)
	matchedIsEnum := isEnumType
	for _, a := range arms {
		if len(a.multiOptionPatterns) > 0 {
			// Combined option patterns: mark all as explicit
			for _, pat := range a.multiOptionPatterns {
				if pat == "err" {
					hasExplicitErr = true
				} else if pat == "nil" {
					hasExplicitNil = true
				} else if pat == "ok" {
					hasExplicitOk = true
				}
			}
		} else if a.isDotVal {
			hasExplicitOk = true
		} else if a.condition != nil {
			if ident, ok := a.condition.(*Identifier); ok {
				if ident.Value == "err" {
					hasExplicitErr = true
				} else if ident.Value == "nil" {
					hasExplicitNil = true
				} else if ident.Value == "ok" {
					hasExplicitOk = true
				} else if matchedIsEnum {
					enumListedVariants[ident.Value] = true
				}
			} else if _, ok := a.condition.(*NilLiteral); ok {
				hasExplicitNil = true
			}
		}
	}

	for i := len(arms) - 1; i >= 0; i-- {
		arm := arms[i]

		// Create per-arm `it` binding with correct unwrapped type for LSP inference.
		// For ?i64: err arm → it: err, nil arm → it: nil, ok arm → it: i64
		if itStmt != nil && !hasRawCond && elemType != "" {
			var armType string
			skipItBinding := false
			if len(arm.multiOptionPatterns) > 0 {
				// Combined option patterns: compute armType from pattern set
				hasOk, hasErr, hasNil := false, false, false
				for _, pat := range arm.multiOptionPatterns {
					switch pat {
					case "ok":
						hasOk = true
					case "err":
						hasErr = true
					case "nil":
						hasNil = true
					}
				}
				if hasOk && hasErr && hasNil {
					armType = "ok_err_nil"
				} else if hasOk && hasErr {
					armType = "ok_err"
				} else if hasOk && hasNil {
					armType = "ok_nil"
				} else if hasErr && hasNil {
					armType = "else" // err | nil
				} else if hasOk {
					armType = "ok"
				} else if hasErr {
					armType = "err"
				} else if hasNil {
					armType = "nil"
				}
			} else if arm.isWildcard {
				if arm.isDotVal {
					armType = "ok" // ok-> is explicit ok case
				} else {
					// Compute complement: which variants remain for -> else arm
					if matchedIsEnum {
						// For enum types, compute complement dynamically
						var remaining []string
						for _, v := range enumVariants {
							if !enumListedVariants[v] {
								remaining = append(remaining, v)
							}
						}
						if len(remaining) == 0 {
							// All enum variants listed — the else arm is dead code
							skipItBinding = true
							pos := arm.pos
							if pos.Line == 0 {
								pos = arm.body.Pos()
							}
							p.saveWarning(fmt.Sprintf("line %d, column %d: '->' arm is unreachable: all enum variants have been listed",
								pos.Line, pos.Column))
						} else {
							armType = strings.Join(remaining, " | ")
						}
					} else {
						// Three variants: ok(elemType), err, nil
						okListed, errListed, nilListed := hasExplicitOk, hasExplicitErr, hasExplicitNil
						if okListed && errListed && nilListed {
							// All three option variants listed — the else arm is dead code
							skipItBinding = true
							pos := arm.pos
							if pos.Line == 0 {
								pos = arm.body.Pos()
							}
							p.saveWarning(fmt.Sprintf("line %d, column %d: '->' arm is unreachable: all option variants (ok, err, nil) have been listed",
								pos.Line, pos.Column))
						} else if okListed && !errListed && nilListed {
							armType = "err" // only err remains
						} else if okListed && errListed && !nilListed {
							armType = "nil" // only nil remains
						} else if okListed && !errListed && !nilListed {
							armType = "else" // err | nil
						} else if !okListed && errListed && nilListed {
							armType = "ok" // only i64 remains
						} else if !okListed && !errListed && nilListed {
							armType = "ok_err" // i64 | err
						} else if !okListed && errListed && !nilListed {
							armType = "ok_nil" // i64 | nil
						} else if !okListed && !errListed && !nilListed {
							armType = "ok_err_nil" // i64 | err | nil
						}
					}
				}
			} else if ident, ok := arm.condition.(*Identifier); ok {
				if ident.Value == "err" || ident.Value == "nil" {
					armType = ident.Value
				} else if ident.Value == "ok" {
					armType = "ok"
				} else if matchedIsEnum {
					armType = ident.Value // Use variant name as arm type for it binding
				}
			} else if _, ok := arm.condition.(*NilLiteral); ok {
				armType = "nil"
			} else if arm.isDotVal {
				// ok-> arm (dotVal wildcard): it should be the unwrapped elemType
				armType = "ok"
			}
			if armType != "" {
			// Use per-arm position so walker/index can distinguish synthetic bindings
				var armTok lexer.Token
				if arm.condition != nil {
					pos := arm.condition.Pos()
					armTok = lexer.Token{Type: lexer.IDENT, Literal: "it", Line: pos.Line, Column: pos.Column}
				} else if len(arm.body.Statements) > 0 {
					pos := arm.body.Statements[0].Pos()
					armTok = lexer.Token{Type: lexer.IDENT, Literal: "it", Line: pos.Line, Column: pos.Column}
				} else {
					armTok = tok
				}
				if armIt := p.buildItBindingForArm(armTok, matched, armType, elemType, matchedVarType, isEnumType); armIt != nil {
					// Set the synthetic end position to cover the arm body
					bodyEnd := arm.body.EndPos()
					if bodyEnd.Line == 0 && bodyEnd.Column == 0 && len(arm.body.Statements) > 0 {
						bodyEnd = arm.body.Statements[len(arm.body.Statements)-1].EndPos()
					}
					armIt.SyntheticEnd = bodyEnd
					arm.body = p.prependStmt(arm.body, armIt)
				}
			} else if !skipItBinding {
				arm.body = p.prependStmt(arm.body, itStmt)
			}
		} else if itStmt != nil && !hasRawCond {
			arm.body = p.prependStmt(arm.body, itStmt)
		}

		if arm.isWildcard {
			if ifExpr == nil {
				if defaultBody != nil && arm.isDotVal {
					// dotVal arm (ok->) after a regular -> wildcard (else):
					// treat as conditional arm: if matched == ok { body } else { defaultBody }
					cond := &InfixExpression{
						Token:    tok,
						Left:     matched,
						Operator: "==",
						Right:    &Identifier{Token: tok, Value: "ok"},
					}
					newIf := &IfExpression{
						Token:           tok,
						Condition:       cond,
						Consequence:     arm.body,
						Alternative:     defaultBody,
						IsBareMatch:     true,
						MatchedExpr:     matched,
						EqualityPattern: &Identifier{Token: tok, Value: "ok"},
					}
					ifExpr = newIf
				} else {
					// 最內層 wildcard：儲存 body 作為下一個條件 arm 的 else
					defaultBody = arm.body
					if arm.isDotVal {
						defaultDotValBody = arm.body
					}
				}
			} else {
				if arm.isDotVal {
					// ok-> when other arms already processed (e.g., {ok ->, nil ->, ->}):
					// wrap as outer condition instead of overwriting the alternative
					cond := &InfixExpression{
						Token:    tok,
						Left:     matched,
						Operator: "==",
						Right:    &Identifier{Token: tok, Value: "ok"},
					}
					newIf := &IfExpression{
						Token:       tok,
						Condition:   cond,
						Consequence: arm.body,
						Alternative: &BlockStatement{
							Token:      tok,
							Statements: []Statement{&ExpressionStatement{Token: tok, Expression: ifExpr}},
						},
						IsBareMatch:     true,
						MatchedExpr:     matched,
						EqualityPattern: &Identifier{Token: tok, Value: "ok"},
					}
					ifExpr = newIf
				} else {
					ifExpr.Alternative = arm.body
				}
			}
		} else {
			// 構造 match 條件
			var cond Expression
			var rangePattern *RangeExpression
			var equalityPattern Expression
			var optionPatterns []string
			var valuePatterns []Expression
			var rawCond Expression
			if len(arm.multiOptionPatterns) > 0 {
				// Combined option patterns: (matched == p1) || (matched == p2) || ...
				optionPatterns = arm.multiOptionPatterns
				for i, pat := range arm.multiOptionPatterns {
					patCond := &InfixExpression{
						Token:    tok,
						Left:     matched,
						Operator: "==",
						Right:    &Identifier{Token: tok, Value: pat},
					}
					if i == 0 {
						cond = patCond
					} else {
						cond = &InfixExpression{
							Token:    tok,
							Left:     cond,
							Operator: "||",
							Right:    patCond,
						}
					}
				}
			} else if len(arm.multiValuePatterns) > 0 {
				// Combined value patterns: 1 || 3 || 5 → (matched == 1) || (matched == 3) || (matched == 5)
				valuePatterns = arm.multiValuePatterns
				for i, pat := range arm.multiValuePatterns {
					patCond := &InfixExpression{
						Token:    tok,
						Left:     matched,
						Operator: "==",
						Right:    pat,
					}
					if i == 0 {
						cond = patCond
					} else {
						cond = &InfixExpression{
							Token:    tok,
							Left:     cond,
							Operator: "||",
							Right:    patCond,
						}
					}
				}
			} else if arm.isRawCond {
				// ok(cond) arm: condition is (matched == ok) && cond
				rawCond = arm.condition
				cond = &InfixExpression{
					Token: tok,
					Left: &InfixExpression{
						Token:    tok,
						Left:     matched,
						Operator: "==",
						Right:    &Identifier{Token: tok, Value: "ok"},
					},
					Operator: "&&",
					Right:    arm.condition,
				}
			} else if rng, isRange := arm.condition.(*RangeExpression); isRange {
				// Range condition: [a..b] → matched >= a && matched <= b
				rangePattern = rng
				cond = p.desugarRangeCondition(tok, matched, rng)
			} else {
				// matched == condition
				// 對 option 變數，condition 為 err/nil 時由 transpiler 生成 tag 比較
				equalityPattern = arm.condition
				cond = &InfixExpression{
					Token:    tok,
					Left:     matched,
					Operator: "==",
					Right:    arm.condition,
				}
			}
			newIf := &IfExpression{
				Token:           tok,
				Condition:       cond,
				Consequence:     arm.body,
				Alternative:     nil,
				IsBareMatch:     true,
				MatchedExpr:     matched,
				RangePattern:    rangePattern,
				EqualityPattern: equalityPattern,
				OptionPatterns:  optionPatterns,
				ValuePatterns:   valuePatterns,
				RawCond:         rawCond,
			}
			if ifExpr != nil {
				newIf.Alternative = &BlockStatement{
					Token:      tok,
					Statements: []Statement{&ExpressionStatement{Token: tok, Expression: ifExpr}},
				}
			} else if defaultBody != nil {
				// 直接使用 wildcard body 作為 else，避免 if 1 {} 包裝
				newIf.Alternative = defaultBody
				if defaultDotValBody == defaultBody {
					newIf.DotValBody = defaultBody
				}
			}
			ifExpr = newIf
		}
	}

	// 若所有 arm 都是 wildcard，或只有 wildcard 而無條件 arm
	if ifExpr == nil {
		if defaultBody != nil {
			// 唯一 arm 是 wildcard：用 if 1 {} 包裝（無法避免）
			ifExpr = &IfExpression{
				Token:           tok,
				Condition:       &IntegerLiteral{Token: tok, Value: 1},
				Consequence:     defaultBody,
				IsBareMatch:     true,
				MatchedExpr:     matched,
				IsMatchWildcard: true,
			}
			if defaultDotValBody == defaultBody {
				ifExpr.DotValBody = defaultBody
			}
		} else {
			return nil
		}
	}

	// When ok(cond) arms exist, wrap the if-chain in `if 1 { it = matched; <if-chain> }`
	// so that `it` is bound before the condition is evaluated.
	if hasRawCond && itStmt != nil {
		ifExpr = &IfExpression{
			Token:     tok,
			Condition: &IntegerLiteral{Token: tok, Value: 1},
			Consequence: &BlockStatement{
				Token: tok,
				Statements: []Statement{
					itStmt,
					&ExpressionStatement{Token: tok, Expression: ifExpr},
				},
			},
			IsBareMatch:    true,
			MatchedExpr:    matched,
			IsMatchWrapper: true,
		}
	}

	return ifExpr
}

// desugarRangeCondition 將 match arm 中的 RangeExpression 條件 desugar 為布爾表達式。
// 有界（4 種）：
//
//	[a..b] → matched >= a && matched <= b
//	[a..b) → matched >= a && matched < b
//	(a..b] → matched > a && matched <= b
//	(a..b) → matched > a && matched < b
//
// 無上限（4 種，End=nil）：
//
//	[a..)  → matched >= a
//	[a..]  → matched >= a
//	(a..)  → matched > a
//	(a..]  → matched > a
//
// 無下限（4 種，Start=nil）：
//
//	[..b]  → matched <= b
//	[..b)  → matched < b
//	(..b]  → matched <= b
//	(..b)  → matched < b
//
// 完全無界（4 種，Start=nil 且 End=nil）：
//
//	[..]   → true (匹配所有值)
//	[..)   → true
//	(..]   → true
//	(..)   → true
func (p *Parser) desugarRangeCondition(tok lexer.Token, matched Expression, rng *RangeExpression) Expression {
	var leftOp string
	if rng.LeftInc {
		leftOp = ">="
	} else {
		leftOp = ">"
	}
	var rightOp string
	if rng.RightInc {
		rightOp = "<="
	} else {
		rightOp = "<"
	}

	if rng.Start != nil && rng.End != nil {
		leftCond := &InfixExpression{
			Token:    tok,
			Left:     matched,
			Operator: leftOp,
			Right:    rng.Start,
		}
		rightCond := &InfixExpression{
			Token:    tok,
			Left:     matched,
			Operator: rightOp,
			Right:    rng.End,
		}
		return &InfixExpression{
			Token:    tok,
			Left:     leftCond,
			Operator: "&&",
			Right:    rightCond,
		}
	}
	if rng.Start != nil {
		return &InfixExpression{
			Token:    tok,
			Left:     matched,
			Operator: leftOp,
			Right:    rng.Start,
		}
	}
	if rng.End != nil {
		return &InfixExpression{
			Token:    tok,
			Left:     matched,
			Operator: rightOp,
			Right:    rng.End,
		}
	}
	// 完全無界 [..], [..), (..], (..) — 永真，匹配所有值
	return &IntegerLiteral{Token: tok, Value: 1}
}

// buildItBinding creates `it = matched` LetStatement when matched is an Identifier.
// Returns nil if matched is not an Identifier.
//
// When the variable has a known parse-time type (in varDeclTypes), the binding
// is created normally. When the type is unknown at parse time (e.g. the variable
// was assigned from a generic method call whose return type is only resolved
// after monomorphization, like v = m.get('a') where get returns ?v), a fallback
// binding with Type = nil is created. The codegen determines the type from
// g.varTypes at generation time.
func (p *Parser) buildItBinding(tok lexer.Token, matched Expression) *LetStatement {
	_, ok := matched.(*Identifier)
	if !ok {
		return nil
	}
	// Create the binding regardless of whether the type is known at parse time.
	// When varDeclTypes doesn't have the variable, Type is left nil so codegen
	// can infer it from g.varTypes (e.g. %option for option-returning calls).
	return &LetStatement{
		Token:       tok,
		Name:        &Identifier{Token: tok, Value: "it"},
		Value:       matched,
		IsSynthetic: true,
	}
}

// unionElemType 從聯合型別字串中提取 payload（非 err/nil）型別。
// "?i64" → "i64"；"i64 | err" → "i64"；"i64 | err | nil" → "i64"。
// 若無法唯一確定（如多個非變體型別）則回傳空字串。
func unionElemType(t string) string {
	if t == "" {
		return ""
	}
	if strings.HasPrefix(t, "?") {
		return strings.TrimPrefix(t, "?")
	}
	parts := strings.Split(t, "|")
	var elem []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "err" || p == "nil" {
			continue
		}
		elem = append(elem, p)
	}
	if len(elem) == 1 {
		return elem[0]
	}
	return ""
}

// buildItBindingForArm creates `it = matched` LetStatement with the correct type
// for the specific match arm. For option types (e.g., ?i64):
//
//	err arm -> it: err (variant type)
//	nil arm -> it: nil (variant type)
//	ok arm  -> it: elemType (e.g., i64 for ?i64)
//
// 型別資訊取自 sm 的解析期快照（MatchedVarType/IsEnumType）。
func (p *Parser) buildItBindingForArm(tok lexer.Token, matched Expression, armType string, elemType string, matchedVarType string, isEnumType bool) *LetStatement {
	_, ok := matched.(*Identifier)
	if !ok {
		return nil
	}
	t := matchedVarType
	if t == "" {
		return nil
	}

	var typeStr string
	if strings.HasPrefix(t, "?") && elemType != "" {
		// Option type: map armType to specific type string
		switch armType {
		case "err":
			typeStr = "err"
		case "nil":
			typeStr = "nil"
		case "ok":
			typeStr = elemType
		case "else":
			typeStr = "err | nil"
		case "ok_err":
			typeStr = elemType + " | err"
		case "ok_nil":
			typeStr = elemType + " | nil"
		case "ok_err_nil":
			typeStr = elemType + " | err | nil"
		default:
			return nil
		}
	} else if !isEnumType && elemType != "" && (strings.Contains(t, "err") || strings.Contains(t, "nil")) {
		// 結果聯合型別（如 i64 | err / i64 | err | nil）：按 armType 收窄 it。
		// 與 ?X 選項型別類似，但變體為 err/nil（而非 ok/err/nil）。
		var remaining []string
		for _, v := range []string{"err", "nil"} {
			if strings.Contains(t, v) {
				remaining = append(remaining, v)
			}
		}
		remStr := strings.Join(remaining, " | ")
		switch armType {
		case "err":
			typeStr = "err"
		case "nil":
			typeStr = "nil"
		case "ok":
			typeStr = elemType
		case "else":
			typeStr = remStr
		case "ok_err":
			typeStr = elemType + " | err"
		case "ok_nil":
			typeStr = elemType + " | nil"
		case "ok_err_nil":
			typeStr = elemType + " | " + remStr
		default:
			return nil
		}
	} else if isEnumType {
		// Enum type: armType is the variant name or union expression (e.g., "status1" or "status2 | status3")
		typeStr = armType
	} else {
		return nil
	}

	return &LetStatement{
		Token:       tok,
		Name:        &Identifier{Token: tok, Value: "it"},
		Value:       matched,
		Type:        &NamedType{Value: typeStr},
		IsSynthetic: true,
	}
}

// prependStmt prepends a statement to a BlockStatement, returning a new BlockStatement.
func (p *Parser) prependStmt(body *BlockStatement, stmt Statement) *BlockStatement {
	if body == nil {
		return &BlockStatement{Statements: []Statement{stmt}}
	}
	stmts := make([]Statement, 0, len(body.Statements)+1)
	stmts = append(stmts, stmt)
	stmts = append(stmts, body.Statements...)
	return &BlockStatement{Token: body.Token, Statements: stmts}
}
