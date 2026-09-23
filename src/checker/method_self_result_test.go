package checker

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/parser"
)

// 本檔案釘住「方法定義的隱式 self 接收者」不得被當成返回值的規則。
//
// parser 把 self 注入為方法定義的第一個返回參數（Results[0]，見
// parser/decl.go：方法語義為 `type.method = (inputs) (self type, rest...) {}`）。
// 因此任何「Results[0] 就是返回型別」的解讀，都會把方法誤判成「回傳其接收者
// 型別」，典型症狀是呼叫端報
//
//	cannot assign bz-huffman value to i64 variable 'sym'
//
// （bzip2.no 的 `sym = ht0.decode-symbol(br)`）。
//
// 唯一入口是 declaredResults()；下列測試逐一釘住使用該入口的各驗證器，避免
// 將來的 parser/ast refactor 再次漏改其中某一處。

// 方法回傳 i64，呼叫端以 i64 變數接收。
const methodRetSrc = `huff {
    tab []i64
}

huff.decode = (n i64) (sym i64) {
    sym = 7
}

caller = (h huff, n i64) (out i64) {
    out = h.decode(n)
}`

// TestMethodReturnTypeIsNotReceiverType 驗證方法呼叫的返回型別取自真正的返回
// 參數，而非隱式 self 接收者（ValidateTypes / validateStmtTypes 的賦值檢查）。
func TestMethodReturnTypeIsNotReceiverType(t *testing.T) {
	res := ValidateTypes(parseProg(t, methodRetSrc))
	for _, r := range res {
		if strings.Contains(r.Message, "cannot assign") {
			t.Fatalf("method return type misread as receiver type: L%d:C%d %s", r.Line, r.Column, r.Message)
		}
	}
}

// TestMethodSelfNotReportedAsUnassignedResult 驗證隱式 self 不會被
// ValidateUnassignedReturns 當成「從未賦值的返回參數」（i3k422u3）誤報。
// self 由呼叫方指標別名提供，函式體無須賦值。
func TestMethodSelfNotReportedAsUnassignedResult(t *testing.T) {
	res := ValidateUnassignedReturns(parseProg(t, methodRetSrc))
	for _, r := range res {
		if strings.Contains(r.Message, "'self'") {
			t.Fatalf("implicit self receiver reported as unassigned result: L%d:C%d %s", r.Line, r.Column, r.Message)
		}
	}
}

// TestMethodRealResultStillReported 是上一個測試的對照組：剔除 self 之後，
// 真正的返回參數若仍未賦值，警告必須照常發出（避免整條檢查被一起關掉）。
func TestMethodRealResultStillReported(t *testing.T) {
	src := `huff {
    tab []i64
}

huff.forget = (n i64) (sym i64) {
}
`
	res := ValidateUnassignedReturns(parseProg(t, src))
	found := false
	for _, r := range res {
		if strings.Contains(r.Message, "result parameter 'sym'") && strings.Contains(r.Message, "never assigned") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected warning for genuinely unassigned result 'sym', got %d results: %+v", len(res), res)
	}
}

// TestOptReturningMethodAbsorbsIndexOut 驗證回傳 ?T 的方法，其函式體內的越界
// 索引在「目標可容納 option」時就地捕獲，不再報未處理索引。
//
// 語意變更（見 .trae/documents/option-capture-assign.md §1/§3）：此不變式先前靠
// lowering 的自動改寫（`v = arr[i]` -> `v ?= arr[i]`，maybeAutoPropagateIndex）
// 保證；該自動上拋已移除，改由「捕獲推導」接手 —— `v = arr[i]` 會把 `v` 推斷成
// `?elem`（合成 ?T 註解）並就地捕獲越界錯誤，故仍不報。
func TestOptReturningMethodAbsorbsIndexOut(t *testing.T) {
	src := `huff {
    tab []i64
}

huff.pick = (arr []i64, i i64) (out ?i64) {
    out = nil
    v = arr[i]
    out = v
}`
	res := ValidateUnhandledIndex(parseProg(t, src), "src/app.no")
	for _, r := range res {
		if r.TraceID == unhandledIndexTraceID {
			t.Fatalf("index-out reported inside ?T-returning method: L%d:C%d %s", r.Line, r.Column, r.Message)
		}
	}
}

// TestPlainLocalReassignToIndexIsReported 是上一個測試的對照組：目標已顯式宣告為
// plain `i64` 時**無法**容納越界 option，捕獲推導依規格不碰它（見方案 §1 觸發條件
// 「變數已宣告為非 option 型別 → 不碰」），因此必須報錯而不是靜默改寫為 `?=`。
func TestPlainLocalReassignToIndexIsReported(t *testing.T) {
	src := `huff {
    tab []i64
}

huff.pick = (arr []i64, i i64) (out ?i64) {
    out = nil
    v i64 = 0
    v = arr[i]
    out = v
}`
	res := ValidateUnhandledIndex(parseProg(t, src), "src/app.no")
	found := false
	for _, r := range res {
		if r.TraceID == unhandledIndexTraceID {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected index-out to be reported when the target is a plain i64 local, got %+v", res)
	}
}

// TestOptReturningMethodAbsorbsOverflow 驗證回傳 ?T 的方法，其函式體內的整數
// 溢位視為被 option 吸收，不再報未處理溢位（ValidateUnhandledOverflow 的 fnOpt）。
func TestOptReturningMethodAbsorbsOverflow(t *testing.T) {
	src := `huff {
    tab []i64
}

huff.add = (a i64, b i64) (out ?i64) {
    x i64 = 0
    x = a + b
    out = x
}`
	res := ValidateUnhandledOverflow(parseProg(t, src), "src/app.no")
	for _, r := range res {
		if r.TraceID == unhandledOverflowTraceID {
			t.Fatalf("overflow reported inside ?T-returning method: L%d:C%d %s", r.Line, r.Column, r.Message)
		}
	}
}

// TestDeclaredResultsStripsSelf 直接釘住 helper 本身的行為：方法定義的
// Results[0] 是隱式 self，必須剔除；非方法函式即使把首個返回參數命名為
// self，也不是接收者，不得剔除。
func TestDeclaredResultsStripsSelf(t *testing.T) {
	prog := parseProg(t, `huff {
    tab []i64
}

huff.decode = (n i64) (sym i64) {
    sym = 7
}

plain = (n i64) (self i64) {
    self = n
}`)
	var sawMethod, sawPlain bool
	for _, stmt := range prog.Statements {
		fd, ok := stmt.(*parser.FunctionDefinition)
		if !ok {
			continue
		}
		rs := declaredResults(fd)
		switch fd.Name {
		case "huff.decode":
			sawMethod = true
			if len(rs) != 1 || rs[0].Name != "sym" {
				t.Fatalf("declaredResults(%s) = %v, want exactly (sym i64)", fd.Name, paramNames(rs))
			}
		case "plain":
			sawPlain = true
			if len(rs) != 1 || rs[0].Name != "self" {
				t.Fatalf("declaredResults(%s) = %v, non-method first result named 'self' must be kept", fd.Name, paramNames(rs))
			}
		}
	}
	if !sawMethod || !sawPlain {
		t.Fatalf("test source did not yield both definitions (method=%v plain=%v)", sawMethod, sawPlain)
	}
}

func paramNames(ps []*parser.Parameter) []string {
	names := make([]string, 0, len(ps))
	for _, p := range ps {
		names = append(names, p.Name)
	}
	return names
}
