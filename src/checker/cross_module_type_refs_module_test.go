package checker

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/parser"
)

// REGRESSION GUARD (merged vet: std struct defined in its own module).
//
// no vet 主檔是「多檔合併程式」：std 依賴檔的結構體定義帶上模組前綴
// （`err.err`），而同檔函式簽名仍寫裸名（`new = (…) (e err)`）。若本地型別
// 只按全域裸名比對，std/err.no 會噴 3 條假 ERROR
// （`type 'err' not found; did you mean 'err.err'?`，File 指向 src/std/err.no）。
// 本地型別必須按「定義所屬模組」判別。
func TestCrossModuleTypeRefsSameModuleQualifiedStruct(t *testing.T) {
	src := `err {
    msg str
}

new = () (e err) {
    e = err {
        msg: ''
    }
}
`
	prog := mustParse(t, src)
	stdFile := "/repo/src/std/err.no"
	// 模擬 merged 模式：結構體名被模組前綴改寫。
	if sd, ok := prog.Statements[0].(*parser.StructDefinition); ok {
		sd.Name = "err.err"
	}
	for _, stmt := range prog.Statements {
		prog.Sem.SetSourceFileOf(stmt, stdFile)
	}
	for _, r := range ValidateCrossModuleTypeRefs(prog) {
		if strings.Contains(r.Message, "not found") {
			t.Fatalf("std module's own type flagged as cross-module: %s", r.Message)
		}
	}
}

// Control: 同一個合併程式裡，**其他**模組裸用 `err` 仍必須被報（修復不得
// 把所有 std 結構體短名當成全域本地型別）。
func TestCrossModuleTypeRefsOtherModuleStillFlagged(t *testing.T) {
	src := `err {
    msg str
}

new = () (e err) {
    e = err {
        msg: ''
    }
}

user = (x err) (out i64) {
    out = 0
}
`
	prog := mustParse(t, src)
	if sd, ok := prog.Statements[0].(*parser.StructDefinition); ok {
		sd.Name = "err.err"
	}
	prog.Sem.SetSourceFileOf(prog.Statements[0], "/repo/src/std/err.no")
	prog.Sem.SetSourceFileOf(prog.Statements[1], "/repo/src/std/err.no")
	prog.Sem.SetSourceFileOf(prog.Statements[2], "/repo/main.no")

	var flagged []string
	for _, r := range ValidateCrossModuleTypeRefs(prog) {
		if strings.Contains(r.Message, "not found") {
			flagged = append(flagged, r.File+": "+r.Message)
		}
	}
	if len(flagged) != 1 {
		t.Fatalf("expected exactly 1 cross-module error (user module only), got %d: %v", len(flagged), flagged)
	}
	if !strings.Contains(flagged[0], "/repo/main.no") {
		t.Fatalf("diagnostic attributed to wrong file: %s", flagged[0])
	}
}
