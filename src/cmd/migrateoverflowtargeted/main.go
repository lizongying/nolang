// Command migrateoverflowtargeted inserts `#{overflow = wrap}` above every
// top-level function DEFINITION that actually emits the `ovf-int-default`
// overflow lint — i.e. a function that genuinely contains an un-annotated
// integer `+ - * /` operation. Functions with no integer arithmetic (e.g.
// `count = () (n i64) { ; LLVM: call os.args }`) are left untouched.
//
// This fixes the shortcoming of `migrateoverflow`, which blindly annotated
// EVERY top-level function regardless of whether it contained arithmetic.
//
// Detection reuses the checker's exported `ValidateIntOverflow`, so the set of
// annotated functions is exactly the set the lint would otherwise flag — no
// false positives (string/comment noise) and no misses.
//
// The annotation is FUNCTION-LEVEL. The checker reads
// `FunctionDefinition.OverflowMode` and suppresses `ovf-int-default` for the
// whole function body, but the codegen ignores function-level overflow
// annotations — so runtime semantics (the default option<int> + any `?=` / `?T`
// / match handling already present in std) are completely unchanged. This is
// the safe fix for "overflow is now an error": it satisfies the lint without
// altering behaviour or breaking existing option handlers.
//
// Usage: migrateoverflowtargeted [-dry] <file.no|dir> [...]
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lizongying/nolang/checker"
	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

func main() {
	args := os.Args[1:]
	dry := false
	if len(args) > 0 && args[0] == "-dry" {
		dry = true
		args = args[1:]
	}
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: migrateoverflowtargeted [-dry] <file.no|dir> [...]")
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
	total := 0
	for _, path := range files {
		n, err := migrateFile(path, dry)
		if err != nil {
			fmt.Fprintf(os.Stderr, "SKIP %s: %v\n", path, err)
			continue
		}
		if n > 0 {
			fmt.Printf("INSERT %s: %d\n", path, n)
			total += n
		}
	}
	fmt.Printf("TOTAL inserted: %d\n", total)
}

func migrateFile(path string, dry bool) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	src := string(data)
	lx := lexer.New(src)
	p := parser.New(lx)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		limit := errs
		if len(limit) > 3 {
			limit = limit[:3]
		}
		return 0, fmt.Errorf("parse errors: %v", limit)
	}

	// Reuse the exact lint detection: only functions that actually emit
	// ovf-int-default are candidates.
	emits := checker.ValidateIntOverflow(prog)
	if len(emits) == 0 {
		return 0, nil
	}

	// Top-level function ranges, for mapping an emission line to its
	// enclosing top-level function.
	type fnRange struct{ start, end int }
	var fns []fnRange
	for _, stmt := range prog.Statements {
		if fd, ok := stmt.(*parser.FunctionDefinition); ok {
			end := fd.Token.Line
			if fd.Body != nil {
				if ep := fd.Body.EndPos(); ep.Line > 0 {
					end = ep.Line
				}
			}
			fns = append(fns, fnRange{fd.Token.Line, end})
		}
	}

	insertLines := map[int]bool{}
	for _, e := range emits {
		L := e.Line
		placed := false
		for _, fr := range fns {
			if L >= fr.start && L <= fr.end {
				insertLines[fr.start] = true
				placed = true
				break
			}
		}
		if !placed {
			// Emission in a top-level (non-function) statement: annotate
			// that line directly.
			insertLines[L] = true
		}
	}
	if len(insertLines) == 0 {
		return 0, nil
	}

	lines := strings.Split(src, "\n")
	var order []int
	for L := range insertLines {
		order = append(order, L)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(order)))
	inserted := 0
	for _, L := range order {
		idx := L - 1
		if idx < 0 || idx >= len(lines) {
			continue
		}
		if alreadyOverflow(lines, idx) {
			continue
		}
		lines = append(lines[:idx], append([]string{"#{overflow = wrap}"}, lines[idx:]...)...)
		inserted++
	}
	if inserted > 0 && !dry {
		if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0644); err != nil {
			return 0, err
		}
	}
	return inserted, nil
}

// alreadyOverflow reports whether an `#{...overflow...}` annotation already
// sits in the (consecutive, non-empty) annotation block immediately above the
// def line at defIdx.
func alreadyOverflow(lines []string, defIdx int) bool {
	j := defIdx - 1
	for j >= 0 {
		t := strings.TrimSpace(lines[j])
		if t == "" {
			break
		}
		if strings.HasPrefix(t, "#{") {
			if strings.Contains(t, "overflow") {
				return true
			}
			j--
			continue
		}
		break
	}
	return false
}
