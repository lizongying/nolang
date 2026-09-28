package checker

// listdir.go — 防「递归删除沿 .. 上爬」类事故（nolang-list-dir-unfiltered）。
//
// 背景：fs.list-dir 遵循 POSIX readdir，返回值【包含】 "." 和 ".."。若把它的结果
// 用于「递归遍历 + 破坏性删除(fs.rmdir/fs.remove)」，一旦进入 ".." 就向上逃逸出
// 目标目录，删除途经的每一层——这是 remove-tree 事故的根因。fs.dir-entries 已过滤
// "."/".."，是安全替代。
//
// 仅在「足以致命」的组合上报警，极低误报：函数同时满足
//   ① 调用 fs.list-dir；② 调用破坏性删除(fs.rmdir/fs.remove) 或对自身递归；
//   ③ 函数体内没有任何 "."/".." 过滤守卫。
// 正确写法（zip/find/du 手动跳过 "."/".."，rm 用 open-dir/read-dir）不满足该组合。
//
// 注意 callee 形态：未合并(LSP/单测)时 `fs.list-dir` 是 DotExpression；模组合并
// (no vet)后可能成为 Value="fs.list-dir" 的点分 Identifier。故统一按“点号最后
// 一段(base)”识别，两种形态都能命中。
//
// 严重级别：WARNING（不阻断编译；--strict 升级为 error）。

import (
	"strings"

	"github.com/lizongying/nolang/parser"
)

const listDirTraceID = "nolang-list-dir-unfiltered"

// calleeParts 从调用目标提取 (完整点分名, 基础名=最后一段)。
func calleeParts(fn parser.Expression) (full, base string) {
	switch f := fn.(type) {
	case *parser.Identifier:
		full = f.Value
		base = lastDotSegment(f.Value)
	case *parser.DotExpression:
		base = f.Property
		if id, ok := f.Receiver.(*parser.Identifier); ok {
			full = id.Value + "." + f.Property
		} else {
			full = f.Property
		}
	}
	return
}

func lastDotSegment(s string) string {
	if i := strings.LastIndexByte(s, '.'); i >= 0 {
		return s[i+1:]
	}
	return s
}

func isFsListDir(base string) bool { return base == "list-dir" }
func isDestructiveFs(base string) bool {
	return base == "rmdir" || base == "remove" || base == "remove-all"
}

// ValidateListDirUnfiltered 报告「递归/删除 + fs.list-dir 且未过滤 "."/".."」的函数。
func ValidateListDirUnfiltered(program *parser.Program) []ValidateResult {
	if program == nil {
		return nil
	}
	var results []ValidateResult
	for _, stmt := range program.Statements {
		fd, ok := stmt.(*parser.FunctionDefinition)
		if !ok || fd.Body == nil {
			continue
		}
		selfBase := lastDotSegment(fd.Name)
		v := &listDirVisitor{selfName: fd.Name, selfBase: selfBase}
		v.walkBlock(fd.Body)
		if v.hasListDir && (v.hasDestructive || v.recursive) && !v.hasDotFilter {
			results = append(results, ValidateResult{
				Line:    fd.Pos().Line,
				Column:  fd.Pos().Column,
				File:    parser.GetSourceFile(fd),
				Message: "函數 " + fd.Name + " 用 fs.list-dir 結果做遞歸/刪除，但未見過濾 \".\" 和 \"..\"；fs.list-dir 含 POSIX 的 \".\"/\"..\"，遞歸進入 \"..\" 會向上逃逸誤刪整棵父樹。請改用 fs.dir-entries（已自動過濾），或在遞歸前顯式跳過 \".\"/\"..\"。",
				TraceID: listDirTraceID,
			})
		}
	}
	return results
}

type listDirVisitor struct {
	selfName       string
	selfBase       string
	hasListDir     bool
	hasDestructive bool
	recursive      bool
	hasDotFilter   bool
}

func (v *listDirVisitor) walkBlock(b *parser.BlockStatement) {
	if b == nil {
		return
	}
	for _, s := range b.Statements {
		v.walkStmt(s)
	}
}

func (v *listDirVisitor) walkStmt(s parser.Statement) {
	if s == nil {
		return
	}
	switch st := s.(type) {
	case *parser.FunctionDefinition:
		v.walkBlock(st.Body)
	case *parser.BlockStatement:
		v.walkBlock(st)
	case *parser.ForStatement:
		v.walkBlock(st.Body)
		if st.Condition != nil {
			v.walkExpr(st.Condition)
		}
	case *parser.LetStatement:
		if st.Value != nil {
			v.walkExpr(st.Value)
		}
	case *parser.ExpressionStatement:
		if st.Expression != nil {
			v.walkExpr(st.Expression)
		}
	case *parser.MultiAssignStatement:
		if st.Value != nil {
			v.walkExpr(st.Value)
		}
	case *parser.ReturnStatement:
		if st.ReturnValue != nil {
			v.walkExpr(st.ReturnValue)
		}
	}
}

func (v *listDirVisitor) walkIf(ie *parser.IfExpression) {
	if ie == nil {
		return
	}
	if ie.Condition != nil {
		v.walkExpr(ie.Condition)
	}
	v.walkBlock(ie.Consequence)
	v.walkBlock(ie.Alternative)
}

func (v *listDirVisitor) walkExpr(e parser.Expression) {
	if e == nil {
		return
	}
	switch ex := e.(type) {
	case *parser.CallExpression:
		full, base := calleeParts(ex.Function)
		if isFsListDir(base) {
			v.hasListDir = true
		}
		if isDestructiveFs(base) {
			v.hasDestructive = true
		}
		if full == v.selfName || base == v.selfBase {
			v.recursive = true
		}
		for _, a := range ex.Arguments {
			v.walkExpr(a)
		}
	case *parser.IfExpression:
		v.walkIf(ex)
	case *parser.InfixExpression:
		v.walkExpr(ex.Left)
		v.walkExpr(ex.Right)
	case *parser.PrefixExpression:
		v.walkExpr(ex.Right)
	case *parser.GroupedExpression:
		v.walkExpr(ex.Expression)
	case *parser.IndexExpression:
		v.walkExpr(ex.Left)
		v.walkExpr(ex.Index)
	case *parser.AssignExpression:
		if ex.Value != nil {
			v.walkExpr(ex.Value)
		}
	case *parser.StringLiteral:
		if ex.Value == "." || ex.Value == ".." {
			v.hasDotFilter = true
		}
	case *parser.CharLiteral:
		if ex.Value == "." {
			v.hasDotFilter = true
		}
	}
}
