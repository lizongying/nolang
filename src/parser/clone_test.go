package parser

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
)

// S5 stage 6：CloneProgram 的工作副本契約。
//
//  1. ParseProgram 輸出不可變——對副本的任何改名 / 改寫都不得洩漏回原樹；
//  2. node-keyed Sem 條目換鍵到副本節點（原節點仍可讀到原條目）；
//  3. 共享子樹拷成同一個副本（指標鍵同一性在副本側保留）。

func parseCloneSrc(t *testing.T, src string) *Program {
	t.Helper()
	p := New(lexer.New(src))
	prog := p.ParseProgram()
	if prog == nil {
		t.Fatal("ParseProgram returned nil")
	}
	return prog
}

func TestCloneProgramMutationsDoNotLeak(t *testing.T) {
	prog := parseCloneSrc(t, "f = (a i64) (r i64) {\n    r = a\n}\n")
	orig, ok := prog.Statements[0].(*FunctionDefinition)
	if !ok {
		t.Fatalf("Statements[0] = %T, want *FunctionDefinition", prog.Statements[0])
	}
	work := CloneProgram(prog)
	if work == prog {
		t.Fatal("CloneProgram returned the same *Program pointer")
	}
	clone, ok := work.Statements[0].(*FunctionDefinition)
	if !ok {
		t.Fatalf("clone Statements[0] = %T, want *FunctionDefinition", work.Statements[0])
	}
	if clone == orig {
		t.Fatal("clone statement shares pointer with the original")
	}
	// 裝配改名落在副本上
	clone.Name = "f_i64"
	if orig.Name != "f" {
		t.Fatalf("original Name mutated to %q — clone leaked writes back", orig.Name)
	}
	// 嵌套表達式亦不共享
	if clone.Body == orig.Body {
		t.Fatal("clone Body shares pointer with the original")
	}
	if len(clone.Body.Statements) != len(orig.Body.Statements) {
		t.Fatalf("clone body stmts = %d, want %d", len(clone.Body.Statements), len(orig.Body.Statements))
	}
}

func TestCloneProgramRemapsSemKeys(t *testing.T) {
	prog := parseCloneSrc(t, "x i64 = 1\n")
	origLet := prog.Statements[0].(*LetStatement)
	prog.Sem.SetTypeOverrideOf(origLet, "i32")
	work := CloneProgram(prog)
	cloneLet := work.Statements[0].(*LetStatement)
	if got := work.Sem.TypeOverrideOf(cloneLet); got != "i32" {
		t.Fatalf("clone Sem TypeOverrideOf = %q, want %q", got, "i32")
	}
	if got := prog.Sem.TypeOverrideOf(origLet); got != "i32" {
		t.Fatalf("original Sem entry lost: %q", got)
	}
	// 兩棵樹的鍵互不串台
	if got := work.Sem.TypeOverrideOf(origLet); got != "" {
		t.Fatalf("clone Sem keyed by original node (got %q) — remap leaked", got)
	}
}

func TestCloneProgramPreservesSharing(t *testing.T) {
	fd := &FunctionDefinition{Name: "shared"}
	prog := &Program{Statements: []Statement{fd, fd}}
	work := CloneProgram(prog)
	a, ok := work.Statements[0].(*FunctionDefinition)
	if !ok {
		t.Fatalf("Statements[0] = %T", work.Statements[0])
	}
	b := work.Statements[1].(*FunctionDefinition)
	if a != b {
		t.Fatal("shared statement cloned into two distinct nodes — pointer-keyed identity would drift")
	}
	if a == fd {
		t.Fatal("clone shares pointer with the original")
	}
}

func TestCloneProgramCopiesBuiltinFuncNames(t *testing.T) {
	prog := &Program{BuiltinFuncNames: map[string]bool{"sqrt": true}}
	work := CloneProgram(prog)
	work.BuiltinFuncNames["asin"] = true
	if prog.BuiltinFuncNames["asin"] {
		t.Fatal("BuiltinFuncNames map shared with the original")
	}
	if !prog.BuiltinFuncNames["sqrt"] || !work.BuiltinFuncNames["sqrt"] {
		t.Fatal("BuiltinFuncNames content not carried over")
	}
}

func TestCloneProgramNil(t *testing.T) {
	if got := CloneProgram(nil); got != nil {
		t.Fatalf("CloneProgram(nil) = %v, want nil", got)
	}
}

// TestCloneProgramCopiesFmtTrivia 是 Stage 2（FmtTrivia 遷出已於 Stage 7 刪除的
// NodeSemantics）的 clone 契約：排版 trivia 表與三張語義表同為 node-keyed，
// CloneProgram 必須一併
// 換鍵複製。CloneProgram 是逐欄位列舉的手工拷貝（clone.go），漏一欄就是 nil
// ——而 formatter 只在 fresh parse 上跑，這種遺漏不會有任何測試立刻報紅，
// 只會在裝配期工作副本上靜默丟 trivia。
func TestCloneProgramCopiesFmtTrivia(t *testing.T) {
	const src = `main = () () {
    x = 1
    x -> {
        io.outln('hi')
    }
}`
	prog := parseCloneSrc(t, src)
	if prog.FmtTrivia == nil || prog.FmtTrivia.Len() == 0 {
		t.Fatalf("anti-vacuity: source produced no FmtTrivia entries; " +
			"the assertion below would be vacuous")
	}
	work := CloneProgram(prog)
	if work.FmtTrivia == nil {
		t.Fatal("CloneProgram dropped FmtTrivia (nil on the work copy)")
	}
	if work.FmtTrivia.Len() != prog.FmtTrivia.Len() {
		t.Fatalf("FmtTrivia entry count changed by clone: orig=%d work=%d",
			prog.FmtTrivia.Len(), work.FmtTrivia.Len())
	}
	// 換鍵：副本的鍵必須指向副本節點，而非原樹節點。
	remapped := 0
	for k := range work.FmtTrivia.entries {
		if _, isOrig := prog.FmtTrivia.entries[k]; !isOrig {
			remapped++
		}
	}
	if remapped == 0 {
		t.Error("no FmtTrivia key was remapped to a cloned node; " +
			"CloneProgram must re-key node-keyed trivia like it does the three semantic tables")
	}
	// 深拷貝條目：改副本不得洩漏回原樹。
	for _, e := range work.FmtTrivia.entries {
		e.RTFlags |= RTEmptyArmDefault
	}
	for _, e := range prog.FmtTrivia.entries {
		if e.RTFlags&RTEmptyArmDefault != 0 {
			t.Fatal("FmtTrivia entries shared with the original (not deep-copied)")
		}
	}
}
