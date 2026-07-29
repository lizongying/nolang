package build

import (
	"strings"

	"github.com/lizongying/nolang/parser"
)

// modulePrefixBuiltinTypes is the set of built-in type names that must never
// receive a module prefix.
var modulePrefixBuiltinTypes = map[string]bool{
	"i8": true, "i16": true, "i32": true, "i64": true,
	"u8": true, "u16": true, "u32": true, "u64": true,
	"bool": true, "byte": true, "char": true,
	"f32": true, "f64": true,
	"str": true, "ptr": true,
	"arr": true, "vec": true, "option": true,
	"any": true, "err": true, "nil": true,
}

// isModulePrefixBuiltinType returns true for built-in / non-user type names
// that must never be prefixed with a module short name.
func isModulePrefixBuiltinType(name string) bool {
	if modulePrefixBuiltinTypes[name] {
		return true
	}
	// Special prefixes that are definitely not user-defined struct types
	if strings.HasPrefix(name, "fn(") ||
		strings.HasPrefix(name, "hashmap-") ||
		strings.HasPrefix(name, "map[") ||
		strings.HasPrefix(name, "[") ||
		strings.HasPrefix(name, "?") ||
		strings.HasPrefix(name, "*") {
		return true
	}
	// Already prefixed (contains ".")
	if strings.Contains(name, ".") {
		return true
	}
	return false
}

// getTypeDefName returns the bare name of a type-definition statement, or "".
func getTypeDefName(stmt parser.Statement) string {
	switch s := stmt.(type) {
	case *parser.StructDefinition:
		return s.Name
	case *parser.InterfaceDefinition:
		return s.Name
	case *parser.TypeAlias:
		return s.Name
	case *parser.EnumDefinition:
		return s.Name
	case *parser.TaggedEnumDefinition:
		return s.Name
	}
	return ""
}

// renameTypeDef changes the name of a type-definition statement.
func renameTypeDef(stmt parser.Statement, newName string) {
	switch s := stmt.(type) {
	case *parser.StructDefinition:
		s.Name = newName
	case *parser.InterfaceDefinition:
		s.Name = newName
	case *parser.TypeAlias:
		s.Name = newName
	case *parser.EnumDefinition:
		s.Name = newName
	case *parser.TaggedEnumDefinition:
		s.Name = newName
	}
}

// prefixModuleStatements renames type definitions and method names in a
// module's statements so they carry the module short-name prefix
// (e.g. `result` → `sql.result`, `result.exec` → `sql.result.exec`).
//
// typeOwner 以 "module.name" 為 key 記錄所有模組的所有型別定義，
// value 為 bareName（如 "client"）。這樣即使多個模組定義同名結構體
// （如 net.client 與 ws.client），也不會互相覆蓋。
func prefixModuleStatements(stmts []parser.Statement, moduleShortName string, typeOwner map[string]string) {
	if moduleShortName == "" {
		return
	}

	// --- Phase 1: collect this module's bare type names ---
	moduleTypes := make(map[string]bool)
	for _, stmt := range stmts {
		if name := getTypeDefName(stmt); name != "" && !strings.Contains(name, ".") {
			moduleTypes[name] = true
		}
	}

	// --- Phase 2: rename type definitions & register in typeOwner ---
	// key = "module.name"，value = bareName。
	// 同名結構體各自有獨立條目（如 "net.client" 和 "ws.client"），不會覆蓋。
	for _, stmt := range stmts {
		name := getTypeDefName(stmt)
		if name == "" || !moduleTypes[name] {
			continue
		}
		newName := moduleShortName + "." + name
		renameTypeDef(stmt, newName)
		typeOwner[newName] = name
	}

	// --- Phase 3: rename method definitions defined in THIS module ---
	// 同模組方法定義（Type.method → module.Type.method）在此直接重命名，
	// 不依賴後續 prefixMethodNames 的全局 typeOwner 查找。這樣即使多個模組
	// 定義同名結構體，各自的方法也能正確攜帶本模組前綴。
	// 跨模組方法定義（接收者類型在 A 模組，方法定義在 B 模組）仍由
	// prefixMethodNames 處理。
	for _, stmt := range stmts {
		fd, ok := stmt.(*parser.FunctionDefinition)
		if !ok {
			continue
		}
		dotIdx := strings.Index(fd.Name, ".")
		if dotIdx <= 0 {
			continue
		}
		typePrefix := fd.Name[:dotIdx]
		if strings.Contains(typePrefix, ".") {
			continue
		}
		if moduleTypes[typePrefix] {
			fd.Name = moduleShortName + "." + fd.Name
		}
	}

	// Note: cross-module method renaming (method defined in B but receiver
	// struct in A) is handled by prefixMethodNames() after ALL modules merged.
}

// prefixMethodNames renames method function names across the merged program
// using the fully-built typeOwner registry.  This handles the case where a
// method is defined in module B but its receiver struct is defined in module A
// (e.g. reader.fill defined in bufio.no, but reader defined in io.no).
//
// Must be called AFTER all modules have been merged and typeOwner is complete.
//
// typeOwner 的 key 為 "module.name"，value 為 bareName。對於跨模組方法定義
// （接收者型別為 bare name），遍歷 typeOwner 查找 value 匹配的條目：
//   - 恰好一個匹配 → 用 "module.name" 作為前綴
//   - 多個匹配（同名結構體跨模組）→ 有歧義，保持原樣要求用戶顯式指定
//   - 零個匹配 → 不處理
func prefixMethodNames(stmts []parser.Statement, typeOwner map[string]string) {
	if len(typeOwner) == 0 {
		return
	}
	for _, stmt := range stmts {
		fd, ok := stmt.(*parser.FunctionDefinition)
		if !ok {
			continue
		}
		dotIdx := strings.Index(fd.Name, ".")
		if dotIdx <= 0 {
			continue
		}
		typePrefix := fd.Name[:dotIdx]
		// Skip if the type prefix already contains a dot (already prefixed)
		if strings.Contains(typePrefix, ".") {
			continue
		}
		// 遍歷 typeOwner 查找 value == typePrefix 的所有 "module.name" 鍵
		match := ""
		count := 0
		for k, v := range typeOwner {
			if v == typePrefix {
				match = k
				count++
			}
		}
		if count == 1 {
			// 恰好一個模組定義該型別，用 "module.name" 前綴
			fd.Name = match + "." + fd.Name
		}
		// count == 0：無定義，不處理
		// count > 1：同名結構體跨模組，bare name 有歧義，保持原樣
	}
}

// rewriteTypeRefs walks every statement in *stmts* and rewrites type
// references so that bare names registered in typeOwner are replaced by
// `module.name`.  This covers:
//   - NamedType.Value inside parameter / field / result / alias types
//   - StructLiteral.Type (string)
//   - StructDefinition.Implements and InterfaceDefinition.Implements ([]string)
//   - FunctionDefinition body statements (let declarations, cast expressions, …)
func rewriteTypeRefs(stmts []parser.Statement, typeOwner map[string]string) {
	if len(typeOwner) == 0 {
		return
	}
	for _, stmt := range stmts {
		rewriteTypeRefsInStmt(stmt, typeOwner)
	}
}

// prefixTypeName returns the module-prefixed form of a type name, or the
// original string if the name is built-in or unknown.
//
// typeOwner 的 key 為 "module.name"，value 為 bareName。查找規則：
//   - name 已含 "."：視為已帶模組前綴，直接返回（typeOwner 中可能記錄也可能未記錄）
//   - name 為 bare name：遍歷 typeOwner 查找 value == name 的鍵
//     - 恰好一個匹配 → 返回 "module.name"
//     - 多個匹配（同名結構體跨模組）→ bare name 無法消歧，保持原樣
//     - 零個匹配 → 保持原樣
func prefixTypeName(name string, typeOwner map[string]string) string {
	if name == "" || isModulePrefixBuiltinType(name) {
		return name
	}
	// 已含 "."：已帶模組前綴，直接返回
	if strings.Contains(name, ".") {
		return name
	}
	// bare name：遍歷 typeOwner 查找 value == name 的鍵
	match := ""
	count := 0
	for k, v := range typeOwner {
		if v == name {
			match = k
			count++
		}
	}
	if count == 1 {
		return match
	}
	// count == 0：未知型別，保持原樣
	// count > 1：同名結構體跨模組，bare name 無法消歧，保持原樣
	return name
}

// ---------------------------------------------------------------------------
// Statement walker
// ---------------------------------------------------------------------------

func rewriteTypeRefsInStmt(stmt parser.Statement, typeOwner map[string]string) {
	if stmt == nil {
		return
	}
	switch s := stmt.(type) {
	case *parser.FunctionDefinition:
		rewriteTypeRefsInFuncSig(&s.FuncSignature, typeOwner)
		if s.Body != nil {
			for _, bs := range s.Body.Statements {
				rewriteTypeRefsInStmt(bs, typeOwner)
			}
		}
	case *parser.LetStatement:
		if s.Type != nil {
			rewriteTypeRef(s.Type, typeOwner)
		}
		if s.Value != nil {
			rewriteTypeRefsInExpr(s.Value, typeOwner)
		}
	case *parser.MultiAssignStatement:
		if s.Value != nil {
			rewriteTypeRefsInExpr(s.Value, typeOwner)
		}
		// Target types (if any Type annotations) — MultiAssign targets are expressions
		for _, tgt := range s.Targets {
			rewriteTypeRefsInExpr(tgt, typeOwner)
		}
	case *parser.ExpressionStatement:
		if s.Expression != nil {
			rewriteTypeRefsInExpr(s.Expression, typeOwner)
		}
	case *parser.ForStatement:
		if s.Init != nil {
			rewriteTypeRefsInStmt(s.Init, typeOwner)
		}
		if s.Condition != nil {
			rewriteTypeRefsInExpr(s.Condition, typeOwner)
		}
		if s.Update != nil {
			rewriteTypeRefsInStmt(s.Update, typeOwner)
		}
		if s.IterRange != nil {
			if s.IterRange.Range != nil {
				rewriteTypeRefsInExpr(s.IterRange.Range, typeOwner)
			}
			if s.IterRange.RangeExpr != nil {
				rewriteTypeRefsInExpr(s.IterRange.RangeExpr, typeOwner)
			}
		}
		if s.CountExpr != nil {
			rewriteTypeRefsInExpr(s.CountExpr, typeOwner)
		}
		if s.Body != nil {
			for _, bs := range s.Body.Statements {
				rewriteTypeRefsInStmt(bs, typeOwner)
			}
		}
	case *parser.BlockStatement:
		for _, bs := range s.Statements {
			rewriteTypeRefsInStmt(bs, typeOwner)
		}
	case *parser.ReturnStatement:
		if s.ReturnValue != nil {
			rewriteTypeRefsInExpr(s.ReturnValue, typeOwner)
		}
	case *parser.BreakStatement:
		// no type refs
	case *parser.ContinueStatement:
		// no type refs
	case *parser.ExternStatement:
		// Extern parameters/results may reference struct types
		for _, p := range s.Parameters {
			if p.Type != nil {
				rewriteTypeRef(p.Type, typeOwner)
			}
		}
		for _, r := range s.Results {
			if r.Type != nil {
				rewriteTypeRef(r.Type, typeOwner)
			}
		}
	}

	// Handle type-definition statements (struct / interface / alias / enum / tagged-enum)
	rewriteTypeRefsInTypeDef(stmt, typeOwner)
}

// rewriteTypeRefsInTypeDef handles type-definition statements.
func rewriteTypeRefsInTypeDef(stmt parser.Statement, typeOwner map[string]string) {
	switch s := stmt.(type) {
	case *parser.StructDefinition:
		// Rewrite field types
		for _, f := range s.Fields {
			if f.Type != nil {
				rewriteTypeRef(f.Type, typeOwner)
			}
		}
		// Rewrite Implements list
		for i, impl := range s.Implements {
			s.Implements[i] = prefixTypeName(impl, typeOwner)
		}
	case *parser.InterfaceDefinition:
		// Rewrite method signatures
		for _, m := range s.Methods {
			for _, p := range m.Parameters {
				if p.Type != nil {
					rewriteTypeRef(p.Type, typeOwner)
				}
			}
			for _, r := range m.Results {
				if r.Type != nil {
					rewriteTypeRef(r.Type, typeOwner)
				}
			}
		}
		// Rewrite Implements list
		for i, impl := range s.Implements {
			s.Implements[i] = prefixTypeName(impl, typeOwner)
		}
	case *parser.TypeAlias:
		if s.Type != nil {
			rewriteTypeRef(s.Type, typeOwner)
		}
		if s.Union != nil {
			for _, t := range s.Union.Types {
				rewriteTypeRef(t, typeOwner)
			}
		}
	case *parser.TaggedEnumDefinition:
		for _, v := range s.Variants {
			if v.Type != nil {
				rewriteTypeRef(v.Type, typeOwner)
			}
		}
	}
}

// rewriteTypeRefsInFuncSig rewrites type references in a FuncSignature.
func rewriteTypeRefsInFuncSig(sig *parser.FuncSignature, typeOwner map[string]string) {
	for _, p := range sig.Parameters {
		if p.Type != nil {
			rewriteTypeRef(p.Type, typeOwner)
		}
	}
	for _, r := range sig.Results {
		if r.Type != nil {
			rewriteTypeRef(r.Type, typeOwner)
		}
	}
}

// ---------------------------------------------------------------------------
// Type node rewriter
// ---------------------------------------------------------------------------

func rewriteTypeRef(t parser.Type, typeOwner map[string]string) {
	if t == nil {
		return
	}
	switch ty := t.(type) {
	case *parser.NamedType:
		ty.Value = prefixTypeName(ty.Value, typeOwner)
	case *parser.ArrayType:
		rewriteTypeRef(ty.Elem, typeOwner)
	case *parser.SliceType:
		rewriteTypeRef(ty.Elem, typeOwner)
	case *parser.MapType:
		rewriteTypeRef(ty.Key, typeOwner)
		rewriteTypeRef(ty.Value, typeOwner)
	case *parser.NullableType:
		rewriteTypeRef(ty.Type, typeOwner)
	case *parser.PointerType:
		rewriteTypeRef(ty.Type, typeOwner)
	case *parser.UnionType:
		for _, ut := range ty.Types {
			rewriteTypeRef(ut, typeOwner)
		}
	case *parser.FunctionType:
		for _, p := range ty.Params {
			if p.Type != nil {
				rewriteTypeRef(p.Type, typeOwner)
			}
		}
		for _, r := range ty.Results {
			if r.Type != nil {
				rewriteTypeRef(r.Type, typeOwner)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Expression walker
// ---------------------------------------------------------------------------

func rewriteTypeRefsInExpr(expr parser.Expression, typeOwner map[string]string) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case *parser.Identifier:
		// no type ref
	case *parser.IntegerLiteral:
	case *parser.FloatLiteral:
	case *parser.StringLiteral:
	case *parser.CharLiteral:
	case *parser.BooleanLiteral:
	case *parser.NilLiteral:
	case *parser.PrefixExpression:
		rewriteTypeRefsInExpr(e.Right, typeOwner)
	case *parser.InfixExpression:
		rewriteTypeRefsInExpr(e.Left, typeOwner)
		rewriteTypeRefsInExpr(e.Right, typeOwner)
	case *parser.ConditionalExpression:
		rewriteTypeRefsInExpr(e.Condition, typeOwner)
		rewriteTypeRefsInExpr(e.Consequence, typeOwner)
		rewriteTypeRefsInExpr(e.Alternative, typeOwner)
	case *parser.GroupedExpression:
		rewriteTypeRefsInExpr(e.Expression, typeOwner)
	case *parser.CallExpression:
		rewriteTypeRefsInExpr(e.Function, typeOwner)
		for _, arg := range e.Arguments {
			rewriteTypeRefsInExpr(arg, typeOwner)
		}
	case *parser.DotExpression:
		rewriteTypeRefsInExpr(e.Receiver, typeOwner)
	case *parser.IndexExpression:
		rewriteTypeRefsInExpr(e.Left, typeOwner)
		rewriteTypeRefsInExpr(e.Index, typeOwner)
	case *parser.SliceExpression:
		rewriteTypeRefsInExpr(e.Left, typeOwner)
		if e.Range.Start != nil {
			rewriteTypeRefsInExpr(e.Range.Start, typeOwner)
		}
		if e.Range.End != nil {
			rewriteTypeRefsInExpr(e.Range.End, typeOwner)
		}
	case *parser.AssignExpression:
		rewriteTypeRefsInExpr(e.Left, typeOwner)
		rewriteTypeRefsInExpr(e.Value, typeOwner)
	case *parser.RangeExpression:
		rewriteTypeRefsInExpr(e.Start, typeOwner)
		rewriteTypeRefsInExpr(e.End, typeOwner)
	case *parser.IfExpression:
		rewriteTypeRefsInExpr(e.Condition, typeOwner)
		if e.Consequence != nil {
			for _, bs := range e.Consequence.Statements {
				rewriteTypeRefsInStmt(bs, typeOwner)
			}
		}
		if e.Alternative != nil {
			for _, bs := range e.Alternative.Statements {
				rewriteTypeRefsInStmt(bs, typeOwner)
			}
		}
	case *parser.FunctionLiteral:
		rewriteTypeRefsInFuncSig(&e.FuncSignature, typeOwner)
		if e.Body != nil {
			for _, bs := range e.Body.Statements {
				rewriteTypeRefsInStmt(bs, typeOwner)
			}
		}
	case *parser.StructLiteral:
		// StructLiteral.Type is a string — rewrite it
		e.Type = prefixTypeName(e.Type, typeOwner)
		for _, f := range e.Fields {
			if f.Type != nil {
				rewriteTypeRef(f.Type, typeOwner)
			}
			if f.Value != nil {
				rewriteTypeRefsInExpr(f.Value, typeOwner)
			}
		}
	case *parser.CastExpression:
		rewriteTypeRefsInExpr(e.Expr, typeOwner)
		if e.Type != nil {
			rewriteTypeRef(e.Type, typeOwner)
		}
	case *parser.RunExpression:
		rewriteTypeRefsInExpr(e.Call, typeOwner)
	case *parser.AwaitExpression:
		rewriteTypeRefsInExpr(e.Right, typeOwner)
	case *parser.MapLiteral:
		if e.MapType != nil {
			rewriteTypeRef(e.MapType, typeOwner)
		}
		for _, pair := range e.Pairs {
			rewriteTypeRefsInExpr(pair.Key, typeOwner)
			rewriteTypeRefsInExpr(pair.Value, typeOwner)
		}
	case *parser.ArrayLiteral:
		for _, elem := range e.Elements {
			rewriteTypeRefsInExpr(elem, typeOwner)
		}
	}
}
