// std_methodify: AST-based rewriter that converts deprecated function-form
// numeric-width conversion calls into type-bound method calls.
//
//   math.f64-to-i64(x)  ->  x.to-i64()
//   math.i64-to-f64(x)  ->  x.to-f64()
//   math.f32-to-f64(x)  ->  x.to-f64()
//   math.f64-to-f32(x)  ->  x.to-f32()
//
// Design safety (mirrors std_qualify; no text/regex editing):
//   - Only the QUALIFIED callee form `math.CONV(...)` is rewritten. Bare
//     `CONV(...)` sites are left untouched — in std the only bare ones are the
//     owning module's (math.no) own self-module primitive calls, which must NOT
//     be turned into method calls (that would invent a math -> number dep).
//   - A site is skipped when its single argument references the implicit self
//     receiver (`.` / `.field`, AST: DotExpression{Receiver: Identifier"self"}),
//     i.e. the low-level float-method implementations in number.no that convert
//     the current value stay as `math.CONV(.)` primitives (converting them would
//     either recurse or emit an invalid `..to-...()`).
//   - The single argument becomes the method receiver; non-atomic receivers
//     (infix / prefix / cast / conditional / ...) are wrapped in a Grouped node
//     so precedence is preserved. The file is re-serialized through `no fmt`.
//
// Usage:
//   go run ./tools/std_methodify [-w] [-only a,no] [src/std]
//     (no -w)  dry-run: print per-file summary + diff, no write
//     -w       write methodified source back to the files
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	noFmt "github.com/lizongying/nolang/fmt"
	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// convMethod maps a qualified conversion function name to its method name.
var convMethod = map[string]string{
	"f64-to-i64": "to-i64",
	"i64-to-f64": "to-f64",
	"f32-to-f64": "to-f64",
	"f64-to-f32": "to-f32",
}

// ownerModule is the module the qualified form must come from to be rewritten.
const ownerModule = "math"

func main() {
	root := "src/std"
	write := false
	var only []string
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-w":
			write = true
		case "-only":
			if i+1 < len(args) {
				i++
				for _, s := range strings.Split(args[i], ",") {
					if s = strings.TrimSpace(s); s != "" {
						only = append(only, s)
					}
				}
			}
		default:
			root = args[i]
		}
	}

	onlySet := map[string]bool{}
	for _, o := range only {
		onlySet[o] = true
	}

	var files []string
	_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(p, ".no") {
			return nil
		}
		base := strings.TrimSuffix(filepath.Base(p), ".no")
		if len(onlySet) > 0 && !onlySet[base] {
			return nil
		}
		files = append(files, p)
		return nil
	})
	sort.Strings(files)

	totalChanges, touchedFiles := 0, 0
	for _, p := range files {
		content, err := os.ReadFile(p)
		if err != nil {
			fmt.Fprintf(os.Stderr, "read %s: %v\n", p, err)
			continue
		}
		out, n, notes := methodifyFile(string(content))
		if n == 0 || out == string(content) {
			continue
		}
		touchedFiles++
		totalChanges += n
		fmt.Printf("=== %s (%d methodified) ===\n", p, n)
		for _, note := range notes {
			fmt.Printf("    %s\n", note)
		}
		if write {
			if err := os.WriteFile(p, []byte(out), 0644); err != nil {
				fmt.Fprintf(os.Stderr, "write %s: %v\n", p, err)
			}
		}
	}
	mode := "DRY-RUN"
	if write {
		mode = "WROTE"
	}
	fmt.Printf("\n[%s] %d file(s), %d call(s) methodified\n", mode, touchedFiles, totalChanges)
}

func methodifyFile(content string) (string, int, []string) {
	prog := parseSurface(content)
	if prog == nil {
		return content, 0, nil
	}
	notes := []string{}
	count := 0
	for _, stmt := range prog.Statements {
		count += walkStmt(stmt, &notes)
	}
	if count == 0 {
		return content, 0, nil
	}
	return noFmt.FormatProgram(prog, content), count, notes
}

func parseSurface(content string) *parser.Program {
	if strings.TrimSpace(content) == "" {
		return nil
	}
	l := lexer.New(content)
	p := parser.New(l)
	p.SkipUnwrapLowering = true
	p.SkipSafeIndexLowering = true
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		fmt.Fprintf(os.Stderr, "parse errors (skip): %v\n", p.Errors()[0])
		return nil
	}
	return prog
}

func walkStmt(stmt parser.Statement, notes *[]string) int {
	n := 0
	switch s := stmt.(type) {
	case *parser.ExpressionStatement:
		if s.Expression != nil {
			s.Expression = walkExpr(s.Expression, notes, &n)
		}
	case *parser.LetStatement:
		if s.Value != nil {
			s.Value = walkExpr(s.Value, notes, &n)
		}
	case *parser.ReturnStatement:
		if s.ReturnValue != nil {
			s.ReturnValue = walkExpr(s.ReturnValue, notes, &n)
		}
	case *parser.MultiAssignStatement:
		for i, t := range s.Targets {
			s.Targets[i] = walkExpr(t, notes, &n)
		}
		if s.Value != nil {
			s.Value = walkExpr(s.Value, notes, &n)
		}
	case *parser.UnwrapAssignStatement:
		if s.Target != nil {
			s.Target = walkExpr(s.Target, notes, &n)
		}
		if s.Value != nil {
			s.Value = walkExpr(s.Value, notes, &n)
		}
	case *parser.FunctionDefinition:
		if s.Body != nil {
			for _, b := range s.Body.Statements {
				n += walkStmt(b, notes)
			}
		}
	case *parser.BlockStatement:
		for _, b := range s.Statements {
			n += walkStmt(b, notes)
		}
	case *parser.ForStatement:
		if s.Init != nil {
			n += walkStmt(s.Init, notes)
		}
		if s.Condition != nil {
			s.Condition = walkExpr(s.Condition, notes, &n)
		}
		if s.Update != nil {
			n += walkStmt(s.Update, notes)
		}
		if s.IterRange != nil && s.IterRange.RangeExpr != nil {
			s.IterRange.RangeExpr = walkExpr(s.IterRange.RangeExpr, notes, &n)
		}
		if s.CountExpr != nil {
			s.CountExpr = walkExpr(s.CountExpr, notes, &n)
		}
		if s.Body != nil {
			for _, b := range s.Body.Statements {
				n += walkStmt(b, notes)
			}
		}
	}
	return n
}

func walkExpr(e parser.Expression, notes *[]string, n *int) parser.Expression {
	if e == nil {
		return e
	}
	switch x := e.(type) {
	case *parser.CallExpression:
		// Normalize inner arguments first so nested conversions convert before
		// this level is examined.
		for i, a := range x.Arguments {
			x.Arguments[i] = walkExpr(a, notes, n)
		}
		for i, g := range x.GenericArgs {
			x.GenericArgs[i] = walkExpr(g, notes, n)
		}
		// Detect the qualified `math.CONV(...)` form only.
		if dot, ok := x.Function.(*parser.DotExpression); ok {
			if recv, ok2 := dot.Receiver.(*parser.Identifier); ok2 && recv.Value == ownerModule {
				if m, isConv := convMethod[dot.Property]; isConv && len(x.Arguments) == 1 {
					arg := x.Arguments[0]
					if !mentionsSelf(arg) {
						recvExpr := arg
						if !isAtomic(recvExpr) {
							recvExpr = &parser.GroupedExpression{Token: exprToken(arg, x.Token), Expression: recvExpr}
						}
						methodCall := &parser.CallExpression{
							Token: x.Token,
							Function: &parser.DotExpression{
								Token:    dot.Token,
								Receiver: recvExpr,
								Property: m,
							},
							Arguments: []parser.Expression{},
						}
						*n++
						*notes = append(*notes, fmt.Sprintf("%s(...) -> .%s()", dot.Property, m))
						return methodCall
					}
				}
			}
		}
		return x

	case *parser.Identifier:
		return x
	case *parser.InfixExpression:
		x.Left = walkExpr(x.Left, notes, n)
		x.Right = walkExpr(x.Right, notes, n)
	case *parser.PrefixExpression:
		x.Right = walkExpr(x.Right, notes, n)
	case *parser.GroupedExpression:
		x.Expression = walkExpr(x.Expression, notes, n)
	case *parser.IndexExpression:
		x.Left = walkExpr(x.Left, notes, n)
		x.Index = walkExpr(x.Index, notes, n)
	case *parser.SliceExpression:
		x.Left = walkExpr(x.Left, notes, n)
		if x.Range != nil {
			x.Range.Start = walkExpr(x.Range.Start, notes, n)
			x.Range.End = walkExpr(x.Range.End, notes, n)
		}
	case *parser.RangeExpression:
		x.Start = walkExpr(x.Start, notes, n)
		x.End = walkExpr(x.End, notes, n)
	case *parser.AssignExpression:
		x.Value = walkExpr(x.Value, notes, n)
	case *parser.ConditionalExpression:
		x.Condition = walkExpr(x.Condition, notes, n)
		x.Consequence = walkExpr(x.Consequence, notes, n)
		x.Alternative = walkExpr(x.Alternative, notes, n)
	case *parser.IfExpression:
		x.Condition = walkExpr(x.Condition, notes, n)
		if x.MatchedExpr != nil {
			x.MatchedExpr = walkExpr(x.MatchedExpr, notes, n)
		}
		if x.Consequence != nil {
			for _, b := range x.Consequence.Statements {
				*n += walkStmt(b, notes)
			}
		}
		if x.Alternative != nil {
			for _, b := range x.Alternative.Statements {
				*n += walkStmt(b, notes)
			}
		}
	case *parser.RunExpression:
		x.Call = walkExpr(x.Call, notes, n)
	case *parser.CoExpression:
		x.Call = walkExpr(x.Call, notes, n)
	case *parser.AwaitExpression:
		x.Right = walkExpr(x.Right, notes, n)
	case *parser.CastExpression:
		x.Expr = walkExpr(x.Expr, notes, n)
	case *parser.ArrayLiteral:
		if x.Size != nil {
			x.Size = walkExpr(x.Size, notes, n)
		}
		for i, el := range x.Elements {
			x.Elements[i] = walkExpr(el, notes, n)
		}
	case *parser.SliceLiteral:
		for i, el := range x.Elements {
			x.Elements[i] = walkExpr(el, notes, n)
		}
	case *parser.MapLiteral:
		for i, pr := range x.Pairs {
			pr.Value = walkExpr(pr.Value, notes, n)
			x.Pairs[i] = pr
		}
	case *parser.StructLiteral:
		for _, fld := range x.Fields {
			if fld.Value != nil {
				fld.Value = walkExpr(fld.Value, notes, n)
			}
		}
	case *parser.FunctionLiteral:
		if x.Body != nil {
			for _, b := range x.Body.Statements {
				*n += walkStmt(b, notes)
			}
		}
	case *parser.DotExpression:
		x.Receiver = walkExpr(x.Receiver, notes, n)
		return x
	}
	return e
}

// exprToken returns a representative lexer.Token for an expression so synthetic
// wrapper nodes carry the correct source line (the formatter uses token
// positions to decide line breaks). Falls back to `fb` when unknown.
func exprToken(e parser.Expression, fb lexer.Token) lexer.Token {
	switch x := e.(type) {
	case *parser.Identifier:
		return x.Token
	case *parser.IntegerLiteral:
		return x.Token
	case *parser.FloatLiteral:
		return x.Token
	case *parser.StringLiteral:
		return x.Token
	case *parser.CharLiteral:
		return x.Token
	case *parser.BooleanLiteral:
		return x.Token
	case *parser.NilLiteral:
		return x.Token
	case *parser.RegexLiteral:
		return x.Token
	case *parser.PrefixExpression:
		return x.Token
	case *parser.InfixExpression:
		return x.Token
	case *parser.CallExpression:
		return x.Token
	case *parser.DotExpression:
		return x.Token
	case *parser.IndexExpression:
		return x.Token
	}
	return fb
}

// isAtomic reports whether e can serve directly as a method receiver without
// needing parentheses (postfix/index/literal/identifier/grouped are atomic).
func isAtomic(e parser.Expression) bool {
	switch e.(type) {
	case *parser.Identifier,
		*parser.IntegerLiteral,
		*parser.FloatLiteral,
		*parser.StringLiteral,
		*parser.CharLiteral,
		*parser.BooleanLiteral,
		*parser.NilLiteral,
		*parser.RegexLiteral,
		*parser.CallExpression,
		*parser.DotExpression,
		*parser.IndexExpression,
		*parser.GroupedExpression,
		*parser.ArrayLiteral,
		*parser.SliceLiteral,
		*parser.MapLiteral,
		*parser.StructLiteral:
		return true
	}
	return false
}

// mentionsSelf reports whether the expression subtree references the implicit
// self receiver, which the parser/fmt represent as DotExpression{Receiver:
// Identifier{"self"}} (bare `.` has Property == "", `.field` has Property set).
func mentionsSelf(e parser.Expression) bool {
	if e == nil {
		return false
	}
	switch x := e.(type) {
	case *parser.Identifier:
		return x.Value == "self"
	case *parser.DotExpression:
		if id, ok := x.Receiver.(*parser.Identifier); ok && id.Value == "self" {
			return true
		}
		return mentionsSelf(x.Receiver)
	case *parser.CallExpression:
		if mentionsSelf(x.Function) {
			return true
		}
		for _, a := range x.Arguments {
			if mentionsSelf(a) {
				return true
			}
		}
		for _, g := range x.GenericArgs {
			if mentionsSelf(g) {
				return true
			}
		}
	case *parser.InfixExpression:
		return mentionsSelf(x.Left) || mentionsSelf(x.Right)
	case *parser.PrefixExpression:
		return mentionsSelf(x.Right)
	case *parser.GroupedExpression:
		return mentionsSelf(x.Expression)
	case *parser.IndexExpression:
		return mentionsSelf(x.Left) || mentionsSelf(x.Index)
	case *parser.SliceExpression:
		if mentionsSelf(x.Left) {
			return true
		}
		if x.Range != nil {
			return mentionsSelf(x.Range.Start) || mentionsSelf(x.Range.End)
		}
	case *parser.RangeExpression:
		return mentionsSelf(x.Start) || mentionsSelf(x.End)
	case *parser.AssignExpression:
		return mentionsSelf(x.Value)
	case *parser.ConditionalExpression:
		return mentionsSelf(x.Condition) || mentionsSelf(x.Consequence) || mentionsSelf(x.Alternative)
	case *parser.CastExpression:
		return mentionsSelf(x.Expr)
	case *parser.AwaitExpression:
		return mentionsSelf(x.Right)
	case *parser.RunExpression:
		return mentionsSelf(x.Call)
	case *parser.CoExpression:
		return mentionsSelf(x.Call)
	case *parser.ArrayLiteral:
		if x.Size != nil && mentionsSelf(x.Size) {
			return true
		}
		for _, el := range x.Elements {
			if mentionsSelf(el) {
				return true
			}
		}
	case *parser.SliceLiteral:
		for _, el := range x.Elements {
			if mentionsSelf(el) {
				return true
			}
		}
	case *parser.MapLiteral:
		for _, pr := range x.Pairs {
			if mentionsSelf(pr.Value) {
				return true
			}
		}
	case *parser.StructLiteral:
		for _, fld := range x.Fields {
			if mentionsSelf(fld.Value) {
				return true
			}
		}
	}
	return false
}
