package build

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/lizongying/nolang/checker"
	"github.com/lizongying/nolang/parser"
)

// findTypeForVar 從區塊語句中查找變數的類型（簡化版）
func findTypeForVar(varName string, block *parser.BlockStatement, lifecycleTypes map[string]bool) string {
	for _, stmt := range block.Statements {
		if ls, ok := stmt.(*parser.LetStatement); ok && ls.Name.Value == varName {
			if ls.Type != nil {
				return ls.Type.String()
			}
		}
	}
	// 默認返回空
	for t := range lifecycleTypes {
		return t
	}
	return ""
}

// buildVarTypes 從程式的頂層陳述句中收集變數型別映射。
// 用於在模組合併後重新建構 varTypes，使 validateArrayBounds 等檢查能應用於 merged 程式。
func buildVarTypes(program *parser.Program) map[string]string {
	varTypes := make(map[string]string)
	for _, stmt := range program.Statements {
		if ls, ok := stmt.(*parser.LetStatement); ok {
			if ov := program.Sem.TypeOverrideOf(ls); ov != "" {
				// S5 stage 5：裝配期有效型別覆蓋（具現化/具象化）優先於宣告型別——
				// 舊版就地寫 ls.Type 使這裡讀到的就是覆寫後的值，搬進 Sem 後必須
				// 显式解析，否則 devirtualizeInterfaceVars 的結果會被此重建覆蓋回介面名。
				varTypes[ls.Name.Value] = ov
			} else if ls.Type != nil {
				varTypes[ls.Name.Value] = ls.Type.String()
			} else if ls.Value != nil {
				if t := inferTypeFromExpr(ls.Value); t != "" {
					varTypes[ls.Name.Value] = t
				}
			}
		}
		if fd, ok := stmt.(*parser.FunctionDefinition); ok {
			for _, p := range fd.Parameters {
				if p.Type != nil {
					varTypes[p.Name] = p.Type.String()
				}
			}
			for _, r := range fd.Results {
				if r.Type != nil {
					varTypes[r.Name] = r.Type.String()
				}
			}
			collectVarTypesFromBody(fd.Body, varTypes, program.Sem)
		}
	}
	return varTypes
}

// buildSizeMaps 單次遍歷 AST 同時收集陣列、切片、字串的大小映射。
// 替代原本獨立呼叫 buildArraySizeMap + buildSliceSizeMap + buildStringSizeMap 的 3 次遍歷。
func buildSizeMaps(program *parser.Program) *sizeMaps {
	sm := &sizeMaps{
		arraySizes:     make(map[string]int64),
		sliceSizes:     make(map[string]int64),
		stringSizes:    make(map[string]int64),
		funcStrReturns: make(map[string]bool),
	}
	// Pre-scan: collect names of user-defined functions whose single result
	// type is str. This allows isStringExprForCollect to recognize calls to
	// str-returning functions (e.g., s = get-str() where get-str returns str)
	// so that the variable is tracked in stringSizes. Without this, s - 'x'
	// is misidentified as byte arithmetic instead of string concatenation.
	for _, stmt := range program.Statements {
		switch s := stmt.(type) {
		case *parser.FunctionDefinition:
			if len(s.Results) == 1 && s.Results[0].Type != nil && s.Results[0].Type.String() == "str" {
				sm.funcStrReturns[s.Name] = true
			}
		case *parser.LetStatement:
			// Method definitions: str.my-method = (self str) (out str) { ... }
			if fl, ok := s.Value.(*parser.FunctionLiteral); ok {
				if s.Name != nil && len(fl.Results) == 1 && fl.Results[0].Type != nil && fl.Results[0].Type.String() == "str" {
					sm.funcStrReturns[s.Name.Value] = true
				}
			}
		}
	}
	for _, stmt := range program.Statements {
		collectSizesFromStmt(stmt, sm)
	}
	return sm
}

// collectSizesFromStmt 遞迴遍歷 AST 節點，同時收集三種大小映射。
// 合併自 collectArraySizesFromStmt / collectSliceSizeMapFromStmt / collectStringSizeMapFromStmt。
func collectSizesFromStmt(stmt parser.Statement, sm *sizeMaps) {
	switch s := stmt.(type) {
	case *parser.LetStatement:
		// 陣列大小收集
		if at, ok := s.Type.(*parser.ArrayType); ok {
			var arraySize int64
			if at.Size != nil {
				if intLit, ok := at.Size.(*parser.IntegerLiteral); ok {
					arraySize = intLit.Value
				}
			} else if arrLit, ok := s.Value.(*parser.ArrayLiteral); ok {
				if intLit, ok := arrLit.Size.(*parser.IntegerLiteral); ok && intLit.Value > 0 {
					arraySize = intLit.Value
				}
			}
			if arraySize > 0 {
				sm.arraySizes[s.Name.Value] = arraySize
			}
		}
		// 切片大小收集
		if _, ok := s.Type.(*parser.SliceType); ok {
			if sl, ok := s.Value.(*parser.SliceLiteral); ok {
				sm.sliceSizes[s.Name.Value] = int64(len(sl.Elements))
			} else {
				sm.sliceSizes[s.Name.Value] = 0 // unknown size
			}
		} else if sl, ok := s.Value.(*parser.SliceLiteral); ok {
			// Also detect slice from SliceLiteral value (inferred type, no [] annotation)
			sm.sliceSizes[s.Name.Value] = int64(len(sl.Elements))
		}
		// 字串大小收集
		if s.Type != nil && (s.Type.String() == "str") {
			if sl, ok := s.Value.(*parser.StringLiteral); ok {
				sm.stringSizes[s.Name.Value] = int64(len(sl.Value))
			} else {
				sm.stringSizes[s.Name.Value] = 0 // unknown size, mark as string but no bound check
			}
		} else if isStringExprForCollect(s.Value, sm.stringSizes, sm.funcStrReturns) {
			// Also detect string from inferred expression (StringLiteral, string concatenation,
			// string method calls like slice/repeat, char-to-str, copy from known string var,
			// or calls to user-defined functions returning str)
			if sl, ok := s.Value.(*parser.StringLiteral); ok {
				sm.stringSizes[s.Name.Value] = int64(len(sl.Value))
			} else {
				sm.stringSizes[s.Name.Value] = 0 // unknown size, mark as string but no bound check
			}
		}
	case *parser.FunctionDefinition:
		// 字串參數與返回值收集（僅 buildStringSizeMap 有此邏輯）
		for _, p := range s.Parameters {
			if p.Type != nil && (p.Type.String() == "str") {
				sm.stringSizes[p.Name] = 0
			}
		}
		for _, p := range s.Results {
			if p.Type != nil && (p.Type.String() == "str") {
				sm.stringSizes[p.Name] = 0
			}
		}
		if s.Body != nil {
			for _, ss := range s.Body.Statements {
				collectSizesFromStmt(ss, sm)
			}
		}
	case *parser.ExpressionStatement:
		// if/else 表達式中的局部变量也需收集
		if ifExpr, ok := s.Expression.(*parser.IfExpression); ok {
			if ifExpr.Consequence != nil {
				for _, ss := range ifExpr.Consequence.Statements {
					collectSizesFromStmt(ss, sm)
				}
			}
			if ifExpr.Alternative != nil {
				for _, ss := range ifExpr.Alternative.Statements {
					collectSizesFromStmt(ss, sm)
				}
			}
		} else if smNode, ok := s.Expression.(*parser.SurfaceMatch); ok {
			for _, arm := range smNode.Arms {
				if arm.Body != nil {
					for _, ss := range arm.Body.Statements {
						collectSizesFromStmt(ss, sm)
					}
				}
			}
		}
	case *parser.ForStatement:
		if s.Init != nil {
			collectSizesFromStmt(s.Init, sm)
		}
		if s.Body != nil {
			for _, ss := range s.Body.Statements {
				collectSizesFromStmt(ss, sm)
			}
		}
	case *parser.BlockStatement:
		for _, ss := range s.Statements {
			collectSizesFromStmt(ss, sm)
		}
	}
}

func buildArraySizeMap(program *parser.Program) map[string]int64 {
	sizes := make(map[string]int64)
	for _, stmt := range program.Statements {
		collectArraySizesFromStmt(stmt, sizes)
	}
	return sizes
}

func collectArraySizesFromStmt(stmt parser.Statement, sizes map[string]int64) {
	switch s := stmt.(type) {
	case *parser.LetStatement:
		if at, ok := s.Type.(*parser.ArrayType); ok {
			var arraySize int64
			if at.Size != nil {
				if intLit, ok := at.Size.(*parser.IntegerLiteral); ok {
					arraySize = intLit.Value
				}
			} else if arrLit, ok := s.Value.(*parser.ArrayLiteral); ok {
				if intLit, ok := arrLit.Size.(*parser.IntegerLiteral); ok && intLit.Value > 0 {
					arraySize = intLit.Value
				}
			}
			if arraySize > 0 {
				sizes[s.Name.Value] = arraySize
			}
		}
	case *parser.ExpressionStatement:
		// if/else 表達式中的局部变量也需收集
		if ifExpr, ok := s.Expression.(*parser.IfExpression); ok {
			if ifExpr.Consequence != nil {
				for _, ss := range ifExpr.Consequence.Statements {
					collectArraySizesFromStmt(ss, sizes)
				}
			}
			if ifExpr.Alternative != nil {
				for _, ss := range ifExpr.Alternative.Statements {
					collectArraySizesFromStmt(ss, sizes)
				}
			}
		} else if smNode, ok := s.Expression.(*parser.SurfaceMatch); ok {
			for _, arm := range smNode.Arms {
				if arm.Body != nil {
					for _, ss := range arm.Body.Statements {
						collectArraySizesFromStmt(ss, sizes)
					}
				}
			}
		}
	case *parser.FunctionDefinition:
		if s.Body != nil {
			for _, ss := range s.Body.Statements {
				collectArraySizesFromStmt(ss, sizes)
			}
		}
	case *parser.ForStatement:
		if s.Init != nil {
			collectArraySizesFromStmt(s.Init, sizes)
		}
		if s.Body != nil {
			for _, ss := range s.Body.Statements {
				collectArraySizesFromStmt(ss, sizes)
			}
		}
	case *parser.BlockStatement:
		for _, ss := range s.Statements {
			collectArraySizesFromStmt(ss, sizes)
		}
	}
}

// buildSliceSizeMap collects names of slice variables and their initial element count
func buildSliceSizeMap(program *parser.Program) map[string]int64 {
	slices := make(map[string]int64)
	for _, stmt := range program.Statements {
		collectSliceSizeMapFromStmt(stmt, slices)
	}
	return slices
}

func collectSliceSizeMapFromStmt(stmt parser.Statement, slices map[string]int64) {
	switch s := stmt.(type) {
	case *parser.LetStatement:
		if _, ok := s.Type.(*parser.SliceType); ok {
			if sl, ok := s.Value.(*parser.SliceLiteral); ok {
				slices[s.Name.Value] = int64(len(sl.Elements))
			} else {
				slices[s.Name.Value] = 0 // unknown size
			}
		} else if sl, ok := s.Value.(*parser.SliceLiteral); ok {
			// Also detect slice from SliceLiteral value (inferred type, no [] annotation)
			slices[s.Name.Value] = int64(len(sl.Elements))
		}
	case *parser.ExpressionStatement:
		// if/else 表達式中的局部变量也需收集
		if ifExpr, ok := s.Expression.(*parser.IfExpression); ok {
			if ifExpr.Consequence != nil {
				for _, ss := range ifExpr.Consequence.Statements {
					collectSliceSizeMapFromStmt(ss, slices)
				}
			}
			if ifExpr.Alternative != nil {
				for _, ss := range ifExpr.Alternative.Statements {
					collectSliceSizeMapFromStmt(ss, slices)
				}
			}
		} else if smNode, ok := s.Expression.(*parser.SurfaceMatch); ok {
			for _, arm := range smNode.Arms {
				if arm.Body != nil {
					for _, ss := range arm.Body.Statements {
						collectSliceSizeMapFromStmt(ss, slices)
					}
				}
			}
		}
	case *parser.FunctionDefinition:
		if s.Body != nil {
			for _, ss := range s.Body.Statements {
				collectSliceSizeMapFromStmt(ss, slices)
			}
		}
	case *parser.ForStatement:
		if s.Init != nil {
			collectSliceSizeMapFromStmt(s.Init, slices)
		}
		if s.Body != nil {
			for _, ss := range s.Body.Statements {
				collectSliceSizeMapFromStmt(ss, slices)
			}
		}
	case *parser.BlockStatement:
		for _, ss := range s.Statements {
			collectSliceSizeMapFromStmt(ss, slices)
		}
	}
}

// buildStringSizeMap collects names of string variables and their literal length
func buildStringSizeMap(program *parser.Program) map[string]int64 {
	strSizes := make(map[string]int64)
	for _, stmt := range program.Statements {
		collectStringSizeMapFromStmt(stmt, strSizes)
	}
	return strSizes
}

func collectStringSizeMapFromStmt(stmt parser.Statement, strSizes map[string]int64) {
	switch s := stmt.(type) {
	case *parser.LetStatement:
		if s.Type != nil && (s.Type.String() == "str") {
			if sl, ok := s.Value.(*parser.StringLiteral); ok {
				strSizes[s.Name.Value] = int64(len(sl.Value))
			} else {
				strSizes[s.Name.Value] = 0 // unknown size, mark as string but no bound check
			}
		} else if isStringExprForCollect(s.Value, strSizes, validateFuncStrReturns) {
			// Also detect string from inferred expression (StringLiteral, string concatenation,
			// string method calls like slice/repeat, char-to-str, copy from known string var,
			if sl, ok := s.Value.(*parser.StringLiteral); ok {
				strSizes[s.Name.Value] = int64(len(sl.Value))
			} else {
				strSizes[s.Name.Value] = 0 // unknown size, mark as string but no bound check
			}
		}
	case *parser.FunctionDefinition:
		// Add str parameters and results to stringSizes so they're recognized as strings
		for _, p := range s.Parameters {
			if p.Type != nil && (p.Type.String() == "str") {
				strSizes[p.Name] = 0
			}
		}
		for _, p := range s.Results {
			if p.Type != nil && (p.Type.String() == "str") {
				strSizes[p.Name] = 0
			}
		}
		if s.Body != nil {
			for _, ss := range s.Body.Statements {
				collectStringSizeMapFromStmt(ss, strSizes)
			}
		}
	case *parser.ExpressionStatement:
		// if/else 表達式中的局部变量也需收集
		if ifExpr, ok := s.Expression.(*parser.IfExpression); ok {
			if ifExpr.Consequence != nil {
				for _, ss := range ifExpr.Consequence.Statements {
					collectStringSizeMapFromStmt(ss, strSizes)
				}
			}
			if ifExpr.Alternative != nil {
				for _, ss := range ifExpr.Alternative.Statements {
					collectStringSizeMapFromStmt(ss, strSizes)
				}
			}
		} else if smNode, ok := s.Expression.(*parser.SurfaceMatch); ok {
			for _, arm := range smNode.Arms {
				if arm.Body != nil {
					for _, ss := range arm.Body.Statements {
						collectStringSizeMapFromStmt(ss, strSizes)
					}
				}
			}
		}
	case *parser.ForStatement:
		if s.Init != nil {
			collectStringSizeMapFromStmt(s.Init, strSizes)
		}
		if s.Body != nil {
			for _, ss := range s.Body.Statements {
				collectStringSizeMapFromStmt(ss, strSizes)
			}
		}
	case *parser.BlockStatement:
		for _, ss := range s.Statements {
			collectStringSizeMapFromStmt(ss, strSizes)
		}
	}
}

// isSliceTypeOrOptionSlice reports whether a type is a slice ([]T) or an
// option wrapping a slice (?[]T). Used to track slice-typed parameters and
// results so that .len = assignments are rejected.
func isSliceTypeOrOptionSlice(t parser.Type) bool {
	if t == nil {
		return false
	}
	if _, ok := t.(*parser.SliceType); ok {
		return true
	}
	if nt, ok := t.(*parser.NullableType); ok {
		if _, ok := nt.Type.(*parser.SliceType); ok {
			return true
		}
	}
	return false
}

// isStringExprForCollect is a stricter version of isStringExpr used during the
// string size map collection phase. Unlike isStringExpr (which defers unknown
// types to LLVM), this function only returns true for expressions that are
// DEFINITELY strings, avoiding false positives like struct field access (DotExpression)
// or array element access (IndexExpression) which may be non-string types.
func isStringExprForCollect(expr parser.Expression, strSizes map[string]int64, funcStrReturns map[string]bool) bool {
	switch e := expr.(type) {
	case *parser.StringLiteral:
		return true
	case *parser.Identifier:
		_, exists := strSizes[e.Value]
		return exists
	case *parser.GroupedExpression:
		return isStringExprForCollect(e.Expression, strSizes, funcStrReturns)
	case *parser.InfixExpression:
		// String concatenation: when both sides are strings, the result is a string
		if e.Operator == "-" {
			return isStringExprForCollect(e.Left, strSizes, funcStrReturns) && isStringExprForCollect(e.Right, strSizes, funcStrReturns)
		}
	case *parser.CallExpression:
		// Check if it's a method call on a known string receiver (e.g., s.slice(), s.repeat())
		if dot, ok := e.Function.(*parser.DotExpression); ok {
			if ident, ok := dot.Receiver.(*parser.Identifier); ok {
				if _, exists := strSizes[ident.Value]; exists {
					return true // method call on known string variable
				}
				// Module-prefixed call to a user-defined function returning str
				// (e.g., my-mod.get-str() where get-str returns str)
				fullName := ident.Value + "." + dot.Property
				if funcStrReturns[fullName] || funcStrReturns[dot.Property] {
					return true
				}
			}
			// 'literal'.method() — method call on string literal
			if _, ok := dot.Receiver.(*parser.StringLiteral); ok {
				return true
			}
		}
		// Check if it's a global builtin function call that returns str (e.g., char-to-str)
		if ident, ok := e.Function.(*parser.Identifier); ok {
			switch ident.Value {
			case "char-to-str":
				return true
			}
			// Check if it's a user-defined function that returns str
			if funcStrReturns[ident.Value] {
				return true
			}
		}
	}
	return false
}

// isStringExpr checks if an expression is a string type
func isStringExpr(expr parser.Expression, stringSizes map[string]int64) bool {
	switch e := expr.(type) {
	case *parser.StringLiteral:
		return true
	case *parser.NilLiteral:
		// nil can be assigned to ?str (option string) variables
		return true
	case *parser.Identifier:
		_, exists := stringSizes[e.Value]
		return exists
	case *parser.GroupedExpression:
		return isStringExpr(e.Expression, stringSizes)
	case *parser.InfixExpression:
		// String concatenation: when both sides are strings, the result is a string
		if e.Operator == "-" {
			return isStringExpr(e.Left, stringSizes) && isStringExpr(e.Right, stringSizes)
		}
	case *parser.CallExpression:
		// Function/method calls (e.g., i64-to-str, s.to-upper) may return strings.
		// Return type cannot be determined at validation time; defer to LLVM type checking.
		return true
	case *parser.DotExpression:
		// Struct field access (e.g., fp.path) may return a string.
		// Return type cannot be determined at validation time; defer to LLVM type checking.
		return true
	case *parser.IndexExpression:
		// Array element access (e.g., req.headers[i], arr[i]) may return a string.
		// Return type cannot be determined at validation time; defer to LLVM type checking.
		return true
	case *parser.SliceExpression:
		// `s[1..3]` on a str yields a str (code-point sliced — see
		// docs/docs/lang/str.md), so it is a valid initializer for a str
		// variable. Slicing a []T / [N]T yields a []T instead; the validator
		// cannot tell the two apart without type inference, and an explicit
		// `x str = a[1..3]` over a non-str container is still rejected by the
		// checker.
		return true
	case *parser.AwaitExpression:
		// `awy <handle>` yields the awaited task's result. The handle is an
		// opaque i64, so the result type is not statically known here — e.g.
		// `v str = awy t` where t came from `run f(...)` and f returns str.
		// Defer to LLVM type checking, like the cases above. Without this the
		// validator rejected a perfectly valid str result.
		return true
	}
	return false
}

// isCharOrUntypedValue reports whether a `x str = <expr>` initializer is a plain
// identifier that is either known to be a `char` or not typed by this pass at
// all. Both are legal initializers for a str:
//
//   - `char` converts to `str` implicitly (the code point is UTF-8 encoded) —
//     see checker.isArgTypeCompatible and docs/docs/lang/str.md. That covers
//     `c char = "A"` followed by `s str = c`, and the iteration variable of
//     `for ch <- s` once it has been declared.
//   - an untyped name is a `for ch <- s` iteration variable (loop variables are
//     not registered by collectVarTypesFromBody) or a global. The checker's own
//     compatibility pass is the authority for those: it rejects a genuine
//     i64/bool/struct initializer with "cannot assign <T> value to str
//     variable". This front-end guard exists only to catch a scalar LITERAL or a
//     variable whose non-string type this pass already knows.
//
// Slicing and indexing (`s[1..3]`, `s[0]`) are accepted by isStringExpr
// directly, so they never reach here.
func isCharOrUntypedValue(expr parser.Expression, varTypes map[string]string) bool {
	id, ok := expr.(*parser.Identifier)
	if !ok {
		return false
	}
	t, known := varTypes[id.Value]
	if !known {
		return true
	}
	return t == "char"
}

// validateDuplicates checks for duplicate variable declarations
func validateDuplicates(program *parser.Program) error {
	seen := make(map[string]bool)
	for _, stmt := range program.Statements {
		if err := validateStmtDuplicates(program.Sem, stmt, seen); err != nil {
			return err
		}
	}
	return nil
}

func validateStmtDuplicates(sem *parser.SemanticContext, stmt parser.Statement, seen map[string]bool) error {
	switch s := stmt.(type) {
	case *parser.LetStatement:
		// In nolang, first assignment is definition, subsequent are reassignments
		// Check for duplicates when:
		//   1. There's an explicit type annotation in source (e.g., `i i64 = 0`), OR
		//   2. The name follows uppercase constant convention (e.g., `A = 0`, `SBOX = 1`)
		// Parser-inferred types (Type.Token position == Name.Token position) are treated as
		// "no explicit annotation" — but uppercase constants still trigger duplicate detection.
		hasExplicitType := s.Type != nil
		if hasExplicitType {
			if nt, ok := s.Type.(*parser.NamedType); ok {
				if nt.Token.Line == s.Name.Token.Line && nt.Token.Column == s.Name.Token.Column {
					// Parser-inferred type, not explicit annotation
					hasExplicitType = false
				}
			}
			// SliceType/ArrayType：parser 自動推導時 Token 與 nameToken 相同（同行同列），
			// 用戶顯式標註時 Token 是 '[' 位置。比較 Token 來區分推導 vs 顯式標註。
			// 這允許 slice/array 重新賦值（如 `local = [1, 2, 3]` 在已宣告後）。
			if st, ok := s.Type.(*parser.SliceType); ok {
				if st.Token.Line == s.Name.Token.Line && st.Token.Column == s.Name.Token.Column {
					hasExplicitType = false
				}
			}
			if at, ok := s.Type.(*parser.ArrayType); ok {
				if at.Token.Line == s.Name.Token.Line && at.Token.Column == s.Name.Token.Column {
					hasExplicitType = false
				}
			}
			// NullableType（?T）：lowering 的捕獲 pass 與 inferOptionDivMod 會就地
			// 合成推斷出的 `?T` 註解（IsInferred: true，Token 取陳述的 name token）。
			// 這與用戶手寫的 `?T`（Token 在 `?` 位置）語義不同：前者是推斷，重新
			// 賦值（`e0 = v[0]` 之後再 `e0 = v[1]`）必須放行。少了這一項，推斷出的
			// `?T` 會被當成顯式標註，同一變數的第二次賦值就報 duplicate variable。
			// 判定方式鏡像上面的 NamedType/SliceType/ArrayType：Token 與 name 同行
			// 同列即為 parser 推斷。
			if nt, ok := s.Type.(*parser.NullableType); ok {
				if nt.Token.Line == s.Name.Token.Line && nt.Token.Column == s.Name.Token.Column {
					hasExplicitType = false
				}
			}
			if s.Type.String() == s.Name.Value {
				// Parser artifact: Type.String() == Name.Value
				hasExplicitType = false
			}
		}
		isConst := checker.IsConstantName(s.Name.Value)
		if !hasExplicitType && !isConst {
			return nil
		}
		// 使用複合 key：name + "\x00" + platformKey（無平台註解則 suffix 為空）
		// 同名 + 同平台才算衝突；不同平台或通用 vs 平台特定不衝突
		sPlatformKeys := sem.PlatformKeysOf(s)
		var dupKeys []string
		if len(sPlatformKeys) == 0 {
			dupKeys = []string{s.Name.Value + "\x00"}
		} else {
			dupKeys = make([]string, 0, len(sPlatformKeys))
			for _, pk := range sPlatformKeys {
				dupKeys = append(dupKeys, s.Name.Value+"\x00"+pk)
			}
		}
		for _, k := range dupKeys {
			if seen[k] {
				return fmt.Errorf("duplicate variable '%s'", s.Name.Value)
			}
		}
		for _, k := range dupKeys {
			seen[k] = true
		}
	case *parser.FunctionDefinition:
		if s.Body != nil {
			bodySeen := make(map[string]bool)
			for _, bStmt := range s.Body.Statements {
				if err := validateStmtDuplicates(sem, bStmt, bodySeen); err != nil {
					return err
				}
			}
		}
	case *parser.BlockStatement:
		for _, bStmt := range s.Statements {
			if err := validateStmtDuplicates(sem, bStmt, seen); err != nil {
				return err
			}
		}
	case *parser.ForStatement:
		if s.Body != nil {
			for _, bStmt := range s.Body.Statements {
				if err := validateStmtDuplicates(sem, bStmt, seen); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// validateLoopScopedVars detects variables that are first declared inside a
// ForStatement body and then used after the loop exits. Because the loop might
// execute zero iterations, such variables would be undef at the point of use,
// leading to undefined behavior (e.g. infinite loops after LLVM optimization).
func validateLoopScopedVars(program *parser.Program) error {
	for _, stmt := range program.Statements {
		if fd, ok := stmt.(*parser.FunctionDefinition); ok && fd.Body != nil {
			// Pre-populate definedBeforeLoop with all function parameters and
			// result parameters, since they are always initialized by the caller
			// or at function entry.
			preDefined := make(map[string]bool)
			for _, p := range fd.Parameters {
				preDefined[p.Name] = true
			}
			for _, r := range fd.Results {
				if r.Name != "" {
					preDefined[r.Name] = true
				}
			}
			if err := validateLoopScopedStmts(fd.Body.Statements, preDefined); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateLoopScopedStmts walks a statement list sequentially, tracking which
// variables are first declared inside a ForStatement body. After a ForStatement,
// it checks ALL subsequent statements for uses of those loop-only variables.
func validateLoopScopedStmts(stmts []parser.Statement, preDefined map[string]bool) error {
	// definedBeforeLoop: variables defined at top level before any loop
	definedBeforeLoop := make(map[string]bool)
	// Pre-populate with function parameters and results
	for k, v := range preDefined {
		definedBeforeLoop[k] = v
	}
	// loopOnlyVars: variables first declared inside a preceding loop body.
	// These might be undef if the loop executed zero iterations.
	loopOnlyVars := make(map[string]bool)
	for _, stmt := range stmts {
		// Check if this statement reads a loop-only variable in an expression
		usedNames := collectExprIdentifiers(stmt)
		for name := range usedNames {
			if loopOnlyVars[name] && !definedBeforeLoop[name] {
				return fmt.Errorf("line %d: variable '%s' is declared inside a loop body and may be uninitialized when used here (loop might execute zero iterations)",
					stmtPosLine(stmt), name)
			}
		}
		// Check if a ForStatement's loop variable reuses a loop-only variable name.
		// This is the exact pattern that caused the md5 hang: a variable declared
		// inside a loop body (c u32 = h2) was reused as a range-for loop variable
		// (c <- (dremain..56)), causing LLVM to generate undef PHI nodes.
		if fs, ok := stmt.(*parser.ForStatement); ok {
			if fs.IterRange != nil && fs.IterRange.Variable != "" {
				v := fs.IterRange.Variable
				if loopOnlyVars[v] && !definedBeforeLoop[v] {
					return fmt.Errorf("line %d: variable '%s' is declared inside a loop body and may be uninitialized when reused as loop variable here (loop might execute zero iterations)",
						fs.Token.Line, v)
				}
			}
		}
		// Track top-level variable declarations
		if ls, ok := stmt.(*parser.LetStatement); ok && ls.Name != nil {
			definedBeforeLoop[ls.Name.Value] = true
			// Once re-declared at top level, it's no longer "loop-only"
			delete(loopOnlyVars, ls.Name.Value)
		}
		// ForStatement: loop variable is always initialized
		if fs, ok := stmt.(*parser.ForStatement); ok {
			if fs.IterRange != nil && fs.IterRange.Variable != "" {
				definedBeforeLoop[fs.IterRange.Variable] = true
				delete(loopOnlyVars, fs.IterRange.Variable)
			}
			// Collect variables declared inside the loop body
			bodyVars := collectLoopBodyVarDecls(fs)
			for v := range bodyVars {
				if !definedBeforeLoop[v] {
					loopOnlyVars[v] = true
				}
			}
			// Recursively validate nested statements inside the loop body.
			// Pass current definedBeforeLoop as preDefined so that variables
			// defined before this loop are still recognized inside the body.
			if fs.Body != nil {
				if err := validateLoopScopedStmts(fs.Body.Statements, definedBeforeLoop); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func collectLoopBodyVarDecls(fs *parser.ForStatement) map[string]bool {
	vars := make(map[string]bool)
	if fs.Body == nil {
		return vars
	}
	collectVarDeclsFromStmts(fs.Body.Statements, vars)
	if fs.IterRange != nil && fs.IterRange.Variable != "" {
		delete(vars, fs.IterRange.Variable)
	}
	return vars
}

func collectVarDeclsFromStmts(stmts []parser.Statement, vars map[string]bool) {
	for _, stmt := range stmts {
		switch s := stmt.(type) {
		case *parser.LetStatement:
			if s.Name != nil {
				vars[s.Name.Value] = true
			}
		case *parser.BlockStatement:
			collectVarDeclsFromStmts(s.Statements, vars)
		case *parser.ForStatement:
			if s.Body != nil {
				collectVarDeclsFromStmts(s.Body.Statements, vars)
			}
		}
	}
}

func collectExprIdentifiers(stmt parser.Statement) map[string]bool {
	idents := make(map[string]bool)
	switch s := stmt.(type) {
	case *parser.LetStatement:
		if s.Value != nil {
			collectIdentsFromExpr(s.Value, idents)
		}
	case *parser.ExpressionStatement:
		if s.Expression != nil {
			collectIdentsFromExpr(s.Expression, idents)
		}
	case *parser.ReturnStatement:
		if s.ReturnValue != nil {
			collectIdentsFromExpr(s.ReturnValue, idents)
		}
	case *parser.ForStatement:
		if s.IterRange != nil {
			if s.IterRange.Range != nil {
				if s.IterRange.Range.Start != nil {
					collectIdentsFromExpr(s.IterRange.Range.Start, idents)
				}
				if s.IterRange.Range.End != nil {
					collectIdentsFromExpr(s.IterRange.Range.End, idents)
				}
			}
			if s.IterRange.RangeExpr != nil {
				collectIdentsFromExpr(s.IterRange.RangeExpr, idents)
			}
		}
		if s.Condition != nil {
			collectIdentsFromExpr(s.Condition, idents)
		}
		if s.CountExpr != nil {
			collectIdentsFromExpr(s.CountExpr, idents)
		}
	case *parser.MultiAssignStatement:
		for _, target := range s.Targets {
			collectIdentsFromExpr(target, idents)
		}
		if s.Value != nil {
			collectIdentsFromExpr(s.Value, idents)
		}
	}
	return idents
}

func collectIdentsFromExpr(expr parser.Expression, idents map[string]bool) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case *parser.Identifier:
		idents[e.Value] = true
	case *parser.PrefixExpression:
		collectIdentsFromExpr(e.Right, idents)
	case *parser.InfixExpression:
		collectIdentsFromExpr(e.Left, idents)
		collectIdentsFromExpr(e.Right, idents)
	case *parser.CallExpression:
		collectIdentsFromExpr(e.Function, idents)
		for _, arg := range e.Arguments {
			collectIdentsFromExpr(arg, idents)
		}
		for _, ga := range e.GenericArgs {
			collectIdentsFromExpr(ga, idents)
		}
	case *parser.DotExpression:
		collectIdentsFromExpr(e.Receiver, idents)
	case *parser.IndexExpression:
		collectIdentsFromExpr(e.Left, idents)
		collectIdentsFromExpr(e.Index, idents)
	case *parser.SliceExpression:
		collectIdentsFromExpr(e.Left, idents)
		if e.Range != nil {
			collectIdentsFromExpr(e.Range.Start, idents)
			collectIdentsFromExpr(e.Range.End, idents)
		}
	case *parser.AssignExpression:
		collectIdentsFromExpr(e.Left, idents)
		collectIdentsFromExpr(e.Value, idents)
	case *parser.ConditionalExpression:
		collectIdentsFromExpr(e.Condition, idents)
		collectIdentsFromExpr(e.Consequence, idents)
		collectIdentsFromExpr(e.Alternative, idents)
	case *parser.SurfaceMatch:
		// S1：糖形態 match。collectIdentsFromExpr 只收 condition 等價子樹
		// （降糖 IfExpression 僅走 Condition，其已內嵌 subject 比較與模式值），
		// 故用 WalkSurfaceMatchChildren 只傳 onExpr（nil onStmt 跳過臂體）。
		parser.WalkSurfaceMatchChildren(e,
			func(x parser.Expression) { collectIdentsFromExpr(x, idents) }, nil)
	case *parser.IfExpression:
		collectIdentsFromExpr(e.Condition, idents)
	case *parser.CastExpression:
		collectIdentsFromExpr(e.Expr, idents)
	case *parser.GroupedExpression:
		collectIdentsFromExpr(e.Expression, idents)
	case *parser.ArrayLiteral:
		for _, elem := range e.Elements {
			collectIdentsFromExpr(elem, idents)
		}
	case *parser.SliceLiteral:
		for _, elem := range e.Elements {
			collectIdentsFromExpr(elem, idents)
		}
	case *parser.MapLiteral:
		for _, pair := range e.Pairs {
			collectIdentsFromExpr(pair.Key, idents)
			collectIdentsFromExpr(pair.Value, idents)
		}
	case *parser.StructLiteral:
		for _, field := range e.Fields {
			collectIdentsFromExpr(field.Value, idents)
		}
	case *parser.RunExpression:
		collectIdentsFromExpr(e.Call, idents)
	case *parser.AwaitExpression:
		collectIdentsFromExpr(e.Right, idents)
	case *parser.RangeExpression:
		collectIdentsFromExpr(e.Start, idents)
		collectIdentsFromExpr(e.End, idents)
	}
}

func stmtPosLine(stmt parser.Statement) int {
	if stmt == nil {
		return 0
	}
	return stmt.Pos().Line
}

// validateArrayBounds 編譯期陣列邊界檢查
// 檢查所有 IndexExpression 中的常數索引是否超出陣列長度
func validateArrayBounds(program *parser.Program, arraySizes map[string]int64, sliceSizes map[string]int64, stringSizes map[string]int64, varTypes map[string]string) error {
	for _, stmt := range program.Statements {
		if err := validateStmtArrayBounds(stmt, arraySizes, sliceSizes, stringSizes, varTypes, program.Sem); err != nil {
			return err
		}
	}
	return nil
}

func validateStmtArrayBounds(stmt parser.Statement, arraySizes map[string]int64, sliceSizes map[string]int64, stringSizes map[string]int64, varTypes map[string]string, sem *parser.SemanticContext) error {
	switch s := stmt.(type) {
	case *parser.ExpressionStatement:
		return validateExprArrayBounds(s.Expression, arraySizes, sliceSizes, stringSizes, varTypes, sem)
	case *parser.LetStatement:
		if s.Value != nil {
			// Skip validation for synthetic `it` bindings injected by match desugar.
			// These have sentinel types ("err", "nil") or element types ("str", "i64")
			// but their value is an option variable, not a direct string/integer.
			if s.IsSynthetic {
				return validateExprArrayBounds(s.Value, arraySizes, sliceSizes, stringSizes, varTypes, sem)
			}
			// Skip string type check for array/slice variables
			isArrayVar := false
			if at, ok := s.Type.(*parser.ArrayType); ok {
				isArrayVar = at != nil
			}
			if !isArrayVar {
				// Only check explicitly str-typed variables (not inferred ones).
				// Inferred string variables (from StringLiteral, etc.) may later be
				// assigned from struct field access or cross-module calls whose
				// return type is unknown at vet time; deferring to LLVM is safer.
				isExplicitStr := s.Type != nil && (s.Type.String() == "str")
				if isExplicitStr {
					if !isStringExpr(s.Value, stringSizes) && !isCharOrUntypedValue(s.Value, varTypes) {
						return fmt.Errorf("cannot assign non-string value to string variable '%s'", s.Name.Value)
					}
				}
			}
			return validateExprArrayBounds(s.Value, arraySizes, sliceSizes, stringSizes, varTypes, sem)
		}
	case *parser.FunctionDefinition:
		// Build a fresh per-function stringSizes map to avoid variable name
		// collisions between functions (e.g., 'pos' may be str in one function
		// and i64 in another). Only include this function's parameters, results,
		// and local variables — not global constants or other functions' variables.
		funcStringSizes := make(map[string]int64)
		funcSliceSizes := make(map[string]int64)
		funcArraySizes := make(map[string]int64)
		// Build a per-function varTypes: start empty, then add this function's
		// params, results, and local variables. We intentionally do NOT copy
		// from the global varTypes (which contains local variables from ALL
		// merged functions, causing cross-function type leakage such as
		// fs.open's local 'm file-mode' shadowing a test file's 'm [str]i64').
		// However, we do need top-level constants for resolving global refs
		// inside function bodies, so we pass the global varTypes separately
		// and fall back to it for unknown variables.
		funcVarTypes := make(map[string]string)
		for _, p := range s.Parameters {
			if p.Type != nil {
				funcVarTypes[p.Name] = p.Type.String()
				if p.Type.String() == "str" {
					funcStringSizes[p.Name] = 0
				}
			}
			// Track slice-typed parameters ([]T and ?[]T) so that .len =
			// assignments are rejected. Skip 'self' to allow vec.truncate/extend
			// and other low-level slice methods to modify .len internally.
			if p.Name != "self" && isSliceTypeOrOptionSlice(p.Type) {
				funcSliceSizes[p.Name] = 0
			}
		}
		for _, p := range s.Results {
			if p.Type != nil {
				funcVarTypes[p.Name] = p.Type.String()
				if p.Type.String() == "str" {
					funcStringSizes[p.Name] = 0
				}
			}
			// Skip 'self' (now in Results) to allow vec.truncate/extend
			// and other low-level slice methods to modify .len internally.
			if p.Name != "self" && isSliceTypeOrOptionSlice(p.Type) {
				funcSliceSizes[p.Name] = 0
			}
		}
		if s.Body != nil {
			collectVarTypesFromBody(s.Body, funcVarTypes, sem)
			for _, ss := range s.Body.Statements {
				collectStringSizeMapFromStmt(ss, funcStringSizes)
				collectSliceSizeMapFromStmt(ss, funcSliceSizes)
				collectArraySizesFromStmt(ss, funcArraySizes)
			}
			for _, ss := range s.Body.Statements {
				if err := validateStmtArrayBounds(ss, funcArraySizes, funcSliceSizes, funcStringSizes, funcVarTypes, sem); err != nil {
					return err
				}
			}
		}
	case *parser.ForStatement:
		if s.Init != nil {
			if err := validateStmtArrayBounds(s.Init, arraySizes, sliceSizes, stringSizes, varTypes, sem); err != nil {
				return err
			}
		}
		if s.Body != nil {
			for _, ss := range s.Body.Statements {
				if err := validateStmtArrayBounds(ss, arraySizes, sliceSizes, stringSizes, varTypes, sem); err != nil {
					return err
				}
			}
		}
	case *parser.BlockStatement:
		for _, ss := range s.Statements {
			if err := validateStmtArrayBounds(ss, arraySizes, sliceSizes, stringSizes, varTypes, sem); err != nil {
				return err
			}
		}
	case *parser.ReturnStatement:
		if s.ReturnValue != nil {
			return validateExprArrayBounds(s.ReturnValue, arraySizes, sliceSizes, stringSizes, varTypes, sem)
		}
	}
	return nil
}

// tryEvalConstInt 嘗試在編譯期求值整數常數表達式。
// 支援：IntegerLiteral、PrefixExpression(-)、InfixExpression(+,-,*)、GroupedExpression。
// 若成功求值回傳 (value, true)，否則回傳 (0, false)。
func tryEvalConstInt(expr parser.Expression) (int64, bool) {
	switch e := expr.(type) {
	case *parser.IntegerLiteral:
		return e.Value, true
	case *parser.PrefixExpression:
		if e.Operator == "-" {
			if v, ok := tryEvalConstInt(e.Right); ok {
				return -v, true
			}
		}
		if e.Operator == "+" {
			return tryEvalConstInt(e.Right)
		}
	case *parser.InfixExpression:
		lv, lok := tryEvalConstInt(e.Left)
		rv, rok := tryEvalConstInt(e.Right)
		if lok && rok {
			switch e.Operator {
			case "+":
				return lv + rv, true
			case "-":
				return lv - rv, true
			case "*":
				return lv * rv, true
			}
		}
	case *parser.GroupedExpression:
		return tryEvalConstInt(e.Expression)
	}
	return 0, false
}

// checkConstIndexBounds 檢查常數索引是否越界（負數或 >= size）。
// 若索引非常數或 size <= 0 則跳過。回傳錯誤或 nil。
func checkConstIndexBounds(idxExpr parser.Expression, size int64, varName string, typeName string) error {
	if size <= 0 {
		return nil
	}
	idx, ok := tryEvalConstInt(idxExpr)
	if !ok {
		return nil
	}
	if idx < 0 {
		return fmt.Errorf("index %d out of bounds for %s '%s' of size %d", idx, typeName, varName, size)
	}
	if idx >= size {
		return fmt.Errorf("index %d out of bounds for %s '%s' of size %d", idx, typeName, varName, size)
	}
	return nil
}

// isArrOrSliceTypeName reports whether a resolved type name denotes a fixed array
// ([N x T]), a slice ([]T), or a vec (%vec). These are the container types whose
// .len property is being deprecated in favour of the .len() method.
func isArrOrSliceTypeName(t string) bool {
	return strings.HasPrefix(t, "[]") || strings.HasPrefix(t, "[") || strings.HasPrefix(t, "%vec")
}

func validateExprArrayBounds(expr parser.Expression, arraySizes map[string]int64, sliceSizes map[string]int64, stringSizes map[string]int64, varTypes map[string]string, sem *parser.SemanticContext) error {
	switch e := expr.(type) {
	case *parser.IndexExpression:
		// 檢查索引是否為常數且超出陣列長度
		if ident, ok := e.Left.(*parser.Identifier); ok {
			if size, exists := arraySizes[ident.Value]; exists {
				if err := checkConstIndexBounds(e.Index, size, ident.Value, "array"); err != nil {
					return err
				}
			}
			// Also check slice bounds
			if size, exists := sliceSizes[ident.Value]; exists {
				if err := checkConstIndexBounds(e.Index, size, ident.Value, "slice"); err != nil {
					return err
				}
			}
			// Also check string index bounds.
			// CODE-POINT indexing made a constant over-the-end index DEFINED
			// behavior rather than UB: a read decodes to -1 (@nolang.utf8_cp_at)
			// and a write clamps to an append (@nolang.utf8_cp_put maintains the
			// byte length), so the upper bound is no longer a compile error — and
			// the collected size is a BYTE length anyway, which cannot decide a
			// CODE-POINT index. Only a negative constant stays an error: no index
			// is negative under either semantics.
			if size, exists := stringSizes[ident.Value]; exists {
				if idx, ok := tryEvalConstInt(e.Index); ok && idx < 0 {
					return fmt.Errorf("index %d out of bounds for string '%s' of size %d", idx, ident.Value, size)
				}
			}
		}
		// 遞迴檢查 Left 和 Index（Index 自身也可能有巢狀索引）
		if err := validateExprArrayBounds(e.Left, arraySizes, sliceSizes, stringSizes, varTypes, sem); err != nil {
			return err
		}
		return validateExprArrayBounds(e.Index, arraySizes, sliceSizes, stringSizes, varTypes, sem)
	case *parser.AssignExpression:
		// array.len = val / slice.len = val / string.len = val → 不允許修改唯讀的 len 欄位
		// 使用 str.truncate(n) 或 []t.truncate(n) 替代直接修改 .len
		if dot, ok := e.Left.(*parser.DotExpression); ok {
			if dot.Property == "len" {
				if ident, ok := dot.Receiver.(*parser.Identifier); ok {
					if _, exists := arraySizes[ident.Value]; exists {
						return fmt.Errorf("cannot modify read-only field 'len' of array '%s'", ident.Value)
					}
					if _, exists := sliceSizes[ident.Value]; exists {
						return fmt.Errorf("cannot modify read-only field 'len' of slice '%s'", ident.Value)
					}
					if _, exists := stringSizes[ident.Value]; exists {
						return fmt.Errorf("cannot modify read-only field 'len' of string '%s'", ident.Value)
					}
				}
			}
		}
		// Note: string type check for reassignments is intentionally omitted.
		// Inferred string variables may be reassigned from struct field access or
		// cross-module calls whose return type is unknown at vet time.
		// The LetStatement check for explicitly str-typed variables is sufficient.
		// a[i] = val → 檢查 Left 中的 IndexExpression
		// （slice 的索引檢查已在 IndexExpression case 中處理）
		return validateExprArrayBounds(e.Left, arraySizes, sliceSizes, stringSizes, varTypes, sem)
	case *parser.InfixExpression:
		if err := validateExprArrayBounds(e.Left, arraySizes, sliceSizes, stringSizes, varTypes, sem); err != nil {
			return err
		}
		return validateExprArrayBounds(e.Right, arraySizes, sliceSizes, stringSizes, varTypes, sem)
	case *parser.PrefixExpression:
		return validateExprArrayBounds(e.Right, arraySizes, sliceSizes, stringSizes, varTypes, sem)
	case *parser.CallExpression:
		if e.Function != nil {
			if err := validateExprArrayBounds(e.Function, arraySizes, sliceSizes, stringSizes, varTypes, sem); err != nil {
				return err
			}
		}
		for _, arg := range e.Arguments {
			if err := validateExprArrayBounds(arg, arraySizes, sliceSizes, stringSizes, varTypes, sem); err != nil {
				return err
			}
		}
	case *parser.ArrayLiteral:
		for _, elem := range e.Elements {
			if err := validateExprArrayBounds(elem, arraySizes, sliceSizes, stringSizes, varTypes, sem); err != nil {
				return err
			}
		}
	case *parser.SliceLiteral:
		for _, elem := range e.Elements {
			if err := validateExprArrayBounds(elem, arraySizes, sliceSizes, stringSizes, varTypes, sem); err != nil {
				return err
			}
		}
	case *parser.SurfaceMatch:
		// S1：糖形態 match。對應 IfExpression 分支：只校 Condition 與各臂體，
		// 不校 Matched（與降糖 IfExpression 不校 MatchedExpr 一致）。
		for i := range e.Arms {
			arm := &e.Arms[i]
			if arm.Condition != nil {
				if err := validateExprArrayBounds(arm.Condition, arraySizes, sliceSizes, stringSizes, varTypes, sem); err != nil {
					return err
				}
			}
			if arm.Body != nil {
				for _, ss := range arm.Body.Statements {
					if err := validateStmtArrayBounds(ss, arraySizes, sliceSizes, stringSizes, varTypes, sem); err != nil {
						return err
					}
				}
			}
		}
	case *parser.IfExpression:
		if e.Condition != nil {
			if err := validateExprArrayBounds(e.Condition, arraySizes, sliceSizes, stringSizes, varTypes, sem); err != nil {
				return err
			}
		}
		if e.Consequence != nil {
			for _, ss := range e.Consequence.Statements {
				if err := validateStmtArrayBounds(ss, arraySizes, sliceSizes, stringSizes, varTypes, sem); err != nil {
					return err
				}
			}
		}
		if e.Alternative != nil {
			for _, ss := range e.Alternative.Statements {
				if err := validateStmtArrayBounds(ss, arraySizes, sliceSizes, stringSizes, varTypes, sem); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// buildArraySizeMap 構建變數名 → 陣列大小的映射
// 從所有 LetStatement 中收集 ArraySize
// sizeMaps 儲存陣列、切片、字串的大小映射，由 buildSizeMaps 單次遍歷填充。
// 合併三個原本各自獨立遍歷 AST 的函數（buildArraySizeMap/buildSliceSizeMap/buildStringSizeMap），
// 將遍歷次數從 3 次降至 1 次，顯著減少大型程序的校驗開銷。
type sizeMaps struct {
	arraySizes     map[string]int64
	sliceSizes     map[string]int64
	stringSizes    map[string]int64
	funcStrReturns map[string]bool // user-defined functions returning str
}

// validateFuncStrReturns is a package-level variable that holds the
// funcStrReturns map from buildSizeMaps. It is set before validateArrayBounds
// runs so that collectStringSizeMapFromStmt (which is called from the
// FunctionDefinition case of validateStmtArrayBounds with a fresh per-function
// stringSizes map) can access funcStrReturns without a signature change.
var validateFuncStrReturns map[string]bool

// validateEntryProgram 入口程序（模組合併前）的編譯期驗證。依序執行：
//
//  1. 編譯期陣列邊界檢查（單次遍歷同時收集陣列、切片、字串大小映射）
//  2. 編譯期重複變數檢查
//  3. 頂層語句標記來源檔（使 .len 廢棄檢查等能豁免內建實作檔）
//  4. 型別檢查七驗證器彙總（收集所有錯誤後統一報告，而非遇錯即返）
//
// 第 4 步的錯誤彙總語意：用戶在 no build 時能一次看到所有型別錯誤，
// 而非逐個修復後才能看到下一個。Stage 6D：validator 已遷移至 THIR
// 型別副表視圖（BuildTHIRView，含 H0 副作用隔離）。
func validateEntryProgram(program *parser.Program, sourcePath string, varTypes map[string]string) error {
	// 編譯期陣列邊界檢查（單次遍歷同時收集陣列、切片、字串大小映射）
	sizeMap := buildSizeMaps(program)
	validateFuncStrReturns = sizeMap.funcStrReturns
	if err := validateArrayBounds(program, sizeMap.arraySizes, sizeMap.sliceSizes, sizeMap.stringSizes, varTypes); err != nil {
		return err
	}
	// 編譯期重複變數檢查
	if err := validateDuplicates(program); err != nil {
		return err
	}
	// Tag top-level statements with their source path BEFORE type-checking so
	// validation passes (e.g. the .len deprecation check) can exempt builtin
	// implementation files (vec.no / byte.no / str.no) by filename. SourceFile
	// is otherwise only set later (after module merge), which is too late for
	// the validation phase.
	for _, stmt := range program.Statements {
		program.Sem.SetSourceFileOf(stmt, sourcePath)
	}
	// 型別檢查（收集所有錯誤後統一報告，而非遇錯即返）
	// 這樣用戶在 no build 時能一次看到所有型別錯誤，而非逐個修復後才能看到下一個。
	// Stage 6D：已遷移 validator 經 THIR 型別副表視圖（BuildTHIRView，含 H0
	// 副作用隔離——validators 看到的 program 狀態與 legacy 路徑完全一致）。
	var lints []checker.LintResult
	thirView := checker.BuildTHIRView(program)
	if typeErrs := checker.ValidateTypesTHIR(program, thirView); len(typeErrs) > 0 {
		for _, e := range typeErrs {
			lints = append(lints, validateResultToLint(e))
		}
	}
	// 函數呼叫引數型別檢查（包括 newtype 語義：fd 變量不能傳給 i64 參數）
	if funcArgErrs := checker.ValidateFuncArgsTHIR(program, filepath.Dir(sourcePath), thirView); len(funcArgErrs) > 0 {
		for _, e := range funcArgErrs {
			lints = append(lints, validateResultToLint(e))
		}
	}
	// 未定義變數檢查（包括 struct 類型名直接賦值欄位，如 s.x = 7 而非 s0.x = 7）
	if undefErrs := checker.ValidateUndefinedVars(program, filepath.Dir(sourcePath), sourcePath); len(undefErrs) > 0 {
		for _, e := range undefErrs {
			lints = append(lints, validateResultToLint(e))
		}
	}
	// 關係運算子（< <= > >=）的多字元字串運算元（編譯硬錯誤）：nolang 沒有字串
	// 字典序比較，codegen 的 str 分支只有 @str_eq（相等），`a >= b`（a/b 為 str）
	// 會靜默退化成恆假。單字元字串字面量（'a'）隱式當 char，仍合法。
	if strOrderErrs := checker.ValidateStrOrderingTHIR(program, thirView); len(strOrderErrs) > 0 {
		for _, e := range strOrderErrs {
			lints = append(lints, validateResultToLint(e))
		}
	}
	// option 生命週期檢查（D2 分級）。同一趟 ValidateUnhandledOverflow 產出兩種 trace：
	//   - ovfhndld（HINT）：整數運算產生 option 並綁定到變數。統一 option 規則（D6）下
	//     這是常態，只提示「該變數是 ?T，記得 match 或 ?=」——**不是**硬錯誤。
	//   - optunuse（ERROR）：該 option 既沒上拋（?=）也沒解包（match / `_ =`），錯誤被
	//     靜默丟棄 —— 這才是唯一該讓 no build / no run / no test 失敗的診斷。
	// 因此這裡必須逐條依 TraceID 過濾，不能整批當硬錯誤（否則每個 `x = a + b` 都會
	// 讓建置失敗，而它其實是合法且常見的寫法）。
	if leakErrs := checker.ValidateUnhandledOverflowTHIR(program, sourcePath, thirView); len(leakErrs) > 0 {
		for _, e := range leakErrs {
			if e.TraceID != checker.OptUnuseTraceID {
				continue
			}
			lints = append(lints, validateResultToLint(e))
		}
	}
	// 未處理的越界索引（D2：降為 HINT，不再阻斷建置）。arr/vec/slice 索引 b[i] 越界時
	// 預設回傳 option<elem>，這是統一規則的常態；「沒解包」由上面的 optunuse 兜底。
	// 診斷仍會經 RunAllLints 出現在 no vet / LSP（灰色提示），故此處不再呼叫
	// checker.ValidateUnhandledIndex。
	//
	// 下標直寫 `a[i] = v`（D9）→ ERROR `idxwrite`，**阻斷建置**。
	// 與上面「讀側只提示」相反：寫側走 emitIndexStore 的原始 GEP+store，
	// **沒有任何邊界檢查**，越界即 raw OOB write，是記憶體安全洞而非風格問題。
	// 必須先用 place-match 解包（多行形式；match 臂以换行分隔）或 `a[i] ?= v`
	// 上拋，故在此逐條過濾 TraceID 後匯入硬錯誤。
	if writeErrs := checker.ValidateIndexWriteTHIR(program, sourcePath, thirView); len(writeErrs) > 0 {
		for _, e := range writeErrs {
			if e.TraceID != checker.IndexWriteTraceID {
				continue
			}
			lints = append(lints, validateResultToLint(e))
		}
	}
	// ?T 輸出參數未初始化檢查（case6）
	if uninitErrs := checker.ValidateUninitOutputParams(program); len(uninitErrs) > 0 {
		for _, e := range uninitErrs {
			lints = append(lints, validateResultToLint(e))
		}
	}
	if len(lints) > 0 {
		return &DiagnosticError{Lints: lints}
	}
	return nil
}

// validateMergedProgram 模組合併後（單態化收斂、第二階段解析完成後）的編譯期
// 驗證。此時 merged 包含全部主程式與模組陳述句，可檢測跨模組問題：
//
//  1. D16：重新執行 validateArrayBounds（導入模組內的邊界錯誤不再被靜默吞掉）
//  2. 編譯期未初始化變數檢查（循環體內聲明、循環外使用）
//  3. Bug 22：跨模組 with-len/with-cap/with-cap-len 無類型標註漏撿
//  4. 問題 8：合併後重新執行函數引數數量檢查（只檢查數量不檢查型別，
//     避免標準庫內部複雜型別推斷產生的誤報）
//  5. 字面零除數檢查（整數 / 與 % 的除數為字面常數 0 會觸發 LLVM UB）
func validateMergedProgram(merged *parser.Program) error {
	// D16: 模組合併後重新執行 validateArrayBounds。
	// 原本僅在主程式合併前執行，導入模組內的錯誤（如 out.len = 1
	// 違反 str.len 只讀約束）被靜默吞掉，直到連結期才以 undefined symbol 報錯。
	// 必須在所有轉換 pass 完成後執行，此時 merged 包含全部主程式與模組陳述句。
	mergedVarTypes := buildVarTypes(merged)
	mergedSizeMap := buildSizeMaps(merged)
	validateFuncStrReturns = mergedSizeMap.funcStrReturns
	if err := validateArrayBounds(merged, mergedSizeMap.arraySizes, mergedSizeMap.sliceSizes, mergedSizeMap.stringSizes, mergedVarTypes); err != nil {
		return err
	}
	// 編譯期未初始化變數檢查：循環體內聲明的變數在循環外使用
	// 必須在模組合併後執行，才能檢查到導入模組（如 md5.no）中的問題
	if err := validateLoopScopedVars(merged); err != nil {
		return err
	}
	// Bug 22: 跨模組 with-len/with-cap/with-cap-len 無類型標註漏檢。
	// 單文件時 ValidateTypes/ValidateFuncArgs 會檢查，但模組合併發生在驗證之後，
	// 導入模組函數體內的 with-len 無類型標註不會被檢查到。
	// 在模組合併後對 merged 程式執行專門的 LHS-inferred builtin 檢查。
	if lhsInfErrs := checker.ValidateLHSInferredBuiltins(merged); len(lhsInfErrs) > 0 {
		var lints []checker.LintResult
		for _, e := range lhsInfErrs {
			lints = append(lints, validateResultToLint(e))
		}
		return &DiagnosticError{Lints: lints}
	}
	// 問題 8: 模組合併後重新執行函數引數數量檢查。
	// ValidateFuncArgs 在模組合併前執行，此時標準庫函數（如
	// sha1.sha1-hex）的函數定義尚未被合併到 program 中，sigs 中沒有它們的
	// 簽名，因此參數數量不匹配（如 sha1-hex() 缺少參數）不會被檢測到。
	// 在模組合併後對 merged 程式重新執行參數數量檢查，使標準庫函數的
	// 參數數量也能被檢查。只檢查數量不檢查型別，避免標準庫內部複雜型別
	// 推斷（泛型、單態化）產生的誤報。
	if mergedArgCountErrs := checker.ValidateFuncArgCount(merged); len(mergedArgCountErrs) > 0 {
		var lints []checker.LintResult
		for _, e := range mergedArgCountErrs {
			lints = append(lints, validateResultToLint(e))
		}
		return &DiagnosticError{Lints: lints, Prefix: "function argument errors"}
	}
	// 字面零除數檢查：整數 / 與 % 的除數為字面常數 0 會觸發 LLVM UB，編譯期攔截。
	// 6C-optional 第三批型別精化：被除數確定為浮點時跳過（fdiv 非 UB），判據走
	// THIRView 表達式級型別表 —— 與 lint 軌道（ValidateDivByZeroTHIR）同步精化，
	// 兩條管道口徑一致。
	if divZeroErrs := checker.ValidateDivByZero(merged, checker.BuildTHIRView(merged)); len(divZeroErrs) > 0 {
		var lints []checker.LintResult
		for _, e := range divZeroErrs {
			lints = append(lints, validateResultToLint(e))
		}
		return &DiagnosticError{Lints: lints}
	}
	return nil
}
