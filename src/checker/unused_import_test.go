package checker

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

func parseForUnusedImport(t *testing.T, src string) *parser.Program {
	t.Helper()
	p := parser.New(lexer.New(src))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse failed: %v", errs)
	}
	return prog
}

func TestValidateUnusedImports(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		wantHit bool
	}{
		{
			name:    "unused module import is hinted",
			src:     "# /glossary/src/nav\nprint('hi')\n",
			wantHit: true,
		},
		{
			name:    "underscore alias silences the hint",
			src:     "# /glossary/src/nav _\nprint('hi')\n",
			wantHit: false,
		},
		{
			name:    "underscore prefixed alias silences the hint",
			src:     "# /glossary/src/nav _nav\nprint('hi')\n",
			wantHit: false,
		},
		{
			name:    "used bare import is not hinted",
			src:     "# /glossary/src/nav\nnav.version()\n",
			wantHit: false,
		},
		{
			name:    "used aliased import is not hinted",
			src:     "# /glossary/src/nav n\nn.version()\n",
			wantHit: false,
		},
		{
			name:    "unused aliased import is hinted",
			src:     "# /glossary/src/nav n\nprint('hi')\n",
			wantHit: true,
		},
		{
			name:    "std imports are exempt",
			src:     "# std/noml\nprint('hi')\n",
			wantHit: false,
		},
		{
			name:    "function imports are exempt",
			src:     "# std/math.sqrt\nprint('hi')\n",
			wantHit: false,
		},
		{
			name:    "doc comment keeps the import",
			src:     "; keep for side effects\n# /glossary/src/nav\nprint('hi')\n",
			wantHit: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prog := parseForUnusedImport(t, tc.src)
			res := ValidateUnusedImports(prog)
			gotHit := len(res) > 0
			if gotHit != tc.wantHit {
				t.Fatalf("ValidateUnusedImports() = %d result(s), wantHit=%v; results: %+v", len(res), tc.wantHit, res)
			}
			if gotHit && !strings.Contains(res[0].Message, "`_`") {
				t.Errorf("hint should teach the `_` escape hatch, got: %q", res[0].Message)
			}
		})
	}
}

func TestUnusedImportLintRegistered(t *testing.T) {
	// Guard against the usual mistake: writing ValidateXxx but forgetting to
	// register the step (the check would then be dead code).
	found := false
	for _, s := range lintSteps {
		if s.name == "unused-imports" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("lintSteps has no unused-imports step")
	}

	// The vet pipeline calls this standalone entry point because module
	// merging strips UseStatements before RunAllLints sees the program.
	prog := parseForUnusedImport(t, "# /glossary/src/nav\nprint('hi')\n")
	res := UnusedImportLints(prog)
	if len(res) != 1 || res[0].TraceID != "unimpuse" {
		t.Fatalf("UnusedImportLints() = %+v, want one unimpuse hint", res)
	}
	if len(res[0].Fixes) == 0 {
		t.Errorf("unimpuse hint should carry a fix description")
	}
}
