// Command rmredundantoverflow removes `#{overflow = <mode>}` annotations that
// govern NO integer arithmetic, i.e. annotations that are provably inert.
//
// Background: an earlier migration (migrateoverflowstmt) inserted
// `#{overflow = wrap}` above every source line whose text contained an
// arithmetic character (`+ - * /`), using a regex heuristic. That heuristic
// over-annotates: it also fires inside URL strings (`get('https://x')`),
// regexes, `a/b` path tokens, and above statements that contain no actual
// integer math at all. The compiler's `#{overflow}` mode only affects `+ - * /`
// (see build/llvm/expr.go: emitOverflowArith is consulted only for add/sub/mul,
// and the checker's overflowArithOps = {+, -, *, /}). An annotation whose
// governing statement subtree contains none of those operators is therefore
// inert and safe to delete — removing it can never change semantics or break
// parsing.
//
// This tool detects redundancy with TWO independent signals (both must agree
// "no arithmetic" before a line is removed, for maximum safety):
//  1. AST walk: the governed statement subtree contains no InfixExpression with
//     operator in {+, -, *, /} (mirrors checker.walkExprForIntOverflow).
//  2. Text fallback: the statement's source line range contains no arithmetic
//     operator outside strings/comments (reuses migrateoverflowstmt's heuristic).
//
// Only when BOTH say "no arithmetic" is the annotation line deleted. Files that
// fail to re-parse after the edit are left untouched.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// overflowRe matches a standalone `#{overflow = <mode>}` annotation line.
var overflowRe = regexp.MustCompile(`^#\{\s*overflow\s*=\s*([\w-]+)\s*\}\s*$`)

// keepOps: operators the #{overflow} annotation actually affects. Matches
// checker.overflowArithOps and the codegen paths (add/sub/mul(/div)).
var keepOps = map[string]bool{"+": true, "-": true, "*": true, "/": true}

// opRe matches a binary arithmetic operator between two operands, catching
// operators without surrounding spaces (e.g. `a*b`, `a/b`). Excludes `-`
// (identifiers contain hyphens) and `->` (arrow). Used only as a SAFETY
// fallback so we never delete an annotation a gap in the AST walk missed.
var opRe = regexp.MustCompile(`[A-Za-z0-9_\)\]\}]\s*[\+\*/]\s*[A-Za-z0-9_\(\[\{]`)

type stmtInfo struct {
	node parser.Statement
	line int // 1-based start line
}

func main() {
	dry := false
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "-dry" {
		dry = true
		args = args[1:]
	}
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: rmredundantoverflow [-dry] <file.no|dir> [...]")
		os.Exit(2)
	}

	var files []string
	for _, a := range args {
		info, err := os.Stat(a)
		if err != nil {
			fmt.Fprintf(os.Stderr, "SKIP %s: %v\n", a, err)
			continue
		}
		if info.IsDir() {
			_ = filepath.WalkDir(a, func(p string, d os.DirEntry, err error) error {
				if err == nil && !d.IsDir() && strings.HasSuffix(p, ".no") {
					files = append(files, p)
				}
				return nil
			})
		} else {
			files = append(files, a)
		}
	}
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "no .no files to process")
		os.Exit(2)
	}

	totalFiles, totalRemoved := 0, 0
	for _, path := range files {
		removed, err := processFile(path, dry)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERR %s: %v\n", path, err)
			continue
		}
		if removed > 0 {
			totalFiles++
			totalRemoved += removed
			fmt.Printf("%s: removed=%d\n", path, removed)
		}
	}
	fmt.Printf("TOTAL: files=%d removed=%d\n", totalFiles, totalRemoved)
}

func processFile(path string, dry bool) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	src := string(data)
	lx := lexer.New(src)
	p := parser.New(lx)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		lim := errs
		if len(lim) > 3 {
			lim = lim[:3]
		}
		return 0, fmt.Errorf("parse errors: %v", lim)
	}
	srcLines := strings.Split(src, "\n")

	// Collect every statement with its 1-based start line.
	var stmts []stmtInfo
	collectStmts(prog.Statements, &stmts)

	// Determine which annotation lines are redundant.
	removeSet := map[int]bool{} // 0-based annotation line index -> true
	for _, si := range stmts {
		annIdx, ok := hasPrecedingAnnotation(srcLines, si.line)
		if !ok {
			continue
		}
		end := si.node.EndPos().Line
		if end < si.line {
			end = si.line
		}
		// Keep if the governed subtree has any + - * / (AST), or if the text
		// fallback finds arithmetic in the statement's source range.
		if stmtHasArith(si.node) {
			continue
		}
		if textRangeHasArith(srcLines, si.line, end) {
			continue
		}
		removeSet[annIdx] = true
	}
	if len(removeSet) == 0 {
		return 0, nil
	}

	// Apply deletions.
	newLines := make([]string, 0, len(srcLines)-len(removeSet))
	for i, line := range srcLines {
		if removeSet[i] {
			continue
		}
		newLines = append(newLines, line)
	}
	newSrc := strings.Join(newLines, "\n")

	if !dry {
		// Final safety re-parse of the rewrite.
		lxf := lexer.New(newSrc)
		pf := parser.New(lxf)
		pf.ParseProgram()
		if errs := pf.Errors(); len(errs) > 0 {
			lim := errs
			if len(lim) > 3 {
				lim = lim[:3]
			}
			return 0, fmt.Errorf("rewrite parse errors: %v", lim)
		}
		if err := os.WriteFile(path, []byte(newSrc), 0644); err != nil {
			return 0, err
		}
	}
	return len(removeSet), nil
}

// collectStmts appends every statement (and recurses into nested blocks,
// for/if bodies, and function/closure bodies) together with its 1-based start
// line. Recursing into children means both function-level and statement-level
// annotations are examined.
func collectStmts(stmts []parser.Statement, out *[]stmtInfo) {
	for _, s := range stmts {
		*out = append(*out, stmtInfo{s, s.Pos().Line})
		switch st := s.(type) {
		case *parser.BlockStatement:
			collectStmts(st.Statements, out)
		case *parser.ForStatement:
			if st.Init != nil {
				collectStmts([]parser.Statement{st.Init}, out)
			}
			if st.Update != nil {
				collectStmts([]parser.Statement{st.Update}, out)
			}
			if st.Body != nil {
				collectStmts(st.Body.Statements, out)
			}
		case *parser.ExpressionStatement:
			if ie, ok := st.Expression.(*parser.IfExpression); ok {
				if ie.Consequence != nil {
					collectStmts(ie.Consequence.Statements, out)
				}
				if ie.Alternative != nil {
					collectStmts(ie.Alternative.Statements, out)
				}
			}
		case *parser.FunctionDefinition:
			if st.Body != nil {
				collectStmts(st.Body.Statements, out)
			}
		case *parser.LetStatement:
			if fl, ok := st.Value.(*parser.FunctionLiteral); ok && fl.Body != nil {
				collectStmts(fl.Body.Statements, out)
			}
		}
	}
}

// hasPrecedingAnnotation reports the 0-based line index of an `#{overflow}`
// annotation immediately above the statement at 1-based `line` (skipping blank
// and `;`-comment lines). Returns ok=false if there is none.
func hasPrecedingAnnotation(srcLines []string, line int) (int, bool) {
	idx := line - 1 // 0-based of the statement's own line
	j := idx - 1
	for j >= 0 {
		t := strings.TrimSpace(srcLines[j])
		if t == "" {
			j--
			continue
		}
		if strings.HasPrefix(t, ";") {
			j--
			continue
		}
		if overflowRe.MatchString(t) {
			return j, true
		}
		break
	}
	return -1, false
}

// stmtHasArith walks a statement subtree for any InfixExpression with operator
// in keepOps. Mirrors checker.walkExprForIntOverflow.
func stmtHasArith(s parser.Statement) bool {
	if s == nil {
		return false
	}
	switch st := s.(type) {
	case *parser.BlockStatement:
		for _, b := range st.Statements {
			if stmtHasArith(b) {
				return true
			}
		}
	case *parser.ForStatement:
		if exprHasArith(st.Condition) {
			return true
		}
		if st.Init != nil && stmtHasArith(st.Init) {
			return true
		}
		if st.Update != nil && stmtHasArith(st.Update) {
			return true
		}
		if st.Body != nil {
			for _, b := range st.Body.Statements {
				if stmtHasArith(b) {
					return true
				}
			}
		}
	case *parser.ExpressionStatement:
		if exprHasArith(st.Expression) {
			return true
		}
		if ie, ok := st.Expression.(*parser.IfExpression); ok {
			if blockHasArith(ie.Consequence) || blockHasArith(ie.Alternative) || blockHasArith(ie.DotValBody) {
				return true
			}
		}
	case *parser.LetStatement:
		if exprHasArith(st.Value) {
			return true
		}
		if fl, ok := st.Value.(*parser.FunctionLiteral); ok && fl.Body != nil {
			for _, b := range fl.Body.Statements {
				if stmtHasArith(b) {
					return true
				}
			}
		}
	case *parser.ReturnStatement:
		if exprHasArith(st.ReturnValue) {
			return true
		}
	case *parser.MultiAssignStatement:
		if exprHasArith(st.Value) {
			return true
		}
	case *parser.UnwrapAssignStatement:
		if exprHasArith(st.Value) {
			return true
		}
	case *parser.FunctionDefinition:
		if st.Body != nil {
			for _, b := range st.Body.Statements {
				if stmtHasArith(b) {
					return true
				}
			}
		}
	}
	return false
}

func blockHasArith(b *parser.BlockStatement) bool {
	if b == nil {
		return false
	}
	for _, s := range b.Statements {
		if stmtHasArith(s) {
			return true
		}
	}
	return false
}

func exprHasArith(e parser.Expression) bool {
	if e == nil {
		return false
	}
	switch x := e.(type) {
	case *parser.InfixExpression:
		if keepOps[x.Operator] {
			return true
		}
		if exprHasArith(x.Left) || exprHasArith(x.Right) {
			return true
		}
	case *parser.PrefixExpression:
		if exprHasArith(x.Right) {
			return true
		}
	case *parser.CallExpression:
		if exprHasArith(x.Function) {
			return true
		}
		for _, a := range x.Arguments {
			if exprHasArith(a) {
				return true
			}
		}
	case *parser.IfExpression:
		if exprHasArith(x.Condition) || blockHasArith(x.Consequence) || blockHasArith(x.Alternative) || blockHasArith(x.DotValBody) {
			return true
		}
	case *parser.IndexExpression:
		if exprHasArith(x.Left) || exprHasArith(x.Index) {
			return true
		}
	case *parser.AssignExpression:
		if exprHasArith(x.Left) || exprHasArith(x.Value) {
			return true
		}
	case *parser.ArrayLiteral:
		for _, el := range x.Elements {
			if exprHasArith(el) {
				return true
			}
		}
	}
	return false
}

// textRangeHasArith is the SAFETY fallback: true if any source line in
// [start, end] (1-based) contains an arithmetic operator outside `;`-comments.
func textRangeHasArith(srcLines []string, start, end int) bool {
	if end < start {
		end = start
	}
	for ln := start; ln <= end; ln++ {
		if ln < 1 || ln > len(srcLines) {
			continue
		}
		if lineNeedsAnnotation(srcLines[ln-1]) {
			return true
		}
	}
	return false
}

func lineNeedsAnnotation(raw string) bool {
	// Strip string/regex literals FIRST so an operator that only appears inside
	// a quoted literal (e.g. the '/' in `get('https://x')`) is not mistaken for
	// arithmetic. A stripper bug can only cause a false KEEP (safe), never a
	// false REMOVE, because removal also requires the AST gate to agree.
	raw = stripStrings(raw)
	if i := strings.Index(raw, ";"); i >= 0 {
		raw = raw[:i]
	}
	if strings.TrimSpace(raw) == "" {
		return false
	}
	if strings.Contains(raw, " + ") || strings.Contains(raw, " - ") ||
		strings.Contains(raw, " * ") || strings.Contains(raw, " / ") {
		return true
	}
	return opRe.MatchString(raw)
}

// stripStrings returns raw with the contents of string literals (delimited by
// ' or ") replaced by spaces, so quoted operators are ignored. Escapes (\')
// are skipped.
func stripStrings(raw string) string {
	var b strings.Builder
	b.Grow(len(raw))
	inStr := false
	var q byte
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if inStr {
			if c == '\\' && i+1 < len(raw) {
				i++ // skip escaped char
				continue
			}
			if c == q {
				inStr = false
			}
			continue
		}
		if c == '\'' || c == '"' {
			inStr = true
			q = c
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}
