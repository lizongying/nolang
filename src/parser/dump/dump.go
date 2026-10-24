// Package dump renders a parser AST as an indented, line-oriented text tree.
//
// # Contract
//
// This package is the **L2 oracle** for the self-hosting effort (see
// docs/design/nolang-selfhost.md §5.3/§7.1). `no ast <file>` output is compared
// byte-for-byte against `noc ast <file>`, so the format is a specification, not
// a debugging convenience.
//
// Three rules make the comparison meaningful:
//
//  1. **No node type may fall through to `default`.** A type missing from the
//     switch prints a single `<unknown *parser.X>` line and **its entire subtree
//     disappears** — not "less detail", but invisible. That is how a parser
//     which mangles map literals, multi-assignment, `co`/`run`/`awy` can still
//     report "byte-identical". `dump_completeness_test.go` walks the whole test
//     corpus and fails if any `<unknown>` survives, so this rule is enforced
//     rather than merely intended.
//
//  2. **No child node may be left unvisited.** A node that prints only a
//     summary of itself (e.g. `StructDefinition{name: x}`) hides every member.
//     Every `Node`-typed field and every element of a `Node`-typed slice is
//     recursed into.
//
//  3. **The output must be deterministic.** Nothing here iterates a Go map or
//     depends on pointer values. Note in particular that the annotation
//     side-table is a `map[Node][]*AnnotationEntry`; it is therefore *looked up per
//     node in tree order*, never ranged over (see (*dumper).annotations).
//
// Scalar fields that carry source-level information are always printed. Fields
// that record the parser's internal desugaring state (synthetic bindings,
// inferred flags, propagation markers, …) are printed **only when set**, so the
// common case stays readable while any exceptional state remains visible and
// diffable.
package dump

import (
	"fmt"
	"strings"

	"github.com/lizongying/nolang/parser"
)

// Options controls the optional parts of the dump.
//
// The zero Options reproduces the structural dump: every node in the tree is
// printed, but the `#{...}` annotation system — which the parser stores in a
// semantic side-table rather than in node fields — is not.
type Options struct {
	// Annotations enables dumping of the `#{...}` annotation entries that the
	// parser attaches to nodes through the semantic side-table.
	Annotations bool
	// Sem is the semantic side-table holding those annotations. Required when
	// Annotations is true; a nil Sem simply yields no annotations.
	//
	// ⚠️ Sem's node-keyed tables are maps keyed by Node. They must never be iterated: Go
	// randomises map iteration order, which would make the dump irreproducible.
	Sem *parser.SemanticContext
}

// Dump returns a debug string representation of an AST node.
func Dump(node parser.Node) string {
	return DumpWithOptions(node, Options{})
}

// DumpWithOptions is Dump with explicit options.
func DumpWithOptions(node parser.Node, opts Options) string {
	d := &dumper{opts: opts}
	d.node(node, 0)
	return strings.TrimRight(d.buf.String(), "\n")
}

type dumper struct {
	buf  strings.Builder
	opts Options
}

func indent(depth int) string { return strings.Repeat("    ", depth) }

// ── field rendering helpers ──────────────────────────────────────────────────
//
// fields() joins the non-empty parts with ", " so that callers can spell out
// every field and let the helpers drop the ones at their zero value.

func fields(parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, ", ")
}

// kv is an always-printed field.
func kv(name, val string) string { return name + ": " + val }

// kvb prints a boolean field only when it is true (the zero value is the
// uninteresting state).
func kvb(name string, v bool) string {
	if !v {
		return ""
	}
	return name + ": true"
}

// kvs prints a string field only when non-empty.
func kvs(name, v string) string {
	if v == "" {
		return ""
	}
	return name + ": " + v
}

// kvn prints an integer field only when non-zero.
func kvn(name string, v int64) string {
	if v == 0 {
		return ""
	}
	return fmt.Sprintf("%s: %d", name, v)
}

// kvc prints a node count, only when non-zero.
func kvc(name string, n int) string {
	if n == 0 {
		return ""
	}
	return fmt.Sprintf("%s: %d", name, n)
}

// typeString renders a type node for an inline summary; "" when absent.
func typeString(t parser.Type) string {
	if t == nil {
		return ""
	}
	return t.String()
}

// labelKindName renders parser.LabelKind, which has no String() method.
func labelKindName(k parser.LabelKind) string {
	switch k {
	case parser.LabelNumeric:
		return "numeric"
	case parser.LabelText:
		return "text"
	default:
		return "none"
	}
}

// ── traversal ────────────────────────────────────────────────────────────────

// node prints one node and, when enabled, the annotations attached to it.
func (d *dumper) node(node parser.Node, depth int) {
	if node == nil {
		fmt.Fprintf(&d.buf, "%s<nil>\n", indent(depth))
		return
	}

	switch n := node.(type) {

	// ── top level ────────────────────────────────────────────────────────────

	case *parser.Program:
		fmt.Fprintf(&d.buf, "%sProgram{%s}\n", indent(depth),
			fields(kvc("warnings", len(n.Warnings))))
		for _, s := range n.Statements {
			d.node(s, depth+1)
		}
		if len(n.FreeComments) > 0 {
			fmt.Fprintf(&d.buf, "%sFreeComments: %d group(s)\n", indent(depth+1), len(n.FreeComments))
		}
		if n.TrailingComments != nil {
			fmt.Fprintf(&d.buf, "%sTrailingComments\n", indent(depth+1))
			d.commentGroup(n.TrailingComments, depth+2)
		}

	// ── statements ───────────────────────────────────────────────────────────

	case *parser.UseStatement:
		fmt.Fprintf(&d.buf, "%sUseStatement{%s}\n", indent(depth),
			fields(kv("path", n.Path), kv("function", n.Function), kv("alias", n.Alias),
				kvb("as", n.AsKeyword)))
		d.comments(n, depth+1)

	case *parser.ExportStatement:
		fmt.Fprintf(&d.buf, "%sExportStatement{%s}\n", indent(depth),
			fields(kv("path", n.Path), kv("function", n.Function), kv("alias", n.Alias),
				kvb("as", n.AsKeyword)))
		d.comments(n, depth+1)

	case *parser.MultiAssignStatement:
		fmt.Fprintf(&d.buf, "%sMultiAssignStatement{%s}\n", indent(depth),
			fields(kvc("targets", len(n.Targets)), kvs("overflow", n.OverflowMode)))
		d.comments(n, depth+1)
		for _, t := range n.Targets {
			d.node(t, depth+1)
		}
		if n.Value != nil {
			d.node(n.Value, depth+1)
		}

	case *parser.UnwrapAssignStatement:
		name := ""
		if n.Name != nil {
			name = n.Name.Value
		}
		fmt.Fprintf(&d.buf, "%sUnwrapAssignStatement{%s}\n", indent(depth), kv("name", name))
		d.comments(n, depth+1)
		if n.Target != nil {
			d.node(n.Target, depth+1)
		}
		if n.Value != nil {
			d.node(n.Value, depth+1)
		}

	case *parser.LetStatement:
		valStr := ""
		if n.Value != nil {
			valStr = d.nodeString(n.Value)
		}
		fmt.Fprintf(&d.buf, "%sLetStatement{%s}\n", indent(depth),
			fields(kv("name", n.Name.Value), kvs("type", typeString(n.Type)), kv("value", valStr),
				kvb("synthetic", n.IsSynthetic), kvb("armBinding", n.IsArmBinding),
				kvb("moduleConst", d.opts.Sem.ModuleConstOf(n)), kvb("propagation", n.IsPropagation),
				kvs("itArmType", n.ItArmType), kvs("overflow", n.OverflowMode)))
		d.comments(n, depth+1)
		if n.Type != nil {
			d.node(n.Type, depth+1)
		}
		if n.Value != nil {
			d.node(n.Value, depth+1)
		}

	case *parser.ReturnStatement:
		if n.ReturnValue != nil {
			fmt.Fprintf(&d.buf, "%sReturnStatement{%s}\n", indent(depth), d.nodeString(n.ReturnValue))
			d.node(n.ReturnValue, depth+1)
		} else {
			fmt.Fprintf(&d.buf, "%sReturnStatement{}\n", indent(depth))
		}
		d.comments(n, depth+1)

	case *parser.ExpressionStatement:
		if n.Expression != nil {
			fmt.Fprintf(&d.buf, "%sExpressionStatement{%s}\n", indent(depth), d.nodeString(n.Expression))
			d.node(n.Expression, depth+1)
		} else {
			fmt.Fprintf(&d.buf, "%sExpressionStatement{nil}\n", indent(depth))
		}
		d.comments(n, depth+1)

	case *parser.BlockStatement:
		fmt.Fprintf(&d.buf, "%sBlockStatement{%s}\n", indent(depth),
			fields(kvb("inline", n.IsInline)))
		d.comments(n, depth+1)
		for _, s := range n.Statements {
			d.node(s, depth+1)
		}
		if n.ClosingBraceComment != nil {
			fmt.Fprintf(&d.buf, "%sClosingBraceComment\n", indent(depth+1))
			d.commentGroup(n.ClosingBraceComment, depth+2)
		}
		if len(n.BetweenComments) > 0 {
			fmt.Fprintf(&d.buf, "%sBetweenComments: %d group(s)\n", indent(depth+1), len(n.BetweenComments))
		}
		if n.TrailingComments != nil {
			fmt.Fprintf(&d.buf, "%sTrailingComments\n", indent(depth+1))
			d.commentGroup(n.TrailingComments, depth+2)
		}

	case *parser.FunctionDefinition:
		fmt.Fprintf(&d.buf, "%sFunctionDefinition{%s}\n", indent(depth),
			fields(kv("name", n.Name),
				kvb("colonSyntax", n.ColonSyntax), kvb("methodDef", n.IsMethodDef),
				kvb("skipNamingCheck", n.IsSkipNamingCheck),
				kvs("overflow", n.OverflowMode),
				kvb("builtinStub", n.BuiltinStub),
				kvs("builtinName", n.BuiltinName), kvb("builtinGroup", n.BuiltinGroup)))
		d.comments(n, depth+1)
		d.signature(n, n.FuncSignature, depth+1)
		if n.Body != nil {
			d.node(n.Body, depth+1)
		}

	case *parser.ExternStatement:
		name := ""
		if n.Name != nil {
			name = n.Name.Value
		}
		fmt.Fprintf(&d.buf, "%sExternStatement{%s}\n", indent(depth),
			fields(kv("lang", n.Lang), kv("name", name),
				kvc("params", len(n.Parameters)), kvc("results", len(n.Results))))
		d.comments(n, depth+1)
		for _, p := range n.Parameters {
			if p != nil {
				d.node(p, depth+1)
			}
		}
		for _, p := range n.Results {
			if p != nil {
				d.node(p, depth+1)
			}
		}

	case *parser.AnnotationStatement:
		fmt.Fprintf(&d.buf, "%sAnnotationStatement{%s}\n", indent(depth),
			fields(kvc("entries", len(n.Entries))))
		d.comments(n, depth+1)
		if d.opts.Annotations {
			for _, e := range n.Entries {
				d.annotationEntry(e, depth+1)
			}
		}

	case *parser.TypeAlias:
		fmt.Fprintf(&d.buf, "%sTypeAlias{%s}\n", indent(depth),
			fields(kv("name", n.Name), kvs("type", typeString(n.Type))))
		d.comments(n, depth+1)
		if n.Type != nil {
			d.node(n.Type, depth+1)
		}
		if n.Union != nil {
			d.node(n.Union, depth+1)
		}

	case *parser.ForStatement:
		fmt.Fprintf(&d.buf, "%sForStatement{%s}\n", indent(depth),
			fields(kv("label", n.Label),
				kvb("condWrapper", n.IsCondWrapper), kvs("overflow", n.OverflowMode)))
		d.comments(n, depth+1)
		if n.Init != nil {
			d.node(n.Init, depth+1)
		}
		if n.Condition != nil {
			d.node(n.Condition, depth+1)
		}
		if n.Update != nil {
			d.node(n.Update, depth+1)
		}
		if n.CountExpr != nil {
			d.node(n.CountExpr, depth+1)
		}
		if n.IterRange != nil {
			d.node(n.IterRange, depth+1)
		}
		if n.Body != nil {
			d.node(n.Body, depth+1)
		}

	case *parser.BreakStatement:
		fmt.Fprintf(&d.buf, "%sBreakStatement{%s}\n", indent(depth),
			fields(kv("label", n.Label), kv("kind", labelKindName(n.LabelKind))))
		d.comments(n, depth+1)

	case *parser.ContinueStatement:
		fmt.Fprintf(&d.buf, "%sContinueStatement{%s}\n", indent(depth),
			fields(kv("label", n.Label), kv("kind", labelKindName(n.LabelKind))))
		d.comments(n, depth+1)

	case *parser.EnumDefinition:
		fmt.Fprintf(&d.buf, "%sEnumDefinition{%s}\n", indent(depth),
			fields(kv("name", n.Name), kvc("values", len(n.Values))))
		d.comments(n, depth+1)
		for _, v := range n.Values {
			if v != nil {
				d.node(v, depth+1)
			}
		}

	case *parser.TaggedEnumDefinition:
		fmt.Fprintf(&d.buf, "%sTaggedEnumDefinition{%s}\n", indent(depth),
			fields(kv("name", n.Name), kvb("builtin", n.Builtin),
				kvc("variants", len(n.Variants))))
		d.comments(n, depth+1)
		for _, v := range n.Variants {
			if v != nil {
				d.node(v, depth+1)
			}
		}

	case *parser.StructDefinition:
		fmt.Fprintf(&d.buf, "%sStructDefinition{%s}\n", indent(depth),
			fields(kv("name", n.Name), kvc("implements", len(n.Implements)),
				kvc("fields", len(n.Fields))))
		d.comments(n, depth+1)
		for _, f := range n.Fields {
			if f != nil {
				d.node(f, depth+1)
			}
		}

	case *parser.InterfaceDefinition:
		fmt.Fprintf(&d.buf, "%sInterfaceDefinition{%s}\n", indent(depth),
			fields(kv("name", n.Name), kvc("methods", len(n.Methods))))
		if len(n.Implements) > 0 {
			fmt.Fprintf(&d.buf, "%sImplements: %v\n", indent(depth+1), n.Implements)
		}
		d.comments(n, depth+1)
		for _, m := range n.Methods {
			d.interfaceMethod(m, depth+1)
		}

	// ── expressions ──────────────────────────────────────────────────────────

	case *parser.FunctionLiteral:
		fmt.Fprintf(&d.buf, "%sFunctionLiteral\n", indent(depth))
		d.comments(n, depth+1)
		d.signature(n, n.FuncSignature, depth+1)
		if n.Body != nil {
			d.node(n.Body, depth+1)
		}

	case *parser.CallExpression:
		fmt.Fprintf(&d.buf, "%sCallExpression{%s}\n", indent(depth),
			fields(kv("fn", d.nodeString(n.Function)), kvc("args", len(n.Arguments))))
		d.node(n.Function, depth+1)
		for _, a := range n.Arguments {
			d.node(a, depth+1)
		}

	case *parser.DotExpression:
		fmt.Fprintf(&d.buf, "%sDotExpression{%s}\n", indent(depth),
			fields(kv("receiver", d.nodeString(n.Receiver)), kv("property", n.Property)))
		d.node(n.Receiver, depth+1)

	case *parser.Identifier:
		fmt.Fprintf(&d.buf, "%sIdentifier{%s}\n", indent(depth),
			fields(kv("name", n.Value), kvb("implicitGeneric", n.IsImplicitGeneric)))

	case *parser.IntegerLiteral:
		fmt.Fprintf(&d.buf, "%sIntegerLiteral{%s}\n", indent(depth),
			fields(kv("value", fmt.Sprintf("%d", n.Value)), kvs("raw", n.Raw)))

	case *parser.FloatLiteral:
		fmt.Fprintf(&d.buf, "%sFloatLiteral{%s}\n", indent(depth),
			fields(kv("value", fmt.Sprintf("%v", n.Value)), kvs("raw", n.Raw)))

	case *parser.StringLiteral:
		fmt.Fprintf(&d.buf, "%sStringLiteral{%s}\n", indent(depth),
			fields(kv("value", n.Value), kvs("raw", n.Raw)))

	case *parser.CharLiteral:
		fmt.Fprintf(&d.buf, "%sCharLiteral{%s}\n", indent(depth),
			fields(kv("value", n.Value), kvs("raw", n.Raw)))

	case *parser.RegexLiteral:
		fmt.Fprintf(&d.buf, "%sRegexLiteral{%s}\n", indent(depth),
			fields(kv("pattern", n.Pattern), kvs("flags", n.Flags)))

	case *parser.BooleanLiteral:
		fmt.Fprintf(&d.buf, "%sBooleanLiteral{%v}\n", indent(depth), n.Value)

	case *parser.NilLiteral:
		fmt.Fprintf(&d.buf, "%sNilLiteral\n", indent(depth))

	case *parser.PrefixExpression:
		fmt.Fprintf(&d.buf, "%sPrefixExpression{%s}\n", indent(depth), kv("op", n.Operator))
		d.node(n.Right, depth+1)

	case *parser.InfixExpression:
		fmt.Fprintf(&d.buf, "%sInfixExpression{%s}\n", indent(depth), kv("op", n.Operator))
		d.node(n.Left, depth+1)
		d.node(n.Right, depth+1)

	case *parser.RunExpression:
		fmt.Fprintf(&d.buf, "%sRunExpression\n", indent(depth))
		if n.Call != nil {
			d.node(n.Call, depth+1)
		}

	case *parser.CoExpression:
		fmt.Fprintf(&d.buf, "%sCoExpression\n", indent(depth))
		if n.Call != nil {
			d.node(n.Call, depth+1)
		}

	case *parser.AwaitExpression:
		fmt.Fprintf(&d.buf, "%sAwaitExpression\n", indent(depth))
		if n.Right != nil {
			d.node(n.Right, depth+1)
		}

	case *parser.IfExpression:
		fmt.Fprintf(&d.buf, "%sIfExpression\n", indent(depth))
		d.node(n.Condition, depth+1)
		if n.Consequence != nil {
			d.node(n.Consequence, depth+1)
		}
		if n.Alternative != nil {
			d.node(n.Alternative, depth+1)
		}

	case *parser.RangeExpression:
		fmt.Fprintf(&d.buf, "%sRangeExpression{%s}\n", indent(depth),
			fields(kv("start", d.nodeString(n.Start)), kv("end", d.nodeString(n.End)),
				kvb("leftInc", n.LeftInc), kvb("rightInc", n.RightInc)))
		if n.Start != nil {
			d.node(n.Start, depth+1)
		}
		if n.End != nil {
			d.node(n.End, depth+1)
		}

	case *parser.IterationExpr:
		fmt.Fprintf(&d.buf, "%sIterationExpr{%s}\n", indent(depth),
			fields(kvs("var", n.Variable), kvs("rangeStr", n.RangeStr)))
		if n.Range != nil {
			d.node(n.Range, depth+1)
		}
		if n.RangeExpr != nil {
			d.node(n.RangeExpr, depth+1)
		}

	case *parser.SliceExpression:
		fmt.Fprintf(&d.buf, "%sSliceExpression\n", indent(depth))
		d.node(n.Left, depth+1)
		if n.Range != nil {
			d.node(n.Range, depth+1)
		}

	case *parser.IndexExpression:
		fmt.Fprintf(&d.buf, "%sIndexExpression\n", indent(depth))
		d.node(n.Left, depth+1)
		d.node(n.Index, depth+1)

	case *parser.AssignExpression:
		fmt.Fprintf(&d.buf, "%sAssignExpression\n", indent(depth))
		d.node(n.Left, depth+1)
		d.node(n.Value, depth+1)

	case *parser.ConditionalExpression:
		fmt.Fprintf(&d.buf, "%sConditionalExpression\n", indent(depth))
		d.node(n.Condition, depth+1)
		d.node(n.Consequence, depth+1)
		d.node(n.Alternative, depth+1)

	case *parser.CastExpression:
		fmt.Fprintf(&d.buf, "%sCastExpression{%s}\n", indent(depth),
			fields(kvs("type", typeString(n.Type))))
		if n.Expr != nil {
			d.node(n.Expr, depth+1)
		}
		if n.Type != nil {
			d.node(n.Type, depth+1)
		}

	case *parser.ArrayLiteral:
		fmt.Fprintf(&d.buf, "%sArrayLiteral{%s}\n", indent(depth),
			fields(kv("size", d.nodeString(n.Size)), kvc("elements", len(n.Elements)),
				kvb("wasSliceLiteral", n.WasSliceLiteral)))
		if n.Size != nil {
			d.node(n.Size, depth+1)
		}
		for _, el := range n.Elements {
			d.node(el, depth+1)
		}

	case *parser.SliceLiteral:
		fmt.Fprintf(&d.buf, "%sSliceLiteral{%s}\n", indent(depth),
			fields(kvc("elements", len(n.Elements))))
		for _, el := range n.Elements {
			d.node(el, depth+1)
		}

	case *parser.MapLiteral:
		fmt.Fprintf(&d.buf, "%sMapLiteral{%s}\n", indent(depth),
			fields(kvc("pairs", len(n.Pairs))))
		// ⚠️ Pairs is []MapPair (a value slice) while the Node methods are on
		// *MapPair, so the element address must be taken.
		for i := range n.Pairs {
			d.node(&n.Pairs[i], depth+1)
		}
		if n.MapType != nil {
			d.node(n.MapType, depth+1)
		}

	case *parser.MapPair:
		fmt.Fprintf(&d.buf, "%sMapPair\n", indent(depth))
		if n.Key != nil {
			d.node(n.Key, depth+1)
		}
		if n.Value != nil {
			d.node(n.Value, depth+1)
		}

	case *parser.StructLiteral:
		fmt.Fprintf(&d.buf, "%sStructLiteral{%s}\n", indent(depth),
			fields(kv("type", n.Type), kvc("fields", len(n.Fields))))
		for _, f := range n.Fields {
			if f != nil {
				d.node(f, depth+1)
			}
		}

	case *parser.StructField:
		fmt.Fprintf(&d.buf, "%sStructField{%s}\n", indent(depth),
			fields(kv("name", n.Name), kvs("type", typeString(n.Type)),
				kvn("arraySize", n.ArraySize),
				kvb("slice", n.IsSlice), kvb("readonly", n.ReadOnly), kvb("sealed", n.Sealed)))
		if n.Type != nil {
			d.node(n.Type, depth+1)
		}
		if n.Value != nil {
			d.node(n.Value, depth+1)
		}

	case *parser.EnumValue:
		fmt.Fprintf(&d.buf, "%sEnumValue{%s}\n", indent(depth),
			fields(kv("name", n.Name), kv("value", fmt.Sprintf("%d", n.Value)),
				kvb("explicit", n.Explicit)))

	case *parser.TaggedEnumVariant:
		fmt.Fprintf(&d.buf, "%sTaggedEnumVariant{%s}\n", indent(depth),
			fields(kv("name", n.Name), kvs("type", typeString(n.Type)),
				kvn("index", n.Index), kvc("fields", len(n.Fields))))
		if n.Type != nil {
			d.node(n.Type, depth+1)
		}
		for _, f := range n.Fields {
			d.taggedEnumField(f, depth+1)
		}

	case *parser.GroupedExpression:
		fmt.Fprintf(&d.buf, "%sGroupedExpression\n", indent(depth))
		d.node(n.Expression, depth+1)

	case *parser.Parameter:
		fmt.Fprintf(&d.buf, "%sParameter{%s}\n", indent(depth),
			fields(kv("name", n.Name), kvs("type", typeString(n.Type))))
		if n.Type != nil {
			d.node(n.Type, depth+1)
		}
		if n.DefaultExpr != nil {
			d.node(n.DefaultExpr, depth+1)
		}

	// ── types ────────────────────────────────────────────────────────────────

	case *parser.NamedType:
		fmt.Fprintf(&d.buf, "%sNamedType{%s}\n", indent(depth),
			fields(kv("name", n.Value), kvb("inferred", n.IsInferred)))

	case *parser.ArrayType:
		fmt.Fprintf(&d.buf, "%sArrayType{%s}\n", indent(depth),
			fields(kv("size", d.nodeString(n.Size)), kvb("inferred", n.IsInferred)))
		if n.Size != nil {
			d.node(n.Size, depth+1)
		}
		if n.Elem != nil {
			d.node(n.Elem, depth+1)
		}

	case *parser.SliceType:
		fmt.Fprintf(&d.buf, "%sSliceType{%s}\n", indent(depth),
			fields(kvb("inferred", n.IsInferred)))
		if n.Elem != nil {
			d.node(n.Elem, depth+1)
		}

	case *parser.MapType:
		fmt.Fprintf(&d.buf, "%sMapType{%s}\n", indent(depth),
			fields(kvb("inferred", n.IsInferred)))
		if n.Key != nil {
			d.node(n.Key, depth+1)
		}
		if n.Value != nil {
			d.node(n.Value, depth+1)
		}

	case *parser.FunctionType:
		fmt.Fprintf(&d.buf, "%sFunctionType{%s}\n", indent(depth),
			fields(kvc("params", len(n.Params)), kvc("results", len(n.Results))))
		for _, p := range n.Params {
			if p != nil {
				d.node(p, depth+1)
			}
		}
		for _, p := range n.Results {
			if p != nil {
				d.node(p, depth+1)
			}
		}

	case *parser.UnionType:
		fmt.Fprintf(&d.buf, "%sUnionType{%s}\n", indent(depth),
			fields(kvc("types", len(n.Types))))
		for _, t := range n.Types {
			d.node(t, depth+1)
		}

	case *parser.NullableType:
		fmt.Fprintf(&d.buf, "%sNullableType\n", indent(depth))
		if n.Type != nil {
			d.node(n.Type, depth+1)
		}

	case *parser.PointerType:
		fmt.Fprintf(&d.buf, "%sPointerType\n", indent(depth))
		if n.Type != nil {
			d.node(n.Type, depth+1)
		}

	case *parser.ViewType:
		fmt.Fprintf(&d.buf, "%sViewType\n", indent(depth))
		if n.Type != nil {
			d.node(n.Type, depth+1)
		}

	// ── annotation values (reached only with Options.Annotations) ────────────

	case *parser.AnnotationBoolValue:
		fmt.Fprintf(&d.buf, "%sAnnotationBoolValue{%v}\n", indent(depth), n.Value)

	case *parser.AnnotationIntValue:
		fmt.Fprintf(&d.buf, "%sAnnotationIntValue{%d}\n", indent(depth), n.Value)

	case *parser.AnnotationStringValue:
		fmt.Fprintf(&d.buf, "%sAnnotationStringValue{%s}\n", indent(depth), n.Value)

	case *parser.AnnotationIdentValue:
		fmt.Fprintf(&d.buf, "%sAnnotationIdentValue{%s}\n", indent(depth), n.Value)

	case *parser.AnnotationArrayValue:
		fmt.Fprintf(&d.buf, "%sAnnotationArrayValue{%s}\n", indent(depth),
			fields(kvc("elements", len(n.Elements))))
		for _, el := range n.Elements {
			if el != nil {
				d.node(el, depth+1)
			}
		}

	case *parser.AnnotationStructValue:
		fmt.Fprintf(&d.buf, "%sAnnotationStructValue{%s}\n", indent(depth),
			fields(kv("type", n.Type), kvc("fields", len(n.Fields))))
		for _, f := range n.Fields {
			d.annotationStructField(f, depth+1)
		}

	case *parser.AnnotationRangeValue:
		fmt.Fprintf(&d.buf, "%sAnnotationRangeValue{%s}\n", indent(depth),
			fields(kvb("leftInc", n.LeftInc), kvb("rightInc", n.RightInc)))
		if n.Start != nil {
			d.node(n.Start, depth+1)
		}
		if n.End != nil {
			d.node(n.End, depth+1)
		}

	case *parser.AnnotationEntry:
		d.annotationEntry(n, depth)

	// SurfaceMatch sits LAST, immediately before default. That is a readability
	// choice, NOT an id-stability one — and the distinction was measured, not
	// assumed.
	//
	// ⚠️ A previous version of this comment claimed the opposite: that
	// gen_ast_spec.py derives the L2 kind-id table from the switch's source
	// order, so inserting a case mid-switch would renumber every id after it.
	// That is FALSE. Both emitters do `kinds = sorted(spec.keys())`
	// (gen_ast_spec.py:636 and :1403) — kind ids are assigned in ALPHABETICAL
	// order of the Go type name and are completely independent of where the
	// case sits. Measured: moving this case from the tail to mid-switch
	// regenerates compiler/src/asp.no and spec_gen_test.go BYTE-IDENTICALLY.
	//
	// The consequence is that the id shift caused by adding SurfaceMatch
	// (ids 65–72, +1) was UNAVOIDABLE: "SurfaceMatch" sorts between
	// "StructLiteral" (64) and "TaggedEnumDefinition" (65→66) no matter where
	// the case is written. There was no placement that kept the diff purely
	// additive.
	//
	// Why keep it at the tail then: the switch is grouped by semantic category
	// (statements → expressions → literals → types → annotations), and
	// SurfaceMatch is a sugar node with no natural group; and sitting adjacent
	// to default — the `<unknown %T>` branch that TestCorpusHasNoUnknownNodes
	// tripwires — puts the newest case next to the failure mode it exists to
	// prevent.
	case *parser.SurfaceMatch:
		fmt.Fprintf(&d.buf, "%sSurfaceMatch{%s}\n", indent(depth),
			fields(kvc("arms", len(n.Arms))))
		d.comments(n, depth+1)
		if n.Matched != nil {
			d.node(n.Matched, depth+1)
		}
		// `&a` (a copy of the arm), not `&n.Arms[i]`: scripts/gen_ast_spec.py's
		// HELPER classifier only accepts a bare identifier as a helper argument,
		// so the indexed form would be reported as "unclassified for body over
		// Arms" and SurfaceMatch would silently drop out of the L2 spec table.
		// Dumping is read-only, so dumping the copy is equivalent.
		for _, a := range n.Arms {
			d.matchArm(&a, depth+1)
		}

	default:
		// Rule 1 violation: an AST node type missing from this switch. Its
		// entire subtree is invisible from here on. dump_completeness_test.go
		// fails the build if the corpus ever reaches this branch.
		fmt.Fprintf(&d.buf, "%s<unknown %T>\n", indent(depth), n)
	}

	d.annotations(node, depth+1)
}

// ── shared sub-structures ────────────────────────────────────────────────────

// signature dumps the parameter/result part shared by FunctionDefinition,
// FunctionLiteral and FunctionType. owner is the enclosing node (for the
// SemanticContext-side variadic-union lookup; S5 stage 4 moved the union
// markers off FuncSignature).
func (d *dumper) signature(owner parser.Node, sig parser.FuncSignature, depth int) {
	if len(sig.GenericParams) > 0 {
		names := make([]string, 0, len(sig.GenericParams))
		for _, g := range sig.GenericParams {
			if g != nil {
				names = append(names, g.Value)
			}
		}
		fmt.Fprintf(&d.buf, "%sGenericParams: %s\n", indent(depth), strings.Join(names, ", "))
	}
	if sig.IsVariadic {
		fmt.Fprintf(&d.buf, "%sVariadic{%s}\n", indent(depth),
			fields(kvs("union", d.opts.Sem.VariadicUnionOf(owner))))
	}
	for _, p := range sig.Parameters {
		if p != nil {
			d.node(p, depth)
		}
	}
	for _, p := range sig.Results {
		if p != nil {
			d.node(p, depth)
		}
	}
}

// interfaceMethod dumps an InterfaceMethod, which is not itself a Node.
func (d *dumper) interfaceMethod(m *parser.InterfaceMethod, depth int) {
	if m == nil {
		fmt.Fprintf(&d.buf, "%s<nil>\n", indent(depth))
		return
	}
	fmt.Fprintf(&d.buf, "%sInterfaceMethod{%s}\n", indent(depth),
		fields(kv("name", m.Name), kvs("receiver", m.Receiver),
			kvb("variadic", m.IsVariadic), kvb("genericReceiver", m.IsGenericReceiver)))
	for _, p := range m.Parameters {
		if p != nil {
			d.node(p, depth+1)
		}
	}
	for _, p := range m.Results {
		if p != nil {
			d.node(p, depth+1)
		}
	}
}

// taggedEnumField dumps a TaggedEnumField, which is not itself a Node.
func (d *dumper) taggedEnumField(f *parser.TaggedEnumField, depth int) {
	if f == nil {
		fmt.Fprintf(&d.buf, "%s<nil>\n", indent(depth))
		return
	}
	fmt.Fprintf(&d.buf, "%sTaggedEnumField{%s}\n", indent(depth),
		fields(kv("name", f.Name), kvs("type", typeString(f.Type))))
	if f.Type != nil {
		d.node(f.Type, depth+1)
	}
}

// annotationStructField dumps an AnnotationStructField, which is not a Node.
func (d *dumper) annotationStructField(f parser.AnnotationStructField, depth int) {
	fmt.Fprintf(&d.buf, "%sAnnotationStructField{%s}\n", indent(depth), kv("name", f.Name))
	if f.Value != nil {
		d.node(f.Value, depth+1)
	}
}

// matchArm dumps one arm of a SurfaceMatch. MatchArm is not a Node, so it has
// no case in dumpNode and is reached only through the SurfaceMatch case.
//
// Every field that buildMatchDesugar READS or WRITES is rendered on purpose.
// The freeze gate (ast_freeze_test.go, G1/C6) uses this dump as its oracle, so
// a field that is not rendered here is a field whose mutation the gate cannot
// see — and ValueCtx / Body are exactly the fields the desugar touches.
func (d *dumper) matchArm(a *parser.MatchArm, depth int) {
	if a == nil {
		fmt.Fprintf(&d.buf, "%s<nil>\n", indent(depth))
		return
	}
	fmt.Fprintf(&d.buf, "%sMatchArm{%s}\n", indent(depth),
		fields(kvb("wildcard", a.IsWildcard), kvb("dotVal", a.IsDotVal),
			kvb("rawCond", a.IsRawCond), kvb("blockBody", a.IsBlockBody),
			kvb("typeArm", a.IsTypeArm), kvs("typePattern", a.TypePattern),
			kvs("typeBinding", a.TypeBindingName), kvb("skipIt", a.SkipItBinding),
			kvb("valueCtx", a.ValueCtx), kv("binding", a.BindingName),
			kv("bindingVariant", a.BindingVariant),
			kvs("multiOption", strings.Join(a.MultiOptionPatterns, ",")),
			kvc("multiValue", len(a.MultiValuePatterns)),
			kvc("bindingNames", len(a.BindingNames)),
			kvc("bindingFields", len(a.BindingFields))))
	if a.Condition != nil {
		d.node(a.Condition, depth+1)
	}
	for _, v := range a.MultiValuePatterns {
		d.node(v, depth+1)
	}
	for _, s := range a.BindingNames {
		fmt.Fprintf(&d.buf, "%sBindingName{%s}\n", indent(depth+1), kv("name", s))
	}
	for _, s := range a.BindingFields {
		fmt.Fprintf(&d.buf, "%sBindingField{%s}\n", indent(depth+1), kv("name", s))
	}
	if a.Body != nil {
		d.node(a.Body, depth+1)
	}
}

// annotationEntry dumps one `key[=value]` annotation entry.
func (d *dumper) annotationEntry(e *parser.AnnotationEntry, depth int) {
	if e == nil {
		fmt.Fprintf(&d.buf, "%s<nil>\n", indent(depth))
		return
	}
	fmt.Fprintf(&d.buf, "%sAnnotationEntry{%s}\n", indent(depth),
		fields(kv("key", e.Key), kvb("trailing", e.Trailing), kvb("propagated", e.Propagated)))
	if e.Value != nil {
		d.node(e.Value, depth+1)
	}
}

// ── comments ─────────────────────────────────────────────────────────────────

func (d *dumper) comments(stmt parser.Node, depth int) {
	if doc, ok := stmt.(interface{ GetDoc() *parser.CommentGroup }); ok {
		if g := doc.GetDoc(); g != nil {
			fmt.Fprintf(&d.buf, "%sDoc: ", indent(depth))
			d.commentGroupInline(g)
		}
	}
	if c, ok := stmt.(interface{ GetComment() *parser.CommentGroup }); ok {
		if g := c.GetComment(); g != nil {
			fmt.Fprintf(&d.buf, "%sComment: ", indent(depth))
			d.commentGroupInline(g)
		}
	}
}

func (d *dumper) commentGroup(cg *parser.CommentGroup, depth int) {
	for _, c := range cg.List {
		fmt.Fprintf(&d.buf, "%s//%s\n", indent(depth), c.Text)
	}
}

func (d *dumper) commentGroupInline(cg *parser.CommentGroup) {
	if len(cg.List) > 0 {
		fmt.Fprintf(&d.buf, "//%s\n", cg.List[0].Text)
	}
}

// ── annotations ──────────────────────────────────────────────────────────────

// annotations prints the `#{...}` entries the parser attached to n.
//
// ⚠️ The annotation side-table is a map[Node][]*AnnotationEntry. It is never
// ranged over —
// Go randomises map iteration order, which would make the dump irreproducible.
// Instead each node does a point lookup as the tree is walked, so the output
// order is the tree order.
func (d *dumper) annotations(n parser.Node, depth int) {
	if !d.opts.Annotations || d.opts.Sem == nil {
		return
	}
	entries := d.opts.Sem.AnnotationsOf(n)
	if len(entries) == 0 {
		return
	}
	fmt.Fprintf(&d.buf, "%sAnnotations{%s}\n", indent(depth), fields(kvc("entries", len(entries))))
	for _, e := range entries {
		d.annotationEntry(e, depth+1)
	}
}

// ── inline summaries ─────────────────────────────────────────────────────────

// nodeString renders a short inline summary of a leaf-ish node for use inside a
// parent's `{...}` header. Non-leaf values fall back to their type name, which
// signals "see the subtree below".
func (d *dumper) nodeString(node parser.Node) string {
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
	case *parser.BooleanLiteral:
		return fmt.Sprintf("%v", n.Value)
	case *parser.NilLiteral:
		return "nil"
	default:
		return strings.TrimPrefix(fmt.Sprintf("%T", n), "*parser.")
	}
}
