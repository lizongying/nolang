package pkg

import (
	"strings"
	"testing"
)

func TestNomlParseBasicObject(t *testing.T) {
	src := `{
  name: 'test1'
  mirrors: []
  compiler: {
    version: 'v0.1.0'
    link-libs: []
    anonymous-fn-type: false
  }
}
`
	m, err := ParseNomlMap([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if m["name"] != "test1" {
		t.Errorf("name = %v", m["name"])
	}
	comp, ok := m["compiler"].(map[string]any)
	if !ok {
		t.Fatalf("compiler = %T", m["compiler"])
	}
	if comp["version"] != "v0.1.0" {
		t.Errorf("compiler.version = %v", comp["version"])
	}
	if comp["anonymous-fn-type"] != false {
		t.Errorf("anonymous-fn-type = %v", comp["anonymous-fn-type"])
	}
}

func TestNomlQuotedKeys(t *testing.T) {
	src := `{
  test1: '/example/test1'
  'github.com/lizongying/nolang/test2': '/example/test2'
  '@': './'
}
`
	m, err := ParseNomlMap([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if m["test1"] != "/example/test1" {
		t.Errorf("test1 = %v", m["test1"])
	}
	if m["github.com/lizongying/nolang/test2"] != "/example/test2" {
		t.Errorf("slug key = %v", m["github.com/lizongying/nolang/test2"])
	}
	if m["@"] != "./" {
		t.Errorf("@ key = %v", m["@"])
	}
}

func TestNomlComments(t *testing.T) {
	src := `{
  ; 本地包註解
  name: 'x' ; 行尾註解
  output: '/dist'
}
`
	m, err := ParseNomlMap([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if m["name"] != "x" || m["output"] != "/dist" {
		t.Errorf("m = %v", m)
	}
}

func TestNomlArrayTrailingComma(t *testing.T) {
	src := `{
  keywords: [
    'nolang',
    'js',
  ]
  empty: []
}
`
	m, err := ParseNomlMap([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	arr, ok := m["keywords"].([]any)
	if !ok || len(arr) != 2 || arr[0] != "nolang" || arr[1] != "js" {
		t.Errorf("keywords = %v", m["keywords"])
	}
	if e, ok := m["empty"].([]any); !ok || len(e) != 0 {
		t.Errorf("empty = %v", m["empty"])
	}
}

func TestNomlStringEscapes(t *testing.T) {
	src := `{
  s: 'a\'b\\c\nd'
  semis: 'https://github.com/o/r'
}
`
	m, err := ParseNomlMap([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if m["s"] != "a'b\\c\nd" {
		t.Errorf("s = %q", m["s"])
	}
	if m["semis"] != "https://github.com/o/r" {
		t.Errorf("semis = %q", m["semis"])
	}
}

func TestNomlNumbersAndNull(t *testing.T) {
	src := `{
  n1: 42
  n2: -3.5
  n3: 1e3
  missing: null
}
`
	m, err := ParseNomlMap([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if m["n1"] != 42.0 || m["n2"] != -3.5 || m["n3"] != 1000.0 {
		t.Errorf("numbers = %v %v %v", m["n1"], m["n2"], m["n3"])
	}
	if v, ok := m["missing"]; !ok || v != nil {
		t.Errorf("null = %v (%v)", m["missing"], ok)
	}
}

func TestNomlErrors(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"comma separated members", "{\n a: 1,\n b: 2\n}"},
		{"two members one line", "{\n a: 1 b: 2\n}"},
		{"unquoted non-ident key", "{\n github.com/x: 'y'\n}"},
		{"double quoted string", "{\n name: \"x\"\n}"},
		{"duplicate key", "{\n name: 'a'\n name: 'b'\n}"},
		{"unterminated string", "{\n name: 'a\n}"},
		{"missing colon", "{\n name 'a'\n}"},
	}
	for _, c := range cases {
		if _, err := ParseNoml([]byte(c.src)); err == nil {
			t.Errorf("%s: expected error, got nil", c.name)
		} else if !strings.HasPrefix(err.Error(), "noml:") {
			t.Errorf("%s: bad error %v", c.name, err)
		}
	}
}

func TestNomlErrorPosition(t *testing.T) {
	_, err := ParseNoml([]byte("{\n  name: 'a'\n  github.com/x: 'y'\n}\n"))
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "line 3") {
		t.Errorf("error should mention line 3: %v", err)
	}
	_, err = ParseNoml([]byte("{\n  name: 'a'\n  size: 3\n  bad\n}\n"))
	if err == nil {
		t.Fatal("expected error for member without colon")
	}
	if !strings.Contains(err.Error(), "line 4") {
		t.Errorf("error should mention line 4: %v", err)
	}
}

func TestNomlSplitDepSpec(t *testing.T) {
	cases := []struct {
		spec string
		key  string
		ver  string
	}{
		{"/example/test2", "/example/test2", "*"},
		{"koffi:^2.9.0", "koffi", "^2.9.0"},
		{"lodash:^4.17.21", "lodash", "^4.17.21"},
		{"github.com/lizongying/nolang/test1:v0.1.0", "github.com/lizongying/nolang/test1", "v0.1.0"},
		{"github.com/lizongying/nolang/test2:*", "github.com/lizongying/nolang/test2", "*"},
		{"nogit:v0.1.33@github.com/lizongying/notools", "nogit@github.com/lizongying/notools", "v0.1.33"},
		{"nogit@github.com/lizongying/notools", "nogit@github.com/lizongying/notools", "*"},
		{"github.com/lizongying/nolang/test2", "github.com/lizongying/nolang/test2", "*"},
	}
	for _, c := range cases {
		k, v := SplitDepSpec(c.spec)
		if k != c.key || v != c.ver {
			t.Errorf("SplitDepSpec(%q) = (%q,%q), want (%q,%q)", c.spec, k, v, c.key, c.ver)
		}
	}
}

func TestNomlDecodePackage(t *testing.T) {
	src := `{
  name: 'esm-imports'
  keywords: ['nolang', 'js']
  dependencies: ['lodash:^4.17.21']
  compiler: {
    version: 'v0.1.0'
    link-libs: ['m']
    anonymous-fn-type: false
    emit: 'js'
  }
  output: '/dist'
}
`
	m, err := ParseNomlMap([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var p Package
	if err := DecodeInto(m, &p); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if p.Name != "esm-imports" || p.Output != "/dist" {
		t.Errorf("name/output = %q %q", p.Name, p.Output)
	}
	if p.Dependencies["lodash"] != "^4.17.21" {
		t.Errorf("dependencies = %v", p.Dependencies)
	}
	if p.Compiler.Emit != "js" || p.Compiler.AnonymousFnType {
		t.Errorf("compiler = %+v", p.Compiler)
	}
	if len(p.Compiler.LinkLibs) != 1 || p.Compiler.LinkLibs[0] != "m" {
		t.Errorf("link-libs = %v", p.Compiler.LinkLibs)
	}
}

func TestNomlDecodeWorkspaceMap(t *testing.T) {
	src := `{
  test1: '/example/test1'
  test: '/test'
}
`
	m, err := ParseNomlMap([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var ws WorkspaceMap
	if err := DecodeInto(m, &ws); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if ws["test1"] != "/example/test1" || ws["test"] != "/test" {
		t.Errorf("ws = %v", ws)
	}
}

func TestNomlRoundTrip(t *testing.T) {
	original := map[string]any{
		"name":   "compiler",
		"output": "/dist",
		"dependencies": []any{
			"lodash:^4.17.21",
		},
		"compiler": map[string]any{
			"version":           "dev",
			"link-libs":         []any{},
			"anonymous-fn-type": false,
			"threshold":         float64(24),
		},
	}
	text, err := MarshalNoml(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	back, err := ParseNomlMap([]byte(text))
	if err != nil {
		t.Fatalf("reparse %q: %v", text, err)
	}
	if back["name"] != "compiler" {
		t.Errorf("round-trip name = %v", back["name"])
	}
	comp := back["compiler"].(map[string]any)
	if comp["threshold"] != 24.0 || comp["anonymous-fn-type"] != false {
		t.Errorf("round-trip compiler = %v", comp)
	}
}

func TestNomlMarshalQuotingRules(t *testing.T) {
	text, err := MarshalNoml(map[string]any{
		"@":                       "./",
		"link-libs":               []any{"m"},
		"test1":                   "/example/test1",
		"github.com/lizongying/x": "v1",
		"1abc":                    "numeric-leading",
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(text, "'@': './'") {
		t.Errorf("@ not quoted:\n%s", text)
	}
	if !strings.Contains(text, "link-libs: [") {
		t.Errorf("ident key should be bare:\n%s", text)
	}
	if !strings.Contains(text, "'github.com/lizongying/x'") {
		t.Errorf("slug key not quoted:\n%s", text)
	}
	if !strings.Contains(text, "'1abc'") {
		t.Errorf("digit-leading key not quoted:\n%s", text)
	}
	// 空陣列內聯、非空陣列帶尾隨逗號
	if !strings.Contains(text, "[\n    'm',\n  ]") {
		t.Errorf("array trailing comma format:\n%s", text)
	}
}

func TestNomlEmptyObject(t *testing.T) {
	m, err := ParseNomlMap([]byte("{\n}\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(m) != 0 {
		t.Errorf("m = %v", m)
	}
}
