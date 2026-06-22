package dump

import (
	"fmt"
	"strings"

	"github.com/lizongying/nolang/parser"
)

// Dump returns a debug string representation of an AST node.
func Dump(node parser.Node) string {
	var buf strings.Builder
	dumpNode(&buf, node, 0)
	return strings.TrimRight(buf.String(), "\n")
}

func indent(depth int) string {
	return strings.Repeat("    ", depth)
}

func dumpNode(buf *strings.Builder, node parser.Node, depth int) {
	if node == nil {
		fmt.Fprintf(buf, "%s<nil>\n", indent(depth))
		return
	}

	switch n := node.(type) {
	case *parser.Program:
		fmt.Fprintf(buf, "%sProgram\n", indent(depth))
		for _, s := range n.Statements {
			dumpNode(buf, s, depth+1)
		}
		if len(n.FreeComments) > 0 {
			fmt.Fprintf(buf, "%sFreeComments: %d group(s)\n", indent(depth+1), len(n.FreeComments))
		}
		if n.TrailingComments != nil {
			fmt.Fprintf(buf, "%sTrailingComments\n", indent(depth+1))
			dumpCommentGroup(buf, n.TrailingComments, depth+2)
		}

	case *parser.UseStatement:
		fmt.Fprintf(buf, "%sUseStatement{path: %s, function: %s, alias: %s}\n",
			indent(depth), n.Path, n.Function, n.Alias)
		dumpComments(buf, n, depth+1)

	case *parser.ExportStatement:
		fmt.Fprintf(buf, "%sExportStatement{path: %s, function: %s, alias: %s}\n",
			indent(depth), n.Path, n.Function, n.Alias)
		dumpComments(buf, n, depth+1)

	case *parser.LetStatement:
		typeStr := ""
		if n.Type != nil {
			typeStr = n.Type.String()
		}
		valStr := ""
		if n.Value != nil {
			valStr = nodeString(n.Value)
		}
		fmt.Fprintf(buf, "%sLetStatement{name: %s, type: %s, value: %s}\n",
			indent(depth), n.Name.Value, typeStr, valStr)
		dumpComments(buf, n, depth+1)
		if n.Value != nil {
			dumpNode(buf, n.Value, depth+1)
		}

	case *parser.ReturnStatement:
		if n.ReturnValue != nil {
			fmt.Fprintf(buf, "%sReturnStatement{%s}\n", indent(depth), nodeString(n.ReturnValue))
			dumpNode(buf, n.ReturnValue, depth+1)
		} else {
			fmt.Fprintf(buf, "%sReturnStatement{}\n", indent(depth))
		}
		dumpComments(buf, n, depth+1)

	case *parser.ExpressionStatement:
		if n.Expression != nil {
			fmt.Fprintf(buf, "%sExpressionStatement{%s}\n", indent(depth), nodeString(n.Expression))
			dumpNode(buf, n.Expression, depth+1)
		} else {
			fmt.Fprintf(buf, "%sExpressionStatement{nil}\n", indent(depth))
		}
		dumpComments(buf, n, depth+1)

	case *parser.BlockStatement:
		fmt.Fprintf(buf, "%sBlockStatement\n", indent(depth))
		dumpComments(buf, n, depth+1)
		for _, s := range n.Statements {
			dumpNode(buf, s, depth+1)
		}
		if n.TrailingComments != nil {
			fmt.Fprintf(buf, "%sTrailingComments\n", indent(depth+1))
			dumpCommentGroup(buf, n.TrailingComments, depth+2)
		}

	case *parser.FunctionDefinition:
		fmt.Fprintf(buf, "%sFunctionDefinition{name: %s}\n", indent(depth), n.Name)
		dumpComments(buf, n, depth+1)
		for _, p := range n.Parameters {
			fmt.Fprintf(buf, "%sParam{%s %s}\n", indent(depth+1), p.Name, p.Type)
		}
		if n.Body != nil {
			dumpNode(buf, n.Body, depth+1)
		}

	case *parser.FunctionLiteral:
		fmt.Fprintf(buf, "%sFunctionLiteral\n", indent(depth))
		for _, p := range n.Parameters {
			fmt.Fprintf(buf, "%sParam{%s %s}\n", indent(depth+1), p.Name, p.Type)
		}
		if n.Body != nil {
			dumpNode(buf, n.Body, depth+1)
		}

	case *parser.CallExpression:
		fmt.Fprintf(buf, "%sCallExpression{fn: %s, args: %d}\n",
			indent(depth), nodeString(n.Function), len(n.Arguments))
		dumpNode(buf, n.Function, depth+1)
		for _, a := range n.Arguments {
			dumpNode(buf, a, depth+1)
		}

	case *parser.DotExpression:
		fmt.Fprintf(buf, "%sDotExpression{receiver: %s, property: %s}\n",
			indent(depth), nodeString(n.Receiver), n.Property)
		dumpNode(buf, n.Receiver, depth+1)

	case *parser.Identifier:
		fmt.Fprintf(buf, "%sIdentifier{%s}\n", indent(depth), n.Value)

	case *parser.IntegerLiteral:
		fmt.Fprintf(buf, "%sIntegerLiteral{%d}\n", indent(depth), n.Value)

	case *parser.FloatLiteral:
		fmt.Fprintf(buf, "%sFloatLiteral{%v}\n", indent(depth), n.Value)

	case *parser.StringLiteral:
		fmt.Fprintf(buf, "%sStringLiteral{%s}\n", indent(depth), n.Value)

	case *parser.CharLiteral:
		fmt.Fprintf(buf, "%sCharLiteral{%s}\n", indent(depth), n.Value)

	case *parser.ByteLiteral:
		fmt.Fprintf(buf, "%sByteLiteral{%d}\n", indent(depth), n.Value)

	case *parser.BooleanLiteral:
		fmt.Fprintf(buf, "%sBooleanLiteral{%v}\n", indent(depth), n.Value)

	case *parser.NilLiteral:
		fmt.Fprintf(buf, "%sNilLiteral\n", indent(depth))

	case *parser.PrefixExpression:
		fmt.Fprintf(buf, "%sPrefixExpression{%s}\n", indent(depth), n.Operator)
		dumpNode(buf, n.Right, depth+1)

	case *parser.InfixExpression:
		fmt.Fprintf(buf, "%sInfixExpression{%s}\n", indent(depth), n.Operator)
		dumpNode(buf, n.Left, depth+1)
		dumpNode(buf, n.Right, depth+1)

	case *parser.IfExpression:
		fmt.Fprintf(buf, "%sIfExpression\n", indent(depth))
		dumpNode(buf, n.Condition, depth+1)
		if n.Consequence != nil {
			dumpNode(buf, n.Consequence, depth+1)
		}
		if n.Alternative != nil {
			dumpNode(buf, n.Alternative, depth+1)
		}

	case *parser.RangeExpression:
		fmt.Fprintf(buf, "%sRangeExpression{%v..%v}\n", indent(depth),
			nodeString(n.Start), nodeString(n.End))

	case *parser.SliceExpression:
		fmt.Fprintf(buf, "%sSliceExpression\n", indent(depth))
		dumpNode(buf, n.Left, depth+1)
		if n.Range != nil {
			dumpNode(buf, n.Range, depth+1)
		}

	case *parser.IndexExpression:
		fmt.Fprintf(buf, "%sIndexExpression\n", indent(depth))
		dumpNode(buf, n.Left, depth+1)
		dumpNode(buf, n.Index, depth+1)

	case *parser.AssignExpression:
		fmt.Fprintf(buf, "%sAssignExpression\n", indent(depth))
		dumpNode(buf, n.Left, depth+1)
		dumpNode(buf, n.Value, depth+1)

	case *parser.ConditionalExpression:
		fmt.Fprintf(buf, "%sConditionalExpression\n", indent(depth))
		dumpNode(buf, n.Condition, depth+1)
		dumpNode(buf, n.Consequence, depth+1)
		dumpNode(buf, n.Alternative, depth+1)

	case *parser.ForStatement:
		fmt.Fprintf(buf, "%sForStatement{label: %s}\n", indent(depth), n.Label)
		dumpComments(buf, n, depth+1)
		if n.Init != nil {
			dumpNode(buf, n.Init, depth+1)
		}
		if n.Condition != nil {
			dumpNode(buf, n.Condition, depth+1)
		}
		if n.Body != nil {
			dumpNode(buf, n.Body, depth+1)
		}

	case *parser.BreakStatement:
		fmt.Fprintf(buf, "%sBreakStatement{label: %s}\n", indent(depth), n.Label)
		dumpComments(buf, n, depth+1)

	case *parser.ContinueStatement:
		fmt.Fprintf(buf, "%sContinueStatement{label: %s}\n", indent(depth), n.Label)
		dumpComments(buf, n, depth+1)

	case *parser.ArrayLiteral:
		fmt.Fprintf(buf, "%sArrayLiteral{size: %s, elements: %d}\n",
			indent(depth), nodeString(n.Size), len(n.Elements))
		for _, el := range n.Elements {
			dumpNode(buf, el, depth+1)
		}

	case *parser.SliceLiteral:
		fmt.Fprintf(buf, "%sSliceLiteral{elements: %d}\n", indent(depth), len(n.Elements))
		for _, el := range n.Elements {
			dumpNode(buf, el, depth+1)
		}

	case *parser.EnumDefinition:
		fmt.Fprintf(buf, "%sEnumDefinition{name: %s}\n", indent(depth), n.Name)
		dumpComments(buf, n, depth+1)

	case *parser.TaggedEnumDefinition:
		fmt.Fprintf(buf, "%sTaggedEnumDefinition{name: %s}\n", indent(depth), n.Name)
		dumpComments(buf, n, depth+1)

	case *parser.InterfaceDefinition:
		fmt.Fprintf(buf, "%sInterfaceDefinition{name: %s}\n", indent(depth), n.Name)
		dumpComments(buf, n, depth+1)
		for _, m := range n.Methods {
			fmt.Fprintf(buf, "%sMethod{%s}\n", indent(depth+1), m.Name)
		}

	case *parser.StructDefinition:
		fmt.Fprintf(buf, "%sStructDefinition{name: %s}\n", indent(depth), n.Name)
		dumpComments(buf, n, depth+1)

	case *parser.StructLiteral:
		fmt.Fprintf(buf, "%sStructLiteral{type: %s}\n", indent(depth), n.Type)

	case *parser.NullableType:
		fmt.Fprintf(buf, "%sNullableType\n", indent(depth))
		dumpNode(buf, n.Type, depth+1)

	case *parser.PointerType:
		fmt.Fprintf(buf, "%sPointerType\n", indent(depth))
		dumpNode(buf, n.Type, depth+1)

	case *parser.GroupedExpression:
		fmt.Fprintf(buf, "%sGroupedExpression\n", indent(depth))
		dumpNode(buf, n.Expression, depth+1)

	default:
		fmt.Fprintf(buf, "%s<unknown %T>\n", indent(depth), n)
	}
}

func nodeString(node parser.Node) string {
	switch n := node.(type) {
	case *parser.Identifier:
		return n.Value
	case *parser.IntegerLiteral:
		return fmt.Sprintf("%d", n.Value)
	case *parser.FloatLiteral:
		return fmt.Sprintf("%v", n.Value)
	case *parser.StringLiteral:
		return n.Value
	case *parser.CharLiteral:
		return n.Value
	case *parser.ByteLiteral:
		return fmt.Sprintf("%d", n.Value)
	case *parser.BooleanLiteral:
		return fmt.Sprintf("%v", n.Value)
	case *parser.NilLiteral:
		return "nil"
	default:
		return fmt.Sprintf("%T", n)
	}
}

func dumpComments(buf *strings.Builder, stmt parser.Node, depth int) {
	if d, ok := stmt.(interface{ GetDoc() *parser.CommentGroup }); ok {
		doc := d.GetDoc()
		if doc != nil {
			fmt.Fprintf(buf, "%sDoc: ", indent(depth))
			dumpCommentGroup(buf, doc, 0)
		}
	}
	if c, ok := stmt.(interface{ GetComment() *parser.CommentGroup }); ok {
		comment := c.GetComment()
		if comment != nil {
			fmt.Fprintf(buf, "%sComment: ", indent(depth))
			dumpCommentGroupInline(buf, comment)
		}
	}
}

func dumpCommentGroup(buf *strings.Builder, cg *parser.CommentGroup, depth int) {
	for _, c := range cg.List {
		fmt.Fprintf(buf, "%s//%s\n", indent(depth), c.Text)
	}
}

func dumpCommentGroupInline(buf *strings.Builder, cg *parser.CommentGroup) {
	if len(cg.List) > 0 {
		fmt.Fprintf(buf, "//%s\n", cg.List[0].Text)
	}
}
