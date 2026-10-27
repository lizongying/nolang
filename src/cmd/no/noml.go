package main

import (
	"fmt"
	"sort"
	"strings"

	pkg "github.com/lizongying/nolang/package"
)

// This file hosts the noml (Nolang Markup Language) helpers used by the
// `no` CLI for workspace/package configuration files. The format itself is
// implemented in src/package/noml.go (parser, marshaler, 'key:version'
// dependency specs); the .jsonc readers/writers that used to live here were
// removed when the toolchain hard-cut to .noml.

// ─── Public API ───────────────────────────────────────────────────────────────

// nomlParseMap parses noml data that is expected to be a flat object whose
// values are all strings (e.g. workspace.noml). Returns map[string]string.
func nomlParseMap(data []byte) (map[string]string, error) {
	m, err := pkg.ParseNomlMap(data)
	if err != nil {
		return nil, err
	}
	result := make(map[string]string, len(m))
	for k, val := range m {
		s, ok := val.(string)
		if !ok {
			return nil, fmt.Errorf("noml: value for key %q is not a string (got %T)", k, val)
		}
		result[k] = s
	}
	return result, nil
}

// nomlMarshalMap serialises a map[string]string to noml text with 2-space
// indentation. Keys are sorted for deterministic output; object members are
// newline-separated (no commas).
func nomlMarshalMap(m map[string]string) string {
	anyMap := make(map[string]any, len(m))
	for k, v := range m {
		anyMap[k] = v
	}
	text, err := pkg.MarshalNoml(anyMap)
	if err != nil {
		// map[string]string 不可能序列化失敗；保底返回空物件。
		return "{\n}\n"
	}
	return text
}

// nomlParseProjectConfig parses noml data into a ProjectConfig.
func nomlParseProjectConfig(data []byte) (*ProjectConfig, error) {
	m, err := pkg.ParseNomlMap(data)
	if err != nil {
		return nil, err
	}
	var cfg ProjectConfig
	if err := pkg.DecodeInto(m, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// ─── package.noml template ───────────────────────────────────────────────────

// renderPackageNoml renders a package.noml document from ProjectConfig.
// dependencies 以 'key:version' 字串陣列落地（排序後輸出求確定性）。
func renderPackageNoml(config ProjectConfig) string {
	var sb strings.Builder
	sb.WriteString("{\n")
	writePair := func(key, value string) {
		sb.WriteString("  ")
		sb.WriteString(nomlFormatKeyLocal(key))
		sb.WriteString(": ")
		sb.WriteString(pkg.NomlQuote(value))
		sb.WriteString("\n")
	}
	writePair("name", config.Name)
	writePair("description", config.Description)
	sb.WriteString("  keywords: []\n")
	writePair("author", config.Author)
	writePair("email", config.Email)
	writePair("organization", config.Organization)
	writePair("repository", config.Repository)
	writePair("homepage", config.Homepage)
	writePair("license", config.License)
	sb.WriteString("  mirrors: []\n")
	sb.WriteString(formatDependencies(config.Dependencies))
	sb.WriteString("  compiler: {\n")
	sb.WriteString("    version: ")
	sb.WriteString(pkg.NomlQuote(config.Compiler.Version))
	sb.WriteString("\n")
	sb.WriteString("    link-libs: []\n")
	sb.WriteString("  }\n")
	writePair("output", config.Output)
	sb.WriteString("  ignore: []\n")
	sb.WriteString("}\n")
	return sb.String()
}

func nomlFormatKeyLocal(k string) string {
	if pkg.IsNomlIdentifierKey(k) {
		return k
	}
	return pkg.NomlQuote(k)
}

// formatDependencies renders the dependencies member as a noml string array
// of 'key:version' elements (sorted for deterministic output).
func formatDependencies(deps map[string]string) string {
	if len(deps) == 0 {
		return "  dependencies: []\n"
	}
	keys := make([]string, 0, len(deps))
	for k := range deps {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	sb.WriteString("  dependencies: [\n")
	for _, k := range keys {
		sb.WriteString("    ")
		sb.WriteString(pkg.NomlQuote(pkg.FormatDepSpec(k, deps[k])))
		sb.WriteString(",\n") // 尾隨逗號：陣列元素一律帶逗號
	}
	sb.WriteString("  ]\n")
	return sb.String()
}
