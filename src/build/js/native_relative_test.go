package js

import (
	"strings"
	"testing"
)

// A native wasm= specifier may be a local file ("./x.mjs", "../x", "/abs/x")
// instead of an npm package: it must be accepted by the build, skipped by the
// dependency check and left out of the browser import map, which never remaps
// relative specifiers.
func TestIsRelativeSpecifier(t *testing.T) {
	relatives := []string{"./math-wasm.mjs", "../pkg/x.mjs", "/abs/x.mjs", "./a/b/c.js"}
	for _, spec := range relatives {
		if !IsRelativeSpecifier(spec) {
			t.Errorf("IsRelativeSpecifier(%q) = false, want true", spec)
		}
	}
	bares := []string{"lodash", "lodash/fp", "@scope/pkg", "pkg/sub/deep", "math-wasm-demo", ""}
	for _, spec := range bares {
		if IsRelativeSpecifier(spec) {
			t.Errorf("IsRelativeSpecifier(%q) = true, want false", spec)
		}
	}
}

// The import map only remaps bare npm specifiers. A relative one would be
// mapped to a meaningless CDN URL (esm.sh/./x.mjs) that browsers ignore
// anyway, so it must be omitted; a map with no bare entry is dropped entirely.
func TestRenderHTMLModuleOmitsRelativeSpecifiers(t *testing.T) {
	html := RenderHTMLModule("t", "main.js", []ESMImport{
		{Specifier: "./math-wasm.mjs", Bind: "wasm", Init: "default"},
		{Specifier: "lodash", ShortName: "lodash", Namespace: true},
	}, "")
	if !strings.Contains(html, `"lodash": "https://esm.sh/lodash"`) {
		t.Errorf("bare specifier missing from import map:\n%s", html)
	}
	for _, bad := range []string{`"./math-wasm.mjs"`, "esm.sh/./"} {
		if strings.Contains(html, bad) {
			t.Errorf("relative specifier leaked into import map (%q):\n%s", bad, html)
		}
	}
	if !strings.Contains(html, `<script type="module" src="main.js"></script>`) {
		t.Errorf("module script missing:\n%s", html)
	}
}

// When every ESM specifier is relative there is no import map at all.
func TestRenderHTMLModuleNoMapWhenOnlyRelative(t *testing.T) {
	html := RenderHTMLModule("t", "main.js", []ESMImport{
		{Specifier: "./math-wasm.mjs", Bind: "wasm", Init: "default"},
	}, "")
	if strings.Contains(html, "importmap") {
		t.Errorf("relative-only program must not emit an import map:\n%s", html)
	}
	if !strings.Contains(html, `<script type="module" src="main.js"></script>`) {
		t.Errorf("module script missing:\n%s", html)
	}
}
