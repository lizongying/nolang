package parser

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/hir"
	"github.com/lizongying/nolang/lexer"
)

// `...` 是佔位陳述（no-op）：AST 有節點、HIR 沒有節點、fmt 原樣印回。
//
// 這些測試釘住三件互相獨立的事：
//  1. 合法位置（獨佔一行）解析成 *EmptyStatement，且沒有任何診斷；
//  2. 非法位置（同行有程式碼、表達式位置、與範圍運算子連寫）各自給出**可照著改**
//     的錯誤，而不是含糊的 "unexpected token"；
//  3. ASTToHIR 真的把它丟掉——這是「build 時被捨棄」的可執行定義。
//
// 少了 (3)，`...` 會以某種形式漏進 HIR/MIR（例如降成 KUnknown），那種洩漏不會
// 讓任何既有測試變紅，只會在 codegen 階段以莫名其妙的形狀出現。

func parseRaw(t *testing.T, src string) (*Parser, *Program) {
	t.Helper()
	p := New(lexer.NewCached("ellipsis_test.no", src))
	prog := p.ParseProgram()
	return p, prog
}

// errorText 把 parser 的診斷串成一段可搜尋的文字。
func errorText(p *Parser) string {
	var sb strings.Builder
	for _, e := range p.Errors() {
		sb.WriteString(e)
		sb.WriteString("\n")
	}
	return sb.String()
}

func TestPlaceholderTripleDotParsesAsEmptyStatement(t *testing.T) {
	src := `f = (n i64) (r i64) {
    ...
    r = n
}
`
	p, prog := parseRaw(t, src)
	if len(p.Errors()) > 0 {
		t.Fatalf("`...` on its own line must parse cleanly, got: %s", errorText(p))
	}
	fn, ok := prog.Statements[0].(*FunctionDefinition)
	if !ok {
		t.Fatalf("expected FunctionDefinition, got %T", prog.Statements[0])
	}
	if len(fn.Body.Statements) != 2 {
		t.Fatalf("expected 2 statements (placeholder + assignment), got %d: %#v",
			len(fn.Body.Statements), fn.Body.Statements)
	}
	if _, ok := fn.Body.Statements[0].(*EmptyStatement); !ok {
		t.Fatalf("first statement must be *EmptyStatement, got %T", fn.Body.Statements[0])
	}
	if fn.Body.Statements[0].Pos().Line != 2 {
		t.Errorf("placeholder position must be line 2, got %d", fn.Body.Statements[0].Pos().Line)
	}
}

// 尾隨 `;` 註釋不是程式碼，允許（文檔裡的 `...  ; 這裡還有內容` 就靠這條）。
func TestPlaceholderTripleDotAllowsTrailingComment(t *testing.T) {
	src := `f = () (r i64) {
    ...        ; TODO: 還沒寫
    r = 1
}
`
	p, _ := parseRaw(t, src)
	if len(p.Errors()) > 0 {
		t.Fatalf("a trailing `;` comment must not count as code on the line, got: %s", errorText(p))
	}
}

func TestPlaceholderTripleDotMustBeOnItsOwnLine(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "code_before_on_same_line",
			src: `f = () (r i64) { ...
    r = 1
}
`,
			want: "own line",
		},
		{
			name: "code_after_on_same_line",
			src: `f = () (r i64) {
    ... r = 1
}
`,
			want: "no code may follow",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, _ := parseRaw(t, tc.src)
			got := errorText(p)
			if !strings.Contains(got, tc.want) {
				t.Fatalf("expected an error mentioning %q, got: %s", tc.want, got)
			}
		})
	}
}

// `...` 沒有值，所以不能出現在表達式位置：`x = ...`、`f(...)`、臂體 `-> ...`
// 都必須報錯，並指向「寫成獨立一行」。
//
// 臂體 `1 -> ...` 觸發的是「同行有程式碼」那一條（`->` 與 `...` 同處一行），
// 不是「沒有值」那一條——兩者都正確，測試只要求訊息能讓讀者知道該怎麼改，
// 因此對它接受任一說法。
func TestPlaceholderTripleDotIsNotAnExpression(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{"let_value", "x = ...\n", []string{"expression position"}},
		{"call_argument", "f = (n i64) {\n}\ng = () {\n    f(...)\n}\n", []string{"expression position"}},
		{"match_arm_body", "x = n: {\n  1 -> ...\n  -> 0\n}\n", []string{"own line", "expression position"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, _ := parseRaw(t, tc.src)
			got := errorText(p)
			for _, want := range tc.want {
				if strings.Contains(got, want) {
					return
				}
			}
			t.Fatalf("expected an error mentioning one of %v, got: %s", tc.want, got)
		})
	}
}

// `...` 收成單一 token 後，`[0...count)`（`..` 與 `.count` 連寫）不再有意義：
// 必須是語法錯誤，而且訊息要直接給出 `[0..self.count)` 這個正確寫法。
func TestPlaceholderTripleDotIsNotARangeOperator(t *testing.T) {
	src := `f = () (r i64) {
    i <- [0...count): {
        r = r + 1
    }
}
`
	p, _ := parseRaw(t, src)
	got := errorText(p)
	if !strings.Contains(got, "not a range operator") {
		t.Fatalf("expected a `... is not a range operator` error, got: %s", got)
	}
	if !strings.Contains(got, "..self.") {
		t.Fatalf("the range error must spell out the fix `[0..self.count)`, got: %s", got)
	}
	// 一個誤寫只吐一條訊息：rangeTripleDotError 吃掉該 token 後，上層不得再把它
	// 當成一條獨立陳述報 elps0ln0 / elps1ln1。
	if n := len(p.Errors()); n != 1 {
		t.Fatalf("expected exactly 1 diagnostic for `[0...count)`, got %d: %s", n, got)
	}
}

// `[0..self.count)` 是建議寫法，必須照常解析成 RangeExpression。
func TestExplicitSelfRangeStillParses(t *testing.T) {
	src := `f = () (r i64) {
    i <- [0..self.count): {
        r = r + 1
    }
}
`
	p, _ := parseRaw(t, src)
	if len(p.Errors()) > 0 {
		t.Fatalf("`[0..self.count)` must keep parsing, got: %s", errorText(p))
	}
}

// ASTToHIR 必須把 `...` 整條丟掉：這是「build 時被捨棄」的可執行定義。
// 對照組是同一函式加上一行 `r = n`，兩者的差別只能剛好是一個 HIR 節點。
func TestPlaceholderTripleDotVanishesFromHIR(t *testing.T) {
	pkg := lowerSrc(t, `f = (n i64) (r i64) {
    ...
}
`)
	fnID, fn := topNode(t, pkg, 0, hir.KFuncDef)
	bodyID := slotOf(pkg, fnID, "body")
	if bodyID == hir.NoID {
		t.Fatalf("function %s has no body slot", pkg.Str(fn.S))
	}
	body := pkg.Node(bodyID)
	if body == nil || body.Kind != hir.KBlock {
		t.Fatalf("body slot is not a KBlock: %#v", body)
	}
	if kids := pkg.Children(bodyID); len(kids) != 0 {
		t.Fatalf("`...` must leave no HIR node behind, block has %d child(ren)", len(kids))
	}

	// 反向對照：HIR 不是「什麼都丟」——同一位置換成真實陳述就會出現節點。
	pkg2 := lowerSrc(t, `f = (n i64) (r i64) {
    r = n
}
`)
	fnID2, _ := topNode(t, pkg2, 0, hir.KFuncDef)
	if kids := pkg2.Children(slotOf(pkg2, fnID2, "body")); len(kids) != 1 {
		t.Fatalf("the control case must lower exactly 1 statement, got %d", len(kids))
	}
}
