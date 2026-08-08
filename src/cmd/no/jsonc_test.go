package main

import (
	"reflect"
	"testing"
)

func TestJSONCParse_LineComment(t *testing.T) {
	input := `{
  // this is a line comment
  "foo": "/foo",
  "bar": "/bar",
}`
	v, err := jsoncParse([]byte(input))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	m, ok := v.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map, got %T", v)
	}
	if m["foo"] != "/foo" || m["bar"] != "/bar" {
		t.Errorf("unexpected values: %v", m)
	}
}

func TestJSONCParse_BlockComment(t *testing.T) {
	input := `{
  /* block comment
     spanning multiple lines */
  "foo": "/foo",
}`
	v, err := jsoncParse([]byte(input))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	m := v.(map[string]interface{})
	if m["foo"] != "/foo" {
		t.Errorf("unexpected value: %v", m["foo"])
	}
}

func TestJSONCParse_TrailingCommaObject(t *testing.T) {
	input := `{
  "foo": "/foo",
  "bar": "/bar",
}`
	v, err := jsoncParse([]byte(input))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	m := v.(map[string]interface{})
	if len(m) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(m))
	}
	if m["foo"] != "/foo" || m["bar"] != "/bar" {
		t.Errorf("unexpected values: %v", m)
	}
}

func TestJSONCParse_TrailingCommaArray(t *testing.T) {
	input := `["a", "b", "c",]`
	v, err := jsoncParse([]byte(input))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	arr, ok := v.([]interface{})
	if !ok {
		t.Fatalf("expected array, got %T", v)
	}
	if len(arr) != 3 {
		t.Fatalf("expected 3 elements, got %d", len(arr))
	}
}

func TestJSONCParseMap_BasicWithComments(t *testing.T) {
	input := `{
  // packages
  "foo": "/foo",
  "bar": "/bar", // trailing comma below
}`
	m, err := jsoncParseMap([]byte(input))
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

func TestJSONCParseMap_Empty(t *testing.T) {
	input := `{
}`
	m, err := jsoncParseMap([]byte(input))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(m) != 0 {
		t.Errorf("expected empty map, got %d entries", len(m))
	}
}

func TestJSONCMarshalMap_RoundTrip(t *testing.T) {
	original := map[string]string{
		"foo": "/foo",
		"bar": "/bar",
	}
	out := jsoncMarshalMap(original)
	m, err := jsoncParseMap([]byte(out))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if !reflect.DeepEqual(m, original) {
		t.Errorf("round-trip mismatch: got %v, want %v", m, original)
	}
}

func TestJSONCParseProjectConfig_WithComments(t *testing.T) {
	input := `{
  // package metadata
  "name": "my-pkg",
  "version": "1.0.0",
  "description": "a test package",
  "keywords": ["test", "demo",],
  "author": "Alice",
  "email": "alice@example.com",
  "organization": "",
  "repository": "https://github.com/user/repo",
  "homepage": "https://github.com/user/repo",
  "license": "MIT",
  "mirrors": [],
  "dependencies": {
    "foo": "*",
  },
  "compiler": {
    "version": "0.1.0",
  },
  "output": "/dist",
  "ignore": [],
}`
	cfg, err := jsoncParseProjectConfig([]byte(input))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if cfg.Name != "my-pkg" {
		t.Errorf("name = %q, want %q", cfg.Name, "my-pkg")
	}
	if cfg.Version != "1.0.0" {
		t.Errorf("version = %q, want %q", cfg.Version, "1.0.0")
	}
	if len(cfg.Keywords) != 2 || cfg.Keywords[0] != "test" || cfg.Keywords[1] != "demo" {
		t.Errorf("keywords = %v, want [test demo]", cfg.Keywords)
	}
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

func TestJSONCParse_StringEscapes(t *testing.T) {
	input := `{"path": "C:\\Users\\test", "quote": "say \"hi\""}`
	v, err := jsoncParse([]byte(input))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	m := v.(map[string]interface{})
	if m["path"] != `C:\Users\test` {
		t.Errorf("path = %q", m["path"])
	}
	if m["quote"] != `say "hi"` {
		t.Errorf("quote = %q", m["quote"])
	}
}

func TestJSONCParse_NestedObject(t *testing.T) {
	input := `{
  "outer": {
    "inner": "value",
    // comment
    "num": 42,
  },
}`
	v, err := jsoncParse([]byte(input))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	m := v.(map[string]interface{})
	outer, ok := m["outer"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected nested map, got %T", m["outer"])
	}
	if outer["inner"] != "value" {
		t.Errorf("inner = %v", outer["inner"])
	}
	if outer["num"] != float64(42) {
		t.Errorf("num = %v, want 42", outer["num"])
	}
}

func TestJSONCParse_BoolAndNull(t *testing.T) {
	input := `{
  "yes": true,
  "no": false,
  "nothing": null,
}`
	v, err := jsoncParse([]byte(input))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	m := v.(map[string]interface{})
	if m["yes"] != true {
		t.Errorf("yes = %v, want true", m["yes"])
	}
	if m["no"] != false {
		t.Errorf("no = %v, want false", m["no"])
	}
	if m["nothing"] != nil {
		t.Errorf("nothing = %v, want nil", m["nothing"])
	}
}
