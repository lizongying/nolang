package lsp

import (
	"strings"
	"testing"
)

// REGRESSION GUARD: `v ?= expr` inside a function with **no** option-typed
// result param must be reported as a diagnostic by the LSP.
//
// This is easy to miss: the LSP's main parse sets parser.SkipUnwrapLowering=true
// (doc.AST is rendered back to source by format-on-save, and the lowered
// __unwrap_N blocks are not re-parseable). That skips lowerUnwrapAssign
// entirely — including its `?=` checks — so the editor stayed silent and the
// user only found out at `no build` time (which then loses the whole module).
//
// Fix: ParseDocument runs a diagnostics-only second parse with lowering enabled
// whenever the text contains `?=` (see collectUnwrapLoweringErrors).
func TestParseDocumentReportsUnwrapAssignError(t *testing.T) {
	dm := NewDocumentManager()
	uri := "file:///test/unwrap_diag.no"
	text := "file.append = (data str, n i64) (success bool) {\n" +
		"    success = false\n" +
		"    size ?= stat-size('/tmp/x')\n" +
		"    size: {\n" +
		"        ok -> success = true\n" +
		"        nil -> success = false\n" +
		"        err -> success = false\n" +
		"    }\n" +
		"}\n"

	if _, err := dm.OpenDocument(uri, text); err != nil {
		t.Fatalf("OpenDocument failed: %v", err)
	}
	if _, errs, err := dm.ParseDocument(uri); err != nil {
		t.Fatalf("ParseDocument failed: %v", err)
	} else if !containsUnwrapResultParamError(errs) {
		t.Fatalf("ParseDocument did not report the `?=` error, got: %v", errs)
	}

	// The diagnostic must land on the `?=` itself, not at (0,0).
	// p.Errors() prefixes messages with the filename when p.Filename is set
	// ("unwrap_diag.no:line 3, column 10: ..."), which used to break the
	// Sscanf in parseErrorToDiagnostic and pin every parse error to (0,0).
	srv := NewServer()
	for _, msg := range mustUnwrapResultParamErrors(t, dm, uri) {
		d := srv.parseErrorToDiagnostic(msg)
		if d.Range.Start.Line != 2 {
			t.Errorf("diagnostic line = %d, want 2 (the `size ?= ...` line); msg=%q", d.Range.Start.Line, msg)
		}
		if d.Range.Start.Character == 0 {
			t.Errorf("diagnostic character = 0, want the `?=` column; msg=%q", msg)
		}
	}
}

func containsUnwrapResultParamError(errs []string) bool {
	for _, e := range errs {
		if strings.Contains(e, "`?=` can only be used inside a function with an option-typed result param") {
			return true
		}
	}
	return false
}

func mustUnwrapResultParamErrors(t *testing.T, dm *DocumentManager, uri string) []string {
	t.Helper()
	_, errs, err := dm.ParseDocument(uri)
	if err != nil {
		t.Fatalf("ParseDocument failed: %v", err)
	}
	var out []string
	for _, e := range errs {
		if strings.Contains(e, "`?=` can only be used inside a function with an option-typed result param") {
			out = append(out, e)
		}
	}
	if len(out) == 0 {
		t.Fatal("no `?=` diagnostic found")
	}
	return out
}

// A legal `?=` (inside a function with an option-typed result param) must NOT
// produce the diagnostic — otherwise every std module using `?=` lights up.
func TestParseDocumentNoUnwrapErrorWithOptionResult(t *testing.T) {
	dm := NewDocumentManager()
	uri := "file:///test/unwrap_diag_ok.no"
	text := "read-size = (p str) (content ?i64) {\n" +
		"    size ?= stat-size(p)\n" +
		"    content = size\n" +
		"}\n"
	if _, err := dm.OpenDocument(uri, text); err != nil {
		t.Fatalf("OpenDocument failed: %v", err)
	}
	if _, errs, err := dm.ParseDocument(uri); err != nil {
		t.Fatalf("ParseDocument failed: %v", err)
	} else if containsUnwrapResultParamError(errs) {
		t.Fatalf("legal `?=` wrongly reported: %v", errs)
	}
}
