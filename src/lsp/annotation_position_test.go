package lsp

import (
	"strings"
	"testing"
)

// The annotation placement rule — `#{...}` may only be written on its own line
// above the target, or trailing on the target's line; a same-line prefix is an
// error — must reach the editor, not just `no build`. The LSP surfaces parser
// diagnostics verbatim (publishDocumentDiagnostics → parseErrorToDiagnostic),
// so this test pins both that the parser reports it and that it is positioned
// on the offending line rather than pinned to (0,0).
func TestParseDocumentReportsPrefixAnnotationError(t *testing.T) {
	dm := NewDocumentManager()
	uri := "file:///test/ann_prefix.no"
	text := "f = () () {\n" +
		"    arr [3]i64 = [1, 2, 3]\n" +
		"    i = 5\n" +
		"    #{index-out = 0} res = arr[i]\n" +
		"    print(res)\n" +
		"}\n"

	if _, err := dm.OpenDocument(uri, text); err != nil {
		t.Fatalf("OpenDocument failed: %v", err)
	}
	_, errs, err := dm.ParseDocument(uri)
	if err != nil {
		t.Fatalf("ParseDocument failed: %v", err)
	}
	var hits []string
	for _, e := range errs {
		if strings.Contains(e, "前方同一行") {
			hits = append(hits, e)
		}
	}
	if len(hits) == 0 {
		t.Fatalf("ParseDocument did not report the prefix-annotation error, got: %v", errs)
	}

	srv := NewServer()
	d := srv.parseErrorToDiagnostic(hits[0])
	if d.Range.Start.Line != 3 {
		t.Errorf("diagnostic line = %d, want 3 (the `#{index-out = 0} res = arr[i]` line); msg=%q",
			d.Range.Start.Line, hits[0])
	}
	if d.Range.Start.Character != 4 {
		t.Errorf("diagnostic character = %d, want 4 (the annotation column); msg=%q",
			d.Range.Start.Character, hits[0])
	}
	// The editor must not be shown the position a second time, nor the raw
	// diagnostic code glued into the prose: Range carries the position, Code
	// carries the code, Message is the sentence alone.
	if strings.Contains(d.Message, "line 3, column 4") {
		t.Errorf("message must not repeat the position: %q", d.Message)
	}
	if strings.Contains(d.Message, "[E_") {
		t.Errorf("message must not contain the raw code tag: %q", d.Message)
	}
	if d.Code != "E_GENERAL" {
		t.Errorf("code = %v, want %q", d.Code, "E_GENERAL")
	}
	if !strings.Contains(d.Message, "前方同一行") {
		t.Errorf("message lost its text: %q", d.Message)
	}

	// Control: the same annotation written trailing on the line is accepted.
	okURI := "file:///test/ann_trailing.no"
	okText := "f = () () {\n" +
		"    arr [3]i64 = [1, 2, 3]\n" +
		"    i = 5\n" +
		"    res = arr[i] #{index-out = 0}\n" +
		"    print(res)\n" +
		"}\n"
	if _, err := dm.OpenDocument(okURI, okText); err != nil {
		t.Fatalf("OpenDocument failed: %v", err)
	}
	if _, errs, err := dm.ParseDocument(okURI); err != nil {
		t.Fatalf("ParseDocument failed: %v", err)
	} else if len(errs) > 0 {
		t.Errorf("trailing annotation must not produce parse errors, got: %v", errs)
	}
}

// `if <cond> #{...} {` is the other place a prefix annotation can hide: the scan
// between the condition and the body brace is deliberately lenient, and it used
// to swallow the whole group — so the editor showed neither an error nor any
// effect. It must reach the editor through the same diagnostic path.
func TestParseDocumentReportsPrefixAnnotationInIfCondition(t *testing.T) {
	dm := NewDocumentManager()
	uri := "file:///test/ann_if_prefix.no"
	text := "f = () () {\n" +
		"    x = 1\n" +
		"    if x > 0 #{overflow = wrap} {\n" +
		"        print(x)\n" +
		"    }\n" +
		"}\n"

	if _, err := dm.OpenDocument(uri, text); err != nil {
		t.Fatalf("OpenDocument failed: %v", err)
	}
	_, errs, err := dm.ParseDocument(uri)
	if err != nil {
		t.Fatalf("ParseDocument failed: %v", err)
	}
	var hits []string
	for _, e := range errs {
		if strings.Contains(e, "前方同一行") {
			hits = append(hits, e)
		}
	}
	if len(hits) == 0 {
		t.Fatalf("ParseDocument did not report the `if` position prefix error, got: %v", errs)
	}

	srv := NewServer()
	d := srv.parseErrorToDiagnostic(hits[0])
	if d.Range.Start.Line != 2 {
		t.Errorf("diagnostic line = %d, want 2 (the `if x > 0 #{...} {` line); msg=%q",
			d.Range.Start.Line, hits[0])
	}
	if d.Range.Start.Character != 13 {
		t.Errorf("diagnostic character = %d, want 13 (the annotation column); msg=%q",
			d.Range.Start.Character, hits[0])
	}
}
