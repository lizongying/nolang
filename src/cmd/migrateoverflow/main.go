// Command migrateoverflow inserts `#{overflow = wrap}` above every top-level
// function definition in the given .no files. This preserves the pre-feature
// silent two's-complement wrap semantics for signed integer subtraction after
// the language changed the default to return option<int> on overflow.
//
// It uses the real nolang parser so only genuine top-level FunctionDefinition
// nodes are annotated (methods included). The operation is purely additive:
// even a misidentified insertion yields a harmless standalone annotation.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: migrateoverflow <file.no|dir> [...]")
		os.Exit(2)
	}
	// Expand directories recursively into .no file lists.
	var files []string
	for _, a := range args {
		info, err := os.Stat(a)
		if err != nil {
			fmt.Fprintf(os.Stderr, "SKIP %s: %v\n", a, err)
			continue
		}
		if info.IsDir() {
			err := filepath.WalkDir(a, func(p string, d os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if !d.IsDir() && strings.HasSuffix(p, ".no") {
					files = append(files, p)
				}
				return nil
			})
			if err != nil {
				fmt.Fprintf(os.Stderr, "WALK %s: %v\n", a, err)
			}
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
		n, err := migrateFile(path)
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

func migrateFile(path string) (int, error) {
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

	var defLines []int
	for _, stmt := range prog.Statements {
		if fd, ok := stmt.(*parser.FunctionDefinition); ok {
			defLines = append(defLines, fd.Token.Line)
		}
	}
	if len(defLines) == 0 {
		return 0, nil
	}

	lines := strings.Split(src, "\n")
	// Process bottom-up so earlier line indices stay valid.
	sort.Sort(sort.Reverse(sort.IntSlice(defLines)))
	inserted := 0
	for _, L := range defLines {
		idx := L - 1 // 0-based index of the def line
		if idx < 0 || idx >= len(lines) {
			continue
		}
		if alreadyOverflow(lines, idx) {
			continue
		}
		lines = append(lines[:idx], append([]string{"#{overflow = wrap}"}, lines[idx:]...)...)
		inserted++
	}
	if inserted > 0 {
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
