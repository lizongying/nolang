package checker

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// parseMergeSrc 解析一段 nolang 原始碼；parser 報錯直接失敗。
func parseMergeSrc(t *testing.T, src string) *parser.Program {
	t.Helper()
	p := parser.New(lexer.New(src))
	prog := p.ParseProgram()
	if prog == nil {
		t.Fatalf("ParseProgram returned nil\n--- src ---\n%s", src)
	}
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v\n--- src ---\n%s", errs, src)
	}
	return prog
}

// mergedMethodNames 返回接口合併後的方法名（保持順序），方便斷言。
func mergedMethodNames(ms []*parser.InterfaceMethod) []string {
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		out = append(out, m.Name)
	}
	return out
}

func diagMessages(rs []ValidateResult) []string {
	out := make([]string, 0, len(rs))
	for _, r := range rs {
		out = append(out, r.Message)
	}
	return out
}

// TestResolveInterfacesTransitive 鎖定遞迴傳遞閉包：
//
//	c { m-c() }
//	b { c }
//	a { b }
//
// ⇒ a 擁有 [m-c]，Parents = [b]，Closure(a) = [a b c]。
func TestResolveInterfacesTransitive(t *testing.T) {
	prog := parseMergeSrc(t, `c {
    m-c()
}
b {
    c
}
a {
    b
}
`)
	set, diags := ResolveInterfaces(prog)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diagMessages(diags))
	}
	if got, want := strings.Join(mergedMethodNames(set.MethodsOf("a")), ","), "m-c"; got != want {
		t.Fatalf("MethodsOf(a) = %q, want %q", got, want)
	}
	if got, want := strings.Join(mergedMethodNames(set.MethodsOf("b")), ","), "m-c"; got != want {
		t.Fatalf("MethodsOf(b) = %q, want %q", got, want)
	}
	closure := strings.Join(set.Closure("a"), ",")
	for _, need := range []string{"a", "b", "c"} {
		if !strings.Contains(closure, need) {
			t.Fatalf("Closure(a) = %q, want it to contain %q", closure, need)
		}
	}
}

// TestResolveInterfacesDedupSameSignature 鎖定同名同簽名去重：父與子都宣告
// `to-str()`，合併後只留一個；Receiver 識別符不同（t / u）不算衝突。
func TestResolveInterfacesDedupSameSignature(t *testing.T) {
	prog := parseMergeSrc(t, `base {
    t.to-str() (s str)
}
child {
    base
    t.to-str() (s str)
    own()
}
`)
	set, diags := ResolveInterfaces(prog)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diagMessages(diags))
	}
	if got, want := strings.Join(mergedMethodNames(set.MethodsOf("child")), ","), "to-str,own"; got != want {
		t.Fatalf("MethodsOf(child) = %q, want %q", got, want)
	}

	// 不同 Receiver 名（t vs u）但簽名等價 ⇒ 仍算同簽名。
	prog2 := parseMergeSrc(t, `p {
    t.gt(b t) (res bool)
}
q {
    u.gt(b u) (res bool)
}
r {
    p
    q
}
`)
	_, diags2 := ResolveInterfaces(prog2)
	if len(diags2) != 0 {
		t.Fatalf("receiver rename must not be a conflict: %v", diagMessages(diags2))
	}
}

// TestResolveInterfacesConflict 鎖定同名異簽名 ⇒ 診斷（不靜默取其一）。
func TestResolveInterfacesConflict(t *testing.T) {
	prog := parseMergeSrc(t, `json {
    t.to-str() (s json)
}
noml {
    t.to-str(a i64) (s str)
}
serialize {
    json
    noml
}
`)
	_, diags := ResolveInterfaces(prog)
	if len(diags) != 1 {
		t.Fatalf("want exactly 1 diagnostic, got %d: %v", len(diags), diagMessages(diags))
	}
	msg := diags[0].Message
	if !strings.Contains(msg, "conflicting signatures") || !strings.Contains(msg, "to-str") {
		t.Fatalf("unexpected diagnostic: %q", msg)
	}
	if diags[0].TraceID != InterfaceMergeTraceID {
		t.Fatalf("TraceID = %q, want %q", diags[0].TraceID, InterfaceMergeTraceID)
	}
}

// TestResolveInterfacesCycle 鎖定循環偵測：`a { b }` + `b { a }` 必須報
// merge cycle，且不得無限遞迴。
func TestResolveInterfacesCycle(t *testing.T) {
	prog := parseMergeSrc(t, `a {
    b
}
b {
    a
}
`)
	_, diags := ResolveInterfaces(prog)
	if len(diags) == 0 {
		t.Fatalf("want a cycle diagnostic, got none")
	}
	found := false
	for _, d := range diags {
		if strings.Contains(d.Message, "merge cycle") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no 'merge cycle' diagnostic in %v", diagMessages(diags))
	}
}

// TestResolveInterfacesUnknownParent 鎖定未知父接口 ⇒ 診斷。
func TestResolveInterfacesUnknownParent(t *testing.T) {
	prog := parseMergeSrc(t, `serialize {
    json
    noml
}
json {
    to-json()
}
`)
	_, diags := ResolveInterfaces(prog)
	if len(diags) != 1 {
		t.Fatalf("want exactly 1 diagnostic, got %d: %v", len(diags), diagMessages(diags))
	}
	if !strings.Contains(diags[0].Message, "unknown parent interface 'noml'") {
		t.Fatalf("unexpected diagnostic: %q", diags[0].Message)
	}
}

// TestResolveInterfacesDottedParent 鎖定帶模組前綴的合併項：`sql.sdb` 在基名
// 唯一時能歸一回已宣告的 `sdb`（模組前綴改寫兩側形態可能不一致）。
func TestResolveInterfacesDottedParent(t *testing.T) {
	prog := parseMergeSrc(t, `sdb {
    enter()
}
appdb {
    sql.sdb
    own()
}
`)
	set, diags := ResolveInterfaces(prog)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diagMessages(diags))
	}
	if got, want := strings.Join(mergedMethodNames(set.MethodsOf("appdb")), ","), "enter,own"; got != want {
		t.Fatalf("MethodsOf(appdb) = %q, want %q", got, want)
	}
	// 完整名先匹配、基名回退：兩種寫法都應拿到同一份方法集。
	if got, want := strings.Join(mergedMethodNames(set.MethodsOf("sql.sdb")), ","), "enter"; got != want {
		t.Fatalf("MethodsOf(sql.sdb) = %q, want %q", got, want)
	}
}

// TestInterfaceMergeSignatureCheckAcrossParents 鎖定「實現簽名校驗穿透父接口」：
// `user serialize` 只宣告實作 serialize，但 to-json 來自父接口 json，
// 實作簽名不符時必須報錯（此前父接口方法根本不參與校驗）。
func TestInterfaceMergeSignatureCheckAcrossParents(t *testing.T) {
	src := `json {
    t.to-json(a str) (s str)
}
serialize {
    json
}
user serialize {
    name str
}
user.to-json = () (s str) {
    s = ''
}
`
	res := runInterfaceImplCore(parseMergeSrc(t, src), nil)
	if len(res) == 0 {
		t.Fatalf("want a parameter-count mismatch for inherited 'to-json', got none")
	}
	if !strings.Contains(res[0].Message, "to-json") {
		t.Fatalf("unexpected diagnostic: %q", res[0].Message)
	}

	// 對照組：簽名完全吻合時不得報錯。
	ok := `json {
    t.n-call(n i64) (ok bool)
}
serialize {
    json
}
user serialize {
    name str
}
user.n-call = (n i64) (ok bool) {
    ok = true
}
`
	if res := runInterfaceImplCore(parseMergeSrc(t, ok), nil); len(res) != 0 {
		t.Fatalf("matching signature must not report: %v", diagMessages(res))
	}
}

// TestInterfaceMergeCollectDefinedVars 鎖定父接口方法也掛在子接口名下，
// 否則跨模組的 `module.serialize.to-json` 會被誤報 is not defined。
func TestInterfaceMergeCollectDefinedVars(t *testing.T) {
	prog := parseMergeSrc(t, `json {
    to-json()
}
serialize {
    json
}
`)
	vars := CollectDefinedVars(prog)
	for _, want := range []string{"json.to-json", "serialize.to-json"} {
		if !vars[want] {
			t.Fatalf("definedVars missing %q (have %v)", want, vars)
		}
	}
}

// TestInterfaceMergeLintErrors 鎖定 lint 步把合併診斷升成 LintError。
func TestInterfaceMergeLintErrors(t *testing.T) {
	prog := parseMergeSrc(t, `serialize {
    nope
}
`)
	res := runLintInterfaceMerge(prog, LintOptions{})
	if len(res) == 0 {
		t.Fatalf("want lint results for unknown parent interface, got none")
	}
	if res[0].Severity != LintError {
		t.Fatalf("Severity = %v, want LintError", res[0].Severity)
	}
	if res[0].Source != "nolang-lint" {
		t.Fatalf("Source = %q, want nolang-lint", res[0].Source)
	}
}
