package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	pkg "github.com/lizongying/nolang/package"
)

// `no fmt` 的 noml 接線測試。
//
// noml 走的是「排版規範化」（見 src/fmt/noml.go 檔頭），與 .no 的語法重排
// 是兩套規則；這裡釘住 CLI 層的三件事：檔名不受源碼命名規範約束、-w 真的寫回、
// 殘缺結構不寫回。

// .noml 的檔名（package.noml / workspace.noml）是工具鏈約定，不受「小寫字母 /
// 數字 / 中劃線」的源碼命名規範約束；目錄段仍然要遵守。
func TestValidateFmtPathAllowsNomlName(t *testing.T) {
	for _, ok := range []string{
		"package.noml",
		"workspace.noml",
		"src/i18n/catalog/locales/zh-cn.noml",
		"glossary/data/traps.noml",
	} {
		if err := validateFmtPath(ok); err != nil {
			t.Errorf("validateFmtPath(%q) = %v, want nil", ok, err)
		}
	}
	if err := validateFmtPath("tmp/bad_dir/x.noml"); err == nil {
		t.Error("validateFmtPath on a directory segment with an underscore must fail")
	}
}

// 端到端：-w 把縮進與冒號空白規範化，註釋與鍵序原樣。
func TestFmtProcessNomlFileWritesInPlace(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.noml")
	src := "; 說明\n{\n     name:   'x'\n  ; 中間註解\n  compiler: {\n        version: 'v1'\n  }\n}\n"
	if err := os.WriteFile(p, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	if err := fmtProcessNomlFile(p, true, false); err != nil {
		t.Fatalf("fmtProcessNomlFile: %v", err)
	}
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	want := "; 說明\n{\n  name: 'x'\n  ; 中間註解\n  compiler: {\n    version: 'v1'\n  }\n}\n"
	if string(got) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}

	// 再跑一次必須是 no-op（冪等）。
	if err := fmtProcessNomlFile(p, true, false); err != nil {
		t.Fatalf("second pass: %v", err)
	}
	again, _ := os.ReadFile(p)
	if string(again) != want {
		t.Fatalf("second pass changed the file:\n%s", again)
	}
}

// package.noml 走欄位集規範化：補齊所有可識別欄位、移除未知欄位，
// 其餘（鍵序、註解、#{...}）保持原樣，且二次執行冪等。
func TestFmtProcessNomlFileNormalizesPackageFields(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "package.noml")
	src := "#{name = 'package-manifest'}\n{\n  name: 'probe'\n  bogus: 'x'\n  version: 'v1.0.0'\n  compiler: { }\n}\n"
	if err := os.WriteFile(p, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	if err := fmtProcessNomlFile(p, true, false); err != nil {
		t.Fatalf("fmtProcessNomlFile: %v", err)
	}
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	out := string(got)
	for _, want := range []string{
		"#{name = 'package-manifest'}",
		"  name: 'probe'",
		"  dependencies: []",
		"  compiler: {",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("formatted manifest missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "bogus") {
		t.Errorf("unknown field survived:\n%s", out)
	}
	// 可選欄位一律不補：publish / dev-dependencies / mirrors / output 留給內建預設。
	for _, unwanted := range []string{"  publish:", "  dev-dependencies:", "  mirrors:", "  output:"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("optional field was auto-added: %q\n%s", unwanted, out)
		}
	}
	// 可選的 threshold 不補、不寫進新清單。
	if strings.Contains(out, "option-payload-inline-threshold") {
		t.Errorf("optional threshold was auto-added:\n%s", out)
	}
	// ignore 也是可識別但不強制的欄位：缺席時不補。
	if strings.Contains(out, "ignore:") {
		t.Errorf("optional ignore was auto-added:\n%s", out)
	}
	// compiler 只有 version 是必寫：其他可選欄位的空值不補。
	for _, unwanted := range []string{"anonymous-fn-type:", "emit:", "link-libs:", "esm-cdn:", "lang:"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("optional compiler field was auto-added: %q\n%s", unwanted, out)
		}
	}
	// 編譯器已移除的欄位連同真實值一起被當成未知欄位清掉。
	for _, unwanted := range []string{"\n  main:", "\n  alias:", "\n  version:"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("removed field survived: %q\n%s", unwanted, out)
		}
	}
	if issues := pkg.CheckManifestFields(got); !issues.Empty() {
		t.Errorf("formatted manifest still has field issues: %+v", issues)
	}

	// 第二次執行不變。
	if err := fmtProcessNomlFile(p, true, false); err != nil {
		t.Fatalf("second pass: %v", err)
	}
	again, _ := os.ReadFile(p)
	if string(again) != out {
		t.Fatalf("second pass changed the file:\n%s", again)
	}
}

// 未閉合的塊註釋：報錯，且檔案一個位元組都不動。
func TestFmtProcessNomlFileLeavesBrokenFileUntouched(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "broken.noml")
	src := "{\n  a: 1\n  ;;\n  未閉合\n"
	if err := os.WriteFile(p, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	err := fmtProcessNomlFile(p, true, false)
	if err == nil {
		t.Fatal("unterminated block comment must be an error")
	}
	if !strings.Contains(err.Error(), "problem") {
		t.Errorf("error = %v", err)
	}
	got, _ := os.ReadFile(p)
	if string(got) != src {
		t.Fatalf("file was modified:\n%q", got)
	}
}

// 檢查模式（無 -w/-d）與 -d 都不得寫回檔案。
func TestFmtProcessNomlFileCheckModeDoesNotWrite(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.noml")
	src := "{\n    a: 1\n}\n"
	if err := os.WriteFile(p, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	if err := fmtProcessNomlFile(p, false, true); err != nil {
		t.Fatalf("diff mode: %v", err)
	}
	if err := fmtProcessNomlFile(p, false, false); err != nil {
		t.Fatalf("check mode: %v", err)
	}
	got, _ := os.ReadFile(p)
	if string(got) != src {
		t.Fatalf("file was modified without -w:\n%q", got)
	}
}
