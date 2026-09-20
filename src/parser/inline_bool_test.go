package parser

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
)

// TestInlineBoolValueSurvivesToHIR pins the whole point of the explicit boolean
// spelling: `#{inline=false}` and `#{inline=true}` must stay distinguishable all
// the way into HIR.
//
// Before this, the parser built the SAME value object for both (no stored
// boolean) and `AnnotationBoolValue.String()` always returned "true". The
// consequences were both silent: `no fmt` rewrote `#{inline=false}` into
// `#{inline=true}`, reversing the meaning, and no downstream consumer could
// have told the two apart even if it wanted to.
func TestInlineBoolValueSurvivesToHIR(t *testing.T) {
	cases := []struct {
		name    string
		ann     string
		present bool
		value   bool
	}{
		{"absent", "", false, false},
		{"bare", "#{inline}\n", true, true},
		{"explicit true", "#{inline=true}\n", true, true},
		{"explicit false", "#{inline=false}\n", true, false},
		{"int 1", "#{inline=1}\n", true, true},
		{"int 0", "#{inline=0}\n", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pkg := lowerSrc(t, tc.ann+"color {\n    red,\n    green,\n}\n")
			if len(pkg.Top) == 0 {
				t.Fatal("no top-level nodes")
			}
			present, value := pkg.AnnotationBool(pkg.Top[0], "inline")
			if present != tc.present || value != tc.value {
				t.Errorf("AnnotationBool(inline) = (present=%v, value=%v), want (%v, %v)",
					present, value, tc.present, tc.value)
			}
		})
	}
}

// inlineEntriesOf parses `src` and returns the annotations attached to its first
// statement (the annotation in `src` is written above that statement).
func inlineEntriesOf(t *testing.T, src string) []*AnnotationEntry {
	t.Helper()
	p := New(lexer.NewCached("inline_bool_test.no", src))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parsing %q failed: %v", src, errs)
	}
	if len(prog.Statements) == 0 {
		t.Fatalf("parsing %q produced no statements", src)
	}
	entries := p.sem.RawAnnotationsOf(prog.Statements[0])
	if len(entries) == 0 {
		entries = p.sem.AnnotationsOf(prog.Statements[0])
	}
	return entries
}

// TestInlineAnnotationValueSpellings covers the parser-side helper directly,
// including the `ok=false` case that the checker turns into a diagnostic.
func TestInlineAnnotationValueSpellings(t *testing.T) {
	cases := []struct {
		ann     string
		present bool
		value   bool
		ok      bool
	}{
		{"#{inline}\n", true, true, true},
		{"#{inline=true}\n", true, true, true},
		{"#{inline=false}\n", true, false, true},
		{"#{inline=1}\n", true, true, true},
		{"#{inline=0}\n", true, false, true},
		{"#{inline=foo}\n", true, false, false},
		{"#{inline='true'}\n", true, false, false},
		{"#{other}\n", false, false, true},
	}
	for _, tc := range cases {
		entries := inlineEntriesOf(t, tc.ann+"color {\n    red,\n}\n")
		gotPresent, gotValue, gotOK := InlineAnnotationValue(entries)
		if gotPresent != tc.present || gotValue != tc.value || gotOK != tc.ok {
			t.Errorf("%q: InlineAnnotationValue = (present=%v, value=%v, ok=%v), want (%v, %v, %v)",
				tc.ann, gotPresent, gotValue, gotOK, tc.present, tc.value, tc.ok)
		}
	}
}

// TestAnnotationBoolValueStringRoundTrip guards the formatter-visible text:
// the whole reason `#{inline=false}` used to be corrupted is that String()
// ignored the value.
func TestAnnotationBoolValueStringRoundTrip(t *testing.T) {
	if got := (&AnnotationBoolValue{Value: true}).String(); got != "true" {
		t.Errorf("true.String() = %q, want \"true\"", got)
	}
	if got := (&AnnotationBoolValue{Value: false}).String(); got != "false" {
		t.Errorf("false.String() = %q, want \"false\"", got)
	}
	if got := (&AnnotationEntry{Key: "inline", Value: &AnnotationBoolValue{Value: false}}).String(); got != "inline=false" {
		t.Errorf("entry String() = %q, want \"inline=false\"", got)
	}
	// A bare flag has no value at all and must stay bare.
	if got := (&AnnotationEntry{Key: "inline"}).String(); got != "inline" {
		t.Errorf("bare entry String() = %q, want \"inline\"", got)
	}
}
