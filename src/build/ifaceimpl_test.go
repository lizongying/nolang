package build

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

func mustParse(t *testing.T, src string) *parser.Program {
	t.Helper()
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	return prog
}

// TestValidateInterfaceImplementation verifies that the
// ValidateInterfaceImplementation validator matches dotted-name
// function definitions against generic-receiver interface method
// declarations.
func TestValidateInterfaceImplementation(t *testing.T) {
	tests := []struct {
		name      string
		src       string
		wantCount int
		wantSub   string // substring that should appear in some warning message
	}{
		{
			name: "i8.gt matches ord.t.gt",
			src: `ord {
    t.gt(b t) (res bool)
}

i8.gt = (b i8) (res bool) {
    res = . > b
}
`,
			wantCount: 0,
		},
		{
			name: "signature mismatch: return type",
			src: `ord {
    t.gt(b t) (res bool)
}

i8.gt = (b i8) (res i64) {
    res = 0
}
`,
			wantCount: 1,
			wantSub:   "expected 'bool'",
		},
		{
			name: "no interface — no checks",
			src: `i8.gt = (b i8) (res bool) {
    res = . > b
}
`,
			wantCount: 0,
		},
		{
			name: "[]ord.ast matching no interface method (no iface with ast)",
			src: `ord {
    t.gt(b t) (res bool)
}

[]ord.ast = () (res []ord) {
}
`,
			wantCount: 0,
		},
		{
			name: "all numeric types implementing ord",
			src: `ord {
    t.gt(b t) (res bool)
}

i8.gt = (b i8) (res bool) { res = . > b }
i16.gt = (b i16) (res bool) { res = . > b }
i32.gt = (b i32) (res bool) { res = . > b }
i64.gt = (b i64) (res bool) { res = . > b }
u8.gt = (b u8) (res bool) { res = . > b }
u16.gt = (b u16) (res bool) { res = . > b }
u32.gt = (b u32) (res bool) { res = . > b }
u64.gt = (b u64) (res bool) { res = . > b }
f32.gt = (b f32) (res bool) { res = . > b }
f64.gt = (b f64) (res bool) { res = . > b }
byte.gt = (b byte) (res bool) { res = . > b }
char.gt = (b char) (res bool) { res = . > b }
str.gt = (b str) (res bool) { res = . > b }
`,
			wantCount: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prog := mustParse(t, tt.src)
			results := ValidateInterfaceImplementation(prog)
			if len(results) != tt.wantCount {
				t.Errorf("expected %d warnings, got %d: %+v", tt.wantCount, len(results), results)
			}
			if tt.wantSub != "" {
				found := false
				for _, r := range results {
					if strings.Contains(r.Message, tt.wantSub) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected warning containing %q, got %+v", tt.wantSub, results)
				}
			}
		})
	}
}
