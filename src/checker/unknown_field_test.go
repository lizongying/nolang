package checker

// unknown_field_test.go — 「接收者型別已知是 struct，卻沒有這個成員」的
// checker 側回歸測試（見 unknown_field.go 開頭的動機）。
//
// 這條檢查的全部價值在於**不誤報**：它是新加的硬錯誤，一旦對合法寫法開火，
// 整個 std / tests 語料會一起紅。所以這裡的負例（方法、內建方法、型別不明、
// 空欄位集）比正例更重要。

import (
	"strings"
	"testing"
)

// TestUnknownFieldReported 正例：`box` 只有 xs，`b.nope` 必須報，
// 且文案同時點名型別與成員名（使用者要能一眼看出打錯哪個字）。
func TestUnknownFieldReported(t *testing.T) {
	src := `box {
    xs []i64
}

probe = () {
    b box = box{}
    io.outln(b.nope)
}`
	prog := parseProg(t, src)
	res := ValidateUnknownFields(prog)
	if len(res) != 1 {
		t.Fatalf("expected exactly 1 report, got %d: %+v", len(res), res)
	}
	r := res[0]
	if !strings.Contains(r.Message, "nope") || !strings.Contains(r.Message, "box") {
		t.Errorf("message must name both the struct and the member, got %q", r.Message)
	}
	if r.Line != 7 {
		t.Errorf("line = %d, want 7 (the `b.nope` read)", r.Line)
	}
}

// TestUnknownFieldAllowsRealMembers 負例：真欄位 / 真方法 / 內建方法名
// 一個都不能報 —— 這是誤報的主要來源。
func TestUnknownFieldAllowsRealMembers(t *testing.T) {
	src := `box {
    xs []i64
}

box.get = () (r i64) {
    r = .xs.len()
}

probe = () {
    b box = box{}
    b.xs = with-len(0)
    n = b.get()
    io.outln(n)
    io.outln(b.xs.len())
    io.outln('done')
}`
	prog := parseProg(t, src)
	if res := ValidateUnknownFields(prog); len(res) != 0 {
		t.Fatalf("real members must not be reported, got %+v", res)
	}
}

// TestUnknownFieldSkipsUnknownReceiver 負例：接收者型別推不出來就放行。
// `make()` 的回傳型別在此檔案裡查不到（沒有 funcTypes 傳入），`x.foo` 只能沉默；
// 這也是「寧可漏報」那條口徑的具體體現。
func TestUnknownFieldSkipsUnknownReceiver(t *testing.T) {
	src := `probe = () {
    x = unknown-call()
    io.outln(x.whatever)
}`
	prog := parseProg(t, src)
	if res := ValidateUnknownFields(prog); len(res) != 0 {
		t.Fatalf("unknown receiver type must be skipped, got %+v", res)
	}
}

// TestUnknownFieldSkipsEmptyFieldSet 負例：欄位集為空的 struct 不判定。
// `collectStructFields` 對「無法字串化的欄位」會跳過，空 map 分不清
// 「這個 struct 真的沒有欄位」與「欄位全沒登記」，拿它判定只會製造噪音。
func TestUnknownFieldSkipsEmptyFieldSet(t *testing.T) {
	src := `empty-struct {
}

probe = () {
    e empty-struct = empty-struct{}
    io.outln(e.anything)
}`
	prog := parseProg(t, src)
	if res := ValidateUnknownFields(prog); len(res) != 0 {
		t.Fatalf("empty field set must be skipped, got %+v", res)
	}
}

// TestUnknownFieldCoversParameters 正例：參數（而不只是區域 let）也要能被判定。
// 參數型別走 declTypeOfRaw 登記，漏了這條就只剩「區域變數」一種形態可查，
// 而 std 裡絕大多數的成員存取都掛在參數上。
func TestUnknownFieldCoversParameters(t *testing.T) {
	src := `box {
    xs []i64
}

probe = (b box) {
    io.outln(b.nope)
}`
	prog := parseProg(t, src)
	res := ValidateUnknownFields(prog)
	if len(res) != 1 {
		t.Fatalf("expected 1 report through the parameter, got %d: %+v", len(res), res)
	}
}
