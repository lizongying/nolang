package checker

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/i18n"
)

// TestKebabSuggestion 釘住 kebab-case 建議的轉換規則：camel/Pascal 邊界、
// 底線折疊、縮寫詞邊界，以及「給不出安全建議」的幾種輸入。
func TestKebabSuggestion(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"camel", "canvasEl", "canvas-el"},
		{"pascal", "MyData", "my-data"},
		{"acronym-boundary", "HTTPServer", "http-server"},
		{"underscore-run", "tmp__1", "tmp-1"},
		{"underscore-mid", "x__y", "x-y"},
		{"underscore-tail", "foo_bar_", "foo-bar"},
		{"all-caps-with-underscore", "LEVEL_ERROR", "level-error"},
		{"digit-then-upper", "a1B2", "a1-b2"},
		{"dotted", "foo.BarBaz", "foo.bar-baz"},
		{"leading-digit", "1foo", ""},
		{"non-ascii", "变量", ""},
		{"bracketed-receiver", "[]t.Desc", ""},
		{"empty", "", ""},
		{"already-kebab", "canvas-el", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := kebabSuggestion(tc.in); got != tc.want {
				t.Fatalf("kebabSuggestion(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestNamingMessageSuggestion 確認帶建議時用的是新文案，且建議名真的出現在
// 訊息裡；判別力由「不等於舊的 2dhoris2 文案」保證。
func TestNamingMessageSuggestion(t *testing.T) {
	got := namingMessage("canvasEl")
	if !strings.Contains(got, "canvas-el") {
		t.Fatalf("suggestion missing from message: %q", got)
	}
	plain := i18n.T("2dhoris2", "'%s' should use only lowercase letters and hyphens", "canvasEl")
	if got == plain {
		t.Fatalf("message fell back to the plain wording: %q", got)
	}
}

// TestNamingMessageNoSuggestion 確認推不出建議時維持原本的單參數文案，
// 不會印出 %!s(MISSING) 或誤導性的改寫。
func TestNamingMessageNoSuggestion(t *testing.T) {
	for _, name := range []string{"1foo", "变量"} {
		got, want := namingMessage(name),
			i18n.T("2dhoris2", "'%s' should use only lowercase letters and hyphens", name)
		if got != want {
			t.Fatalf("namingMessage(%q) = %q, want plain %q", name, got, want)
		}
		if strings.Contains(got, "%!") {
			t.Fatalf("format error in message: %q", got)
		}
	}
}

// TestNamingDiagnosticsCarrySuggestion 走完整 ValidateNaming：變數宣告、
// struct 型別名兩條出口都要帶建議，且 TraceID 仍是 2dhoris2。
func TestNamingDiagnosticsCarrySuggestion(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"top-level-let", "canvasEl = 1\n", "canvas-el"},
		{"local-let", "f = () {\n    canvasEl = 1\n}\n", "canvas-el"},
		{"struct-type", "c = net.Conn {\n    host: \"x\"\n}\n", "conn"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			found := false
			for _, r := range ValidateNaming(mustParse(t, tc.src)) {
				if r.TraceID != "2dhoris2" {
					continue
				}
				found = true
				if !strings.Contains(r.Message, tc.want) {
					t.Fatalf("diagnostic %q does not suggest %q", r.Message, tc.want)
				}
			}
			if !found {
				t.Fatal("no 2dhoris2 diagnostic emitted")
			}
		})
	}
}
