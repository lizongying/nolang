package checker

import (
	"testing"

	"github.com/lizongying/nolang/parser"
)

// 合併模式（no vet 目錄/套件模式）下，program 含來自其他模組的語句副本，其行號是
// 相對於來源檔的。若 lint 結果不帶 File，RunAllLints 的行號範圍回退歸因會把別檔的
// 行號對到被 vet 的主檔上，出現「訊息講 A 檔的符號、行列卻指向 B 檔」的張冠李戴
// （實測：std net/ip.no:34 的 IP-ZERO 被報成 byte.no:34、char.no:34……）。
// 本測試釘住節點級歸因：語句自帶 SourceFile 時，結果必須帶著同一個檔案路徑。

func TestValidateUnusedVarsCarriesSourceFile(t *testing.T) {
	prog := mustParse(t, `IP-ZERO = 0

f = () (x i64) {
    x = 1
}
`)
	for _, stmt := range prog.Statements {
		parser.SetSourceFile(stmt, "/abs/path/std/net/ip.no")
	}
	results := ValidateUnusedVars(prog, nil)
	if len(results) != 1 {
		t.Fatalf("expected 1 unused-var hint, got %d: %+v", len(results), results)
	}
	if results[0].TraceID != "6kryrbsq" {
		t.Fatalf("unexpected TraceID %q: %+v", results[0].TraceID, results[0])
	}
	if results[0].File != "/abs/path/std/net/ip.no" {
		t.Errorf("expected File of the defining statement, got %q (%+v)", results[0].File, results[0])
	}
}

func TestValidateRedundantTypeAnnotationCarriesSourceFile(t *testing.T) {
	// 主檔語句（SourceFile 為空）+ std 副本語句（自帶 SourceFile）混在同一 merged 程式裡。
	prog := mustParse(t, `main-f = () (x i64) {
    y i64 = 2
    x = y
}

std-f = () (x i64) {
    z i64 = 3
    x = z
}
`)
	parser.SetSourceFile(prog.Statements[1], "/abs/path/std/str.no")
	results := ValidateRedundantTypeAnnotation(prog)
	if len(results) != 2 {
		t.Fatalf("expected 2 redundant-annotation hints, got %d: %+v", len(results), results)
	}
	byLine := map[int]string{}
	for _, r := range results {
		if r.TraceID != "tcpoxtfd" {
			t.Fatalf("unexpected TraceID %q: %+v", r.TraceID, r)
		}
		byLine[r.Line] = r.File
	}
	if f, ok := byLine[2]; !ok || f != "" {
		t.Errorf("main-file hint (line 2) should have empty File (falls back to vetted file), got %q", f)
	}
	if f, ok := byLine[7]; !ok || f != "/abs/path/std/str.no" {
		t.Errorf("std copy hint (line 7) should carry its own file, got %q", f)
	}
}

// TestStatementWalkingLintsCarrySourceFile 覆蓋所有「走訪語句」型 lint：只要語句自帶
// SourceFile，結果就必須帶著它——否則 RunAllLints 的行號範圍回退歸因會把別檔行號對到
// 主檔上（用戶實測：std net.no:147 的 listener.close 被報成 mysql.no:147:10）。
func TestStatementWalkingLintsCarrySourceFile(t *testing.T) {
	const stdFile = "/abs/path/std/net/ip.no"
	cases := []struct {
		name     string
		src      string
		validate func(*parser.Program) []ValidateResult
	}{
		{"naming", "BadName = () {\n    bad-thing i64 = 1\n    x = bad-thing\n}\n", ValidateNaming},
		{"hex-case", "f = () {\n    x = 0xFF\n    x = x\n}\n", ValidateHexCase},
		{"unassigned-returns", "f = () (res i64) {\n    x = 1\n}\n", ValidateUnassignedReturns},
		{"duplicate-vars", "x i64 = 1\nx i64 = 2\n", ValidateDuplicateVars},
		{"async-naming", "work = () {\n}\nf = () {\n    run work()\n}\n", ValidateAsyncNaming},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prog := mustParse(t, tc.src)
			for _, stmt := range prog.Statements {
				parser.SetSourceFile(stmt, stdFile)
			}
			results := tc.validate(prog)
			if len(results) == 0 {
				t.Fatalf("fixture produced no %s diagnostic; fix the test input: %q", tc.name, tc.src)
			}
			for _, r := range results {
				if r.File != stdFile {
					t.Errorf("expected File %q, got %q (%+v)", stdFile, r.File, r)
				}
			}
		})
	}
}
