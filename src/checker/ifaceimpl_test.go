package checker

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

// TestValidateInterfaceImplementationScoping 確認比對只發生在「宣告實作該介面」的
// 型別上：struct 定義有 Implements 清單時，未列出的介面不可按方法名硬套，否則
// `net.listener.close`（0 返回）會被 `db { fs.close() (ok bool) }` 誤報。
// 無 struct 定義的內建型別（i8/str 等）仍走隱式結構匹配。
func TestValidateInterfaceImplementationScoping(t *testing.T) {
	tests := []struct {
		name      string
		src       string
		wantCount int
		wantSub   string
	}{
		{
			name: "unrelated struct is not matched by method name",
			src: `db enter, leave {
    fs.close() (ok bool)
}

listener {
    fd fd
}

listener.close = () {
}
`,
			wantCount: 0,
		},
		{
			name: "declared implementor is still checked",
			src: `db enter, leave {
    fs.close() (ok bool)
}

db-mysql db {
    fd fd
}

db-mysql.close = () {
}
`,
			wantCount: 1,
			wantSub:   "interface expects 1",
		},
		{
			name: "qualified implements list matches by base name",
			src: `db enter, leave {
    fs.close() (ok bool)
}

db-mysql sql.db {
    fd fd
}

db-mysql.close = () (ok bool) {
}
`,
			wantCount: 0,
		},
		{
			name: "builtin type without struct keeps implicit matching",
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

// TestValidateInterfaceImplementationFileAttribution 確認診斷攜帶語句自身的來源檔：
// vet 合併模式下 std 的方法定義若不帶 File，RunAllLints 會回退歸因到被 vet 的主檔，
// 出現「訊息講 std 的方法、行列卻對到使用者檔案」的張冠李戴。
func TestValidateInterfaceImplementationFileAttribution(t *testing.T) {
	prog := mustParse(t, `db enter, leave {
    fs.close() (ok bool)
}

db-mysql db {
    fd fd
}

db-mysql.close = () {
}
`)
	for _, stmt := range prog.Statements {
		if fd, ok := stmt.(*parser.FunctionDefinition); ok {
			parser.SetSourceFile(fd, "/std/net/net.no")
		}
	}
	results := ValidateInterfaceImplementation(prog)
	if len(results) == 0 {
		t.Fatalf("expected at least 1 warning, got none")
	}
	for _, r := range results {
		if r.File != "/std/net/net.no" {
			t.Errorf("expected File %q, got %q (%+v)", "/std/net/net.no", r.File, r)
		}
	}
}

// TestValidateInterfaceImplementationLegacy verifies that the
// ValidateInterfaceImplementation validator matches dotted-name
// function definitions against generic-receiver interface method
// declarations.
func TestValidateInterfaceImplementationLegacy(t *testing.T) {
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
