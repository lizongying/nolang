// Command migrateoverflowstmt converts function-level `#{overflow = <mode>}`
// annotations into statement-level ones.
//
// Background: an earlier migration (`migrateoverflow`) inserted
// `#{overflow = wrap}` directly above every top-level function definition to
// preserve the pre-feature silent two's-complement wrap semantics after the
// language changed the default to return option<int> on overflow.
//
// The intended / LSP-quickfix convention, however, is to place the annotation
// ABOVE the specific operation line that may overflow, with the same indentation
// as that line — e.g.
//
//	rotr32 = (x u32, n i64) (r u32) {
//	    #{overflow = wrap}
//	    r = ((x >> n) | (x << (32 - n))) & BLAKE2S-MASK
//	}
//
// This tool rewrites every function (and method) that currently carries a
// function/method-level overflow annotation by:
//  1. Removing the function/method-level annotation line.
//  2. Inserting `#{overflow = <mode>}` (preserving the exact mode: wrap,
//     clamp0, min, max, saturate, u8-max, …) directly above every statement
//     whose source line(s) contain an integer binary arithmetic operator
//     (+, -, *, /). Indentation matches the statement's own leading whitespace.
//
// Statement-level annotations are fully supported by the compiler:
// generateStatement reads the per-statement annotation and sets
// curOverflowMode for that statement's codegen, exactly mirroring the old
// function-level behaviour (which wrapped every operation in the function).
// Over-annotating is harmless (curOverflowMode only affects emitOverflowArith);
// under-annotating is caught by the build (an un-annotated op returns
// option<int>, which cannot assign to a plain int variable).
//
// It uses the real nolang parser so only genuine function/method definitions
// annotated at the definition level are rewritten. The rewritten file is
// re-parsed; on any parse error the file is left untouched.
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

// opRe matches a binary arithmetic operator (+, *, /) between two operands, so
// we can flag operator occurrences that lack surrounding spaces (e.g. `a*b`,
// `a/b`, `a+b`). It deliberately EXCLUDES `-` because nolang identifiers
// contain hyphens (`blake2b-g`, `BLAKE2B-MASK`); subtraction is instead
// detected only with surrounding whitespace via the " - " check below. It also
// excludes `->` (arrow).
var opRe = regexp.MustCompile(`[A-Za-z0-9_\)\]\}]\s*[\+\*/]\s*[A-Za-z0-9_\(\[\{]`)

func main() {
	args := os.Args[1:]
	dry := false
	if len(args) > 0 && args[0] == "-dry" {
		dry = true
		args = args[1:]
	}
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: migrateoverflowstmt [-dry] <file.no|dir> [...]")
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

	totalFiles, totalFns, totalIns, totalDel := 0, 0, 0, 0
	for _, path := range files {
		fns, ins, del, err := migrateFile(path, dry)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERR %s: %v\n", path, err)
			continue
		}
		if fns > 0 || ins > 0 || del > 0 {
			totalFiles++
			totalFns += fns
			totalIns += ins
			totalDel += del
			fmt.Printf("%s: fns=%d inserted=%d removed=%d\n", path, fns, ins, del)
		}
	}
	fmt.Printf("TOTAL: files=%d fns=%d inserted=%d removed=%d\n", totalFiles, totalFns, totalIns, totalDel)
}

type target struct {
	defLineIdx int // 0-based index of the definition line
	annLineIdx  int // 0-based index of the annotation line to remove (-1 if none)
	mode        string
	body        *parser.BlockStatement
}

func migrateFile(path string, dry bool) (int, int, int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, 0, err
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
		return 0, 0, 0, fmt.Errorf("parse errors: %v", lim)
	}
	srcLines := strings.Split(src, "\n")

	// Identify target functions/methods that carry a definition-level overflow
	// annotation directly above the definition line.
	var targets []target
	for _, stmt := range prog.Statements {
		switch s := stmt.(type) {
		case *parser.FunctionDefinition:
			if s.Body == nil {
				continue
			}
			defLineIdx := s.Token.Line - 1
			if annLineIdx, mode := findOverflowAbove(srcLines, defLineIdx); annLineIdx >= 0 {
				targets = append(targets, target{defLineIdx, annLineIdx, mode, s.Body})
			}
		case *parser.LetStatement:
			fl, ok := s.Value.(*parser.FunctionLiteral)
			if !ok || fl.Body == nil {
				continue
			}
			defLineIdx := s.Token.Line - 1
			if annLineIdx, mode := findOverflowAbove(srcLines, defLineIdx); annLineIdx >= 0 {
				targets = append(targets, target{defLineIdx, annLineIdx, mode, fl.Body})
			}
		}
	}
	if len(targets) == 0 {
		return 0, 0, 0, nil
	}

	// For each target function, compute its own rewrite, then trial-parse the
	// WHOLE file with ONLY that function converted. If the trial fails to parse,
	// the function almost certainly contains a `{ } (cond)` post-condition loop
	// whose brace-matching is confused by the annotation's `}`; in that case we
	// keep the function-level annotation in place (the build still type-checks,
	// since function-level overflow covers every op in the function). This keeps
	// the migration safe: we never emit a file that fails to parse.
	type trial struct {
		t           target
		insertAbove map[int]string
		deleteSet   map[int]bool
		convertible bool
	}
	var trials []trial
	for _, t := range targets {
		insertAbove := map[int]string{}
		deleteSet := map[int]bool{}
		opLines := map[int]bool{}
		collectOpLines(t.body.Statements, srcLines, opLines)
		for ln := range opLines { // ln is 1-based
			idx := ln - 1
			if idx < 0 || idx >= len(srcLines) {
				continue
			}
			// Skip if an overflow annotation already sits directly above this line.
			if idx-1 >= 0 && overflowRe.MatchString(strings.TrimSpace(srcLines[idx-1])) {
				continue
			}
			indent := leadingWS(srcLines[idx])
			insertAbove[idx] = indent + "#{overflow = " + t.mode + "}"
		}
		if t.annLineIdx >= 0 {
			deleteSet[t.annLineIdx] = true
		}
		conv := false
		if len(insertAbove) > 0 || len(deleteSet) > 0 {
			cand := applyEdits(srcLines, insertAbove, deleteSet)
			lxc := lexer.New(strings.Join(cand, "\n"))
			pc := parser.New(lxc)
			pc.ParseProgram()
			conv = len(pc.Errors()) == 0
		}
		trials = append(trials, trial{t, insertAbove, deleteSet, conv})
	}

	// Combine only the convertible functions.
	combInsert := map[int]string{}
	combDelete := map[int]bool{}
	converted, skipped := 0, 0
	for _, tr := range trials {
		if tr.convertible {
			converted++
			for k, v := range tr.insertAbove {
				combInsert[k] = v
			}
			for k := range tr.deleteSet {
				combDelete[k] = true
			}
		} else {
			skipped++
		}
	}
	if skipped > 0 {
		fmt.Fprintf(os.Stderr, "KEEP %s: %d function(s) kept function-level (contain cond-loop)\n", path, skipped)
	}
	if len(combInsert) == 0 && len(combDelete) == 0 {
		return converted + skipped, 0, 0, nil
	}

	newLines := applyEdits(srcLines, combInsert, combDelete)
	newSrc := strings.Join(newLines, "\n")

	if !dry {
		// Final safety re-parse of the combined rewrite.
		lxf := lexer.New(newSrc)
		pf := parser.New(lxf)
		pf.ParseProgram()
		if errs := pf.Errors(); len(errs) > 0 {
			lim := errs
			if len(lim) > 3 {
				lim = lim[:3]
			}
			return 0, 0, 0, fmt.Errorf("combined rewrite parse errors: %v", lim)
		}
		if err := os.WriteFile(path, []byte(newSrc), 0644); err != nil {
			return 0, 0, 0, err
		}
	}

	return converted + skipped, len(combInsert), len(combDelete), nil
}

// applyEdits builds a new line slice from the original: lines in deleteSet are
// omitted; for each index in insertAbove the annotation text is inserted
// immediately before that line.
func applyEdits(srcLines []string, insertAbove map[int]string, deleteSet map[int]bool) []string {
	var out []string
	for i, line := range srcLines {
		if deleteSet[i] {
			continue
		}
		if text, ok := insertAbove[i]; ok {
			out = append(out, text)
		}
		out = append(out, line)
	}
	return out
}

// findOverflowAbove scans upward from defLineIdx, skipping blank and `;`-comment
// lines, looking for a standalone `#{overflow = <mode>}` annotation. Returns the
// 0-based line index of that annotation and the mode, or (-1, "") if none.
func findOverflowAbove(srcLines []string, defLineIdx int) (int, string) {
	j := defLineIdx - 1
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
		if m := overflowRe.FindStringSubmatch(t); m != nil {
			return j, m[1]
		}
		break
	}
	return -1, ""
}

// collectOpLines walks statements (recursing into block/if/for bodies, but NOT
// into nested function literals / closures which have their own overflow scope)
// and marks the 1-based start line of every statement whose source line(s)
// contain an integer binary arithmetic operator.
//
// Control-flow statements (for / if) are marked only by their own header line
// (so an operation in the condition is covered) and their bodies are recursed
// into; this avoids redundantly annotating both a loop header and every inner
// statement. Leaf statements are checked across their full line range to catch
// operators that span multiple source lines.
func collectOpLines(stmts []parser.Statement, srcLines []string, out map[int]bool) {
	for _, s := range stmts {
		start := s.Pos().Line
		end := s.EndPos().Line
		switch st := s.(type) {
		case *parser.ForStatement:
			if start > 0 && lineRangeNeedsAnnotation(srcLines, start, start) {
				out[start] = true
			}
			if st.Body != nil {
				collectOpLines(st.Body.Statements, srcLines, out)
			}
		case *parser.ExpressionStatement:
			if ie, ok := st.Expression.(*parser.IfExpression); ok {
				if start > 0 && lineRangeNeedsAnnotation(srcLines, start, start) {
					out[start] = true
				}
				if ie.Consequence != nil {
					collectOpLines(ie.Consequence.Statements, srcLines, out)
				}
				if ie.Alternative != nil {
					collectOpLines(ie.Alternative.Statements, srcLines, out)
				}
			} else if start > 0 && lineRangeNeedsAnnotation(srcLines, start, end) {
				out[start] = true
			}
		case *parser.BlockStatement:
			collectOpLines(st.Statements, srcLines, out)
		default:
			if start > 0 && lineRangeNeedsAnnotation(srcLines, start, end) {
				out[start] = true
			}
		}
	}
}

func lineRangeNeedsAnnotation(srcLines []string, start, end int) bool {
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
	// nolang comments start with ';' — strip them before scanning operators.
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

func leadingWS(s string) string {
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	return s[:i]
}
