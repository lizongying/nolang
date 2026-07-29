package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sort"
)

func main() {
	srcPath := "src/build/transpiler.go"
	srcBytes, _ := os.ReadFile(srcPath)
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, srcPath, srcBytes, parser.ParseComments)
	if err != nil {
		panic(err)
	}

	extraFuncs := map[string]bool{"inferExprType": true}
	extraTypes := map[string]bool{
		"ValidateResult": true, "ModuleExport": true, "StdModuleInfo": true,
		"JsModuleInfo": true, "funcSig": true, "validationFuncTypes": true,
	}

	type span struct {
		start, end int
		name       string
	}
	var spans []span

	for _, decl := range file.Decls {
		start := fset.Position(decl.Pos()).Offset
		end := fset.Position(decl.End()).Offset
		sLine := fset.Position(decl.Pos()).Line
		eLine := fset.Position(decl.End()).Line

		inRange := sLine >= 4206 && eLine <= 8870
		include := inRange
		var name string
		switch d := decl.(type) {
		case *ast.FuncDecl:
			name = d.Name.Name
			if extraFuncs[name] {
				include = true
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					name = s.Name.Name
					if extraTypes[name] {
						include = true
					}
				case *ast.ValueSpec:
					if len(s.Names) > 0 {
						name = s.Names[0].Name
						if extraTypes[name] {
							include = true
						}
					}
				}
			}
		}
		if include {
			spans = append(spans, span{start, end, name})
		}
	}

	sort.Slice(spans, func(i, j int) bool { return spans[i].start < spans[j].start })

	// Build checker.go
	var out []byte
	out = append(out, []byte("package checker\n\n")...)
	out = append(out, []byte("import (\n")...)
	out = append(out, []byte("\t\"bytes\"\n\t\"errors\"\n\t\"fmt\"\n\t\"os\"\n\t\"path/filepath\"\n\t\"regexp\"\n\t\"sort\"\n\t\"strconv\"\n\t\"strings\"\n\t\"sync\"\n\n\t\"github.com/lizongying/nolang/lexer\"\n\t\"github.com/lizongying/nolang/parser\"\n)\n\n")...)
	out = append(out, []byte("// Migrated from build/transpiler.go: semantic-checker subsystem\n// (validators + module/type resolution helpers).\n\n")...)

	// Extract each span and append
	for _, s := range spans {
		block := srcBytes[s.start:s.end]
		out = append(out, block...)
		out = append(out, '\n')
	}

	if err := os.WriteFile("src/checker/checker.go", out, 0o644); err != nil {
		panic(err)
	}

	// Rebuild transpiler.go without the spans
	sort.Slice(spans, func(i, j int) bool { return spans[i].start > spans[j].start })
	newSrc := make([]byte, len(srcBytes))
	copy(newSrc, srcBytes)
	for _, s := range spans {
		// blank the span (keep a marker comment line)
		for i := s.start; i < s.end; i++ {
			if newSrc[i] == '\n' {
				continue
			}
			newSrc[i] = ' '
		}
	}
	// collapse multiple blank lines
	var collapsed []byte
	prevNL := false
	for _, c := range newSrc {
		if c == '\n' {
			if prevNL {
				continue
			}
			prevNL = true
		} else {
			prevNL = false
		}
		collapsed = append(collapsed, c)
	}
	if err := os.WriteFile(srcPath, collapsed, 0o644); err != nil {
		panic(err)
	}

	os.Stdout.WriteString("extracted spans: " + itoa(len(spans)) + "\n")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
