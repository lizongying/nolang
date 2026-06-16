package no

import (
	"fmt"
	"strings"

	"github.com/lizongying/nolang/parser"
)

type Generator struct {
	indentLevel    int
	declaredVars   map[string]bool
	inFunctionBody bool
}

func NewGenerator() *Generator {
	return &Generator{
		declaredVars: make(map[string]bool),
	}
}

func (g *Generator) indent() string {
	return strings.Repeat("\t", g.indentLevel)
}

func (g *Generator) Generate(program *parser.Program) string {
	var sb strings.Builder

	// 首先生成所有结构体定义
	for _, stmt := range program.Statements {
		if sd, ok := stmt.(*parser.StructDefinition); ok {
			sb.WriteString(g.generateStructDefinition(sd))
			sb.WriteString("\n")
		}
	}

	// 然后生成所有函数定义
	for _, stmt := range program.Statements {
		if fd, ok := stmt.(*parser.FunctionDefinition); ok {
			sb.WriteString(g.generateFunctionDefinition(fd))
			sb.WriteString("\n")
		}
	}

	// 生成顶层语句（不在函数内的代码）
	hasTopLevelCode := false
	for _, stmt := range program.Statements {
		if _, ok := stmt.(*parser.FunctionDefinition); !ok {
			if _, ok := stmt.(*parser.StructDefinition); !ok {
				hasTopLevelCode = true
				break
			}
		}
	}

	if hasTopLevelCode {
		sb.WriteString("// Auto-generated main entry\n")
		sb.WriteString("main() {\n")
		g.indentLevel++

		for _, stmt := range program.Statements {
			if _, ok := stmt.(*parser.FunctionDefinition); !ok {
				if _, ok := stmt.(*parser.StructDefinition); !ok {
					sb.WriteString(g.generateStatement(stmt))
				}
			}
		}

		g.indentLevel--
		sb.WriteString("}\n")
	}

	return sb.String()
}

func (g *Generator) generateStatement(stmt parser.Statement) string {
	switch s := stmt.(type) {
	case *parser.LetStatement:
		return g.generateLetStatement(s)
	case *parser.ReturnStatement:
		return g.generateReturnStatement(s)
	case *parser.ExpressionStatement:
		return g.generateExpressionStatement(s)
	case *parser.BlockStatement:
		return g.generateBlockStatement(s)
	case *parser.FunctionDefinition:
		return g.generateFunctionDefinition(s)
	case *parser.ForStatement:
		return g.generateForStatement(s)
	case *parser.BreakStatement:
		if s.Label != "" {
			return g.indent() + "break " + s.Label + "\n"
		}
		return g.indent() + "break\n"
	case *parser.ContinueStatement:
		if s.Label != "" {
			return g.indent() + "continue " + s.Label + "\n"
		}
		return g.indent() + "continue\n"
	case *parser.InterfaceDefinition:
		return g.generateInterfaceDefinition(s)
	case *parser.EnumDefinition:
		return g.generateEnumDefinition(s)
	case *parser.StructDefinition:
		return g.generateStructDefinition(s)
	default:
		return ""
	}
}

func (g *Generator) generateLetStatement(stmt *parser.LetStatement) string {
	var sb strings.Builder
	sb.WriteString(g.indent())

	name := stmt.Name.Value

	// 如果有类型标注
	if stmt.Type != nil {
		sb.WriteString(name)
		sb.WriteString(" ")
		sb.WriteString(stmt.Type.String())
		sb.WriteString(" = ")
	} else {
		sb.WriteString(name)
		sb.WriteString(" = ")
	}

	sb.WriteString(g.generateExpression(stmt.Value))
	sb.WriteString("\n")

	return sb.String()
}

func (g *Generator) generateReturnStatement(stmt *parser.ReturnStatement) string {
	if stmt.ReturnValue != nil {
		return g.indent() + "return " + g.generateExpression(stmt.ReturnValue) + "\n"
	}
	return g.indent() + "return\n"
}

func (g *Generator) generateExpressionStatement(stmt *parser.ExpressionStatement) string {
	var sb strings.Builder
	sb.WriteString(g.indent())
	sb.WriteString(g.generateExpression(stmt.Expression))
	sb.WriteString("\n")
	return sb.String()
}

func (g *Generator) generateBlockStatement(stmt *parser.BlockStatement) string {
	var sb strings.Builder
	sb.WriteString(g.indent())
	sb.WriteString("{")
	g.indentLevel++
	sb.WriteString("\n")

	for _, s := range stmt.Statements {
		sb.WriteString(g.generateStatement(s))
	}

	g.indentLevel--
	sb.WriteString(g.indent())
	sb.WriteString("}")
	return sb.String()
}

func (g *Generator) generateExpression(expr parser.Expression) string {
	switch e := expr.(type) {
	case *parser.Identifier:
		return e.Value
	case *parser.ArrayLiteral:
		return g.generateArrayLiteral(e)
	case *parser.SliceLiteral:
		return g.generateSliceLiteral(e)
	case *parser.StructLiteral:
		return g.generateStructLiteral(e)
	case *parser.IntegerLiteral:
		return fmt.Sprintf("%d", e.Value)
	case *parser.ByteLiteral:
		return fmt.Sprintf("%d", e.Value)
	case *parser.FloatLiteral:
		return fmt.Sprintf("%g", e.Value)
	case *parser.StringLiteral:
		return fmt.Sprintf("'%s'", e.Value)
	case *parser.BooleanLiteral:
		return fmt.Sprintf("%t", e.Value)
	case *parser.NilLiteral:
		return "nil"
	case *parser.PrefixExpression:
		return g.generatePrefixExpression(e)
	case *parser.InfixExpression:
		return g.generateInfixExpression(e)
	case *parser.DotExpression:
		return g.generateDotExpression(e)
	case *parser.SliceExpression:
		return g.generateSliceExpression(e)
	case *parser.IndexExpression:
		return g.generateIndexExpression(e)
	case *parser.IfExpression:
		return g.generateIfExpression(e)
	case *parser.ConditionalExpression:
		return g.generateConditionalExpression(e)
	case *parser.FunctionLiteral:
		return g.generateFunctionLiteral(e)
	case *parser.CallExpression:
		return g.generateCallExpression(e)
	case *parser.NullableType:
		return g.generateNullableType(e)
	case *parser.PointerType:
		return "ptr(" + e.Type.String() + ")"
	case *parser.GroupedExpression:
		return g.generateExpression(e.Expression)
	default:
		return ""
	}
}

func (g *Generator) generateDotExpression(expr *parser.DotExpression) string {
	return fmt.Sprintf("%s.%s", g.generateExpression(expr.Receiver), expr.Property)
}

func (g *Generator) generateSliceExpression(expr *parser.SliceExpression) string {
	left := g.generateExpression(expr.Left)
	r := expr.Range

	open := expr.Token.Literal // "[" or "("
	close := "]"
	if r != nil && !r.RightInc {
		close = ")"
	}

	startStr := ""
	endStr := ""
	if r != nil {
		if r.Start != nil {
			startStr = g.generateExpression(r.Start)
		}
		if r.End != nil {
			endStr = g.generateExpression(r.End)
		}
	}

	return fmt.Sprintf("%s%s%s..%s%s", left, open, startStr, endStr, close)
}

func (g *Generator) generateIndexExpression(expr *parser.IndexExpression) string {
	return fmt.Sprintf("%s[%s]", g.generateExpression(expr.Left), g.generateExpression(expr.Index))
}

func (g *Generator) generatePrefixExpression(expr *parser.PrefixExpression) string {
	return fmt.Sprintf("%s%s", expr.Operator, g.generateExpression(expr.Right))
}

func (g *Generator) generateInfixExpression(expr *parser.InfixExpression) string {
	left := g.generateExpression(expr.Left)
	right := g.generateExpression(expr.Right)
	return fmt.Sprintf("%s %s %s", left, expr.Operator, right)
}

func (g *Generator) generateIfExpression(expr *parser.IfExpression) string {
	var sb strings.Builder
	sb.WriteString("if ")
	sb.WriteString(g.generateExpression(expr.Condition))
	sb.WriteString(" ")
	sb.WriteString(g.generateBlockStatement(expr.Consequence))

	if expr.Alternative != nil {
		sb.WriteString(" else ")
		sb.WriteString(g.generateBlockStatement(expr.Alternative))
	}

	return sb.String()
}

func (g *Generator) generateConditionalExpression(expr *parser.ConditionalExpression) string {
	return fmt.Sprintf("%s ? %s : %s",
		g.generateExpression(expr.Condition),
		g.generateExpression(expr.Consequence),
		g.generateExpression(expr.Alternative))
}

func (g *Generator) generateFunctionLiteral(expr *parser.FunctionLiteral) string {
	var sb strings.Builder
	sb.WriteString("(")

	for i, param := range expr.Parameters {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(param.Name)
		if param.Type != nil {
			sb.WriteString(" ")
			sb.WriteString(param.Type.String())
		}
	}

	sb.WriteString(") ")
	sb.WriteString(g.generateBlockStatement(expr.Body))

	return sb.String()
}

func (g *Generator) generateFunctionDefinition(fd *parser.FunctionDefinition) string {
	var sb strings.Builder
	sb.WriteString(fd.Name)
	sb.WriteString("(")

	for i, param := range fd.Parameters {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(param.Name)
		if param.Type != nil {
			sb.WriteString(" ")
			sb.WriteString(param.Type.String())
		}
	}

	sb.WriteString(") ")
	sb.WriteString(g.generateBlockStatement(fd.Body))
	sb.WriteString("\n")

	return sb.String()
}

func (g *Generator) generateCallExpression(expr *parser.CallExpression) string {
	var sb strings.Builder
	sb.WriteString(g.generateExpression(expr.Function))
	sb.WriteString("(")

	for i, arg := range expr.Arguments {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(g.generateExpression(arg))
	}

	sb.WriteString(")")
	return sb.String()
}

func (g *Generator) generateNullableType(expr *parser.NullableType) string {
	return fmt.Sprintf("?%s", expr.Type.String())
}

func (g *Generator) generateForStatement(stmt *parser.ForStatement) string {
	var sb strings.Builder
	sb.WriteString(g.indent())
	sb.WriteString("for")

	// 条件循环 for condition { }
	if stmt.Condition != nil {
		sb.WriteString(" ")
		sb.WriteString(g.generateExpression(stmt.Condition))
	}

	sb.WriteString(" ")
	sb.WriteString(g.generateBlockStatement(stmt.Body))
	sb.WriteString("\n")
	return sb.String()
}

func (g *Generator) generateArrayLiteral(arr *parser.ArrayLiteral) string {
	var sb strings.Builder
	sb.WriteString(g.generateExpression(arr.Size))
	sb.WriteString("[")

	for i, elem := range arr.Elements {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(g.generateExpression(elem))
	}

	sb.WriteString("]")
	return sb.String()
}

func (g *Generator) generateSliceLiteral(slice *parser.SliceLiteral) string {
	var sb strings.Builder
	sb.WriteString("[")

	for i, elem := range slice.Elements {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(g.generateExpression(elem))
	}

	sb.WriteString("]")
	return sb.String()
}

func (g *Generator) generateEnumDefinition(ed *parser.EnumDefinition) string {
	var sb strings.Builder
	sb.WriteString(ed.Name)
	sb.WriteString(" {\n")
	g.indentLevel++
	for _, v := range ed.Values {
		sb.WriteString(g.indent())
		sb.WriteString(v.Name)
		sb.WriteString(",\n")
	}
	g.indentLevel--
	sb.WriteString("}\n")
	return sb.String()
}

func (g *Generator) generateInterfaceDefinition(id *parser.InterfaceDefinition) string {
	var sb strings.Builder
	sb.WriteString(id.Name)
	sb.WriteString(" {\n")
	g.indentLevel++
	for _, m := range id.Methods {
		sb.WriteString(g.indent())
		sb.WriteString(m.Name)
		sb.WriteString("()\n")
	}
	g.indentLevel--
	sb.WriteString("}\n")
	return sb.String()
}

func (g *Generator) generateStructDefinition(sd *parser.StructDefinition) string {
	var sb strings.Builder
	sb.WriteString(sd.Name)
	for i, iface := range sd.Implements {
		if i == 0 {
			sb.WriteString(" ")
		} else {
			sb.WriteString(", ")
		}
		sb.WriteString(iface)
	}
	sb.WriteString(" {\n")

	g.indentLevel++
	for _, field := range sd.Fields {
		sb.WriteString(g.indent())
		sb.WriteString(field.Name)
		sb.WriteString(" ")
		if field.ArraySize > 0 {
			sb.WriteString(fmt.Sprintf("[%d]", field.ArraySize))
			if field.Type != nil {
				sb.WriteString(field.Type.String())
			}
		} else if field.IsSlice {
			sb.WriteString("[]")
			if field.Type != nil {
				sb.WriteString(field.Type.String())
			}
		} else if field.Type != nil {
			sb.WriteString(field.Type.String())
		} else {
			sb.WriteString("i64")
		}
		sb.WriteString("\n")
	}
	g.indentLevel--

	sb.WriteString("}")
	return sb.String()
}

func (g *Generator) generateStructLiteral(sl *parser.StructLiteral) string {
	var sb strings.Builder
	sb.WriteString(sl.Type)
	sb.WriteString(" {")

	for i, field := range sl.Fields {
		if i > 0 {
			sb.WriteString(" ")
		}
		sb.WriteString(field.Name)
		sb.WriteString(": ")
		sb.WriteString(g.generateExpression(field.Value))
	}

	sb.WriteString(" }")
	return sb.String()
}
