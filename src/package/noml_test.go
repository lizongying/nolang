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
  no-tail: [
    'a'
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
	// 尾隨逗號是書寫風格約定；解析器對省略尾逗號同樣宽容
	if nt, ok := m["no-tail"].([]any); !ok || len(nt) != 1 || nt[0] != "a" {
		t.Errorf("no-tail = %v", m["no-tail"])
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

// TestNomlMemberAnnotations 鎖定：成員上方與同一行尾隨的 `#{...}` 都會
// 收集到對應路徑，且不影響值樹（與 nolang 註解語法一致）。
func TestNomlMemberAnnotations(t *testing.T) {
	src := `{
  #{type = 'str', doc = '套件名'}
  name: 'demo'
  #{type = '[]str'}
  keywords: []
  dependencies: ['lodash:^4.17.21'] #{type = '[]str'}
  compiler: {
    #{type = 'str'}
    version: 'v0.1.0' #{doc = 'manifest version'}
    #{type = '[]str'}
    link-libs: []
  }
}
`
	doc, err := ParseNomlDoc([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if doc.Root["name"] != "demo" {
		t.Errorf("name = %v", doc.Root["name"])
	}
	if got := doc.TypeOf("name"); got != "str" {
		t.Errorf("type(name) = %q, want str", got)
	}
	if got := doc.TypeOf("keywords"); got != "[]str" {
		t.Errorf("type(keywords) = %q, want []str", got)
	}
	if got := doc.TypeOf("dependencies"); got != "[]str" {
		t.Errorf("type(dependencies) = %q, want []str (trailing annotation)", got)
	}
	if got := doc.TypeOf("compiler.version"); got != "str" {
		t.Errorf("type(compiler.version) = %q, want str (nested path)", got)
	}
	if got := doc.TypeOf("compiler.link-libs"); got != "[]str" {
		t.Errorf("type(compiler.link-libs) = %q, want []str", got)
	}
	// 尾隨註解與上方註解合併（同一成員多個鍵）。
	if len(doc.Annotations["compiler.version"]) != 2 {
		t.Errorf("compiler.version annotations = %v, want 2 entries", doc.Annotations["compiler.version"])
	}
	// 值樹不受註解影響，仍可 DecodeInto。
	base, err := ParseNomlMap([]byte(src))
	if err != nil {
		t.Fatalf("ParseNomlMap: %v", err)
	}
	comp, ok := base["compiler"].(map[string]any)
	if !ok || comp["version"] != "v0.1.0" {
		t.Errorf("compiler = %v", base["compiler"])
	}
}

// TestNomlAnnotationPositionRule 鎖定與 nolang 一致的位置規則：
// 只允許獨立成行寫在成員上方，或寫在成員同一行尾隨；前綴寫法報錯。
func TestNomlAnnotationPositionRule(t *testing.T) {
	bad := []struct {
		name string
		src  string
	}{
		{"prefix same line", "{\n  #{type = 'str'} name: 'x'\n}"},
		{"annotation without member", "{\n  #{type = 'str'}\n}"},
		{"unterminated annotation", "{\n  #{type = 'str'\n  name: 'x'\n}"},
	}
	for _, c := range bad {
		if _, err := ParseNomlDoc([]byte(c.src)); err == nil {
			t.Errorf("%s: expected error, got nil", c.name)
		}
	}
}

// TestNomlDocRoundTrip 鎖定 ParseNomlDoc → MarshalNomlDoc → ParseNomlDoc
// 完整重建（值樹與成員註解都不丟）。
func TestNomlDocRoundTrip(t *testing.T) {
	src := `{
  #{type = 'str'}
  name: 'demo'
  #{type = '[]str'}
  link-libs: []
  compiler: {
    #{type = 'bool'}
    anonymous-fn-type: false
    #{type = 'i64', doc = 'bytes'}
    option-inline-threshold: 24
  }
}
`
	doc, err := ParseNomlDoc([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	text, err := MarshalNomlDoc(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	back, err := ParseNomlDoc([]byte(text))
	if err != nil {
		t.Fatalf("reparse:\n%s\nerr: %v", text, err)
	}
	for _, path := range []string{"name", "link-libs", "compiler.anonymous-fn-type", "compiler.option-inline-threshold"} {
		if doc.TypeOf(path) != back.TypeOf(path) {
			t.Errorf("type(%s) = %q, want %q\n%s", path, back.TypeOf(path), doc.TypeOf(path), text)
		}
	}
	if len(back.Annotations["compiler.option-inline-threshold"]) != 2 {
		t.Errorf("annotation entries lost:\n%s", text)
	}
	if back.Root["name"] != "demo" {
		t.Errorf("name = %v", back.Root["name"])
	}
}

// TestNomlRootAnnotation 鎖定文件級註解：寫在頂層值上方的 `#{...}` 屬於整份
// 文件（保存在空路徑），`name` 指定重建後的結構體名，`type` 為等價寫法。
func TestNomlRootAnnotation(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"name", "#{name = 'server-config'}\n{\n  port: 8080\n}\n", "server-config"},
		{"type", "#{type = 'server-options'}\n{\n  port: 8080\n}\n", "server-options"},
		{"name wins", "#{type = 'a-b', name = 'c-d'}\n{\n  port: 8080\n}\n", "c-d"},
		{"trailing", "{\n  port: 8080\n} #{name = 'e-f'}\n", "e-f"},
		{"multiple groups", "#{doc = 'config'}\n#{name = 'g-h'}\n{\n  port: 8080\n}\n", "g-h"},
		{"none", "{\n  port: 8080\n}\n", ""},
	}
	for _, c := range cases {
		doc, err := ParseNomlDoc([]byte(c.src))
		if err != nil {
			t.Fatalf("%s: parse: %v", c.name, err)
		}
		if got := doc.RootName(); got != c.want {
			t.Errorf("%s: RootName() = %q, want %q", c.name, got, c.want)
		}
		if doc.Root["port"] != float64(8080) {
			t.Errorf("%s: value tree broken: %v", c.name, doc.Root)
		}
		// 往返：文件級註解輸出在頂層值上方，重建後仍在。
		text, err := MarshalNomlDoc(doc)
		if err != nil {
			t.Fatalf("%s: marshal: %v", c.name, err)
		}
		back, err := ParseNomlDoc([]byte(text))
		if err != nil {
			t.Fatalf("%s: reparse:\n%s\nerr: %v", c.name, text, err)
		}
		if back.RootName() != doc.RootName() {
			t.Errorf("%s: round trip RootName() = %q, want %q\n%s", c.name, back.RootName(), doc.RootName(), text)
		}
	}
}

// TestNomlRootAnnotationPositionRule 鎖定頂部註解同樣只能用文件級位置：
// 與頂層 `{` 同一行的前綴寫法報錯。
func TestNomlRootAnnotationPositionRule(t *testing.T) {
	if _, err := ParseNomlDoc([]byte("#{name = 'x'} {\n  port: 1\n}")); err == nil {
		t.Error("expected an error for an annotation sharing the line with the top level object")
	}
}

// TestNomlRawString locks the backtick form: it is the only way to write a
// multi-line value without `\n` escapes, and it must behave exactly like the
// nolang source lexer (opening backtick followed by a newline, closing backtick
// alone on its line, no escape processing at all).
func TestNomlRawString(t *testing.T) {
	src := "{\n  doc: `\nline one\nline two\n`\n  name: 'x'\n}"
	v, err := ParseNoml([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	m := v.(map[string]any)
	if got, want := m["doc"], "line one\nline two"; got != want {
		t.Fatalf("doc = %q, want %q", got, want)
	}
	if got, want := m["name"], "x"; got != want {
		t.Fatalf("name = %q, want %q", got, want)
	}
}

// TestNomlRawStringNoEscapes proves the body is verbatim: a backslash-n inside a
// raw block is two characters, not a newline. If this ever started unescaping,
// translated text containing a literal backslash would silently change.
func TestNomlRawStringNoEscapes(t *testing.T) {
	src := "{\n  doc: `\na\\nb 'quoted' \"dq\"\n`\n}"
	v, err := ParseNoml([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got, want := v.(map[string]any)["doc"], `a\nb 'quoted' "dq"`; got != want {
		t.Fatalf("doc = %q, want %q", got, want)
	}
}

// TestNomlRawStringIndentedClose accepts a closing backtick preceded by
// whitespace, which is what an indented member looks like.
func TestNomlRawStringIndentedClose(t *testing.T) {
	src := "{\n  doc: `\nbody\n   `\n}"
	v, err := ParseNoml([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got, want := v.(map[string]any)["doc"], "body"; got != want {
		t.Fatalf("doc = %q, want %q", got, want)
	}
}

// TestNomlRawStringErrors pins the two failure modes with messages that tell the
// author what to fix, rather than a bare "unexpected character".
func TestNomlRawStringErrors(t *testing.T) {
	if _, err := ParseNoml([]byte("{\n  doc: `same line`\n}")); err == nil {
		t.Error("expected an error when the opening backtick is not followed by a newline")
	}
	if _, err := ParseNoml([]byte("{\n  doc: `\nno terminator\n}")); err == nil {
		t.Error("expected an error for an unterminated raw string")
	}
}

// TestNomlRawStringAnnotation covers the case the syntax exists for: a multi-line
// doc annotation on a member (module / method / function documentation).
func TestNomlRawStringAnnotation(t *testing.T) {
	src := "{\n  #{doc = `\nReads a file.\nReturns option<str>.\n`}\n  read: 'x'\n}"
	doc, err := ParseNomlDoc([]byte(src))
	if err != nil {
		t.Fatalf("parse doc: %v", err)
	}
	anns := doc.Annotations["read"]
	if len(anns) != 1 || anns[0].Key != "doc" {
		t.Fatalf("annotations = %+v", anns)
	}
	if got, want := anns[0].Value, "Reads a file.\nReturns option<str>."; got != want {
		t.Fatalf("doc value = %q, want %q", got, want)
	}
}

// TestNomlQuoteValueRoundTrip is the property that matters for the catalogs: what
// the generator writes must parse back to the identical string, for both the raw
// form and the fallback single-quoted form.
func TestNomlQuoteValueRoundTrip(t *testing.T) {
	cases := []string{
		"single line",
		"has 'quote' and \\backslash",
		"two\nlines",
		"trailing newline\n",
		"blank\n\nline",
		"a\n`\nb", // a line that IS the terminator: must fall back
	}
	for _, c := range cases {
		quoted := NomlQuoteValue(c)
		src := "{\n  k: " + quoted + "\n}"
		v, err := ParseNoml([]byte(src))
		if err != nil {
			t.Fatalf("%q -> %q: parse: %v", c, quoted, err)
		}
		if got := v.(map[string]any)["k"]; got != c {
			t.Fatalf("%q -> %q round-tripped to %q", c, quoted, got)
		}
	}
	if q := NomlQuoteValue("a\n`\nb"); q[0] == '`' {
		t.Errorf("a value containing a terminator line must fall back to the quoted form, got %q", q)
	}
}
