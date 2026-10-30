package build

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/lizongying/nolang/builtin"
	"github.com/lizongying/nolang/checker"
	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
	"github.com/lizongying/nolang/ty"
)

// monomorphizeGenerics 對泛型函數進行單態化
func monomorphizeGenerics(program *parser.Program, varTypes map[string]string, typeOwner map[string]string) {
	// 收集所有泛型函數定義
	genericFns := make(map[string]*parser.FunctionDefinition)
	for _, stmt := range program.Statements {
		if fd, ok := stmt.(*parser.FunctionDefinition); ok {
			if len(fd.GenericParams) > 0 {
				genericFns[fd.Name] = fd
			} else if isGenericMethod(fd.Name) {
				// Method definitions like [n]t.fill have implicit generic params
				genericFns[fd.Name] = fd
			}
		}
	}
	if len(genericFns) == 0 {
		return
	}
	// 遞迴掃描所有陳述句尋找泛型呼叫（包括函數體內）
	var newStmts []parser.Statement
	for _, stmt := range program.Statements {
		scanStmtForGenericCalls(stmt, genericFns, varTypes, program, &newStmts, typeOwner)
	}
	program.Statements = append(program.Statements, newStmts...)
}

// isGenericMethod checks if a function name like "[n]t.method" has generic type params
func isGenericMethod(name string) bool {
	if len(name) > 3 && name[0] == '[' {
		closeB := strings.IndexByte(name, ']')
		if closeB > 0 && closeB+1 < len(name) {
			sizeParam := name[1:closeB]
			elemParam := name[closeB+1:]
			// Check for "." separator
			dotIdx := strings.IndexByte(elemParam, '.')
			if dotIdx > 0 {
				elem := elemParam[:dotIdx]
				return (isLowerLetter(sizeParam) || sizeParam == "") && isLowerLetter(elem)
			}
		}
	}
	if strings.HasPrefix(name, "[].") {
		return false // [].method - no generics
	}
	if len(name) > 2 && name[0] == '[' && name[1] == ']' {
		dotIdx := strings.IndexByte(name, '.')
		if dotIdx > 2 {
			elem := name[2:dotIdx]
			return isLowerLetter(elem)
		}
	}
	return false
}

// scanStmtForGenericCalls recursively scans statements for generic calls
func scanStmtForGenericCalls(stmt parser.Statement, genericFns map[string]*parser.FunctionDefinition,
	varTypes map[string]string, program *parser.Program, newStmts *[]parser.Statement, typeOwner map[string]string) {
	switch s := stmt.(type) {
	case *parser.ExpressionStatement:
		if ce, ok := s.Expression.(*parser.CallExpression); ok {
			processCallExpression(ce, genericFns, varTypes, program, newStmts, typeOwner)
		}
		// Also handle IfExpression (e.g. `if cond { ... }` as a statement),
		// whose Condition may contain method calls (e.g. `elif path.starts-with(x)`).
		if ie, ok := s.Expression.(*parser.IfExpression); ok {
			scanIfExpressionForGenericCalls(ie, genericFns, varTypes, program, newStmts, typeOwner)
		}
	case *parser.FunctionDefinition:
		if s.Body != nil {
			// Build per-function varTypes to avoid cross-function name pollution.
			// The global varTypes is shared across all functions, so a local variable
			// named `resp` of type `str` in one function would pollute the lookup for
			// a parameter named `resp` of type `response` in another function.
			funcVarTypes := make(map[string]string)
			for k, v := range varTypes {
				funcVarTypes[k] = v
			}
			for _, p := range s.Parameters {
				if p.Type != nil {
					funcVarTypes[p.Name] = p.Type.String()
				}
			}
			for _, r := range s.Results {
				if r.Name != "" && r.Type != nil {
					funcVarTypes[r.Name] = r.Type.String()
				}
			}
			collectVarTypesFromBody(s.Body, funcVarTypes, program.Sem)
			for _, bodyStmt := range s.Body.Statements {
				scanStmtForGenericCalls(bodyStmt, genericFns, funcVarTypes, program, newStmts, typeOwner)
			}
		}
	case *parser.LetStatement:
		// Method definitions: type.method = (params) { ... }
		// These are LetStatements with FunctionLiteral values; scan their bodies
		// for method calls that need resolution.
		if fl, ok := s.Value.(*parser.FunctionLiteral); ok && fl.Body != nil {
			funcVarTypes := make(map[string]string)
			for k, v := range varTypes {
				funcVarTypes[k] = v
			}
			for _, p := range fl.Parameters {
				if p.Type != nil {
					funcVarTypes[p.Name] = p.Type.String()
				}
			}
			for _, r := range fl.Results {
				if r.Name != "" && r.Type != nil {
					funcVarTypes[r.Name] = r.Type.String()
				}
			}
			collectVarTypesFromBody(fl.Body, funcVarTypes, program.Sem)
			for _, bodyStmt := range fl.Body.Statements {
				scanStmtForGenericCalls(bodyStmt, genericFns, funcVarTypes, program, newStmts, typeOwner)
			}
		} else if s.Value != nil {
			// Regular variable assignment: scan the value expression
			// for method calls that need resolution (e.g., s = "A".to-str())
			scanExprForGenericCalls(s.Value, genericFns, varTypes, program, newStmts, typeOwner)
		}
	case *parser.ForStatement:
		if s.Body != nil {
			for _, bodyStmt := range s.Body.Statements {
				scanStmtForGenericCalls(bodyStmt, genericFns, varTypes, program, newStmts, typeOwner)
			}
		}
	case *parser.BlockStatement:
		for _, bodyStmt := range s.Statements {
			scanStmtForGenericCalls(bodyStmt, genericFns, varTypes, program, newStmts, typeOwner)
		}
	}
}

// scanIfExpressionForGenericCalls recursively scans an IfExpression's Condition,
// Consequence, and Alternative for method calls that need resolution.
func scanIfExpressionForGenericCalls(ie *parser.IfExpression, genericFns map[string]*parser.FunctionDefinition,
	varTypes map[string]string, program *parser.Program, newStmts *[]parser.Statement, typeOwner map[string]string) {
	if ie.Condition != nil {
		scanExprForGenericCalls(ie.Condition, genericFns, varTypes, program, newStmts, typeOwner)
	}
	if ie.Consequence != nil {
		// Build block-scoped varTypes so that local variables declared inside
		// the top-level {} block are visible for generic method resolution
		// (e.g. arr.sort-asc() where arr is a local []i64).
		blockVarTypes := make(map[string]string)
		for k, v := range varTypes {
			blockVarTypes[k] = v
		}
		collectVarTypesFromBody(ie.Consequence, blockVarTypes, program.Sem)
		for _, s := range ie.Consequence.Statements {
			scanStmtForGenericCalls(s, genericFns, blockVarTypes, program, newStmts, typeOwner)
		}
	}
	if ie.Alternative != nil {
		altVarTypes := make(map[string]string)
		for k, v := range varTypes {
			altVarTypes[k] = v
		}
		collectVarTypesFromBody(ie.Alternative, altVarTypes, program.Sem)
		for _, s := range ie.Alternative.Statements {
			scanStmtForGenericCalls(s, genericFns, altVarTypes, program, newStmts, typeOwner)
		}
	}
}

// scanSurfaceMatchForGenericCalls 是 scanIfExpressionForGenericCalls 的 S1 糖形態對應。
// 降糖後 IfExpression.Condition 已內嵌 subject 比較，故只掃 Condition；糖形態下
// subject 在 sm.Matched、模式值在 arm.MultiValuePatterns，須顯式掃，否則會漏掉
// 這些位置的泛型/聯合呼叫。
func scanSurfaceMatchForGenericCalls(sm *parser.SurfaceMatch, genericFns map[string]*parser.FunctionDefinition,
	varTypes map[string]string, program *parser.Program, newStmts *[]parser.Statement, typeOwner map[string]string) {
	if sm.Matched != nil {
		scanExprForGenericCalls(sm.Matched, genericFns, varTypes, program, newStmts, typeOwner)
	}
	// 型別解構 match 的臂綁定只在 H0 降糖（ASTToHIR 的 desugarSurfaceMatch →
	// buildTypeMatchDesugar）才以合成 let 落地，晚於本 pass。先在臂作用域內
	// 登記窄化型別，讓臂體的方法呼叫看得见接收者型别（見 surfaceMatchIsTypeDispatch）。
	var unionMembers map[string][]string
	if program != nil && program.Sem != nil {
		unionMembers = program.Sem.UnionMembers
	}
	typeDispatch := surfaceMatchIsTypeDispatch(sm.Arms, unionMembers)
	for i := range sm.Arms {
		arm := &sm.Arms[i]
		if arm.Condition != nil {
			scanExprForGenericCalls(arm.Condition, genericFns, varTypes, program, newStmts, typeOwner)
		}
		for _, vp := range arm.MultiValuePatterns {
			if vp != nil {
				scanExprForGenericCalls(vp, genericFns, varTypes, program, newStmts, typeOwner)
			}
		}
		if arm.Body != nil {
			// 每個臂是獨立作用域，同 scanIfExpressionForGenericCalls 的 Consequence/Alternative。
			blockVarTypes := make(map[string]string)
			for k, v := range varTypes {
				blockVarTypes[k] = v
			}
			if typeDispatch {
				// 與 buildTypeMatchDesugar 的合成綁定對齊：`v i32 ->` 登記 v:i32，
				// 裸型別臂 `i32 ->` 登記 it:i32。只写本臂的 blockVarTypes（每臂獨立
				// 拷貝），不污染其他臂。
				if name, t := surfaceMatchArmTypeBinding(arm, unionMembers); name != "" {
					blockVarTypes[name] = t
				}
			}
			collectVarTypesFromBody(arm.Body, blockVarTypes, program.Sem)
			for _, s := range arm.Body.Statements {
				scanStmtForGenericCalls(s, genericFns, blockVarTypes, program, newStmts, typeOwner)
			}
		}
	}
}

// surfaceMatchIsOptionArm mirrors parser/lowering.go isOptionMatchArm: an
// ok/nil/err arm (or a `.->` dot-value arm) makes the whole match an OPTION
// match, which is dispatched by buildMatchDesugar — not buildTypeMatchDesugar.
// The parser keeps that predicate unexported; the arm shape it tests is pure
// syntax, so mirroring it here cannot drift with semantic side tables.
func surfaceMatchIsOptionArm(a *parser.MatchArm) bool {
	if a == nil {
		return false
	}
	switch c := a.Condition.(type) {
	case *parser.NilLiteral:
		return true
	case *parser.CallExpression:
		if id, ok := c.Function.(*parser.Identifier); ok {
			switch id.Value {
			case "ok", "err":
				return true
			}
		}
	case *parser.Identifier:
		switch c.Value {
		case "ok", "nil", "err":
			return true
		}
	}
	return a.IsDotVal
}

// surfaceMatchIsTypeDispatch mirrors the parser's dispatch decision for a
// SURFACE match (lowerSurfaceMatch / desugarSurfaceMatch): a match with no
// option arm whose arms carry type patterns is lowered by
// buildTypeMatchDesugar, which prepends a synthetic `let <bind> = <subject>`
// narrowed to the member type. Only the ARM SHAPE is tested here — the other
// half of the parser's condition (subject static type is a union) cannot be
// evaluated in this pass and is redundant anyway: arm shape alone forces the
// type-match path (see parser armsCarryTypePatterns).
func surfaceMatchIsTypeDispatch(arms []parser.MatchArm, unionMembers map[string][]string) bool {
	for i := range arms {
		if surfaceMatchIsOptionArm(&arms[i]) {
			return false
		}
	}
	for i := range arms {
		if _, t := surfaceMatchArmTypeBinding(&arms[i], unionMembers); t != "" {
			return true
		}
	}
	return false
}

// surfaceMatchArmTypeBinding returns the narrowed binding a type arm
// introduces: `v i32 ->` binds v (TypeBindingName) to i32 (TypePattern); a
// bare `i32 ->` arm binds the implicit `it` to the member named by the
// condition. The union-membership test for the bare form mirrors the
// parser's isUnionMemberName — direct members of ANY known union alias
// (SemanticContext.UnionMembers, exported from the parser at ParseProgram).
func surfaceMatchArmTypeBinding(arm *parser.MatchArm, unionMembers map[string][]string) (string, string) {
	if arm == nil {
		return "", ""
	}
	if arm.IsTypeArm && arm.TypePattern != "" {
		name := arm.TypeBindingName
		if name == "" {
			name = "it"
		}
		return name, arm.TypePattern
	}
	if id, ok := arm.Condition.(*parser.Identifier); ok && isUnionMemberNameIn(id.Value, unionMembers) {
		return "it", id.Value
	}
	return "", ""
}

func isUnionMemberNameIn(name string, unionMembers map[string][]string) bool {
	if name == "" {
		return false
	}
	for _, members := range unionMembers {
		for _, m := range members {
			if m == name {
				return true
			}
		}
	}
	return false
}

// scanExprForGenericCalls recursively walks an expression tree to find
// CallExpressions (including method calls) that need generic/method resolution.
func scanExprForGenericCalls(expr parser.Expression, genericFns map[string]*parser.FunctionDefinition,
	varTypes map[string]string, program *parser.Program, newStmts *[]parser.Statement, typeOwner map[string]string) {
	switch e := expr.(type) {
	case *parser.CallExpression:
		processCallExpression(e, genericFns, varTypes, program, newStmts, typeOwner)
		for _, arg := range e.Arguments {
			scanExprForGenericCalls(arg, genericFns, varTypes, program, newStmts, typeOwner)
		}
	case *parser.InfixExpression:
		scanExprForGenericCalls(e.Left, genericFns, varTypes, program, newStmts, typeOwner)
		scanExprForGenericCalls(e.Right, genericFns, varTypes, program, newStmts, typeOwner)
	case *parser.PrefixExpression:
		scanExprForGenericCalls(e.Right, genericFns, varTypes, program, newStmts, typeOwner)
	case *parser.SurfaceMatch:
		scanSurfaceMatchForGenericCalls(e, genericFns, varTypes, program, newStmts, typeOwner)
	case *parser.IfExpression:
		scanIfExpressionForGenericCalls(e, genericFns, varTypes, program, newStmts, typeOwner)
	case *parser.GroupedExpression:
		scanExprForGenericCalls(e.Expression, genericFns, varTypes, program, newStmts, typeOwner)
	}
}

// monomorphizeUnions 對聯合型別（union type alias）進行單態化。
// 對每個帶有 ..T（T 為 union alias）的函數（variadic），或參數/結果
// 使用 union alias 的非 variadic 函數，生成 N 個函數（每個 union
// 成員一個），函數名為 "<原名>__<成員>"。原函數定義保留作為「範本」
// 供後續步驟識別用途，但會在 codegen 階段被跳過（靠 IsVariadic &&
// VariadicUnion != "" 判斷；或 GenericUnion != "" 判斷）。
func monomorphizeUnions(program *parser.Program) {
	aliases, _ := checker.ValidateUnionTypes(program)
	if os.Getenv("NOLANG_UNION_DEBUG") != "" {
		fmt.Fprintf(os.Stderr, "[union-debug] monomorphizeUnions: %d aliases, %d statements\n", len(aliases), len(program.Statements))
		typeAliasCount := 0
		for _, stmt := range program.Statements {
			if ta, ok := stmt.(*parser.TypeAlias); ok {
				typeAliasCount++
				fmt.Fprintf(os.Stderr, "[union-debug]   TypeAlias: name=%s isUnion=%v\n", ta.Name, ta.IsUnion())
			}
		}
		fmt.Fprintf(os.Stderr, "[union-debug]   total TypeAlias statements: %d\n", typeAliasCount)
		for name, ta := range aliases {
			fmt.Fprintf(os.Stderr, "[union-debug]   alias %s: isUnion=%v\n", name, ta.IsUnion())
		}
		for _, stmt := range program.Statements {
			if fd, ok := stmt.(*parser.FunctionDefinition); ok {
				fmt.Fprintf(os.Stderr, "[union-debug]   func %s: IsVariadic=%v VariadicUnion=%q GenericUnion=%q\n",
					fd.Name, fd.IsVariadic, program.Sem.VariadicUnionOf(fd), program.Sem.GenericUnionOf(fd))
			}
		}
	}
	if len(aliases) == 0 {
		return
	}
	// 收集所有需要單態化的函數
	type pending struct {
		fd        *parser.FunctionDefinition
		unionName string
		members   []parser.Type
	}
	var pendingFns []pending
	for _, stmt := range program.Statements {
		fd, ok := stmt.(*parser.FunctionDefinition)
		if !ok {
			continue
		}
		var unionName string
		if fd.IsVariadic && program.Sem.VariadicUnionOf(fd) != "" {
			unionName = program.Sem.VariadicUnionOf(fd)
		} else if !fd.IsVariadic && program.Sem.GenericUnionOf(fd) != "" {
			unionName = program.Sem.GenericUnionOf(fd)
		}
		if unionName == "" {
			if os.Getenv("NOLANG_UNION_DEBUG") != "" {
				fmt.Fprintf(os.Stderr, "[union-debug] skip %s: IsVariadic=%v VariadicUnion=%q GenericUnion=%q\n",
					fd.Name, fd.IsVariadic, program.Sem.VariadicUnionOf(fd), program.Sem.GenericUnionOf(fd))
			}
			continue
		}
		if os.Getenv("NOLANG_UNION_DEBUG") != "" {
			fmt.Fprintf(os.Stderr, "[union-debug] monomorphize %s: union=%s\n", fd.Name, unionName)
		}
		members := checker.FlattenUnion(unionName, aliases)
		if len(members) == 0 {
			continue
		}
		pendingFns = append(pendingFns, pending{fd: fd, unionName: unionName, members: members})
	}
	if len(pendingFns) == 0 {
		return
	}
	var newStmts []parser.Statement
	for _, p := range pendingFns {
		for _, mem := range p.members {
			nt, ok := mem.(*parser.NamedType)
			if !ok {
				continue
			}
			concrete := cloneUnionVariant(p.fd, nt.Value, aliases, program.Sem)
			// 攜帶模板函數的註解（如 #{overflow = wrap}）到單態化複本，
			// 否則 generic/union 方法體內的有符號相減會遺失溢出模式而誤用預設 option 路徑。
			if program.Sem != nil {
				if ents := program.Sem.AnnotationsOf(p.fd); len(ents) > 0 {
					program.Sem.SetRawAnnotations(concrete, ents)
				}
			}
			newStmts = append(newStmts, concrete)
		}
		// 標記原函數為「範本」：在 name 末尾加 __TEMPLATE 使其不與生成版本衝突
		p.fd.Name = p.fd.Name + "__" + p.unionName + "_TEMPLATE"
	}
	program.Statements = append(program.Statements, newStmts...)
}

// rewriteUnionCalls 重寫對聯合型別泛型函數的呼叫。
// 在 monomorphizeUnions 之後，原函數被改名為 "<name>__<union>_TEMPLATE"，
// 具體版本為 "<name>__<memberType>"。此函數遍歷所有呼叫點，
// 根據引數型別推斷應使用的具體版本，並將呼叫名改寫為 "<name>__<memberType>"。
func rewriteUnionCalls(program *parser.Program, varTypes map[string]string) {
	// 收集所有模板函數：原名 → templateInfo
	// 模板名格式：<origName>__<unionName>_TEMPLATE
	templates := make(map[string]*unionTemplateInfo)
	for _, stmt := range program.Statements {
		fd, ok := stmt.(*parser.FunctionDefinition)
		if !ok {
			continue
		}
		if strings.HasSuffix(fd.Name, "_TEMPLATE") {
			base := strings.TrimSuffix(fd.Name, "_TEMPLATE")
			parts := strings.SplitN(base, "__", 2)
			if len(parts) != 2 {
				continue
			}
			origName := parts[0]
			unionName := parts[1]
			aliases, _ := checker.ValidateUnionTypes(program)
			members := checker.FlattenUnion(unionName, aliases)
			memberNames := make([]string, 0, len(members))
			for _, m := range members {
				if nt, ok := m.(*parser.NamedType); ok {
					memberNames = append(memberNames, nt.Value)
				}
			}
			// 定位 union 形參在「攤平後呼叫實參向量」中的下標：宣告參數裡找型別末段
			// 等於 union 末段的那一位（如 `b big`）；方法式呼叫被 resolveMethodCall
			// 前置接收者，故整體右移 1。找不到（例如 union 位就是隱含 self、或符號
			// 未前綴對齊）則退回 0，維持舊的自由函數語義。
			unionTail := unionMemberTail(unionName)
			declaredIdx := 0
			found := false
			for i, p := range fd.Parameters {
				nt, ok := p.Type.(*parser.NamedType)
				if !ok {
					continue
				}
				if unionMemberTail(nt.Value) == unionTail {
					declaredIdx = i
					found = true
					break
				}
			}
			argIdx := declaredIdx
			if fd.IsMethodDef {
				argIdx++
			}
			if !found {
				argIdx = 0
			}
			// 推定失敗時的回退成員：union 中唯一的非內建標量（聚合體）成員。至多一個
			// 才採用（多個聚合體無法判別該選哪個 → 留空，維持 i64 默認）。
			fallback := ""
			aggregateCount := 0
			for _, m := range memberNames {
				if !unionScalarMemberTail(unionMemberTail(m)) {
					aggregateCount++
					fallback = m
				}
			}
			if aggregateCount != 1 {
				fallback = ""
			}
			templates[origName] = &unionTemplateInfo{origName: origName, unionName: unionName, members: memberNames, unionArgIdx: argIdx, fallbackMember: fallback}
		}
	}
	if len(templates) == 0 {
		if os.Getenv("NOLANG_UNION_DEBUG") != "" {
			fmt.Fprintf(os.Stderr, "[union-debug] rewriteUnionCalls: no templates found\n")
		}
		return
	}
	if os.Getenv("NOLANG_UNION_DEBUG") != "" {
		fmt.Fprintf(os.Stderr, "[union-debug] rewriteUnionCalls: %d templates\n", len(templates))
		for name, tpl := range templates {
			fmt.Fprintf(os.Stderr, "[union-debug]   template %s: union=%s members=%v\n", name, tpl.unionName, tpl.members)
		}
	}
	// 遍歷所有語句，重寫呼叫
	rewriteUnionCallStmts(program.Statements, templates, varTypes, program.Sem)
}

// rewriteUnionCallStmts 遍歷語句列表，對每個語句中的聯合型別泛型呼叫進行重寫。
// 此函數與 rewriteUnionCallExpr 互相遞迴：rewriteUnionCallExpr 處理 IfExpression
// 的 Consequence/Alternative 時會呼叫本函數，以正確走訪所有語句類型
// （包括 LetStatement、ReturnStatement 等，而不僅是 ExpressionStatement）。
func rewriteUnionCallStmts(stmts []parser.Statement, templates map[string]*unionTemplateInfo, vt map[string]string, sem *parser.SemanticContext) {
	for _, stmt := range stmts {
		switch s := stmt.(type) {
		case *parser.ExpressionStatement:
			rewriteUnionCallExpr(s.Expression, templates, vt, sem)
		case *parser.LetStatement:
			if s.Value != nil {
				rewriteUnionCallExpr(s.Value, templates, vt, sem)
			}
		case *parser.FunctionDefinition:
			if os.Getenv("NOLANG_UNION_DEBUG") != "" {
				fmt.Fprintf(os.Stderr, "[union-debug] walk FunctionDefinition: %s, body=%d stmts\n", s.Name, len(s.Body.Statements))
			}
			if s.Body != nil {
				// Augment varTypes with the function's parameter types to
				// correctly infer argument types for identifier expressions.
				// This prevents cross-module template matching (e.g. bigint.gcd
				// should not be rewritten by the number.gcd template).
				localVt := make(map[string]string)
				for k, v := range vt {
					localVt[k] = v
				}
				for _, param := range s.Parameters {
					if nt, ok := param.Type.(*parser.NamedType); ok {
						localVt[param.Name] = nt.Value
					}
				}
				for _, result := range s.Results {
					if nt, ok := result.Type.(*parser.NamedType); ok {
						localVt[result.Name] = nt.Value
					}
				}
				// Also collect local LetStatement types so that variables shadowing
				// globals or same-name locals from other functions (e.g. `c` in
				// dns-parse-records vs `c []str` elsewhere) resolve to the correct
				// union member type. Without this, a single global varTypes entry
				// would leak across functions and produce wrong memberType inference.
				collectVarTypesFromBody(s.Body, localVt, sem)
				rewriteUnionCallStmts(s.Body.Statements, templates, localVt, sem)
			}
		case *parser.BlockStatement:
			rewriteUnionCallStmts(s.Statements, templates, vt, sem)
		case *parser.ForStatement:
			if s.Condition != nil {
				rewriteUnionCallExpr(s.Condition, templates, vt, sem)
			}
			if s.Body != nil {
				rewriteUnionCallStmts(s.Body.Statements, templates, vt, sem)
			}
		case *parser.MultiAssignStatement:
			if s.Value != nil {
				rewriteUnionCallExpr(s.Value, templates, vt, sem)
			}
		case *parser.ReturnStatement:
			if s.ReturnValue != nil {
				rewriteUnionCallExpr(s.ReturnValue, templates, vt, sem)
			}
		}
	}
}

// rewriteUnionCallExpr 遞迴重寫表達式中的聯合型別呼叫
// unionMemberTail 取型別名的模組限定末段並剝 option／view 前綴，供「實參推定型別」
// 與「複本後綴（union 成員名）」做去前綴比對：`bigint.bigint`→`bigint`、`?i64`→`i64`、
// `&point`→`point`、`i64`→`i64`。用戶碼方法呼叫的實參型別常被前綴成 `mod.Type`，
// 而 monomorphizeUnions 的複本後綴用 union 成員裸名，兩者末段一致才代表同一成員。
func unionMemberTail(name string) string {
	name = unwrapType(name)
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		return name[i+1:]
	}
	return name
}

func rewriteUnionCallExpr(expr parser.Expression, templates map[string]*unionTemplateInfo, varTypes map[string]string, sem *parser.SemanticContext) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case *parser.CallExpression:
		// 先遞迴處理引數中的呼叫
		for _, arg := range e.Arguments {
			rewriteUnionCallExpr(arg, templates, varTypes, sem)
		}
		// 處理 curried 呼叫：(innerCall)(args)
		if _, ok := e.Function.(*parser.CallExpression); ok {
			rewriteUnionCallExpr(e.Function, templates, varTypes, sem)
			return
		}
		// 檢查是否為聯合型別泛型呼叫
		if ident, ok := e.Function.(*parser.Identifier); ok {
			if tpl, exists := templates[ident.Value]; exists {
				memberType := inferArgMemberType(e, tpl, varTypes)
				if os.Getenv("NOLANG_UNION_DEBUG") != "" {
					fmt.Fprintf(os.Stderr, "[union-debug] rewrite call %s: memberType=%s\n", ident.Value, memberType)
				}
				if memberType != "" {
					// Verify memberType is a valid member of the template's union.
					// 用去前綴末段比對——用戶碼實參型別常被前綴成 `bigint.bigint` 或
					// 僅末段 `bigint`，而複本後綴（tpl.members 一項，即 cloneUnionVariant
					// 用的 nt.Value）可能是另一種前綴形態；只比末段才不會漏改寫。跨模組
					// 同名呼叫已由 `templates[ident.Value]` 的「全名精確命中」擋掉，此處
					// 只決定選哪個 member，後綴一律用 members 的權威寫法。
					matchedMember := ""
					for _, m := range tpl.members {
						if unionMemberTail(m) == unionMemberTail(memberType) {
							matchedMember = m
							break
						}
					}
					if matchedMember != "" {
						ident.Value = ident.Value + "__" + matchedMember
						if os.Getenv("NOLANG_UNION_DEBUG") != "" {
							fmt.Fprintf(os.Stderr, "[union-debug]   rewritten to %s\n", ident.Value)
						}
					}
				}
			} else {
				// Not found by plain name; try method-style resolution
				// e.g., "sign" → "num.sign"
				for tplName, tpl := range templates {
					if strings.HasSuffix(tplName, "."+ident.Value) {
						// Found a method template, rewrite the identifier to use it
						memberType := inferArgMemberType(e, tpl, varTypes)
						if memberType != "" {
							matchedMember := ""
							for _, m := range tpl.members {
								if unionMemberTail(m) == unionMemberTail(memberType) {
									matchedMember = m
									break
								}
							}
							if matchedMember != "" {
								ident.Value = tplName + "__" + matchedMember
								if os.Getenv("NOLANG_UNION_DEBUG") != "" {
									fmt.Fprintf(os.Stderr, "[union-debug]   rewritten (method) %s → %s\n", ident.Value, ident.Value)
								}
								break
							}
						}
					}
				}
			}
		} else if dot, ok := e.Function.(*parser.DotExpression); ok {
			// 路線 C：運算符重載 desugar 產出 `a._add(b)`——Function 是 DotExpression
			// 而非 Identifier，舊碼只改寫 Identifier 呼叫，導致方法式聯合呼叫綁到
			// 未實例化的模板名（monomorphizeUnions 已把 `bigint._add` 改名成
			// `bigint._add__big_TEMPLATE` 並生成複本 `bigint._add__<member>`）。此處把
			// 方法呼叫的 Property 追加 `__<member>`，讓下游 resolveCallee
			// （recvTypeName + "." + method）命中對應複本。依「實參（右運算數）」推定
			// member 型別；比對時去模組前綴（末段），讓用戶碼 `a + b`（兩 bigint、實參
			// 型別被前綴成 `bigint.bigint`）也能對上複本後綴 `bigint`。
			//
			// 先遞迴進 Receiver：聯合方法呼叫常嵌在另一方法呼叫的接收者位，如
			// `(a + 1).to-str()`——運算子 `a._add(1)` 藏在 `.to-str` 的 Receiver 裡，
			// 舊碼不下降 Receiver 就永遠走不到，產生 "unknown callee ..._add"。
			rewriteUnionCallExpr(dot.Receiver, templates, varTypes, sem)
			matched := false
			for tplName, tpl := range templates {
				if !strings.HasSuffix(tplName, "."+dot.Property) {
					continue
				}
				memberType := inferArgMemberType(e, tpl, varTypes)
				if os.Getenv("NOLANG_UNION_DEBUG") != "" { // TEMP DEBUG
					fmt.Fprintf(os.Stderr, "[union-debug] method %s via %s: memberType=%q arg=%T\n", dot.Property, tplName, memberType, argAt(e, tpl))
				}
				if memberType == "" {
					continue
				}
				for _, m := range tpl.members {
					if unionMemberTail(m) == unionMemberTail(memberType) {
						dot.Property = dot.Property + "__" + m
						matched = true
						break
					}
				}
				if matched {
					break
				}
				if os.Getenv("NOLANG_UNION_DEBUG") != "" { // TEMP DEBUG
					fmt.Fprintf(os.Stderr, "[union-debug]   method %s NOT rewritten (members=%v fallback=%q)\n",
						dot.Property, tpl.members, tpl.fallbackMember)
				}
			}
		}
	case *parser.InfixExpression:
		rewriteUnionCallExpr(e.Left, templates, varTypes, sem)
		rewriteUnionCallExpr(e.Right, templates, varTypes, sem)
	case *parser.GroupedExpression:
		// `(a + 1).to-str()` 等場景：聯合方法呼叫被括號包成 GroupedExpression 作
		// 另一呼叫的 Receiver／實參。舊碼不下降到 Grouped 內層，導致嵌在括號裡的
		// `_add(a, 1)` 永遠漏改寫 → "unknown callee ..._add"。展開內層表達式續走。
		rewriteUnionCallExpr(e.Expression, templates, varTypes, sem)
	case *parser.PrefixExpression:
		rewriteUnionCallExpr(e.Right, templates, varTypes, sem)
	case *parser.SurfaceMatch:
		// S1：糖形態 match。降糖後 Condition 已內嵌 subject 比較與模式值，
		// 此處分臂顯式重寫 Matched / Condition / MultiValuePatterns / Body。
		if e.Matched != nil {
			rewriteUnionCallExpr(e.Matched, templates, varTypes, sem)
		}
		for i := range e.Arms {
			arm := &e.Arms[i]
			if arm.Condition != nil {
				rewriteUnionCallExpr(arm.Condition, templates, varTypes, sem)
			}
			for _, vp := range arm.MultiValuePatterns {
				if vp != nil {
					rewriteUnionCallExpr(vp, templates, varTypes, sem)
				}
			}
			if arm.Body != nil {
				rewriteUnionCallStmts(arm.Body.Statements, templates, varTypes, sem)
			}
		}
	case *parser.IfExpression:
		if e.Condition != nil {
			rewriteUnionCallExpr(e.Condition, templates, varTypes, sem)
		}
		// 走訪 Consequence/Alternative 中的所有語句類型（不僅 ExpressionStatement），
		// 以正確重寫 LetStatement（如 `blen-str = body-len.to-str()`）等內部的呼叫。
		// 僅走 ExpressionStatement 會導致 standalone if-then (`cond -> { let = call() }`)
		// 內的聯合型別呼叫未被重寫，產生 undefined `@int.to-str` 錯誤。
		if e.Consequence != nil {
			rewriteUnionCallStmts(e.Consequence.Statements, templates, varTypes, sem)
		}
		if e.Alternative != nil {
			rewriteUnionCallStmts(e.Alternative.Statements, templates, varTypes, sem)
		}
	case *parser.AssignExpression:
		rewriteUnionCallExpr(e.Value, templates, varTypes, sem)
	}
}

// argAt TEMP DEBUG：回傳 union 實參位上的表達式（供除錯印型別用）。
func argAt(call *parser.CallExpression, tpl *unionTemplateInfo) parser.Expression {
	if call == nil || len(call.Arguments) == 0 {
		return nil
	}
	idx := tpl.unionArgIdx
	if idx < 0 || idx >= len(call.Arguments) {
		idx = 0
	}
	return call.Arguments[idx]
}

// inferArgMemberType 從呼叫引數推斷應使用的聯合成員型別
func inferArgMemberType(call *parser.CallExpression, tpl *unionTemplateInfo, varTypes map[string]string) string {
	if len(call.Arguments) == 0 {
		return ""
	}
	// 用 union 形參所在實參位（tpl.unionArgIdx）——方法式呼叫已把接收者前置，
	// 舊碼硬編 arguments[0] 會讀到接收者型別而非 union 實參型別。越界時退回 0。
	idx := tpl.unionArgIdx
	if idx < 0 || idx >= len(call.Arguments) {
		idx = 0
	}
	member, known := inferExprMemberType(call.Arguments[idx], varTypes)
	if !known && tpl.fallbackMember != "" {
		// 實參型別無法靜態推定（未知标识符／呼叫結果）：對含聚合體成員的 union
		// 回退到該聚合體（std 內部運算符運算幾乎皆同型聚合體×聚合體）。
		return tpl.fallbackMember
	}
	return member
}

// inferExprMemberType 從表達式推斷聯合成員型別。第二返回值 known=false 表示「無法
// 靜態推定」（未知標識符、呼叫結果等），調用方可據此決定是否回退；known=true 時
// 返回的是可靠推定（字面量、varTypes 命中的標識符）。舊默認 i64 僅在 known=false
// 且無聚合體回退時由調用方保留。
func inferExprMemberType(expr parser.Expression, varTypes map[string]string) (string, bool) {
	switch v := expr.(type) {
	case *parser.IntegerLiteral:
		return "i64", true // 整數字面常量預設為 i64
	case *parser.FloatLiteral:
		return "f64", true
	case *parser.Identifier:
		// Look up the variable's actual type
		if varTypes != nil {
			if t, ok := varTypes[v.Value]; ok {
				// A union method reached through an OPTIONAL receiver dispatches
				// on the PAYLOAD type: the implicit option unwrap (`size ?i64`
				// -> `size.to-str()` calls `i64.to-str`) happens before the
				// union is consulted. Keeping the marker made "?i64" fail the
				// template's membership test below, so the call kept the
				// un-instantiated template name and the backend reported
				// "unknown callee number.int.to-str" (notools ls-run:
				// `size = fs.stat-size(full)` then `size.to-str()`). Strip the
				// option / view markers so the member type matches.
				return unwrapType(t), true
			}
		}
		// 未知標識符：無法靜態推定（舊碼硬回 i64，對聚合體 union 會誤綁）。
		return "i64", false
	case *parser.PrefixExpression:
		return inferExprMemberType(v.Right, varTypes)
	case *parser.GroupedExpression:
		return inferExprMemberType(v.Expression, varTypes)
	case *parser.InfixExpression:
		// 算術實參（`pick((k - 1))`、`m + (c - 1)`）：整顆 InfixExpression 曾被
		// 一律当成「無法推定」，於是「union 內恰有一個聚合體成員」的回退邏輯選中
		// 聚合體（`_add__bigint`），呼叫點卻仍傳 i64 槽——opaque pointer 讓 LLVM 不再
		// 在編譯期報 ABI 不符，錯誤一路活到運行期（`malloc: pointer being freed was
		// not allocated` → abort，實見 tmp/bx/d2.no）。
		//
		// 判據：變數才表意圖，字面量只表數量級。`1 + c`（c 是聚合體）不能因左側
		// 是整數字面量就定成 i64；`a + 1`（a 推不定）也必須保留「未知」，讓原本的
		// 聚合體回退照常生效——任一側推不定即整體未知，宁可回到舊行為。
		isLit := func(e parser.Expression) bool {
			switch e.(type) {
			case *parser.IntegerLiteral, *parser.FloatLiteral:
				return true
			}
			return false
		}
		lt, lok := inferExprMemberType(v.Left, varTypes)
		rt, rok := inferExprMemberType(v.Right, varTypes)
		if lok && !isLit(v.Left) {
			return lt, true
		}
		if rok && !isLit(v.Right) {
			return rt, true
		}
		if lok && rok {
			return lt, true // 兩側都是字面量：`pick(1 + 2)` 確為 i64
		}
		return "i64", false
	}
	// 呼叫結果等其它表達式：無法靜態推定。
	return "i64", false
}

// inferTypeFromExpr 嘗試從值表達式推斷變數型別。無法推斷時返回空白字串。
func inferTypeFromExpr(expr parser.Expression) string {
	switch e := expr.(type) {
	case *parser.IntegerLiteral:
		// 十六進位字面量（0xNN）推斷為 byte，十進位整數推斷為 i64
		raw := e.Token.Literal
		if len(raw) > 2 && raw[0] == '0' && (raw[1] == 'x' || raw[1] == 'X') {
			return "byte"
		}
		return "i64"
	case *parser.FloatLiteral:
		return "f64"
	case *parser.StringLiteral:
		return "str"
	case *parser.RegexLiteral:
		return "regexp"
	case *parser.BooleanLiteral:
		return "bool"
	case *parser.StructLiteral:
		return e.Type
	case *parser.PrefixExpression:
		if e.Operator == "-" || e.Operator == "+" {
			return inferTypeFromExpr(e.Right)
		}
		if e.Operator == "!" {
			return "bool"
		}
	case *parser.InfixExpression:
		// Comparison and logical operators always produce bool
		switch e.Operator {
		case "==", "!=", "<", ">", "<=", ">=", "&&", "||":
			return "bool"
		}
		if t := inferTypeFromExpr(e.Left); t != "" {
			return t
		}
		return inferTypeFromExpr(e.Right)
	case *parser.GroupedExpression:
		return inferTypeFromExpr(e.Expression)
	case *parser.ConditionalExpression:
		// `cond ? a : b` — type is the type of the consequence (or alternative)
		if t := inferTypeFromExpr(e.Consequence); t != "" {
			return t
		}
		return inferTypeFromExpr(e.Alternative)
	case *parser.MapLiteral:
		if e.MapType != nil {
			return e.MapType.String()
		}
		if len(e.Pairs) > 0 {
			keyType := inferTypeFromExpr(e.Pairs[0].Key)
			valType := inferTypeFromExpr(e.Pairs[0].Value)
			if keyType != "" && valType != "" {
				return "[" + keyType + "]" + valType
			}
		}
		return ""
	case *parser.CallExpression:
		if ident, ok := e.Function.(*parser.Identifier); ok {
			switch ident.Value {
			case "char-to-str":
				return "str"
			case "i64-to-str":
				return "str"
			case "f64-to-str":
				return "str"
			case "bool-to-str":
				return "str"
			case "byte-to-str":
				return "str"
			case "load-le-u16":
				return "u16"
			case "load-le-u32":
				return "u32"
			case "load-le-u64":
				return "u64"
			}
		}
		// Method call: receiver.load-le-u32(off) → DotExpression
		if dot, ok := e.Function.(*parser.DotExpression); ok {
			switch dot.Property {
			case "load-le-u16":
				return "u16"
			case "load-le-u32":
				return "u32"
			case "load-le-u64":
				return "u64"
			}
		}
	}
	return ""
}

// isValidType 檢查是否為有效的 Nolang 型別名
func isValidType(name string) bool {
	switch name {
	case "i8", "i16", "i32", "i64", "i128", "u8", "u16", "u32", "u64", "u128", "f32", "f64":
		return true
	}
	return false
}

// cloneUnionVariant 為 union 函數的某個成員型別複製一份具體實例。
// 替換函數簽名中的 variadic 元素型別為該成員（若為 variadic），
// 並將所有 union 別名的參數/結果型別替換為具體成員型別。
// 對於函數體內所有對「自己」的遞迴呼叫改名為具體版本。
func cloneUnionVariant(fd *parser.FunctionDefinition, memberType string, aliases map[string]*parser.TypeAlias, sem *parser.SemanticContext) *parser.FunctionDefinition {
	// 簡單深拷貝：先淺拷貝結構體，再逐欄位拷貝容器。
	clone := *fd
	clone.Parameters = make([]*parser.Parameter, len(fd.Parameters))
	for i, p := range fd.Parameters {
		pCopy := *p
		if i == len(fd.Parameters)-1 && fd.IsVariadic {
			// 最後一個參數是 variadic；元素型別改為具體成員
			var tok lexer.Token
			// Use the underlying type's token if available, otherwise the param token
			if p.Type != nil {
				if st, ok := p.Type.(*parser.SliceType); ok {
					tok = st.Token
				}
			}
			if tok.Type == 0 {
				tok = p.Token
			}
			pCopy.Type = &parser.SliceType{
				Token: tok,
				Elem:  &parser.NamedType{Value: memberType},
			}
		} else {
			// 非 variadic 參數：若型別是 union 別名，替換為具體成員
			if nt, ok := p.Type.(*parser.NamedType); ok {
				if _, isUnion := aliases[nt.Value]; isUnion {
					pCopy.Type = &parser.NamedType{Value: memberType}
				}
			}
		}
		clone.Parameters[i] = &pCopy
	}
	clone.Results = make([]*parser.Parameter, len(fd.Results))
	for i, r := range fd.Results {
		rCopy := *r
		// 若結果型別是 union 別名，替換為具體成員型別
		if nt, ok := r.Type.(*parser.NamedType); ok {
			if _, isUnion := aliases[nt.Value]; isUnion {
				rCopy.Type = &parser.NamedType{Value: memberType}
			}
		}
		clone.Results[i] = &rCopy
	}
	clone.Name = fd.Name + "__" + memberType
	// union 標記（VariadicUnion / GenericUnion）在 S5 stage 4 已遷入
	// SemanticContext side-table：克隆體是新節點、Sem 中無條目即空字串，
	// 等同舊版「克隆後清空標記」（實例化後該函數就是具體的）。
	// 繼承模板函數的溢出模式（#{overflow = wrap|clamp0}），使複本方法體內的
	// 有符號相減沿用正確模式（HIR 模式下 side-table 註解拷貝無效，必須以欄位攜帶）。
	clone.OverflowMode = fd.OverflowMode
	// 深拷貝 Body
	clone.Body = cloneBlockForUnion(fd.Body, fd.Name, clone.Name, memberType, sem)
	// 🔴 來源檔必須**深**繼承（2026-10-26）：cloneStmtForUnion 是淺拷貝 + 逐欄位
	// 搬運（TypeOverride 等），它不知道 NodeFacts 裡還有 SourceFile。少了這一行，
	// 單態化出來的 div__u8 / div__u16 / … 全體失去來源檔，lint 端於是回退成
	// 「空 SourceFile ＝ 主檔」的假設，把 std 的行號貼到用戶主檔上
	// （`main.no:250`，而主檔只有 9 行）。
	//
	// 為什麼到 2026-10-26 才暴露：在此之前 `#{...}` 註解存在各 Program 自己的
	// `SemanticContext.anns` 裡，克隆體是新節點、查不到條目 ⇒ std 的
	// `#{overflow=...}` 從來不觸發 lint，這條歸因路徑也就沒被走到。註解改為住在
	// 節點上之後它們隨淺拷貝一起進了克隆體，歸因缺陷才第一次顯形。
	//
	// 用 SetSourceFileDeep 而不是逐點 SetSourceFileOf：克隆體的每個子節點都是新
	// 指標，必須整棵子樹一起標（與 loadStdModulesOnDemand 的口徑一致）。
	if src := sem.SourceFileOf(fd); src != "" {
		sem.SetSourceFileDeep(&clone, src)
	}
	return &clone
}

// cloneBlockForUnion 深拷貝一個 block，遞迴地把對 <oldName> 的呼叫
// 改名為 <newName>。<memberType> 是當前單態化的具體型別。
func cloneBlockForUnion(bs *parser.BlockStatement, oldName, newName, memberType string, sem *parser.SemanticContext) *parser.BlockStatement {
	if bs == nil {
		return nil
	}
	out := &parser.BlockStatement{Token: bs.Token, RBrace: bs.RBrace}
	for _, s := range bs.Statements {
		out.Statements = append(out.Statements, cloneStmtForUnion(s, oldName, newName, memberType, sem))
	}
	return out
}

func cloneStmtForUnion(stmt parser.Statement, oldName, newName, memberType string, sem *parser.SemanticContext) parser.Statement {
	if stmt == nil {
		return nil
	}
	// IfExpression 在源碼中是 *parser.ExpressionStatement 包裝的
	// IfExpression（因為 IfExpression 實現了 Expression 而非 Statement）。
	// 我們在 ExpressionStatement case 內處理遞歸；不再單獨 case *IfExpression。
	switch s := stmt.(type) {
	case *parser.ExpressionStatement:
		// shallow-copy the wrapper and rewrite its expression
		es := *s
		es.Expression = cloneExprForUnion(s.Expression, oldName, newName, memberType, sem)
		return &es
	case *parser.LetStatement:
		ls := *s
		if s.Name != nil {
			n := *s.Name
			ls.Name = &n
		}
		ls.Type = s.Type
		// TypeOverride 必須繼承（S5 stage 5）：舊版複製的是已被裝配覆寫的
		// s.Type；覆寫搬進 Sem 後，不帶走條目會讓克隆體退回宣告型別。
		// 與 GenericUnion 的「克隆不繼承」相反——那裡是實例化後語義消失。
		if ov := sem.TypeOverrideOf(s); ov != "" {
			sem.SetTypeOverrideOf(&ls, ov)
		}
		ls.Value = cloneExprForUnion(s.Value, oldName, newName, memberType, sem)
		return &ls
	case *parser.BlockStatement:
		return cloneBlockForUnion(s, oldName, newName, memberType, sem)
	case *parser.ForStatement:
		fs := *s
		if s.IterRange != nil {
			fs.IterRange = cloneIterForUnion(s.IterRange, oldName, newName, memberType, sem)
		}
		if s.Condition != nil {
			fs.Condition = cloneExprForUnion(s.Condition, oldName, newName, memberType, sem)
		}
		fs.Body = cloneBlockForUnion(s.Body, oldName, newName, memberType, sem)
		return &fs
	case *parser.ReturnStatement:
		rs := *s
		rs.ReturnValue = cloneExprForUnion(s.ReturnValue, oldName, newName, memberType, sem)
		return &rs
	case *parser.MultiAssignStatement:
		mas := *s
		mas.Targets = append([]parser.Expression{}, s.Targets...)
		mas.Value = cloneExprForUnion(s.Value, oldName, newName, memberType, sem)
		return &mas
	}
	// Fallback: shallow copy via type assertion to the concrete type
	return stmt
}

func cloneIterForUnion(it *parser.IterationExpr, oldName, newName, memberType string, sem *parser.SemanticContext) *parser.IterationExpr {
	if it == nil {
		return nil
	}
	cp := *it
	if it.Range != nil {
		// RangeExpression has Start and End
		cp.Range = cloneRangeForUnion(it.Range, oldName, newName, memberType, sem)
	}
	if it.RangeExpr != nil {
		cp.RangeExpr = cloneExprForUnion(it.RangeExpr, oldName, newName, memberType, sem)
	}
	return &cp
}

func cloneRangeForUnion(r *parser.RangeExpression, oldName, newName, memberType string, sem *parser.SemanticContext) *parser.RangeExpression {
	if r == nil {
		return nil
	}
	cp := *r
	if r.Start != nil {
		cp.Start = cloneExprForUnion(r.Start, oldName, newName, memberType, sem)
	}
	if r.End != nil {
		cp.End = cloneExprForUnion(r.End, oldName, newName, memberType, sem)
	}
	return &cp
}

func cloneExprForUnion(expr parser.Expression, oldName, newName, memberType string, sem *parser.SemanticContext) parser.Expression {
	if expr == nil {
		return nil
	}
	switch e := expr.(type) {
	case *parser.CallExpression:
		ce := *e
		ce.Function = cloneExprForUnion(e.Function, oldName, newName, memberType, sem)
		ce.Arguments = make([]parser.Expression, len(e.Arguments))
		for i, a := range e.Arguments {
			ce.Arguments[i] = cloneExprForUnion(a, oldName, newName, memberType, sem)
		}
		return &ce
	case *parser.Identifier:
		cp := *e
		if cp.Value == oldName {
			cp.Value = newName
		}
		return &cp
	case *parser.DotExpression:
		de := *e
		de.Receiver = cloneExprForUnion(e.Receiver, oldName, newName, memberType, sem)
		return &de
	case *parser.SurfaceMatch:
		// S1：糖形態 match。克隆 Matched + 每臂 Condition / MultiValuePatterns / Body，
		// 其餘 MatchArm 欄位（綁定名、型別臂等）隨值拷貝保留。
		newSm := &parser.SurfaceMatch{
			Token:   e.Token,
			Matched: cloneExprForUnion(e.Matched, oldName, newName, memberType, sem),
		}
		if len(e.Arms) > 0 {
			newSm.Arms = make([]parser.MatchArm, len(e.Arms))
			for i := range e.Arms {
				arm := e.Arms[i]
				if arm.Condition != nil {
					arm.Condition = cloneExprForUnion(arm.Condition, oldName, newName, memberType, sem)
				}
				if len(arm.MultiValuePatterns) > 0 {
					arm.MultiValuePatterns = make([]parser.Expression, len(arm.MultiValuePatterns))
					for j, vp := range arm.MultiValuePatterns {
						arm.MultiValuePatterns[j] = cloneExprForUnion(vp, oldName, newName, memberType, sem)
					}
				}
				arm.Body = cloneBlockForUnion(arm.Body, oldName, newName, memberType, sem)
				newSm.Arms[i] = arm
			}
		}
		return newSm
	case *parser.IfExpression:
		ie := *e
		ie.Condition = cloneExprForUnion(e.Condition, oldName, newName, memberType, sem)
		ie.Consequence = cloneBlockForUnion(e.Consequence, oldName, newName, memberType, sem)
		if e.Alternative != nil {
			ie.Alternative = cloneBlockForUnion(e.Alternative, oldName, newName, memberType, sem)
		}
		return &ie
	case *parser.InfixExpression:
		ie := *e
		ie.Left = cloneExprForUnion(e.Left, oldName, newName, memberType, sem)
		ie.Right = cloneExprForUnion(e.Right, oldName, newName, memberType, sem)
		return &ie
	case *parser.PrefixExpression:
		pe := *e
		pe.Right = cloneExprForUnion(e.Right, oldName, newName, memberType, sem)
		return &pe
	}
	return expr
}

// processCallExpression handles a single CallExpression for generic resolution
func processCallExpression(ce *parser.CallExpression, genericFns map[string]*parser.FunctionDefinition,
	varTypes map[string]string, program *parser.Program, newStmts *[]parser.Statement, typeOwner map[string]string) {
	// Regular function call: fn(args)
	if fnName, ok := ce.Function.(*parser.Identifier); ok {
		if fd, exists := genericFns[fnName.Value]; exists {
			genericArgs := ce.GenericArgs
			if len(genericArgs) == 0 {
				genericArgs = inferGenericArgs(fd, ce, program)
			}
			if len(genericArgs) > 0 {
				concrete := cloneAndSubstitute(program, fd, genericArgs)
				// 攜帶模板函數的註解（如 #{overflow = wrap}）到單態化複本，
				// 否則 generic 方法體內的有符號相減會遺失溢出模式而誤用預設 option 路徑。
				if program.Sem != nil {
					if ents := program.Sem.AnnotationsOf(fd); len(ents) > 0 {
						program.Sem.SetRawAnnotations(concrete, ents)
					}
				}
				*newStmts = append(*newStmts, concrete)
				fnName.Value = concrete.Name
				ce.GenericArgs = nil
			}
		}
		// Check format string for :v spec on container variables.
		// When {var:v} is used with a container type (vec, arr, map, struct),
		// ensure the .to-str() method is monomorphized by synthesizing a
		// dummy call to var.to-str().
		isFmtFn := isFmtFunctionName(fnName.Value)
		if isFmtFn && len(ce.Arguments) > 0 {
			triggerToStrForFormatSpec(ce.Arguments[0], varTypes, genericFns, newStmts, program, typeOwner)
		}
	}
	// Method call: receiver.method(args)
	if dot, ok := ce.Function.(*parser.DotExpression); ok {
		resolveMethodCall(dot, ce, genericFns, varTypes, newStmts, program, typeOwner)
	}
	// Recurse into arguments
	for _, arg := range ce.Arguments {
		if innerCe, ok := arg.(*parser.CallExpression); ok {
			processCallExpression(innerCe, genericFns, varTypes, program, newStmts, typeOwner)
		}
	}
}

// isFmtFunctionName reports whether the function name is a format string
// function (print, println, printf, eprint, eprintln, eprintf, sprintf, format).
func isFmtFunctionName(name string) bool {
	switch name {
	case "print", "println", "printf", "eprint", "eprintln", "eprintf", "sprintf", "format":
		return true
	}
	return false
}

// triggerToStrForFormatSpec scans a format string literal for {name:v} patterns
// and triggers .to-str() monomorphization for container-typed variables.
func triggerToStrForFormatSpec(formatArg parser.Expression,
	varTypes map[string]string, genericFns map[string]*parser.FunctionDefinition,
	newStmts *[]parser.Statement, program *parser.Program, typeOwner map[string]string) {
	sl, ok := formatArg.(*parser.StringLiteral)
	if !ok || sl.Value == "" {
		return
	}
	// Parse format string for {name:spec} patterns where spec ends with 'v'
	segments, err := parser.ParseFormatString(sl.Value)
	if err != nil {
		return
	}
	for _, seg := range segments {
		if seg.Field == nil || seg.Field.Name == "" {
			continue
		}
		if seg.Field.Parsed == nil || seg.Field.Parsed.Type != 'v' {
			continue
		}
		varType, ok := varTypes[seg.Field.Name]
		if !ok {
			continue
		}
		// Check if the variable is a container type
		if isContainerTypeNolang(varType) {
			// Synthesize a dummy method call: var.to-str() to trigger
			// monomorphization of the generic to-str method.
			recv := &parser.Identifier{Value: seg.Field.Name}
			dot := &parser.DotExpression{Receiver: recv, Property: "to-str"}
			dummyCe := &parser.CallExpression{Function: dot}
			resolveMethodCall(dot, dummyCe, genericFns, varTypes, newStmts, program, typeOwner)
		}
	}
}

// fmtArgRecvTypes returns the candidate receiver types of a print argument.
// A fixed array [N]T yields both [N]T and its slice view []T (slicing an array
// produces a slice, and the backends may resolve to-str through either form).
// isContainerTypeNolang reports whether a Nolang type string (from varTypes)
// represents a container type that has a .to-str() method.
func isContainerTypeNolang(typeStr string) bool {
	// Slice: []T
	if strings.HasPrefix(typeStr, "[]") {
		return true
	}
	// Array: [N]T — skip for now as [n]t.to-str has a known codegen issue
	// Map: map[K]V or hashmap-K-V
	if strings.HasPrefix(typeStr, "map[") || strings.HasPrefix(typeStr, "hashmap-") {
		return true
	}
	// Struct: user-defined type name (not a builtin primitive)
	if !isBuiltinType(typeStr) && typeStr != "" && typeOf(typeStr).Kind != ty.Option {
		return true
	}
	return false
}

// fnExistsInProgram checks if a function or method with the given name exists
// in the program's top-level statements. Method definitions (e.g. f64.to-str,
// int.to-str) are stored as *parser.FunctionDefinition with the full dotted
// name, so a simple Name match suffices.
func fnExistsInProgram(program *parser.Program, name string) bool {
	if program == nil {
		return false
	}
	for _, stmt := range program.Statements {
		if fd, ok := stmt.(*parser.FunctionDefinition); ok && fd.Name == name {
			return true
		}
	}
	return false
}

// isBuiltinType reports whether name is a builtin primitive type (str, i64, etc.)
func isBuiltinType(name string) bool {
	switch name {
	case "i8", "i16", "i32", "i64", "i128",
		"u8", "u16", "u32", "u64", "u128",
		"f32", "f64",
		"byte", "bool", "str", "txt":
		return true
	}
	return false
}

// ifaceImplsOf 取出單一編譯單元的「介面 -> 實作者」對映。刻意從 Program 的語義
// 副表讀而不是全域表：見上方跨檔案污染的說明。
func ifaceImplsOf(program *parser.Program, iface string) []string {
	if program == nil || program.Sem == nil || iface == "" {
		return nil
	}
	return program.Sem.IfaceImplsOf(iface)
}

// devirtualizeInterfaceVars 把「宣告型別是介面、而具體實作可靜態唯一確定」的
// 變數具象化（devirtualization）：`ls.Type` 與 `varTypes` 一併換成具體實作型別。
//
// nolang 是單態化編譯器，介面宣告（`db enter, leave { ... }`，見
// src/std/database/sql.no）表達的是「一組方法簽名的約束」，不是執行期型別。
// `d db = sqlite.open(':memory:')` 之後 d 的實際型別就是驅動回傳的 `db-sqlite`。
// 若不具象化，方法攤平時會依介面名生成 `sql.db.exec` —— 一個不存在的函式，
// MIR 端報：
//
//	EmitLLVM: unknown callee sql.db.exec in func main
//
// 而且 `d.handle` 這類具體欄位存取也無從解析（介面本身沒有欄位）。
//
// 判據刻意保守：只有在「該介面在整個編譯單元裡恰好只有一個實作者」時才具象化。
// 多個實作者（例如同時 import sqlite 與 mysql 驅動）時保持原樣 —— 寧可讓下游
// 給出明確診斷，也不要猜一個實作而產生錯誤分派。
// ifaceImplTable 記錄「介面 -> 實作者型別」映射，由 devirtualizeInterfaceVars
// 在每次編譯時寫入 **該次編譯的 merged Program 的語義副表**（SemanticContext.
// IfaceImpls）；resolveMethodCall 在接收者的靜態型別是介面名時靠它回退到
// 唯一實作者 —— 介面本身只有方法簽名、沒有方法體可呼叫。
//
// 為什麼必須是 per-Program 而不能是全域變數：它是「單一編譯單元」的狀態。
// 放在全域時，`no vet <dir>` 的平行處理會讓先編譯的檔案（例如引入 sqlite 驅動
// 的 database-sql.no）把 `db -> [db-sqlite]` 留在表裡，後編譯的 ffi-mysql.no
// 於是把自己的介面呼叫攤平成 `sqlite.db-sqlite.query` —— 該定義在這個程式裡
// 並不存在，最終報成 `'sqlite.db-sqlite.query' is not defined` 假警報。
//
// 為什麼不能只靠 varTypes：合併後 buildVarTypes(merged) 會從 AST 重建一份
// 變數型別表，重建結果與 CompileTarget 早期那張表並非同一個 map，時序上無法
// 保證具象化寫入一定活到攤平那一刻。這張表按「介面名」查詢，與 varTypes 的
// 生命周期解耦。key 同時含完整名（sql.db）與去掉模組前綴的裸名（db），因為
// 不同 pass 拿到的名字形態不一致。
func devirtualizeInterfaceVars(mainProg, merged *parser.Program,
	varTypes map[string]string, typeOwner map[string]string) {
	if mainProg == nil || merged == nil || varTypes == nil {
		return
	}
	interfaces := make(map[string]bool)
	impls := make(map[string][]string)
	for _, stmt := range merged.Statements {
		switch s := stmt.(type) {
		case *parser.InterfaceDefinition:
			interfaces[s.Name] = true
		case *parser.StructDefinition:
			for _, im := range s.Implements {
				if im == "" {
					continue
				}
				impls[im] = append(impls[im], s.Name)
			}
		}
	}
	if len(interfaces) == 0 {
		return
	}
	// 供 resolveMethodCall 回退使用（見 ifaceImplTable 的說明）。
	if os.Getenv("NOLANG_IFACEDBG") != "" {
		fmt.Fprintf(os.Stderr, "[IFACEDBG] devirt merged=%p sem=%p\n", merged, merged.Sem)
	}
	for k, v := range impls {
		if merged.Sem != nil {
			merged.Sem.SetIfaceImpls(k, v)
			if i := strings.LastIndex(k, "."); i >= 0 {
				merged.Sem.SetIfaceImpls(k[i+1:], v)
			}
		}
	}
	// 宣告型別可能是 bare 名（尚未前綴化）或已前綴名，兩者都認定為介面。
	isIfaceName := func(name string) bool {
		return name != "" && (interfaces[name] || interfaces[prefixTypeName(name, typeOwner)])
	}
	// 第一遍：從主程序決定「變數名 -> 具體實作型別」。
	concreteByVar := make(map[string]string)
	for _, stmt := range mainProg.Statements {
		ls, ok := stmt.(*parser.LetStatement)
		if !ok || ls.Type == nil || ls.Name == nil {
			continue
		}
		declared := ls.Type.String()
		if !isIfaceName(declared) {
			continue
		}
		key := declared
		if !interfaces[key] {
			key = prefixTypeName(declared, typeOwner)
		}
		if list := impls[key]; len(list) == 1 {
			concreteByVar[ls.Name.Value] = list[0]
		}
	}
	if len(concreteByVar) == 0 {
		return
	}
	// 第二遍：主程序與 merged 一併套用。merged 裡可能存在主程序語句的**副本**，
	// 只改主程序那一份不足以影響後續 buildVarTypes(merged) 的重建（它會把
	// varTypes 從 AST 重新讀一遍，把具象化結果覆蓋回介面名）。套用時仍要求
	// 該宣告本身的型別是介面，避免誤傷其他模組裡恰好同名的變數。
	applyTo := func(prog *parser.Program) {
		if prog == nil {
			return
		}
		for _, stmt := range prog.Statements {
			ls, ok := stmt.(*parser.LetStatement)
			if !ok || ls.Type == nil || ls.Name == nil {
				continue
			}
			concrete, ok := concreteByVar[ls.Name.Value]
			if !ok || !isIfaceName(ls.Type.String()) {
				continue
			}
			// S5 stage 5：原就地寫 `ls.Type = &NamedType{…}`（違反 §1.4）改為
			// 寫 Sem.TypeOverride（覆蓋語義；ls.Type 保留宣告的介面名不動）。
			prog.Sem.SetTypeOverrideOf(ls, concrete)
		}
	}
	applyTo(mainProg)
	applyTo(merged)
	for name, c := range concreteByVar {
		varTypes[name] = c
	}
}

// resolveMethodCall resolves a DotExpression-based method call.
// Returns true if the call was resolved and rewritten.
func resolveMethodCall(dot *parser.DotExpression, ce *parser.CallExpression,
	genericFns map[string]*parser.FunctionDefinition, varTypes map[string]string,
	newStmts *[]parser.Statement, program *parser.Program, typeOwner map[string]string) bool {
	// Get receiver variable name and type
	recvIdent, ok := dot.Receiver.(*parser.Identifier)
	if !ok {
		return false
	}
	recvType, ok := varTypes[recvIdent.Value]
	if os.Getenv("NOLANG_DEBUG_DEVIRT") != "" {
		fmt.Fprintf(os.Stderr, "[devirt] flatten recv=%s ok=%v recvType=%q method=%s\n",
			recvIdent.Value, ok, recvType, dot.Property)
	}
	if !ok {
		return false
	}
	// Option receivers (?T): a method on ?T is a method on the inner type T
	// (e.g. ?[]byte.to-str is []byte.to-str, ?i64.to-str is i64.to-str). The
	// synthetic match binding `it` (it = <option>) is typed as the inner
	// element (e.g. []byte) by buildItBindingForArm, but varTypes still carries
	// the option type (?[]byte) from the original `let it = b`. Without stripping
	// the '?', matchTypePattern("[]t", "?[]byte") fails (concrete doesn't start
	// with "[]"), the builtin/slice prefix checks (HasPrefix(recvType,"[]")) also
	// miss, and the call collapses to the generic []t.to-str instance instead of
	// the concrete []byte.to-str monomorph — producing a type-mismatched
	// receiver and a runtime crash for `it.to-str()` on an option payload.
	// Stripping here mirrors the existing non-generic branch (recvTypeForMethod)
	// and is semantically correct: methods on ?T dispatch on T.
	recvType = unwrapType(recvType)
	// A VIEW receiver (`&T` / `?&T`) dispatches on the borrowed type: `v.sum()`
	// on a `&point` is `point.sum`. A view is `T*` at the LLVM level and a
	// method's `self` parameter is `T*` too, so the receiver needs no
	// conversion — only the callee name does.
	// （View 前綴已由上一行的 unwrapType 一併剝除）
	// 接收者靜態型別是介面（`d : db`）時，介面只有方法簽名、沒有方法體，
	// 攤平成 `sql.db.exec` 會得到不存在的函式（MIR: unknown callee）。
	// 回退到唯一實作者 —— 多個實作者時保持原樣，讓下游給出明確診斷，
	// 而不是猜一個實作產生錯誤分派。
	if impl := ifaceImplsOf(program, recvType); len(impl) == 1 {
		recvType = impl[0]
	}
	methodName := dot.Property
	// Search for matching generic method FIRST, so that generic methods whose
	// name collides with a builtin (e.g. "[n]t.sort-asc" on a fixed array
	// [N]T) are monomorphized before the builtin early-exit below can skip
	// them. The builtin early-exit previously ran first and skipped
	// monomorphization of sort-asc for fixed arrays, leaving an undefined
	// '@sort-asc' reference. vec.sort-asc is unaffected because the generic
	// pattern [n]t only matches fixed arrays, not []T slice receivers, so it
	// still falls through to the builtin dispatch.
	var candNames []string
	var candFds []*parser.FunctionDefinition
	var candArgs [][]parser.Expression
	for name, fd := range genericFns {
		dotIdx := strings.LastIndex(name, ".")
		if dotIdx < 0 {
			continue
		}
		typePrefix := name[:dotIdx]
		methodSuffix := name[dotIdx+1:]
		if methodSuffix != methodName {
			continue
		}
		// Try to match typePrefix (e.g., "[n]t") against recvType (e.g., "[4]i64")
		genericArgs := matchTypePattern(typePrefix, recvType, fd)
		if len(genericArgs) == 0 {
			continue
		}
		// COLLECT ALL matching generic methods (diagnostic: detect collisions
		// where >1 generic method matches the same receiver+method; map
		// iteration order then decides the winner nondeterministically).
		candNames = append(candNames, name)
		candFds = append(candFds, fd)
		candArgs = append(candArgs, genericArgs)
	}
	// All matching generic methods collected. Map iteration order is
	// nondeterministic, so if >1 candidate matched, the "first" (index 0)
	// winner varies run-to-run — a latent source of flaky/incorrect
	// monomorphization. Diagnose and deterministically pick the BEST match.
	if len(candNames) > 0 {
		// Deterministic selection: Go map iteration order is nondeterministic,
		// so if >1 generic method matched the same receiver+method the
		// original "first match wins" picked a random winner -> flaky/incorrect
		// monomorphization (e.g. []byte.to-str winning over []t.to-str for a
		// []i64 receiver). Prefer an EXACT type-prefix match (no generic type
		// variable) over a generic one; ties within a class keep insertion order.
		fd := candFds[0]
		genericArgs := candArgs[0]
		for i, cn := range candNames {
			if isExactTypePrefix(cn) {
				fd = candFds[i]
				genericArgs = candArgs[i]
				break
			}
		}
		// If the receiver is itself generic (e.g. self.foo() inside a generic
		// function whose receiver is `[]t`), the codegen cannot resolve the
		// method call, so we MUST monomorphize at HIR and flatten it into a
		// concrete function call (self becomes a by-value first argument).
		//
		// For a CONCRETE receiver (e.g. v : []i64) we must NOT flatten: leaving
		// the call as a method call lets the codegen resolve it and pass the
		// receiver by reference. Flattening would demote `self` from a method
		// receiver to a plain by-value parameter, so mutations such as
		// `self.len++` / `data[i] = val` would not propagate to the caller —
		// breaking mutating generic methods like vec.insert / vec.reverse.
		// This matches the alwaysAutoLoadStd baseline, which never pre-
		// monomorphizes std module methods and relies on the codegen to do it
		// per call site (with the receiver passed by reference).
		// Create concrete version and flatten the call.
		concrete := cloneAndSubstitute(program, fd, genericArgs)
		// 攜帶模板函數的註解（如 #{overflow = wrap}）到單態化複本，
		// 否則 generic 方法體內的有符號相減會遺失溢出模式而誤用預設 option 路徑。
		if program.Sem != nil {
			if ents := program.Sem.AnnotationsOf(fd); len(ents) > 0 {
				program.Sem.SetRawAnnotations(concrete, ents)
			}
		}
		*newStmts = append(*newStmts, concrete)
		// Rewrite call: replace DotExpression with Identifier, prepend receiver
		ce.Function = &parser.Identifier{
			Token: lexer.Token{Type: lexer.IDENT, Literal: concrete.Name},
			Value: concrete.Name,
		}
		// Prepend receiver as first argument
		receiverArg := &parser.Identifier{
			Token: recvIdent.Token,
			Value: recvIdent.Value,
		}
		ce.Arguments = append([]parser.Expression{receiverArg}, ce.Arguments...)
		// 記一筆「這是一次被攤平的方法呼叫」到語義副表：攤平後 DotExpression
		// 消失、接收者變成 Arguments[0]，這個事實已無法從 AST 自證，
		// checker 需要它來判定接收者被改寫（見 parser.CalleeKind）。
		if program != nil && program.Sem != nil {
			program.Sem.SetCallee(ce, parser.CalleeMethod)
		}
		return true
	}
	// Early exit: if the method is a builtin (e.g. "len") and no generic method
	// matched above, don't attempt non-generic rewriting — the codegen handles
	// builtin method calls via DotExpression dispatch + ForwardFunc / inline
	// field access. This prevents .len() inside method bodies from being
	// rewritten to _xbyte.len(self) (generic) or str.len(self) (non-generic),
	// which would generate a function call instead of inline field access.
	// Builtins like str.len are registered with MethodName="len" (no type
	// prefix), so we check the bare method name.
	if bi := builtin.FindBuiltinMethod(methodName); bi != nil && bi.ReceiverType != builtin.ReceiverGlobal {
		// Only skip if the receiver is a builtin type (str, []T, [N]T, i64, etc.)
		isBuiltinRecv := isBuiltinType(recvType) || isBuiltinContainer(recvType)
		if isBuiltinRecv {
			return false
		}
	}
	// Try non-generic method: type.method already exists
	// Rewrite to direct call with receiver prepended
	// Map types use "hashmap-K-V" naming convention (not "[K]V")
	// Option type (?T): method is defined on the inner type T, not on option.
	// Strip the leading "?" so concreteName becomes "T.method" (e.g. ?str.to-lower → str.to-lower).
	recvTypeForMethod := recvType
	if typeOf(recvTypeForMethod).Kind == ty.Option {
		recvTypeForMethod = optionInner(recvTypeForMethod)
	}
	// Apply module prefix if recvTypeForMethod is a bare user-defined type name.
	// varTypes is built before rewriteTypeRefs, so it may contain the un-prefixed
	// form (e.g. "conn" instead of "tls.conn"). Without this, the rewrite
	// would produce "conn.recv" but the function definition was renamed to
	// "tls.conn.recv" by prefixMethodNames.
	// typeOwner 的 key 為 "module.name"，value 為 bareName，故複用 prefixTypeName
	// 進行查找：恰好一個模組定義該型別時補上前綴，多個模組同名時保持原樣。
	if typeOwner != nil {
		recvTypeForMethod = prefixTypeName(recvTypeForMethod, typeOwner)
	}
	concreteName := recvTypeForMethod + "." + methodName
	if hmName := mapTypeToHashmapName(recvTypeForMethod); hmName != "" {
		concreteName = hmName + "." + methodName
	}
	// Don't rewrite if this is a builtin method (e.g. vec.push, vec.clear).
	// Builtins are registered with LLVM type prefixes (vec, arr, str, etc.),
	// not Nolang type names ([]str, []i64, etc.). The LLVM code generator
	// handles builtin method calls via DotExpression dispatch + ForwardFunc.
	// Rewriting them to []str.push(out, ...) would create undefined function calls.
	if typeOf(recvType).Kind == ty.Slice {
		// Slice types ([]str, []i64, []byte, etc.) map to "vec" builtins.
		// Check both "vec.<method>" and the concrete type prefix.
		if builtin.FindBuiltinMethod("vec."+methodName) != nil {
			return false
		}
		// Also check the concrete name (e.g. []byte.slice is registered directly)
		if builtin.FindBuiltinMethod(concreteName) != nil {
			return false
		}
		// Also check the bare method name (e.g. "len") — builtins like
		// str.len are registered with MethodName="len" (no type prefix),
		// so FindBuiltinMethod("vec.len") returns nil but
		// FindBuiltinMethod("len") finds it.
		if builtin.FindBuiltinMethod(methodName) != nil {
			return false
		}
	}
	if typeOf(recvType).Kind == ty.Array {
		// Array types ([N]T) map to "arr" builtins
		if builtin.FindBuiltinMethod("arr."+methodName) != nil {
			return false
		}
		// Some array builtins are registered under the slice name (e.g. []byte.zero
		// via ForwardFunc "arr-zero"); the codegen dispatches them for [N]T too.
		// Strip the size prefix so [32]byte.zero matches []byte.zero registration.
		if closeIdx := strings.IndexByte(recvType, ']'); closeIdx > 0 && closeIdx+1 < len(recvType) {
			elemType := recvType[closeIdx+1:]
			sliceBuiltinName := "[]" + elemType + "." + methodName
			if builtin.FindBuiltinMethod(sliceBuiltinName) != nil {
				return false
			}
		}
		// Also check the bare method name for the same reason as slices.
		if builtin.FindBuiltinMethod(methodName) != nil {
			return false
		}
	}
	// Check concrete name for other types (str, i64, etc.)
	if builtin.FindBuiltinMethod(concreteName) != nil {
		return false
	}
	// Also check the bare method name (e.g. "len") — some builtins like
	// str.len are registered with MethodName="len" (no type prefix), so
	// FindBuiltinMethod("str.len") returns nil but FindBuiltinMethod("len")
	// finds it. Without this check, .len() inside method bodies would be
	// rewritten to str.len(self), generating a function call instead of
	// inline field access.
	if isBuiltinType(recvType) {
		if builtin.FindBuiltinMethod(methodName) != nil {
			return false
		}
	}
	// Check if recvType is a member of a union type alias
	// If so, use the union alias prefix instead of the concrete type —
	// BUT only when the union method actually exists. When the union method
	// is not defined (e.g. float.to-str was removed in favor of f64.to-str),
	// keep the concrete type name so codegen can dispatch correctly.
	if program != nil {
		for _, stmt := range program.Statements {
			ta, ok := stmt.(*parser.TypeAlias)
			if !ok || ta.Union == nil {
				continue
			}
			for _, member := range ta.Union.Types {
				if nt, ok := member.(*parser.NamedType); ok && nt.Value == recvType {
					unionMethodName := ta.Name + "." + methodName
					if fnExistsInProgram(program, unionMethodName) {
						concreteName = unionMethodName
					}
					break
				}
			}
			if concreteName != recvType+"."+methodName {
				break
			}
		}
	}
	ce.Function = &parser.Identifier{
		Token: lexer.Token{Type: lexer.IDENT, Literal: concreteName},
		Value: concreteName,
	}
	receiverArg := &parser.Identifier{
		Token: recvIdent.Token,
		Value: recvIdent.Value,
	}
	ce.Arguments = append([]parser.Expression{receiverArg}, ce.Arguments...)
	// 同上：非泛型分支的攤平也要留記錄（見 parser.CalleeKind）。
	if program != nil && program.Sem != nil {
		program.Sem.SetCallee(ce, parser.CalleeMethod)
	}
	return true
}

// matchTypePattern matches a type pattern like "[n]t" against a concrete type like "[4]i64".
// Returns generic args (e.g., n=4, t=i64) or nil if no match.
func matchTypePattern(pattern, concrete string, fd *parser.FunctionDefinition) []parser.Expression {
	// Match [n]t against [4]i64
	if len(pattern) > 3 && pattern[0] == '[' {
		closeBracket := strings.IndexByte(pattern, ']')
		if closeBracket > 0 && closeBracket+1 < len(pattern) {
			sizeParam := pattern[1:closeBracket]
			elemParam := pattern[closeBracket+1:]
			if len(concrete) > 2 && concrete[0] == '[' {
				argClose := strings.IndexByte(concrete, ']')
				if argClose > 0 {
					argSize := concrete[1:argClose]
					argElem := concrete[argClose+1:]
					// [n]t pattern is for fixed-size arrays only.
					// If sizeParam is empty, pattern is []t (slice) — skip to
					// the []t matching logic below, don't return nil here.
					if sizeParam != "" && argSize == "" {
						return nil
					}
					var args []parser.Expression
					if sizeParam != "" {
						if isLowerLetter(sizeParam) {
							// [n]t pattern requires a numeric size; non-numeric
							// argSize (e.g. MapType [str]i64 where argSize="str")
							// must not match.
							val, err := strconv.ParseInt(argSize, 10, 64)
							if err != nil {
								return nil
							}
							args = append(args, &parser.IntegerLiteral{Value: val})
						} else if sizeParam != argSize {
							// Concrete size (e.g. "4"): must match exactly.
							return nil
						}
					}
					if elemParam != "" {
						if len(elemParam) == 1 && isLowerLetter(elemParam) {
							// Generic element variable (e.g. `t`): bind any concrete type.
							args = append(args, &parser.StringLiteral{Value: argElem})
						} else if elemParam == argElem {
							// Concrete element type: exact match only.
							args = append(args, &parser.StringLiteral{Value: argElem})
						} else {
							return nil
						}
					}
					if len(args) > 0 {
						return args
					}
				}
			}
		}
	}
	// Match []t against []i64 (slice pattern)
	if strings.HasPrefix(pattern, "[]") {
		elemParam := pattern[2:]
		if strings.HasPrefix(concrete, "[]") {
			argElem := concrete[2:]
			if len(elemParam) == 1 && isLowerLetter(elemParam) {
				// Generic element variable (e.g. `t`): bind to any concrete
				// element type. ([]t.to-str matches []i64, []byte, ...)
				return []parser.Expression{&parser.StringLiteral{Value: argElem}}
			}
			// Concrete element type (e.g. `byte`, `str`): must match EXACTLY.
			// Without this, isLowerLetter("byte") is true (because 'b' is a
			// lower letter) and []byte.to-str wrongly matches a []i64 receiver
			// (binding byte->i64), causing nondeterministic/incorrect
			// monomorphization when map iteration order flips the winning
			// candidate. See zz_multi flaky vec-to-str bug.
			if elemParam == argElem {
				return []parser.Expression{&parser.StringLiteral{Value: argElem}}
			}
		}
	}
	return nil
}

// isGenericReceiver reports whether the genericArgs resolved by matchTypePattern
// indicate that the method receiver itself is generic (e.g. a call to
// self.foo() inside a generic function whose receiver is `[]t`). In that case the
// codegen cannot resolve the method call, so HIR-level monomorphization must
// flatten it into a concrete function call. If the receiver is a CONCRETE type
// (e.g. `[]i64`), isGenericReceiver returns false and the call should be left as
// a method call so the codegen resolves it and passes the receiver by reference.
func isGenericReceiver(pattern string, args []parser.Expression) bool {
	var params []string
	if strings.HasPrefix(pattern, "[]") {
		params = append(params, pattern[2:])
	} else if len(pattern) > 3 && pattern[0] == '[' {
		closeB := strings.IndexByte(pattern, ']')
		if closeB > 0 && closeB+1 < len(pattern) {
			sizeP := pattern[1:closeB]
			elemP := pattern[closeB+1:]
			if isLowerLetter(sizeP) {
				params = append(params, sizeP)
			}
			if isLowerLetter(elemP) {
				params = append(params, elemP)
			}
		}
	}
	if len(params) == 0 {
		return false
	}
	for _, a := range args {
		sl, ok := a.(*parser.StringLiteral)
		if !ok {
			return false
		}
		for _, p := range params {
			if sl.Value == p {
				return true
			}
		}
	}
	return false
}

// inferGenericArgs 從函數呼叫的引數型別推斷泛型參數
// 例如 fn(arr [n]t) 被以 [8]byte 引數呼叫 → n=8, t=byte
func inferGenericArgs(fd *parser.FunctionDefinition, call *parser.CallExpression, program *parser.Program) []parser.Expression {
	if len(fd.Parameters) == 0 || len(call.Arguments) == 0 {
		return nil
	}
	var args []parser.Expression
	for pi, param := range fd.Parameters {
		if pi >= len(call.Arguments) {
			break
		}
		arg := call.Arguments[pi]
		argType := inferArgType(arg, program)
		paramType := param.Type.String()
		// 匹配泛型型別：t 與具體型別 i64
		if len(paramType) == 1 && paramType[0] >= 'a' && paramType[0] <= 'z' {
			if isLowerLetter(paramType) && argType != "" {
				args = append(args, &parser.StringLiteral{Value: argType})
			}
		}
		// 匹配參數型別 [n]t 與引數型別 [8]byte
		if len(paramType) > 3 && paramType[0] == '[' {
			closeBracket := strings.IndexByte(paramType, ']')
			if closeBracket > 0 && closeBracket+1 < len(paramType) {
				sizeParam := paramType[1:closeBracket]  // n
				elemParam := paramType[closeBracket+1:] // t
				// 從引數型別中提取具體值
				if len(argType) > 2 && argType[0] == '[' {
					argClose := strings.IndexByte(argType, ']')
					if argClose > 0 {
						argSize := argType[1:argClose]  // 8
						argElem := argType[argClose+1:] // byte
						if isLowerLetter(sizeParam) {
							if val, err := strconv.ParseInt(argSize, 10, 64); err == nil {
								args = append(args, &parser.IntegerLiteral{Value: val})
							}
						}
						if isLowerLetter(elemParam) {
							// 型別引數目前用字串表示
							args = append(args, &parser.StringLiteral{Value: argElem})
						}
					}
				}
			}
		}
	}
	return args
}

func isLowerLetter(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < 'a' || c > 'z' {
			return false
		}
	}
	return true
}

// isExactTypePrefix reports whether the receiver type prefix of a method name
// (the part before the final '.') is a CONCRETE type with no generic type
// variable. Generic params are always single lowercase letters (t, n, e, ...),
// so a prefix is generic iff it embeds one. This lets resolveMethodCall prefer
// an exact match over a generic one when several generic methods match the
// same receiver — making candidate selection deterministic instead of depending
// on Go map iteration order (which caused the flaky []i64.to-str bug).
func isExactTypePrefix(name string) bool {
	dotIdx := strings.LastIndex(name, ".")
	if dotIdx < 0 {
		return false
	}
	prefix := name[:dotIdx]
	// Bare generic variable, e.g. `t.method`.
	if len(prefix) == 1 && isLowerLetter(prefix) {
		return false
	}
	if strings.HasPrefix(prefix, "[]") {
		elem := prefix[2:]
		if elem == "" {
			return true
		}
		// []t is generic; []byte / []str are concrete.
		return !(len(elem) == 1 && isLowerLetter(elem))
	}
	if len(prefix) > 3 && prefix[0] == '[' {
		closeB := strings.IndexByte(prefix, ']')
		if closeB > 0 && closeB+1 < len(prefix) {
			sizeP := prefix[1:closeB]
			elemP := prefix[closeB+1:]
			// [n]t is generic; [4]i64 is concrete.
			if (len(sizeP) == 1 && isLowerLetter(sizeP)) || (len(elemP) == 1 && isLowerLetter(elemP)) {
				return false
			}
			return true
		}
	}
	// Plain concrete receiver (str, vec, i64, tls.conn, ...).
	return true
}

func inferArgType(expr parser.Expression, program *parser.Program) string {
	switch e := expr.(type) {
	case *parser.Identifier:
		for _, stmt := range program.Statements {
			if ls, ok := stmt.(*parser.LetStatement); ok {
				if ls.Name != nil && ls.Name.Value == e.Value && ls.Type != nil {
					return ls.Type.String()
				}
			}
		}
	case *parser.IntegerLiteral:
		return "i64"
	case *parser.FloatLiteral:
		return "f64"
	case *parser.StringLiteral:
		return "str"
	case *parser.RegexLiteral:
		return "regexp"
	case *parser.BooleanLiteral:
		return "bool"
	case *parser.GroupedExpression:
		return inferArgType(e.Expression, program)
	}
	return ""
}

func stmtCount(body *parser.BlockStatement) int {
	if body == nil {
		return -1
	}
	return len(body.Statements)
}

// cloneAndSubstitute 複製泛型函數並以具體值替換泛型參數
func cloneAndSubstitute(program *parser.Program, fd *parser.FunctionDefinition, genericArgs []parser.Expression) *parser.FunctionDefinition {
	if len(genericArgs) == 0 {
		return fd
	}
	// 複製並替換參數類型中的泛型標記
	subst := make(map[string]string) // 泛型參數名 → 具體值字串
	// For explicit generic params (positional matching)
	// Skip for implicit generic methods like [n]t.method - use name-based matching below
	isImplicitGenericMethod := len(fd.Name) > 3 && fd.Name[0] == '['
	if !isImplicitGenericMethod {
		for i, gp := range fd.GenericParams {
			if i < len(genericArgs) {
				if lit, ok := genericArgs[i].(*parser.IntegerLiteral); ok {
					subst[gp.Value] = fmt.Sprintf("%d", lit.Value)
				} else if lit, ok := genericArgs[i].(*parser.StringLiteral); ok {
					subst[gp.Value] = lit.Value
				}
			}
		}
	}
	// For implicit generic methods like [n]t.method:
	// Extract size/elem param names from the method name and match by type (not position)
	var sizeVal string
	var elemVal string
	for _, arg := range genericArgs {
		if lit, ok := arg.(*parser.IntegerLiteral); ok {
			sizeVal = fmt.Sprintf("%d", lit.Value)
		} else if lit, ok := arg.(*parser.StringLiteral); ok {
			elemVal = lit.Value
		}
	}
	if isImplicitGenericMethod {
		closeB := strings.IndexByte(fd.Name, ']')
		if closeB > 0 && closeB+1 < len(fd.Name) {
			sizeParam := fd.Name[1:closeB]
			elemPart := fd.Name[closeB+1:]
			dotIdx := strings.IndexByte(elemPart, '.')
			var elemParam string
			if dotIdx > 0 {
				elemParam = elemPart[:dotIdx]
			}
			// Add to subst if not already set by positional matching
			if isLowerLetter(sizeParam) && sizeVal != "" {
				if _, exists := subst[sizeParam]; !exists {
					subst[sizeParam] = sizeVal
				}
			}
			if isLowerLetter(elemParam) && elemVal != "" {
				if _, exists := subst[elemParam]; !exists {
					subst[elemParam] = elemVal
				}
			}
		}
	}
	// Build mangled name
	mangledName := fd.Name
	if isImplicitGenericMethod {
		// Replace generic type prefix with LLVM-safe name: [n]t.fill → _4xi64.fill
		closeB := strings.IndexByte(mangledName, ']')
		dotIdx := strings.IndexByte(mangledName, '.')
		if closeB > 0 && dotIdx > closeB {
			sizeParam := mangledName[1:closeB]
			elemParam := mangledName[closeB+1 : dotIdx]
			_ = sizeParam // used implicitly via isLowerLetter check below
			_ = elemParam
			if isLowerLetter(string(mangledName[1])) && isLowerLetter(string(mangledName[closeB+1])) {
				mangledName = "_" + sizeVal + "x" + elemVal + mangledName[dotIdx:]
			}
			// Handle slice generics ([]t.method): mangledName starts with "[]",
			// so mangledName[1] is ']' (not a lower letter). Produce _x<elem>.method.
			if mangledName[0] == '[' && mangledName[1] == ']' && elemVal != "" {
				mangledName = "_x" + elemVal + mangledName[dotIdx:]
			}
		}
	} else {
		// Regular generic function: append args to name
		for _, arg := range genericArgs {
			if lit, ok := arg.(*parser.IntegerLiteral); ok {
				mangledName += fmt.Sprintf(".%d", lit.Value)
			} else if lit, ok := arg.(*parser.StringLiteral); ok {
				mangledName += "." + lit.Value
			}
		}
	}
	// 複製參數
	newParams := make([]*parser.Parameter, len(fd.Parameters))
	for i, p := range fd.Parameters {
		newParams[i] = &parser.Parameter{
			Token: p.Token,
			Name:  p.Name,
			Type:  substituteType(p.Type, subst),
		}
	}
	// 複製回傳值
	newResults := make([]*parser.Parameter, len(fd.Results))
	for i, r := range fd.Results {
		newResults[i] = &parser.Parameter{
			Token: r.Token,
			Name:  r.Name,
			Type:  substituteType(r.Type, subst),
		}
	}
	// 複製並替換函數體
	newBody := substituteBody(fd.Body, subst)
	nf := &parser.FunctionDefinition{
		Token: fd.Token,
		Name:  mangledName,
		FuncSignature: parser.FuncSignature{
			GenericParams: nil, // 具體化後無泛型參數
			Parameters:    newParams,
			Results:       newResults,
		},
		Body:         newBody,
		IsMethodDef:  fd.IsMethodDef,
		OverflowMode: fd.OverflowMode,
	}
	// 傳承模板函式的來源檔 / 模組歸屬：單態化產生的是新節點，若不複製會讓
	// SourceFile 變成空值，導致 (a) lint 診斷無法歸因到正確源檔、(b) 標準庫
	// 泛型（如 vec/map）的實例被誤判為主程式碼而觸發使用者向檢查（如未處理
	// 溢出 option 的 ovfhndld）。
	program.Sem.SetSourceFileOf(nf, program.Sem.SourceFileOf(fd))
	program.Sem.SetModuleOwnerOf(nf, program.Sem.ModuleOwnerOf(fd))
	return nf
}

// substituteBody 遞迴替換函數體中的泛型參數
func substituteBody(body *parser.BlockStatement, subst map[string]string) *parser.BlockStatement {
	if body == nil || len(subst) == 0 {
		return body
	}
	newStmts := make([]parser.Statement, len(body.Statements))
	for i, stmt := range body.Statements {
		newStmts[i] = substituteStmt(stmt, subst)
	}
	return &parser.BlockStatement{
		Token:      body.Token,
		Statements: newStmts,
	}
}

func substituteStmt(stmt parser.Statement, subst map[string]string) parser.Statement {
	switch s := stmt.(type) {
	case *parser.ExpressionStatement:
		return &parser.ExpressionStatement{
			Token:        s.Token,
			Expression:   substituteExpr(s.Expression, subst),
			OverflowMode: s.OverflowMode,
		}
	case *parser.LetStatement:
		return &parser.LetStatement{
			Token:        s.Token,
			Name:         s.Name,
			Value:        substituteExpr(s.Value, subst),
			Type:         substituteType(s.Type, subst),
			IsSynthetic:  s.IsSynthetic,
			OverflowMode: s.OverflowMode,
		}
	case *parser.ForStatement:
		newFor := &parser.ForStatement{
			Token:         s.Token,
			Body:          substituteBody(s.Body, subst),
			Label:         s.Label,
			IsCondWrapper: s.IsCondWrapper,
			OverflowMode:  s.OverflowMode,
		}
		if s.IterRange != nil {
			newFor.IterRange = &parser.IterationExpr{
				Variable:  s.IterRange.Variable,
				Range:     substituteRange(s.IterRange.Range, subst),
				RangeStr:  s.IterRange.RangeStr,
				RangeExpr: s.IterRange.RangeExpr,
			}
			// Also copy RangeExpr (identifier/slice) - it may contain generic types too
			if ident, ok := s.IterRange.RangeExpr.(*parser.Identifier); ok {
				if val, ok2 := subst[ident.Value]; ok2 {
					newFor.IterRange.RangeExpr = &parser.Identifier{Token: ident.Token, Value: val}
				}
			}
		}
		// 也替換 for i < n 條件中的 n
		if s.Condition != nil {
			newFor.Condition = substituteExpr(s.Condition, subst)
		}
		if s.CountExpr != nil {
			newFor.CountExpr = substituteExpr(s.CountExpr, subst)
		}
		return newFor
	case *parser.BlockStatement:
		return substituteBody(s, subst)
	case *parser.ReturnStatement:
		return &parser.ReturnStatement{
			Token:        s.Token,
			ReturnValue:  substituteExpr(s.ReturnValue, subst),
			OverflowMode: s.OverflowMode,
		}
	case *parser.MultiAssignStatement:
		newMulti := &parser.MultiAssignStatement{
			Token:        s.Token,
			OverflowMode: s.OverflowMode,
		}
		for _, tgt := range s.Targets {
			newMulti.Targets = append(newMulti.Targets, substituteExpr(tgt, subst))
		}
		newMulti.Value = substituteExpr(s.Value, subst)
		return newMulti
	default:
		return stmt
	}
}

func substituteExpr(expr parser.Expression, subst map[string]string) parser.Expression {
	if expr == nil {
		return nil
	}
	switch e := expr.(type) {
	case *parser.Identifier:
		if val, ok := subst[e.Value]; ok {
			// 整數替換值（如陣列大小泛型 [n]t）：轉為 IntegerLiteral
			if intVal, err := strconv.ParseInt(val, 10, 64); err == nil {
				return &parser.IntegerLiteral{
					Token: e.Token,
					Value: intVal,
				}
			}
			// 型別參數替換（值為簡單型別名稱如 "i64"、"str"、"bool"）：
			// 在表達式語境中跳過。型別參數（如 `v`、`k`）在方法體內作為
			// 變數名使用，不應被替換為型別名稱（否則 `v = .vals[i]` 會變成
			// `i64 = .vals[i]`，產生 `%i64` 未定義錯誤）。型別參數僅在型別
			// 語境中替換（由 substituteType 處理）。
			// 結構/方法名稱替換（值含連字號或點）仍正常套用。
			if !strings.Contains(val, "-") && !strings.Contains(val, ".") {
				return e
			}
			// 非整數替換值（如方法名重寫 hashmap-*-tmpl.hash → hashmap-K-V.hash）：保留為 Identifier
			return &parser.Identifier{
				Token: e.Token,
				Value: val,
			}
		}
		return e
	case *parser.IntegerLiteral:
		return e
	case *parser.InfixExpression:
		return &parser.InfixExpression{
			Token:    e.Token,
			Left:     substituteExpr(e.Left, subst),
			Operator: e.Operator,
			Right:    substituteExpr(e.Right, subst),
		}
	case *parser.PrefixExpression:
		return &parser.PrefixExpression{
			Token:    e.Token,
			Operator: e.Operator,
			Right:    substituteExpr(e.Right, subst),
		}
	case *parser.CallExpression:
		newCe := &parser.CallExpression{
			Token:     e.Token,
			Function:  substituteExpr(e.Function, subst),
			Arguments: make([]parser.Expression, len(e.Arguments)),
		}
		for i, arg := range e.Arguments {
			newCe.Arguments[i] = substituteExpr(arg, subst)
		}
		return newCe
	case *parser.IndexExpression:
		return &parser.IndexExpression{
			Token: e.Token,
			Left:  substituteExpr(e.Left, subst),
			Index: substituteExpr(e.Index, subst),
		}
	case *parser.GroupedExpression:
		return &parser.GroupedExpression{
			Token:      e.Token,
			Expression: substituteExpr(e.Expression, subst),
		}
	case *parser.SurfaceMatch:
		// S1：糖形態 match。重建 Matched + 每臂 Condition / MultiValuePatterns / Body，
		// 其餘 MatchArm 欄位隨值拷貝保留。對應 IfExpression 分支的 Condition /
		// MatchedExpr / ValuePatterns / Consequence / Alternative 替換。
		newSm := &parser.SurfaceMatch{
			Token:   e.Token,
			Matched: substituteExpr(e.Matched, subst),
		}
		if len(e.Arms) > 0 {
			newSm.Arms = make([]parser.MatchArm, len(e.Arms))
			for i := range e.Arms {
				arm := e.Arms[i]
				if arm.Condition != nil {
					arm.Condition = substituteExpr(arm.Condition, subst)
				}
				if len(arm.MultiValuePatterns) > 0 && len(subst) > 0 {
					arm.MultiValuePatterns = make([]parser.Expression, len(arm.MultiValuePatterns))
					for j, vp := range arm.MultiValuePatterns {
						arm.MultiValuePatterns[j] = substituteExpr(vp, subst)
					}
				}
				if arm.Body != nil {
					arm.Body = substituteBody(arm.Body, subst)
				}
				newSm.Arms[i] = arm
			}
		}
		return newSm
	case *parser.IfExpression:
		// S1：主編譯管線不再降糖 match，此分支在 monomorph 階段只代表三元
		// if（a ? b : c）。matched 表達式的泛型替換已由上方專屬的
		// *parser.SurfaceMatch 分支經 e.Matched 處理；此處 MatchedExpr /
		// RangePattern / EqualityPattern / OptionPatterns / ValuePatterns /
		// RawCond 恆為零值（降糖形態僅 JS/WASM 後端使用，且它們不跑 monomorph），
		// 故不複製這些死欄位。與 scanIfExpressionForGenericCalls 的「只看
		// Condition / Consequence / Alternative」口徑一致。
		newIf := &parser.IfExpression{
			Token:     e.Token,
			Condition: substituteExpr(e.Condition, subst),
		}
		if e.Consequence != nil {
			newIf.Consequence = substituteBody(e.Consequence, subst)
		}
		if e.Alternative != nil {
			newIf.Alternative = substituteBody(e.Alternative, subst)
		}
		return newIf
	case *parser.AssignExpression:
		return &parser.AssignExpression{
			Token: e.Token,
			Left:  substituteExpr(e.Left, subst),
			Value: substituteExpr(e.Value, subst),
		}
	case *parser.DotExpression:
		return &parser.DotExpression{
			Token:    e.Token,
			Receiver: substituteExpr(e.Receiver, subst),
			Property: e.Property,
		}
	case *parser.ConditionalExpression:
		return &parser.ConditionalExpression{
			Token:       e.Token,
			Condition:   substituteExpr(e.Condition, subst),
			Consequence: substituteExpr(e.Consequence, subst),
			Alternative: substituteExpr(e.Alternative, subst),
		}
	case *parser.SliceExpression:
		return &parser.SliceExpression{
			Token: e.Token,
			Left:  substituteExpr(e.Left, subst),
			Range: substituteRange(e.Range, subst),
		}
	default:
		return e
	}
}

func substituteRange(r *parser.RangeExpression, subst map[string]string) *parser.RangeExpression {
	if r == nil {
		return nil
	}
	return &parser.RangeExpression{
		Token:    r.Token,
		LeftInc:  r.LeftInc,
		RightInc: r.RightInc,
		Start:    substituteExpr(r.Start, subst),
		End:      substituteExpr(r.End, subst),
	}
}

// substituteType 替換類型中的泛型參數
// 遞迴處理所有 Type 節點
func substituteType(t parser.Type, subst map[string]string) parser.Type {
	if len(subst) == 0 || t == nil {
		return t
	}
	switch typ := t.(type) {
	case *parser.NamedType:
		if val, ok := subst[typ.Value]; ok {
			return &parser.NamedType{Token: typ.Token, Value: val, IsInferred: typ.IsInferred}
		}
		return typ
	case *parser.ArrayType:
		newSize := typ.Size
		if ident, ok := typ.Size.(*parser.Identifier); ok {
			if val, ok := subst[ident.Value]; ok {
				if intVal, err := strconv.ParseInt(val, 10, 64); err == nil {
					newSize = &parser.IntegerLiteral{Token: ident.Token, Value: intVal}
				}
			}
		}
		newElem := substituteType(typ.Elem, subst)
		return &parser.ArrayType{Token: typ.Token, Size: newSize, Elem: newElem, IsInferred: typ.IsInferred}
	case *parser.SliceType:
		newElem := substituteType(typ.Elem, subst)
		return &parser.SliceType{Token: typ.Token, Elem: newElem, IsInferred: typ.IsInferred}
	case *parser.NullableType:
		newInner := substituteType(typ.Type, subst)
		return &parser.NullableType{Token: typ.Token, Type: newInner, IsInferred: typ.IsInferred}
	case *parser.ViewType:
		newInner := substituteType(typ.Type, subst)
		return &parser.ViewType{Token: typ.Token, Type: newInner}
	case *parser.PointerType:
		newInner := substituteType(typ.Type, subst)
		return &parser.PointerType{Token: typ.Token, Type: newInner}
	case *parser.FunctionType:
		// Function types are not subject to generic substitution in Phase 1.
		return t
	default:
		return t
	}
}

// collectVarTypesFromBody recursively collects variable types from a function body
func collectVarTypesFromBody(body *parser.BlockStatement, varTypes map[string]string, sem *parser.SemanticContext) {
	if body == nil {
		return
	}
	// declared tracks the names bound WITHIN this body (as opposed to names
	// inherited from an enclosing/global scope), so the "uninferable value"
	// branch below can tell a REASSIGNMENT of a local apart from a fresh
	// binding that merely shadows a global of the same name.
	declared := make(map[string]bool)
	collectVarTypesFromBodyIn(body, varTypes, declared, sem)
}

func collectVarTypesFromBodyIn(body *parser.BlockStatement, varTypes map[string]string, declared map[string]bool, sem *parser.SemanticContext) {
	if body == nil {
		return
	}
	for _, stmt := range body.Statements {
		if ls, ok := stmt.(*parser.LetStatement); ok {
			if ov := sem.TypeOverrideOf(ls); ov != "" {
				// S5 stage 5：裝配期有效型別覆蓋（具現化/具象化）優先於宣告型別。
				varTypes[ls.Name.Value] = ov
				declared[ls.Name.Value] = true
			} else if ls.Type != nil {
				varTypes[ls.Name.Value] = ls.Type.String()
				declared[ls.Name.Value] = true
			} else if ls.Value != nil {
				if t := inferTypeFromExpr(ls.Value); t != "" {
					varTypes[ls.Name.Value] = t
					declared[ls.Name.Value] = true
				} else if !declared[ls.Name.Value] {
					// Can't infer type (e.g., method call result) — delete any
					// stale entry inherited from another scope (e.g., a global
					// variable with the same name) to prevent wrong method resolution.
					//
					// A name already bound EARLIER IN THIS BODY is a reassignment
					// (`b [3]i64 = [10,20,30]` ... `b = b.clone()`), not a stale
					// global: deleting it here dropped the declared type, so
					// resolveMethodCall could not type the receiver `b` and
					// never monomorphized `[n]t.clone` — the call stayed an
					// unresolved `@b.clone` in BOTH backends. Keep the declared
					// type; the reassignment cannot change it.
					delete(varTypes, ls.Name.Value)
				}
			}
		}
		if bs, ok := stmt.(*parser.BlockStatement); ok {
			collectItScoped(varTypes, func() {
				collectVarTypesFromBodyIn(bs, varTypes, declared, sem)
			})
		}
		if fs, ok := stmt.(*parser.ForStatement); ok {
			if fs.Body != nil {
				collectItScoped(varTypes, func() {
					collectVarTypesFromBodyIn(fs.Body, varTypes, declared, sem)
				})
			}
		}
		// ExpressionStatement may wrap an IfExpression (e.g. `cond -> { body }`
		// match arms desugared to if/else). Recurse into its consequence and
		// alternative blocks so that local LetStatements inside match arms
		// shadow globals / outer-locals correctly during method resolution.
		if es, ok := stmt.(*parser.ExpressionStatement); ok {
			collectItScoped(varTypes, func() {
				collectVarTypesFromExpr(es.Expression, varTypes, sem)
			})
		}
	}
}

// collectVarTypesFromExpr recurses into expression nodes that contain
// BlockStatement bodies (IfExpression, etc.) and collects local variable types.
func collectVarTypesFromExpr(expr parser.Expression, varTypes map[string]string, sem *parser.SemanticContext) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case *parser.SurfaceMatch:
		// S1：糖形態 match。每個臂是獨立的 `it` 作用域（見 IfExpression 分支說明），
		// 只收臂體內聲明的區域變數型別，不收 Condition / MultiValuePatterns。
		//
		// 每臂用**獨立拷貝**收集，臂內 let 的推斷型別不得洩漏到 match 之後：
		// 降糖形態下臂體是 if 分支，收集順序 Consequence→Alternative→DotValBody，
		// 而 option match 的 ok 臂落在 DotValBody（最後處理）——其 `let p = it`
		// 不可推斷 ⇒ 把 p 從表裡刪掉，等效於「match 之後沒有 p」。
		// 糖形態若共享表，最後一個臂的推斷型別會留下來並蓋過 HIR 的更精確型別。
		// 實測回歸（A/B vs 基線）：tests/match-index-option.no 的
		//   parts[i]: { ok -> { p = it }  -> { p = '' } }
		//   print(p.to-str())
		// 工作樹把 p 記成 str（預設臂的字面值推斷）⇒ resolveMethodCall 攤成
		// 不存在的 @str_to_str；基線表裡沒有 p ⇒ 交給 MIR 用 HIR 型別 txt
		// 攤成 @txt_to_str，正確。共用表還會讓值 match 的臂內宣告洩漏
		//（`1 -> { q = 'a' }  -> { q = 'b' }` 之後 q:str——降糖形態同樣洩漏，
		//  兩版同壞），拷貝後統一交給 HIR 裁決。
		for i := range e.Arms {
			arm := &e.Arms[i]
			if arm.Body != nil {
				armVT := make(map[string]string, len(varTypes))
				for k, v := range varTypes {
					armVT[k] = v
				}
				collectItScoped(armVT, func() { collectVarTypesFromBody(arm.Body, armVT, sem) })
			}
		}
	case *parser.IfExpression:
		// S1：主管線不再降糖 match，此分支只代表三元 if（a ? b : c），其
		// Consequence / Alternative 各自是獨立的 `it` 作用域（見 SurfaceMatch
		// 分支說明）。DotValBody 是降糖 option match 的尾部臂載體；主管線
		// IfExpression 分支不含 match（match 由上方 SurfaceMatch 分支處理），
		// 故不處理——即便走到也只會是 collectVarTypesFromBody(nil) 的 no-op。
		collectItScoped(varTypes, func() { collectVarTypesFromBody(e.Consequence, varTypes, sem) })
		collectItScoped(varTypes, func() { collectVarTypesFromBody(e.Alternative, varTypes, sem) })
	case *parser.GroupedExpression:
		collectVarTypesFromExpr(e.Expression, varTypes, sem)
	case *parser.ConditionalExpression:
		collectItScoped(varTypes, func() { collectVarTypesFromExpr(e.Consequence, varTypes, sem) })
		collectItScoped(varTypes, func() { collectVarTypesFromExpr(e.Alternative, varTypes, sem) })
	}
}

// collectItScoped runs fn with `it` block-scoped, restoring the enclosing
// binding afterwards.
//
// `it` is a single function-global name: every match arm prepends a synthetic
// `let it = <subject>`, so a flat collection pass lets the LAST arm (including
// arms of a match NESTED inside an outer arm) win. Method calls are resolved
// against this table during parse/lower, so the outer arm's `it.show()` was
// resolved as `bool.show` when a nested `w ?bool` match followed it:
//
//	t ?thing = mk()
//	t: { -> {
//	        s = it.show()      ; resolved as bool.show -> "unknown callee"
//	        w ?bool = it.get()
//	        w: { -> ... }      ; nested arm rebinds it = bool
//	    } }
//
// Restoring around each nested body keeps `it` = the nearest enclosing arm's
// subject, mirroring the per-level restore the MIR lowering does for the
// runtime value (hir2mir lowerIf).
func collectItScoped(varTypes map[string]string, fn func()) {
	saved, had := varTypes["it"]
	fn()
	if had {
		varTypes["it"] = saved
	} else {
		delete(varTypes, "it")
	}
}

// makeIdent 建立 Identifier AST 節點
func makeIdent(name string) *parser.Identifier {
	return &parser.Identifier{
		Token: lexer.Token{Type: lexer.IDENT, Literal: name},
		Value: name,
	}
}

// makeMethodCall 建立 varName.methodName() 的 ExpressionStatement
func makeMethodCall(varName, method string) *parser.ExpressionStatement {
	return &parser.ExpressionStatement{
		Token: lexer.Token{Type: lexer.IDENT, Literal: varName},
		Expression: &parser.CallExpression{
			Token: lexer.Token{Type: lexer.LPAREN, Literal: "("},
			Function: &parser.DotExpression{
				Token:    lexer.Token{Type: lexer.DOT, Literal: "."},
				Receiver: makeIdent(varName),
				Property: method,
			},
			Arguments: []parser.Expression{},
		},
	}
}

// unionTemplateInfo 記錄聯合型別模板函數的資訊
type unionTemplateInfo struct {
	origName  string
	unionName string
	members   []string
	// unionArgIdx 是「實參向量中承載 union 形參的那一位」的下標。自由泛型函數
	// `f(x num)` → 0。方法式泛型 `Recv.m(self, b big)` 被 resolveMethodCall 攤平成
	// `Recv.m(recv, b)` 後，接收者佔 arguments[0]，union 形參右移到 (1 + 宣告位)。
	// 舊碼硬編 arguments[0]，對方法式呼叫會誤用「接收者型別」推 member，導致用戶碼
	// `a + 1`（接收者 bigint、實參 i64）被判成 bigint、又因 bigint≠成員 `bigint.bigint`
	// 而匹配失敗、呼叫保留成未實例化的 `bigint.bigint._add` → unknown callee。
	unionArgIdx int
	// fallbackMember 是「實參型別無法靜態推定」時的默認成員。運算符重載的聯合
	// （`big = bigint | i64`）在 std 內部幾乎全是 bigint×bigint（如 number.no
	// f64.to-str 的 `base._mul(pow10)`），而實参常是大整數工廠呼叫／運算符結果等
	// 無法由 `inferTypeFromExpr` 語法推定的區域變數——舊邏輯一律默認 i64，會把
	// bigint 實参誤綁到 `__i64` 複本，進而不兼容型別（aggregate 傳给 scalar 参數）
	// 編譯失敗。此處記錄 union 中唯一的非內建標量成員（聚合體，如 bigint）；推定
	// 失敗時回退到它。**僅對含聚合體成員的 union 生效**：純標量 union（num/int/
	// float）無此成員，fallbackMember 為空，推定失敗仍走 i64 默認，行為逐位元組不變。
	fallbackMember string
}

// unionScalarMemberTail 判定 union 成員末段是否為內建標量型別（可安全傳入標量参數）。
// 與 checker.isBuiltinType 對齊並補上 `char`（i32 表示，仍是標量）。聚合體成員
// （struct／str／容器等）返回 false，以被選為 fallbackMember。
func unionScalarMemberTail(tail string) bool {
	switch tail {
	case "i8", "i16", "i32", "i64", "i128",
		"u8", "u16", "u32", "u64", "u128",
		"f32", "f64",
		"byte", "bool", "char", "str", "txt":
		return true
	}
	return false
}

// runMonomorphizationLoop 單態化迭代收斂迴圈（頂層語句加入 merged 之後、
// 第二階段解析之前）。迴圈體依序：
//
//  1. monomorphizeGenerics：泛型函數具現化（使用 globalVarTypes——僅頂層
//     變數——避免其他函數的局部變數型別洩漏到 method resolution；傳入
//     mainLocalOwner 以便 resolveMethodCall 為跨模組型別補上模組前綴）。
//     迴圈頭部先跑 monomorphizeSliceStructs（切片欄位泛型結構體單態化，
//     必須在 monomorphizeGenerics 之前執行）。
//  2. 過濾：移除尚未具現化的泛型函數定義（只有具體版本才能產生 LLVM IR）。
//  3. 掃描合併後程式，找出泛型具現化新引入的 std 模組引用。
//  4. 若有新引用：決定性載入順序（tier 0 先於 tier 1，map 迭代順序不確定）
//     批次合併模組體，僅對「新合併」的陳述句重跑前綴 / 型別參考重寫。
//  5. 重新過濾已具現化的泛型定義，直到不再出現新的 std 模組引用（收斂）。
//
// 泛型具現化待處理佇列（HIR 側收斂）：monomorphizeGenerics 動態新增的具體
// 函數體可能帶入先前未載入的 std 模組依賴（例如泛型 T 實例化為 map<K,V>
// 後，參數型別註解暴露 collection/map 依賴；或實例化後引用 vec/str 內部函數）。
// 這類依賴不交給 MIR 處理（mir.LowerHIR 是純一次性 lowering，不回碰 HIR），
// 而是在 HIR（build）側維護待處理佇列：反覆掃描合併後的 merged、批次合併新
// 引用的 std 模組體、重新執行必要的前綴/解析/單態化步驟，直到不再出現新的
// std 模組引用（迭代收斂）。
func (a *compileAssembly) runMonomorphizationLoop() error {
	t := a.t
	// 切片欄位泛型結構體單態化：由工廠呼叫 `Type.init(concreteBuffer)` 推斷元素
	// 型別，生成具現化結構體 + 方法，標註工廠結果變數型別，並移除模板。
	// 必須在 monomorphizeGenerics 之前執行：實例方法呼叫 `h.push` 需保持為接收者
	// 為變數的 DotExpression，交由 resolveMethodCall 依 varTypes[h]=具現化名自然攤平。
	monomorphizeSliceStructs(a.merged, a.globalVarTypes)
	checker.DebugCountHashFns("after-monomorphizeSliceStructs", a.merged)
	for {
		monomorphizeGenerics(a.merged, a.globalVarTypes, a.mainLocalOwner)
		checker.DebugCountHashFns("after-monomorphizeGenerics", a.merged)
		// 過濾：移除尚未具現化的泛型函數定義（只有具體版本才能產生 LLVM IR）
		filtered := make([]parser.Statement, 0, len(a.merged.Statements))
		for _, stmt := range a.merged.Statements {
			if fd, ok := stmt.(*parser.FunctionDefinition); ok {
				if len(fd.GenericParams) > 0 {
					continue // 跳過泛型函數（GenericParams 未被清空說明尚未具現化）
				}
			}
			filtered = append(filtered, stmt)
		}
		a.merged.Statements = filtered
		checker.DebugCountHashFns("after-filter-generic", a.merged)
		// 掃描合併後程式，找出泛型實例化新引入的 std 模組引用
		pendingRefs := t.collectReferencedStdModules(a.merged)
		if t.programUsesPrint(a.merged) || t.programUsesPrint(a.program) || pendingRefs["io"] {
			for _, name := range []string{"fmt", "io", "str", "byte"} {
				if _, ok := stdModuleLookup()[name]; ok {
					pendingRefs[name] = true
				}
			}
		}
		var newRefs []string
		for ref := range pendingRefs {
			if !a.loadedStd[ref] {
				newRefs = append(newRefs, ref)
			}
		}
		if len(newRefs) == 0 {
			break
		}
		// 決定性載入順序（與主 worklist 一致）：輕量型別模組先於重型模組，
		// 避免泛型實例化新引入的依賴因 map 迭代順序不確定而打亂 str 等型別
		// 模組的就位次序，導致 legacy codegen 簽名解析錯誤。
		sort.Slice(newRefs, func(i, j int) bool {
			pi, pj := stdLoadTier(newRefs[i]), stdLoadTier(newRefs[j])
			if pi != pj {
				return pi < pj
			}
			return newRefs[i] < newRefs[j]
		})
		// 批次合併新引用的 std 模組體（含其傳遞閉包）
		before := len(a.merged.Statements)
		for _, ref := range newRefs {
			a.loadedStd[ref] = true
			if _, err := t.loadStdModuleBody(ref, a.merged, a.typeOwner, a.mainVarNames, a.mergedGlobalConsts, a.globalVarTypes, a.loadedStd, a.explicitStdModules); err != nil {
				return err
			}
		}
		newStmts := a.merged.Statements[before:]
		// 僅對「新合併」的陳述句重跑前綴/型別參考重寫 pass。對整個 merged 程式
		// 重跑 prefixMethodNames 並非安全冪等：已處理的函數（如 process.exec 回傳
		// exec-result 的 str 欄位）會被錯誤重命名/重寫，導致 str 回傳的 sret
		// 暫存器型別被誤判為 i64（test-tcp-fork.no 回歸）。只處理新增語句可避免
		// 污染既有程式，同時仍讓泛型具現化新引入的 std 模組函數正確接入。
		if len(a.typeOwner) > 0 && len(newStmts) > 0 {
			localMainSet := make(map[parser.Statement]bool, len(a.program.Statements))
			for _, mainStmt := range a.program.Statements {
				localMainSet[mainStmt] = true
			}
			for _, ns := range newStmts {
				if localMainSet[ns] {
					rewriteTypeRefsInStmt(ns, a.mainLocalOwner)
				} else {
					rewriteTypeRefsInStmt(ns, a.typeOwner)
				}
			}
			prefixMethodNames(newStmts, a.typeOwner, a.mainLocalTypes)
		}
		// 注意：下面的整程式（whole-merged）解析 pass（prefixCollidingFunctions /
		// ResolveModuleConstants / ResolveModuleCalls / ResolveSelfMethodCalls /
		// rewriteTypeRefs / ResolveMethodCalls）刻意「不」在迴圈內每輪重跑。
		// 對整個 merged 程式重跑這些 pass 並非安全冪等：已處理的函數會被錯誤
		// 重命名 / 重寫（如 process.exec 回傳 exec-result 的 str 欄位 sret 暫存器
		// 被誤判為 i64，見 test-tcp-fork.no；以及 static-hashmap.put 的 insert-idx
		// 局部變數被誤併入 first-tomb，見 test-map.no）。正確做法是：迴圈內只對
		// 「新合併」的陳述句做前綴 / 型別參考重寫（上方 newStmts 區塊），待待處理
		// 佇列收斂、所有 std 模組就位後，由第二階段（rewriteTypeRefs /
		// ResolveModuleCalls / ResolveMethodCalls，見 secondStageResolution）在
		// 「完整程式」上統一解析一次。
		// 重新過濾已具現化的泛型定義，避免下一輪 monomorphizeGenerics 重複處理
		filtered2 := make([]parser.Statement, 0, len(a.merged.Statements))
		for _, stmt := range a.merged.Statements {
			if fd, ok := stmt.(*parser.FunctionDefinition); ok {
				if len(fd.GenericParams) > 0 {
					continue
				}
			}
			filtered2 = append(filtered2, stmt)
		}
		a.merged.Statements = filtered2
	}
	return nil
}

// finalizeMonomorphization 收斂後的最終單態化 + 名稱修飾（第二階段解析之前）。
// 依序：
//
//  1. monomorphizeGenericStructs：泛型結構體單態化（掃描 map[K]V 使用點，
//     自 hashmap-*-tmpl 模板生成具體結構與方法）。必須在 monomorphizeGenerics
//     之後（避免與 [n]t 泛型衝突）、monomorphizeUnions 之前執行。
//  2. monomorphizeUnions：聯合型別單態化——對帶 ..T（T 為 union alias）的函數，
//     為 union 的每個具體型別生成一個函數版本（命名 "<原名>__<成員型別>"）。
//  3. rewriteUnionCalls：將對聯合型別泛型函數的呼叫改為具體版本（max → max__i64）。
//  4. mangleOverloads(merged, nil)：合併所有 std 模組後再次名稱修飾，處理跨模組
//     的重載衝突（如 bigint.div-mod vs number.div-mod）。
func (a *compileAssembly) finalizeMonomorphization() {
	// 泛型結構體單態化：掃描 map[K]V 使用點，自 hashmap-*-tmpl 模板生成具體結構與方法。
	// 必須在 monomorphizeGenerics 之後（避免與 [n]t 泛型衝突）、monomorphizeUnions 之前執行。
	monomorphizeGenericStructs(a.merged)
	checker.DebugCountHashFns("after-monomorphizeGenericStructs", a.merged)
	// 聯合型別單態化：對帶 ..T（T 為 union alias）的函數，
	// 為 union 的每個具體型別生成一個函數版本。生成函數的命名
	// 採用 "<原名>__<成員型別>" 的形式；對函數體內對自己的呼叫也
	// 一併替換。
	monomorphizeUnions(a.merged)
	checker.DebugCountHashFns("after-monomorphizeUnions", a.merged)
	// 重寫對聯合型別泛型函數的呼叫：將 max(args) 改為 max__i64(args)
	rewriteUnionCalls(a.merged, a.varTypes)
	checker.DebugCountHashFns("after-rewriteUnionCalls", a.merged)
	// 在合併所有 std 模組後再做一次名稱修飾，
	// 處理跨模組的重載衝突（如 bigint.div-mod vs number.div-mod）
	mangleOverloads(a.merged, nil)
	checker.DebugCountHashFns("after-mangleOverloads", a.merged)
}
