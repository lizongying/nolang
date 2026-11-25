package pkg

import (
	"reflect"
	"strings"
	"testing"
)

func fieldKeys(fields []ManifestField) []string {
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		out = append(out, f.Key)
	}
	return out
}

// 規範欄位集直接由結構體 json tag 派生，因此 Publish 一旦加進 Package 就自動
// 進入欄位集，不會出現「結構體有、fmt 不補」的漂移。
func TestManifestFieldSets(t *testing.T) {
	top := fieldKeys(ManifestFields())
	wantTop := []string{
		"name", "description", "category", "keywords", "author", "email",
		"organization", "repository", "homepage", "license", "publish",
		"dependencies", "dev-dependencies", "ignore", "mirrors", "compiler", "output",
	}
	if !reflect.DeepEqual(top, wantTop) {
		t.Fatalf("ManifestFields() = %v, want %v", top, wantTop)
	}
	// 編譯器已移除的欄位不得再出現在欄位集裡（no fmt 視為未知欄位並移除）。
	for _, removed := range []string{"main", "alias", "version"} {
		for _, k := range top {
			if k == removed {
				t.Errorf("%q must not be a manifest field", removed)
			}
		}
	}
	sub := fieldKeys(CompilerManifestFields())
	wantSub := []string{"anonymous-fn-type", "version", "emit", "link-libs", "option-payload-inline-threshold", "esm-cdn", "lang"}
	if !reflect.DeepEqual(sub, wantSub) {
		t.Fatalf("CompilerManifestFields() = %v, want %v", sub, wantSub)
	}
	for _, k := range []string{
		"option-payload-inline-threshold", "anonymous-fn-type", "emit", "link-libs", "esm-cdn", "lang",
	} {
		if !IsNonRequiredManifestField("compiler." + k) {
			t.Errorf("compiler.%s must be recognized but not required", k)
		}
	}
	for _, k := range []string{"dev-dependencies", "publish", "mirrors", "output", "ignore"} {
		if !IsNonRequiredManifestField(k) {
			t.Errorf("%s must be recognized but not required", k)
		}
	}
	requiredTop := fieldKeys(RequiredManifestFields())
	for _, k := range []string{"dev-dependencies", "publish", "mirrors", "output", "ignore"} {
		for _, got := range requiredTop {
			if got == k {
				t.Errorf("%s must not be a required top-level field", k)
			}
		}
	}
	requiredSub := fieldKeys(RequiredCompilerManifestFields())
	if !reflect.DeepEqual(requiredSub, []string{"version"}) {
		t.Fatalf("RequiredCompilerManifestFields() = %v, want [version]", requiredSub)
	}
	for _, f := range ManifestFields() {
		if f.Key == "publish" && f.Default != "true" {
			t.Errorf("publish default = %q, want true", f.Default)
		}
	}
	for _, f := range append(ManifestFields(), CompilerManifestFields()...) {
		if f.Default == "" {
			t.Errorf("field %q has no default", f.Key)
		}
	}
	// 兩個依賴欄位在 noml 裡一律是 vec。
	for _, f := range ManifestFields() {
		if (f.Key == "dependencies" || f.Key == "dev-dependencies") && f.Default != "[]" {
			t.Errorf("%s default = %q, want []", f.Key, f.Default)
		}
	}
}

func TestCheckManifestFields(t *testing.T) {
	complete := `{
  name: 'x'
  description: ''
  category: ''
  keywords: []
  author: ''
  email: ''
  organization: ''
  repository: ''
  homepage: ''
  license: ''
  publish: true
  dependencies: []
  dev-dependencies: []
  ignore: []
  mirrors: []
  compiler: {
    version: ''
  }
  output: ''
}
`
	if issues := CheckManifestFields([]byte(complete)); !issues.Empty() {
		t.Fatalf("complete manifest reported issues: %+v", issues)
	}
	withoutIgnore := strings.Replace(complete, "  ignore: []\n", "", 1)
	if issues := CheckManifestFields([]byte(withoutIgnore)); !issues.Empty() {
		t.Fatalf("manifest without optional ignore reported issues: %+v", issues)
	}
	withoutOptional := complete
	for _, line := range []string{
		"  publish: true\n", "  dev-dependencies: []\n", "  mirrors: []\n", "  output: ''\n",
	} {
		withoutOptional = strings.Replace(withoutOptional, line, "", 1)
	}
	if issues := CheckManifestFields([]byte(withoutOptional)); !issues.Empty() {
		t.Fatalf("manifest without optional fields reported issues: %+v", issues)
	}
	minimal := strings.Replace(complete, `  compiler: {
    version: ''
  }
`, `  compiler: {
    version: 'v1'
  }
`, 1)
	if issues := CheckManifestFields([]byte(minimal)); !issues.Empty() {
		t.Fatalf("compiler with only version reported issues: %+v", issues)
	}
	// 已移除的欄位即使寫了真實值也算未知欄位。
	legacy := "{\n  name: 'x'\n  version: 'v1.0.0'\n  main: 'app.no'\n  alias: {}\n}\n"
	legacyIssues := CheckManifestFields([]byte(legacy))
	if !reflect.DeepEqual(legacyIssues.Extra, []string{"alias", "main", "version"}) {
		t.Errorf("legacy fields Extra = %v", legacyIssues.Extra)
	}

	partial := "{\n  name: 'x'\n  extra: 1\n  compiler: {\n    version: 'v1'\n    bogus: 2\n  }\n}\n"
	issues := CheckManifestFields([]byte(partial))
	if !reflect.DeepEqual(issues.Extra, []string{"extra"}) {
		t.Errorf("Extra = %v", issues.Extra)
	}
	if !reflect.DeepEqual(issues.CompilerExtra, []string{"compiler.bogus"}) {
		t.Errorf("CompilerExtra = %v", issues.CompilerExtra)
	}
	if len(issues.Missing) == 0 {
		t.Errorf("Missing must be reported: %+v", issues)
	}
	// version 已給出，compiler 的其他子欄位可選，不應再報告 CompilerMissing。
	if len(issues.CompilerMissing) != 0 {
		t.Errorf("CompilerMissing must be empty when version is present: %+v", issues)
	}
	for _, key := range issues.Missing {
		if key == "compiler" {
			t.Error("present compiler must not be listed as missing")
		}
	}
	// compiler 缺席時只報 compiler 本身，不重複列所有子欄位。
	noCompiler := CheckManifestFields([]byte("{\n  name: 'x'\n}\n"))
	foundCompiler := false
	for _, k := range noCompiler.Missing {
		if k == "compiler" {
			foundCompiler = true
		}
	}
	if !foundCompiler || len(noCompiler.CompilerMissing) != 0 {
		t.Errorf("missing-compiler issues = %+v", noCompiler)
	}
}

// 手術必須保留鍵序、註解、註解群組與多行值，只動欄位集。
func TestNormalizeManifestFieldsPreservesLayout(t *testing.T) {
	src := `#{name = 'package-manifest'}
{
  ; 段落註解
  name: 'x'
  old: [
    'a',
    'b',
  ]
  bogus: 'y' #{type = 'str'}
  compiler: {
    version: 'v1'
    stale: 1
  }
}
`
	want := `#{name = 'package-manifest'}
{
  ; 段落註解
  name: 'x'
  compiler: {
    version: 'v1'
  }
  description: ''
  category: ''
  keywords: []
  author: ''
  email: ''
  organization: ''
  repository: ''
  homepage: ''
  license: ''
  dependencies: []
}
`
	got, issues, err := NormalizeManifestFields(src)
	if err != nil {
		t.Fatal(err)
	}
	if issues.Empty() {
		t.Fatal("pre-normalization issues must be reported to the caller")
	}
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	// 冪等
	again, againIssues, err := NormalizeManifestFields(got)
	if err != nil {
		t.Fatal(err)
	}
	if again != got || !againIssues.Empty() {
		t.Fatalf("second pass changed the document (issues=%+v):\n%s", againIssues, again)
	}
	// 可選欄位不補：缺席時交給各自內建預設，不寫空值／預設值行。
	for _, optional := range []string{"publish:", "dev-dependencies:", "mirrors:", "output:", "ignore:"} {
		if strings.Contains(got, optional) {
			t.Errorf("optional field was auto-added: %q\n%s", optional, got)
		}
	}
	// 補出來的字面值仍須能被正規解析器讀回。
	m, err := ParseNomlMap([]byte(got))
	if err != nil {
		t.Fatalf("normalized manifest does not parse: %v", err)
	}
	var p Package
	if err := DecodeInto(m, &p); err != nil {
		t.Fatalf("normalized manifest does not decode: %v", err)
	}
	if p.Name != "x" {
		t.Errorf("Package.Name = %q, want x", p.Name)
	}
}

// 移除未知欄位時，它的多行／原始串值要一起移除；不相干的塊註釋逐位元組保留。
func TestNormalizeManifestFieldsRawAndBlock(t *testing.T) {
	src := "{\n  name: 'x'\n  note: `\n    raw } text\n  `\n  ;; block\n  { ]\n  ;;\n}\n"
	got, _, err := NormalizeManifestFields(src)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "  ;; block\n  { ]\n  ;;") {
		t.Fatalf("block comment was rewritten:\n%s", got)
	}
	for _, gone := range []string{"note:", "raw } text", "  `\n"} {
		if strings.Contains(got, gone) {
			t.Fatalf("removed field leaked %q:\n%s", gone, got)
		}
	}
}
