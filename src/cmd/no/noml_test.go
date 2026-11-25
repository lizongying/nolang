package main

import (
	"reflect"
	"strings"
	"testing"

	pkg "github.com/lizongying/nolang/package"
)

// These tests exercise the noml CLI helpers in noml.go (which delegate to the
// src/package/noml.go parser/marshaler). They replace the old JSONC parser
// tests that were removed with the hard cut to .noml.

func TestNomlParseMap_BasicWithComments(t *testing.T) {
	input := `{
  ; packages
  foo: '/foo'
  bar: '/bar' ; trailing comment
}`
	m, err := nomlParseMap([]byte(input))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(m) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(m))
	}
	if m["foo"] != "/foo" || m["bar"] != "/bar" {
		t.Errorf("unexpected values: %v", m)
	}
}

func TestNomlParseMap_QuotedKeys(t *testing.T) {
	input := `{
  'github.com/lizongying/nolang/test2': '/test'
}`
	m, err := nomlParseMap([]byte(input))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if m["github.com/lizongying/nolang/test2"] != "/test" {
		t.Errorf("unexpected values: %v", m)
	}
}

func TestNomlParseMap_Empty(t *testing.T) {
	m, err := nomlParseMap([]byte("{\n}\n"))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(m) != 0 {
		t.Errorf("expected empty map, got %d entries", len(m))
	}
}

func TestNomlParseMap_RejectsNonStringValue(t *testing.T) {
	input := `{
  foo: ['a', 'b']
}`
	if _, err := nomlParseMap([]byte(input)); err == nil {
		t.Fatal("expected error: workspace values must be strings")
	}
}

func TestNomlMarshalMap_RoundTrip(t *testing.T) {
	original := map[string]string{
		"foo": "/foo",
		"bar": "/bar",
	}
	out := nomlMarshalMap(original)
	m, err := nomlParseMap([]byte(out))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if !reflect.DeepEqual(m, original) {
		t.Errorf("round-trip mismatch: got %v, want %v", m, original)
	}
}

func TestNomlMarshalMap_QuotesNonIdentifierKeys(t *testing.T) {
	original := map[string]string{"github.com/a/a": "/a"}
	out := nomlMarshalMap(original)
	// 非識別符鍵必須以單引號落地。
	if !reflect.DeepEqual(parseMust(out), original) {
		t.Errorf("round-trip mismatch: got %v, want %v", parseMust(out), original)
	}
}

func parseMust(s string) map[string]string {
	m, err := nomlParseMap([]byte(s))
	if err != nil {
		panic(err)
	}
	return m
}

func TestNomlParseProjectConfig_WithComments(t *testing.T) {
	input := `{
  ; package metadata
  name: 'my-pkg'
  version: '1.0.0'
  description: 'a test package'
  keywords: ['test', 'demo']
  author: 'Alice'
  email: 'alice@example.com'
  organization: ''
  repository: 'https://github.com/user/repo'
  homepage: 'https://github.com/user/repo'
  license: 'MIT'
  mirrors: []
  dependencies: [
    'foo',
  ]
  compiler: {
    version: '0.1.0'
  }
  output: '/dist'
  ignore: []
}`
	cfg, err := nomlParseProjectConfig([]byte(input))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if cfg.Name != "my-pkg" {
		t.Errorf("name = %q, want %q", cfg.Name, "my-pkg")
	}
	if len(cfg.Keywords) != 2 || cfg.Keywords[0] != "test" || cfg.Keywords[1] != "demo" {
		t.Errorf("keywords = %v, want [test demo]", cfg.Keywords)
	}
	// dependencies 字串陣列拆成 key:version（省略版本預設 *）。
	if cfg.Dependencies["foo"] != "*" {
		t.Errorf("dependencies[foo] = %q, want *", cfg.Dependencies["foo"])
	}
	if cfg.Compiler.Version != "0.1.0" {
		t.Errorf("compiler.version = %q, want 0.1.0", cfg.Compiler.Version)
	}
	if cfg.Output != "/dist" {
		t.Errorf("output = %q, want /dist", cfg.Output)
	}
}

func TestRenderPackageNoml_RoundTrips(t *testing.T) {
	config := ProjectConfig{
		Name:        "demo",
		Description: "a demo package",
		Author:      "Alice",
		License:     "Apache-2.0",
		Dependencies: map[string]string{
			"fmt": "^0.1.0",
		},
		Compiler: CompilerConfig{Version: "v0.1.0"},
	}
	text := renderPackageNoml(config)
	// 依賴必須是 vec，不能出現 {}。
	if !strings.Contains(text, "  dependencies: [\n") {
		t.Fatalf("dependencies must be rendered as vec:\n%s", text)
	}
	if strings.Contains(text, ": {}") {
		t.Fatalf("rendered manifest contains an object where a vec is expected:\n%s", text)
	}
	// 可選欄位不寫進新清單，留下內建預設。
	for _, unwanted := range []string{
		"  dev-dependencies:", "  publish:", "  mirrors:", "  output:", "  ignore:",
	} {
		if strings.Contains(text, unwanted) {
			t.Errorf("rendered manifest contains optional field %q:\n%s", unwanted, text)
		}
	}
	if issues := pkg.CheckManifestFields([]byte(text)); !issues.Empty() {
		t.Fatalf("rendered package.noml is not complete: %+v\n%s", issues, text)
	}
	cfg, err := nomlParseProjectConfig([]byte(text))
	if err != nil {
		t.Fatalf("rendered noml failed to parse: %v\n%s", err, text)
	}
	if cfg.Name != "demo" {
		t.Errorf("name = %q, want demo", cfg.Name)
	}
	if cfg.Dependencies["fmt"] != "^0.1.0" {
		t.Errorf("dependencies[fmt] = %q, want ^0.1.0", cfg.Dependencies["fmt"])
	}
	if cfg.Compiler.Version != "v0.1.0" {
		t.Errorf("compiler.version = %q, want v0.1.0", cfg.Compiler.Version)
	}
}
