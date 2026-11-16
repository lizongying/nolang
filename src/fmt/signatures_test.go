package fmt

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/parser"
	"github.com/lizongying/nolang/ty"
)

// signatures_test.go 釘住「注入 / 未注入」兩種模式的邊界：
// fmt 包刻意不 import checker，這裡的簽名表全部手工構造，因此可以在
// StdSigKey 為空的 bare `go test ./fmt/` 下照常跑。

func TestParseSourceWithConfigExposesExternSignatures(t *testing.T) {
	src := "f = () {\n    ok = fs.write-str('/tmp/x', 'y')\n    ok == true -> return\n}\n"
	cfg := ParseConfig{
		FuncSigs: map[string][]string{"fs.write-str": {"?bool"}},
	}

	prog, errs := ParseSourceWithConfig(src, cfg)
	if len(errs) > 0 || prog == nil {
		t.Fatalf("ParseSourceWithConfig: prog=%v errs=%v", prog, errs)
	}
	ext := ProgramExternSignatures(prog)
	if got := ext.FuncSigs["fs.write-str"]; len(got) == 0 || got[0] != "?bool" {
		t.Fatalf("extern signature not injected into program.Sem: %#v", ext.FuncSigs)
	}

	plain, perrs := ParseSourceWithConfig(src, ParseConfig{})
	if len(perrs) > 0 || plain == nil {
		t.Fatalf("zero-config parse: prog=%v errs=%v", plain, perrs)
	}
	if n := len(ProgramExternSignatures(plain).FuncSigs); n != 0 {
		t.Fatalf("zero config leaked %d extern signatures", n)
	}
}

func TestFormatFileWithConfigSimplifiesCrossModuleBool(t *testing.T) {
	src := "f = () {\n    ok = fs.is-empty()\n    ok == true -> return\n}\n"
	cfg := ParseConfig{
		FuncSigs: map[string][]string{"fs.is-empty": {"bool"}},
	}

	got, errs := FormatFileWithConfig(src, cfg, LoopStylePrefix)
	if len(errs) > 0 {
		t.Fatalf("FormatFileWithConfig: %v", errs)
	}
	if !strings.Contains(got, "ok -> return") || strings.Contains(got, "ok == true") {
		t.Fatalf("cross-module bool result should simplify, got:\n%s", got)
	}

	// 未注入配置：同一份源碼必須維持「未知不化簡」的舊行為。
	plain, perrs := FormatFileWithConfig(src, ParseConfig{}, LoopStylePrefix)
	if len(perrs) > 0 {
		t.Fatalf("zero-config format: %v", perrs)
	}
	if !strings.Contains(plain, "ok == true") {
		t.Fatalf("unknown operand must keep comparison, got:\n%s", plain)
	}
}

func TestFormatFileWithConfigKeepsOptionComparison(t *testing.T) {
	// ?bool 是 option：即使簽名表看得見，硬門禁仍禁止化簡（v0.3.3
	// option-as-i1 crash 的形狀）。
	src := "# std/fs\nf = () {\n    ok = fs.write-str('/tmp/x', 'y')\n    ok == true -> return\n}\n"
	cfg := ParseConfig{
		FuncSigs: map[string][]string{"fs.write-str": {"?bool"}},
	}
	got, errs := FormatFileWithConfig(src, cfg, LoopStylePrefix)
	if len(errs) > 0 {
		t.Fatalf("FormatFileWithConfig: %v", errs)
	}
	if !strings.Contains(got, "ok == true") {
		t.Fatalf("option operand must keep comparison, got:\n%s", got)
	}
}

func TestZeroConfigMatchesLegacyFormatAPI(t *testing.T) {
	src := "f = () {\n    x = 1 + 2\n    x == false -> return\n}\n"
	legacy, legacyErrs := FormatFileWithErrorsAndLoopStyle(src, LoopStylePrefix)
	got, errs := FormatFileWithConfig(src, ParseConfig{}, LoopStylePrefix)
	if len(legacyErrs) != len(errs) {
		t.Fatalf("errs mismatch: legacy=%v new=%v", legacyErrs, errs)
	}
	if got != legacy {
		t.Fatalf("zero config changed output:\nlegacy: %q\nnew:    %q", legacy, got)
	}
}

// 方法呼叫的 receiver 是變數時，查表鍵是 `s.starts-with`，簽名表卻是
// `str.starts-with`。externMethodResultsForVars 用 Sem 已推斷的 `s: str`
// 補鍵，讓 bool 證據能命中；未注入設定時仍維持「未知不化簡」。
func TestFormatFileWithConfigSimplifiesTypedMethodResult(t *testing.T) {
	src := "f = () {\n    s = 'abc'\n    ok = s.starts-with('a')\n    ok == true -> return\n}\n"
	cfg := ParseConfig{
		MethodSigs: map[string][]string{"str.starts-with": {"bool"}},
	}
	got, errs := FormatFileWithConfig(src, cfg, LoopStylePrefix)
	if len(errs) > 0 {
		t.Fatalf("FormatFileWithConfig: %v", errs)
	}
	if !strings.Contains(got, "ok -> return") || strings.Contains(got, "ok == true") {
		t.Fatalf("typed method bool result should simplify, got:\n%s", got)
	}

	plain, perrs := FormatFileWithConfig(src, ParseConfig{}, LoopStylePrefix)
	if len(perrs) > 0 {
		t.Fatalf("zero-config format: %v", perrs)
	}
	if !strings.Contains(plain, "ok == true") {
		t.Fatalf("zero config must keep the comparison, got:\n%s", plain)
	}
}

func TestFormatFileWithConfigKeepsOptionMethodResult(t *testing.T) {
	src := "f = () {\n    s = 'abc'\n    ok = s.starts-with('a')\n    ok == true -> return\n}\n"
	cfg := ParseConfig{
		MethodSigs: map[string][]string{"str.starts-with": {"?bool"}},
	}
	got, errs := FormatFileWithConfig(src, cfg, LoopStylePrefix)
	if len(errs) > 0 {
		t.Fatalf("FormatFileWithConfig: %v", errs)
	}
	if !strings.Contains(got, "ok == true") {
		t.Fatalf("option method result must keep comparison, got:\n%s", got)
	}
}

func TestExternMethodResultsForVarsOnlyMatchesInferredType(t *testing.T) {
	sem := parser.NewSemanticContext()
	sem.VarTypes = map[string]*ty.Type{"s": ty.Parse("str"), "n": ty.Parse("i64")}
	sem.ExternMethodSigs = map[string][]string{
		"str.starts-with": {"bool"},
		"i64.starts-with": {"?bool"},
	}
	got := externMethodResultsForVars(sem)
	if t1 := got["s.starts-with"]; t1 == nil || !t1.IsBool() {
		t.Fatalf("str receiver should resolve to bool: %#v", got)
	}
	if t2 := got["n.starts-with"]; t2 == nil || !t2.IsOption() {
		t.Fatalf("i64 receiver should resolve to ?bool: %#v", got)
	}
	if _, ok := got["unknown.starts-with"]; ok {
		t.Fatalf("untyped variable must not be guessed: %#v", got)
	}
}

func TestExternMethodResultsForVarsSkipsAmbiguousVar(t *testing.T) {
	sem := parser.NewSemanticContext()
	sem.VarTypes = map[string]*ty.Type{"s": ty.Parse("str")}
	sem.FuncVarTypes = map[string]map[string]*ty.Type{
		"g": {"s": ty.Parse("i64")},
	}
	sem.ExternMethodSigs = map[string][]string{"str.starts-with": {"bool"}}
	if got := externMethodResultsForVars(sem); len(got) != 0 {
		t.Fatalf("scope-conflicting variable must be skipped: %#v", got)
	}
}
