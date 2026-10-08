// std_qualify: AST-based cross-module qualifier for src/std.
//
// It rewrites bare cross-module references to their module-qualified form
// (functions: `fn(...)` → `mod.fn(...)`; constants: `CONST` → `mod.CONST`)
// using the SAME ownership the checker/build rely on, then prints the file
// back through the `no fmt` AST path (formatter re-serializes from a parsed
// Program). Because only real CallExpression callees and value-position
// Identifiers are touched, method/parameter/interface DECLARATIONS, type
// positions, comments and string literals are never modified.
//
// Safety invariants (mirrors the global.no contract + build normalization):
//   - The build pre-fills importedModules with EVERY std ShortName
//     (transpiler.go), so a qualified `mod.fn` is normalized back to bare at
//     build → qualification is behavior-preserving for genuinely-owned fns.
//   - Only qualify when the name has a UNIQUE owning module (multi-owner names
//     are handled by prefixCollidingFunctions at build, leave them bare).
//   - Never qualify the owning module's own references (self-call stays bare).
//   - Never qualify the 6 default globals (print/eprint/format/with-cap/
//     with-len/with-cap-len) or the option constructors err/ok/val/nil/it.
//   - Never qualify names bound locally in the file (params/locals/top-level).
//
// Usage:
//   go run ./tools/std_qualify [-w] [-only a,no] [src/std]
//     (no -w)  dry-run: print per-file change summary + unified diff, no write
//     -w       write qualified source back to the files
//     -only    comma-separated module file names (without .no) to restrict scope
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

	"github.com/lizongying/nolang/checker"
)

// the 6 truly-global functions documented in global.no (plus deprecated aliases)
var defaultGlobals = map[string]bool{
	"print": true, "eprint": true, "format": true,
	"with-cap": true, "with-len": true, "with-cap-len": true,
	"printf": true, "eprintf": true, "sprintf": true, // deprecated, kept bare
}

// option constructors / option-pattern keywords: language-level, never qualify.
// (err collides with io.err the function, but std's bare err(...) is the ctor.)
var optionCtors = map[string]bool{
	"err": true, "ok": true, "val": true, "nil": true, "it": true,
}

var (
	// fnOwners maps a bare function name to the SET of std module ShortNames
	// that own it (authoritative, from the generated stdSigsCache).
	fnOwners map[string]map[string]bool
	// constOwners maps an uppercase constant name to its owning module set.
	constOwners map[string]map[string]bool
	// diskToModule maps an on-disk std .no path to its module ShortName.
	diskToModule map[string]string
)

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

	buildOwnership(root)

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
		out, n, notes := qualifyFile(p, string(content))
		if n == 0 || out == string(content) {
			continue
		}
		touchedFiles++
		totalChanges += n
		fmt.Printf("=== %s (%d qualified) ===\n", p, n)
		for _, note := range notes {
			fmt.Printf("    %s\n", note)
		}
		if write {
			if err := os.WriteFile(p, []byte(out), 0644); err != nil {
				fmt.Fprintf(os.Stderr, "write %s: %v\n", p, err)
			}
		} else {
			printUnifiedDiff(string(content), out, p)
		}
	}
	mode := "DRY-RUN"
	if write {
		mode = "WROTE"
	}
	fmt.Printf("\n[%s] %d file(s), %d reference(s) qualified\n", mode, touchedFiles, totalChanges)
}

// buildOwnership fills fnOwners (from stdSigsCache) and constOwners (from
// scanning std top-level uppercase LetStatements).
func buildOwnership(root string) {
	fnOwners = map[string]map[string]bool{}
	constOwners = map[string]map[string]bool{}
	diskToModule = map[string]string{}

	// module ShortName set (only real std modules count as owners)
	validMod := map[string]bool{}
	for _, info := range checker.KnownStdModules() {
		validMod[info.ShortName] = true
	}

	// ---- function owners from the generated signature table ----
	funcSigs, _ := checker.CollectStdModuleSignatures()
	for key := range funcSigs {
		idx := strings.Index(key, ".")
		if idx <= 0 {
			continue // bare key, no module prefix
		}
		mod, fn := key[:idx], key[idx+1:]
		if strings.Contains(fn, ".") {
			continue // struct/union method entry, not a plain top-level fn
		}
		if mod == "global" || !validMod[mod] {
			continue
		}
		addOwner(fnOwners, fn, mod)
	}

	// ---- module ShortName per disk file, plus constant owners ----
	// map std/-relative fullpath -> ShortName
	pathToShort := map[string]string{}
	for _, info := range checker.KnownStdModules() {
		pathToShort[info.FullPath] = info.ShortName
	}

	_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(p, ".no") {
			return nil
		}
		rel := relStdPath(root, p) // e.g. "crypto/rand" (no .no)
		short := pathToShort[rel]
		if short == "" {
			short = filepath.Base(rel) // fall back to last segment
		}
		diskToModule[p] = short

		content, rerr := os.ReadFile(p)
		if rerr != nil {
			return nil
		}
		prog := parseSurface(string(content))
		if prog == nil {
			return nil
		}
		for _, stmt := range prog.Statements {
			ls, ok := stmt.(*parser.LetStatement)
			if !ok || ls.Name == nil || !checker.IsConstantName(ls.Name.Value) {
				continue
			}
			// only real std modules own constants
			if validMod[short] {
				addOwner(constOwners, ls.Name.Value, short)
			}
		}
		return nil
	})
}

func addOwner(m map[string]map[string]bool, name, mod string) {
	if m[name] == nil {
		m[name] = map[string]bool{}
	}
	m[name][mod] = true
}

// relStdPath turns an absolute-ish disk path under root into std-relative path
// without the extension (e.g. src/std/crypto/rand.no -> crypto/rand).
func relStdPath(root, p string) string {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		rel = p
	}
	rel = filepath.ToSlash(rel)
	rel = strings.TrimSuffix(rel, ".no")
	return rel
}

// uniqueCrossOwner returns the sole owning module when name is owned by exactly
// one module that is neither the current file's module nor excluded, else "".
func uniqueCrossOwner(owners map[string]map[string]bool, name, current string) string {
	set := owners[name]
	if len(set) != 1 {
		return ""
	}
	for mod := range set {
		if mod == current {
			return "" // self-module reference stays bare
		}
		return mod
	}
	return ""
}

// qualifyFile parses one std file and returns the reformatted source plus a
// count of qualified references and human-readable notes.
func qualifyFile(path, content string) (string, int, []string) {
	prog := parseSurface(content)
	if prog == nil {
		return content, 0, nil
	}
	current := diskToModule[path]

	// names bound anywhere in this file → never qualify (shadowing safety)
	bound := collectBoundNames(prog)

	notes := []string{}
	count := 0
	for _, stmt := range prog.Statements {
		count += walkStmt(stmt, current, bound, &notes)
	}
	if count == 0 {
		return content, 0, nil
	}
	out := noFmt.FormatProgram(prog, content)
	return out, count, notes
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

// collectBoundNames records every identifier bound in the file: top-level and
// nested LetStatement names, function/method definition names, and parameter
// names. A call/const reference matching one of these is left bare.
func collectBoundNames(prog *parser.Program) map[string]bool {
	bound := map[string]bool{}
	var each func(stmts []parser.Statement)

	each = func(stmts []parser.Statement) {
		for _, s := range stmts {
			switch st := s.(type) {
			case *parser.LetStatement:
				if st.Name != nil {
					bound[st.Name.Value] = true
				}
				if fl, ok := st.Value.(*parser.FunctionLiteral); ok {
					for _, prm := range fl.Parameters {
						if prm.Name != "" {
							bound[prm.Name] = true
						}
					}
					if fl.Body != nil {
						each(fl.Body.Statements)
					}
				}
			case *parser.FunctionDefinition:
				if st.Name != "" {
					bound[st.Name] = true
				}
				for _, prm := range st.Parameters {
					if prm.Name != "" {
						bound[prm.Name] = true
					}
				}
				for _, prm := range st.Results {
					if prm.Name != "" {
						bound[prm.Name] = true
					}
				}
				if st.Body != nil {
					each(st.Body.Statements)
				}
			}
		}
	}
	each(prog.Statements)
	return bound
}

// walkStmt recurses through every statement, mutating call targets and returning
// the count of qualifications made within it.
func walkStmt(stmt parser.Statement, current string, bound map[string]bool, notes *[]string) int {
	n := 0
	switch s := stmt.(type) {
	case *parser.ExpressionStatement:
		if s.Expression != nil {
			s.Expression = walkExpr(s.Expression, current, bound, notes, &n)
		}
	case *parser.LetStatement:
		if s.Value != nil {
			s.Value = walkExpr(s.Value, current, bound, notes, &n)
		}
	case *parser.ReturnStatement:
		if s.ReturnValue != nil {
			s.ReturnValue = walkExpr(s.ReturnValue, current, bound, notes, &n)
		}
	case *parser.MultiAssignStatement:
		for i, t := range s.Targets {
			s.Targets[i] = walkExpr(t, current, bound, notes, &n)
		}
		if s.Value != nil {
			s.Value = walkExpr(s.Value, current, bound, notes, &n)
		}
	case *parser.UnwrapAssignStatement:
		if s.Target != nil {
			s.Target = walkExpr(s.Target, current, bound, notes, &n)
		}
		if s.Value != nil {
			s.Value = walkExpr(s.Value, current, bound, notes, &n)
		}
	case *parser.FunctionDefinition:
		if s.Body != nil {
			for _, b := range s.Body.Statements {
				n += walkStmt(b, current, bound, notes)
			}
		}
	case *parser.BlockStatement:
		for _, b := range s.Statements {
			n += walkStmt(b, current, bound, notes)
		}
	case *parser.ForStatement:
		if s.Init != nil {
			n += walkStmt(s.Init, current, bound, notes)
		}
		if s.Condition != nil {
			s.Condition = walkExpr(s.Condition, current, bound, notes, &n)
		}
		if s.Update != nil {
			n += walkStmt(s.Update, current, bound, notes)
		}
		if s.IterRange != nil {
			if s.IterRange.RangeExpr != nil {
				s.IterRange.RangeExpr = walkExpr(s.IterRange.RangeExpr, current, bound, notes, &n)
			}
		}
		if s.CountExpr != nil {
			s.CountExpr = walkExpr(s.CountExpr, current, bound, notes, &n)
		}
		if s.Body != nil {
			for _, b := range s.Body.Statements {
				n += walkStmt(b, current, bound, notes)
			}
		}
	}
	return n
}

// walkExpr recurses through expressions, qualifying call targets and constant
// references; it returns the (possibly replaced) expression and increments *n.
func walkExpr(e parser.Expression, current string, bound map[string]bool, notes *[]string, n *int) parser.Expression {
	if e == nil {
		return e
	}
	switch x := e.(type) {
	case *parser.CallExpression:
		// curried calls: inner is also a CallExpression
		x.Function = walkExpr(x.Function, current, bound, notes, n)
		if ident, ok := x.Function.(*parser.Identifier); ok {
			if mod := qualifyTarget(ident.Value, current, bound, false); mod != "" {
				x.Function = dotted(ident, mod)
				*n++
				*notes = append(*notes, fmt.Sprintf("call %s(%s)", ident.Value, mod))
			}
		}
		for i, a := range x.Arguments {
			x.Arguments[i] = walkExpr(a, current, bound, notes, n)
		}
		for i, g := range x.GenericArgs {
			x.GenericArgs[i] = walkExpr(g, current, bound, notes, n)
		}
		return x

	case *parser.Identifier:
		// value-position constant reference (never a call target: those were the
		// CallExpression.Function handled above)
		if checker.IsConstantName(x.Value) {
			if mod := qualifyTarget(x.Value, current, bound, true); mod != "" {
				*n++
				*notes = append(*notes, fmt.Sprintf("const %s(%s)", x.Value, mod))
				return dotted(x, mod)
			}
		}
		return x

	case *parser.InfixExpression:
		x.Left = walkExpr(x.Left, current, bound, notes, n)
		x.Right = walkExpr(x.Right, current, bound, notes, n)
	case *parser.PrefixExpression:
		x.Right = walkExpr(x.Right, current, bound, notes, n)
	case *parser.GroupedExpression:
		x.Expression = walkExpr(x.Expression, current, bound, notes, n)
	case *parser.IndexExpression:
		x.Left = walkExpr(x.Left, current, bound, notes, n)
		x.Index = walkExpr(x.Index, current, bound, notes, n)
	case *parser.SliceExpression:
		x.Left = walkExpr(x.Left, current, bound, notes, n)
		if x.Range != nil {
			x.Range.Start = walkExpr(x.Range.Start, current, bound, notes, n)
			x.Range.End = walkExpr(x.Range.End, current, bound, notes, n)
		}
	case *parser.RangeExpression:
		x.Start = walkExpr(x.Start, current, bound, notes, n)
		x.End = walkExpr(x.End, current, bound, notes, n)
	case *parser.AssignExpression:
		x.Value = walkExpr(x.Value, current, bound, notes, n) // leave Left (target) alone
	case *parser.ConditionalExpression:
		x.Condition = walkExpr(x.Condition, current, bound, notes, n)
		x.Consequence = walkExpr(x.Consequence, current, bound, notes, n)
		x.Alternative = walkExpr(x.Alternative, current, bound, notes, n)
	case *parser.IfExpression:
		x.Condition = walkExpr(x.Condition, current, bound, notes, n)
		if x.MatchedExpr != nil {
			x.MatchedExpr = walkExpr(x.MatchedExpr, current, bound, notes, n)
		}
		if x.Consequence != nil {
			for _, b := range x.Consequence.Statements {
				*n += walkStmt(b, current, bound, notes)
			}
		}
		if x.Alternative != nil {
			for _, b := range x.Alternative.Statements {
				*n += walkStmt(b, current, bound, notes)
			}
		}
	case *parser.RunExpression:
		x.Call = walkExpr(x.Call, current, bound, notes, n)
	case *parser.CoExpression:
		x.Call = walkExpr(x.Call, current, bound, notes, n)
	case *parser.AwaitExpression:
		x.Right = walkExpr(x.Right, current, bound, notes, n)
	case *parser.CastExpression:
		x.Expr = walkExpr(x.Expr, current, bound, notes, n)
	case *parser.ArrayLiteral:
		if x.Size != nil {
			x.Size = walkExpr(x.Size, current, bound, notes, n)
		}
		for i, el := range x.Elements {
			x.Elements[i] = walkExpr(el, current, bound, notes, n)
		}
	case *parser.SliceLiteral:
		for i, el := range x.Elements {
			x.Elements[i] = walkExpr(el, current, bound, notes, n)
		}
	case *parser.MapLiteral:
		for i, pr := range x.Pairs {
			pr.Value = walkExpr(pr.Value, current, bound, notes, n)
			x.Pairs[i] = pr
		}
	case *parser.StructLiteral:
		for _, fld := range x.Fields {
			if fld.Value != nil {
				fld.Value = walkExpr(fld.Value, current, bound, notes, n)
			}
		}
	case *parser.FunctionLiteral:
		if x.Body != nil {
			for _, b := range x.Body.Statements {
				*n += walkStmt(b, current, bound, notes)
			}
		}
	case *parser.DotExpression:
		// qualified method/field access: recurse receiver only (never the property,
		// and never treat the whole dotted chain as a bare reference).
		x.Receiver = walkExpr(x.Receiver, current, bound, notes, n)
		return x
	}
	return e
}

// qualifyTarget returns the owning module to prefix `name` with, or "" when the
// reference must stay bare. isConst selects the constant ownership table.
func qualifyTarget(name, current string, bound map[string]bool, isConst bool) string {
	if name == "" || strings.Contains(name, ".") {
		return ""
	}
	if defaultGlobals[name] {
		return ""
	}
	if !isConst && optionCtors[name] {
		return ""
	}
	if bound[name] {
		return "" // shadowed by a local/param/top-level binding in this file
	}
	table := fnOwners
	if isConst {
		table = constOwners
	}
	return uniqueCrossOwner(table, name, current)
}

// dotted rewrites an Identifier reference into a module-qualified DotExpression:
// `mod.name`. The formatter re-serializes this back to `mod.name(...)` / `mod.name`.
func dotted(ident *parser.Identifier, mod string) *parser.DotExpression {
	return &parser.DotExpression{
		Token:    ident.Token,
		Receiver: &parser.Identifier{Token: lexer.Token{Type: lexer.IDENT, Literal: mod}, Value: mod},
		Property: ident.Value,
	}
}

func printUnifiedDiff(orig, changed, path string) {
	oa := strings.Split(orig, "\n")
	cb := strings.Split(changed, "\n")
	max := len(oa)
	if len(cb) > max {
		max = len(cb)
		shown := 0
		for i := 0; i < max; i++ {
			var a, b string
			if i < len(oa) {
				a = oa[i]
			}
			if i < len(cb) {
				b = cb[i]
			}
			if a != b {
				fmt.Printf("  -L%d: %s\n  +L%d: %s\n", i+1, a, i+1, b)
				shown++
				if shown >= 60 {
					fmt.Printf("  ... (diff truncated)\n")
					break
				}
			}
		}
	}
}
