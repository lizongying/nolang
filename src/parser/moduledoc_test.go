package parser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
)

func parseForModuleDoc(t *testing.T, src string) *Program {
	t.Helper()
	p := New(lexer.New(src))
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v\nsrc:\n%s", p.Errors(), src)
	}
	return prog
}

// docText renders a Doc group's payload so assertions read like the source.
// Each line loses the single space the lexer keeps after the `;` marker (the
// formatter re-adds it on output), so expectations can be written verbatim.
func docText(cg *CommentGroup) string {
	if cg == nil {
		return ""
	}
	var b strings.Builder
	for i, c := range cg.List {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(strings.TrimPrefix(c.Text, " "))
	}
	return b.String()
}

// firstStmtDoc returns the Doc of the first statement, or "" when it has none.
func firstStmtDoc(prog *Program) string {
	if len(prog.Statements) == 0 {
		return ""
	}
	if d, ok := prog.Statements[0].(interface{ GetDoc() *CommentGroup }); ok {
		return docText(d.GetDoc())
	}
	return ""
}

const moduleDocSrc = `; arr — 陣列操作
;
; 提供固定大小陣列的通用操作。
; [arr](#arr)

; 注意：這是成員的說明
; [len](#arr.len)
[n]t.len = () (out i64) {
    out = n
}
`

// TestModuleDocAttachesToProgram 釘住模組 doc 的歸屬：檔案開頭那段到「無點錨點」
// 為止（含錨點行）掛 Program.Doc，錨點之後的註解仍屬下面那個宣告。這正是
// 「模組說明與函式說明不能混成一團」的判準。
func TestModuleDocAttachesToProgram(t *testing.T) {
	prog := parseForModuleDoc(t, moduleDocSrc)

	want := "arr — 陣列操作\n\n提供固定大小陣列的通用操作。\n[arr](#arr)"
	if got := docText(prog.Doc); got != want {
		t.Fatalf("Program.Doc =\n%q\nwant\n%q", got, want)
	}
	if want := "注意：這是成員的說明\n[len](#arr.len)"; firstStmtDoc(prog) != want {
		t.Fatalf("first stmt Doc =\n%q\nwant\n%q", firstStmtDoc(prog), want)
	}
	if len(prog.Statements) != 1 {
		t.Fatalf("statements = %d, want 1", len(prog.Statements))
	}
}

// TestModuleDocNeedsDotlessAnchor 是負向對照：只有成員錨點（`#arr.len`）的檔案頭
// 不得被當成模組 doc —— 否則每個函式的說明都會被搬去模組上，正是這次要修的
// 「整段併進首個宣告」的反向壞法。
func TestModuleDocNeedsDotlessAnchor(t *testing.T) {
	src := `; len: 回傳陣列長度
; [len](#arr.len)
[n]t.len = () (out i64) {
    out = n
}
`
	prog := parseForModuleDoc(t, src)
	if prog.Doc != nil {
		t.Fatalf("Program.Doc = %q, want nil (only a member anchor present)", docText(prog.Doc))
	}
	if want := "len: 回傳陣列長度\n[len](#arr.len)"; firstStmtDoc(prog) != want {
		t.Fatalf("first stmt Doc =\n%q\nwant\n%q", firstStmtDoc(prog), want)
	}
}

// TestModuleDocOnlyAtFileStart 保證錨點出現在檔案中間時不會被誤當模組 doc：
// 那時 Program.Doc 必須是 nil，該註解仍掛在它下方的陳述上。
func TestModuleDocOnlyAtFileStart(t *testing.T) {
	src := `a = ()(r i64) {
    r = 1
}

; 檔案中間的說明
; [mid](#mid)
b = ()(r i64) {
    r = 2
}
`
	prog := parseForModuleDoc(t, src)
	if prog.Doc != nil {
		t.Fatalf("Program.Doc = %q, want nil", docText(prog.Doc))
	}
	if len(prog.Statements) != 2 {
		t.Fatalf("statements = %d, want 2", len(prog.Statements))
	}
	if want := "檔案中間的說明\n[mid](#mid)"; firstStmtDoc(&Program{Statements: prog.Statements[1:]}) != want {
		t.Fatalf("second stmt Doc =\n%q\nwant\n%q", firstStmtDoc(&Program{Statements: prog.Statements[1:]}), want)
	}
}

// TestStdArrModuleDoc 是端到端試點：src/std/arr.no 的檔案頭已改成以
// `; [arr](#arr)` 收尾，所以它整段必須落在 Program.Doc 上，且首個陳述
// （`[n]t.len`）只保留自己的 doc。這條同時把「模組 doc 真的能從 AST 取到」
// 釘在真實語料上，而不只是合成輸入。
func TestStdArrModuleDoc(t *testing.T) {
	src, err := os.ReadFile("../std/arr.no")
	if err != nil {
		t.Fatalf("read std/arr.no: %v", err)
	}
	p := New(lexer.New(string(src)))
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse std/arr.no: %v", p.Errors())
	}
	if prog.Doc == nil || len(prog.Doc.List) == 0 {
		t.Fatal("std/arr.no: Program.Doc is nil; the module anchor did not attach")
	}
	last := prog.Doc.List[len(prog.Doc.List)-1].Text
	if strings.TrimSpace(last) != "[arr](#arr)" {
		t.Fatalf("std/arr.no: last module doc line = %q, want the #arr anchor", last)
	}
	if i := strings.Index(docText(prog.Doc), "陣列操作"); i < 0 {
		t.Fatalf("std/arr.no: module doc lost its title:\n%s", docText(prog.Doc))
	}
	if got := firstStmtDoc(prog); strings.Contains(got, "陣列操作") {
		t.Fatalf("std/arr.no: module preamble leaked into the first statement doc:\n%s", got)
	}
}

// TestStdModuleDocAnchors 是全量 std 的模組 doc 门禁：
//   - 每个 src/std/*.no 的無點錨点必须恰好一个，且等于文件名（模块名）——
//     写成 `; [crc16](#crc16)` 之类的笔误会让翻译键与模块对不上，只能靠这条挡；
//   - 有锚点的文件必须真的有 Program.Doc，且锚点行是它的最后一行
//     （「注解在下」：锚点与下方宣告相邻的那一端才是它）；
//   - 没有锚点的文件（enter/leave/sort 三个只有介面声明的模块）必须是 Doc == nil，
//     否则说明某个成員 doc 被误判成模組 doc 了。
func TestStdModuleDocAnchors(t *testing.T) {
	paths, err := filepath.Glob("../std/*.no")
	if err != nil {
		t.Fatal(err)
	}
	nested, err := filepath.Glob("../std/*/*.no")
	if err != nil {
		t.Fatal(err)
	}
	paths = append(paths, nested...)
	if len(paths) < 100 {
		t.Fatalf("only %d std files found; the glob is wrong", len(paths))
	}
	withDoc := 0
	for _, path := range paths {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		stem := strings.TrimSuffix(filepath.Base(path), ".no")
		var anchors []string
		for _, line := range strings.Split(string(src), "\n") {
			if _, a, ok := ParseDocLinkLine(strings.TrimPrefix(strings.TrimSpace(line), ";")); ok && !strings.Contains(a, ".") {
				anchors = append(anchors, a)
			}
		}
		p := New(lexer.New(string(src)))
		prog := p.ParseProgram()
		if len(p.Errors()) > 0 {
			continue // 語料本身语法坏掉由别的门禁管
		}
		if len(anchors) == 0 {
			if prog.Doc != nil {
				t.Errorf("%s: Program.Doc set with no module anchor", path)
			}
			continue
		}
		if len(anchors) > 1 {
			t.Errorf("%s: %d dotless anchors %v; a file declares at most one module", path, len(anchors), anchors)
		}
		if anchors[0] != stem {
			t.Errorf("%s: module anchor #%s does not match file stem %q", path, anchors[0], stem)
		}
		if prog.Doc == nil || len(prog.Doc.List) == 0 {
			t.Errorf("%s: Program.Doc is nil despite module anchor #%s", path, anchors[0])
			continue
		}
		last := strings.TrimSpace(prog.Doc.List[len(prog.Doc.List)-1].Text)
		if last != "["+stem+"](#"+stem+")" {
			t.Errorf("%s: module doc does not end with its anchor (last = %q)", path, last)
		}
		withDoc++
	}
	if withDoc < 100 {
		t.Fatalf("only %d modules carry a module doc; expected the bulk of src/std", withDoc)
	}
}
