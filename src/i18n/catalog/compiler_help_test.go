package catalog_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/lizongying/nolang/i18n/catalog"
)

// englishHelpLines re-derives the English help block from the CLI source rather
// than from a frozen copy, so the gate cannot rot when printUsage changes: every
// fmt.Println/Printf call in printUsage contributes exactly one line.
//
// This mirrors what printUsage prints: Println appends a newline to its
// argument, and the single Printf format already carries its own trailing "\\n",
// so trimming one trailing newline per literal reconstructs the lines verbatim.
func englishHelpLines(t *testing.T) []string {
	t.Helper()
	path := filepath.Join("..", "..", "cmd", "no", "main.go")
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	var lines []string
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "printUsage" || fn.Body == nil {
			continue
		}
		for _, stmt := range fn.Body.List {
			expr, ok := stmt.(*ast.ExprStmt)
			if !ok {
				continue
			}
			call, ok := expr.X.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				continue
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				continue
			}
			if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "fmt" {
				continue
			}
			if sel.Sel.Name != "Println" && sel.Sel.Name != "Printf" {
				continue
			}
			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				continue
			}
			s, err := strconv.Unquote(lit.Value)
			if err != nil {
				t.Fatalf("unquote %s line: %v", fset.Position(lit.Pos()), err)
			}
			lines = append(lines, strings.TrimSuffix(s, "\n"))
		}
	}
	if len(lines) == 0 {
		t.Fatalf("no fmt.Println/Printf lines found in printUsage (%s)", path)
	}
	return lines
}

// helpCommandToken returns the leading indentation and the `no <verb>` token of a
// usage line, or ok=false when the line is prose. Command tokens are the part of
// the help that must survive translation untouched.
func helpCommandToken(line string) (indent int, token string, ok bool) {
	trimmed := strings.TrimLeft(line, " ")
	indent = len(line) - len(trimmed)
	if !strings.HasPrefix(trimmed, "no ") {
		return 0, "", false
	}
	fields := strings.Fields(trimmed)
	if len(fields) < 2 {
		return 0, "", false
	}
	return indent, fields[0] + " " + fields[1], true
}

// TestCompilerHelpStructureAllLocales holds every `<tag>-compiler.noml` to the
// shape of the English block.
//
// Parsing the committed file is half the test: these are real noml documents, so
// a typo in the single key, a value that is not an array of strings, or an
// element carrying an embedded newline is rejected here instead of silently
// degrading the locale to English. The other half is structural, because a
// translation is free text: a dropped line, a translated `no fmt` token, or a
// stray '%' (which makes fmt.Printf(s, version) print a %! verb error) is
// invisible to the parser.
func TestCompilerHelpStructureAllLocales(t *testing.T) {
	english := englishHelpLines(t)
	englishText := strings.Join(english, "\n")
	wantPct := strings.Count(englishText, "%")

	covered := 0
	for _, tag := range catalog.Locales() {
		if tag == "en" {
			continue // English is the inline default, no file by design
		}
		name := strings.ToLower(tag) + catalog.CompilerHelpSuffix
		path := filepath.Join("locales", name)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("%s: %v (this locale falls back to English)", tag, err)
			continue
		}
		content, err := catalog.ParseCompilerHelp(path, data)
		if err != nil {
			t.Errorf("%s: %v", tag, err)
			continue
		}
		covered++
		lines := strings.Split(content, "\n")
		for i, line := range lines {
			// The lexer ends a raw string at the first line whose first
			// non-space character is a backtick, so such a line truncates the
			// block (and the truncation usually fails to parse). Keep the check
			// anyway: it names the real cause instead of leaving a puzzling
			// line-count report.
			if strings.HasPrefix(strings.TrimLeft(line, " \t"), "`") {
				t.Errorf("%s:%d: line starts with a backtick, which closes the raw string: %q",
					tag, i+1, line)
			}
		}

		// The runtime path must return exactly this text plus the trailing
		// newline a raw string cannot carry.
		text, ok := catalog.LoadCompilerHelp(tag)
		if !ok {
			t.Errorf("%s: LoadCompilerHelp not ok although %s parses", tag, name)
			continue
		}
		if want := content + "\n"; text != want {
			t.Errorf("%s: LoadCompilerHelp rendered %d bytes, want %d (content + newline)",
				tag, len(text), len(want))
			continue
		}
		if !utf8.ValidString(text) {
			t.Errorf("%s: help file is not valid UTF-8", tag)
			continue
		}
		if gotPct := strings.Count(text, "%"); gotPct != wantPct {
			t.Errorf("%s: %d '%%' characters, want %d (only the version placeholder is allowed)",
				tag, gotPct, wantPct)
		}
		if rendered := fmt.Sprintf(text, "VER"); strings.Contains(rendered, "%!") {
			t.Errorf("%s: help file breaks fmt.Printf (stray format verb): %q",
				tag, snippetAround(rendered, "%!"))
		}
		if text == englishText {
			t.Errorf("%s: help file is an untranslated copy of the English block", tag)
		}

		if len(lines) != len(english) {
			t.Errorf("%s: %d lines, want %d (the English block); a line was added or dropped",
				tag, len(lines), len(english))
			continue
		}
		for i := range english {
			wantBlank := strings.TrimSpace(english[i]) == ""
			gotBlank := strings.TrimSpace(lines[i]) == ""
			if wantBlank != gotBlank {
				t.Errorf("%s:%d: blank line does not match English layout: en=%q loc=%q",
					tag, i+1, english[i], lines[i])
			}
			enIndent, enToken, enIsCmd := helpCommandToken(english[i])
			locIndent, locToken, locIsCmd := helpCommandToken(lines[i])
			if enIsCmd != locIsCmd {
				t.Errorf("%s:%d: command line mismatch: en=%q loc=%q", tag, i+1, english[i], lines[i])
				continue
			}
			if enIsCmd && (enToken != locToken || enIndent != locIndent) {
				t.Errorf("%s:%d: command token must stay verbatim: en=%q loc=%q", tag, i+1, enToken, locToken)
			}
		}
	}

	if covered == 0 {
		t.Fatal("no compiler help translations were checked")
	}
}

// TestCompilerHelpSchema pins the decoder contract: one key, a non-empty raw
// string. The last two cases are the ones the lexer imposes: a value that is not
// a string, and a content line beginning with a backtick, which closes the raw
// string early and leaves the remainder to be parsed as statements.
func TestCompilerHelpSchema(t *testing.T) {
	good := "{\n  'compiler': `\na\n\nb't `no` c\n`\n}\n"
	content, err := catalog.ParseCompilerHelp("good", []byte(good))
	if err != nil {
		t.Fatalf("good document rejected: %v", err)
	}
	if want := "a\n\nb't `no` c"; content != want {
		t.Errorf("content = %q, want %q (raw string keeps quotes and inner backticks verbatim)", content, want)
	}

	bad := map[string]string{
		"missing key":      "{\n  'help': `\na\n`\n}\n",
		"wrong key":        "{\n  'compilers': `\na\n`\n}\n",
		"extra key":        "{\n  'compiler': `\na\n`,\n  'other': `\nb\n`\n}\n",
		"array value":      "{\n  'compiler': ['a', 'b']\n}\n",
		"number value":     "{\n  'compiler': 1\n}\n",
		"empty value":      "{\n  'compiler': `\n`\n}\n",
		"backtick at line": "{\n  'compiler': `\nline1\n  `no` runs\nline3\n`\n}\n",
		"not noml at all":  "Nolang 编程语言（版本 %s）\n",
	}
	for name, src := range bad {
		if _, err := catalog.ParseCompilerHelp(name, []byte(src)); err == nil {
			t.Errorf("%s: accepted, want a schema error", name)
		}
	}
}

// TestCompilerHelpFallbacks pins the loader contract the CLI relies on: English
// and unknown tags return ok=false so printUsage falls back to its inline block.
func TestCompilerHelpFallbacks(t *testing.T) {
	for _, lang := range []string{"", "en", "en-US", "xx-YY"} {
		if _, ok := catalog.LoadCompilerHelp(lang); ok {
			t.Errorf("LoadCompilerHelp(%q) = ok, want fallback to the inline English help", lang)
		}
	}
	for _, lang := range []string{"zh-cn", "zh-tw", "ja"} {
		if _, ok := catalog.LoadCompilerHelp(lang); !ok {
			t.Errorf("LoadCompilerHelp(%q) = !ok, want an embedded translation", lang)
		}
	}
}

// snippetAround returns a short window of s centred on the first occurrence of
// marker, so a failure message points at the offending character instead of
// dumping the whole help block.
func snippetAround(s, marker string) string {
	i := strings.Index(s, marker)
	if i < 0 {
		i = 0
	}
	const window = 40
	start := i - window
	if start < 0 {
		start = 0
	}
	end := i + window
	if end > len(s) {
		end = len(s)
	}
	return s[start:end]
}
