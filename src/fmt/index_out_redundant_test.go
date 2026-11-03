package fmt

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// REGRESSION GUARD: a `#{index-out = ...}` annotation governing a place-match
// (`state[1]: { ... }`) whose subject is a container index read is redundant —
// the match's ok/nil arms already guarantee out-of-bounds safety for the subject,
// and (under the safe-gate) no arm-body index read depends on the annotation's
// default. `no fmt` must strip it. See docs decision 2026-11-02 (safe-gate).
//
// The annotation is only removed when the safe-gate holds; if an inner read
// (e.g. `state[5]`) depends on the default, it must be preserved (D7 three-way
// consistency: fmt / vet / codegen must agree).

func parseSugarForFmt(t *testing.T, src string) *parser.Program {
	t.Helper()
	p := parser.New(lexer.New(src))
	p.SkipLowering = true
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	return prog
}

// TestFormatRemovesRedundantPlaceMatchIndexOut: standalone annotation above a
// subject-only place-match is removed.
func TestFormatRemovesRedundantPlaceMatchIndexOut(t *testing.T) {
	input := "f = () {\n" +
		"    state [4]i64 = [1, 2, 3, 4]\n" +
		"    #{index-out=zero}\n" +
		"    state[10]: {\n" +
		"        ok(v) -> it = v\n" +
		"        -> _\n" +
		"    }\n" +
		"    _ = state[1]\n" +
		"}\n"
	program := parseSugarForFmt(t, input)
	out := FormatProgramWithOverflow(program, input, nil, nil)
	if strings.Contains(out, "index-out") {
		t.Errorf("redundant standalone #{index-out} on place-match subject must be removed:\n%s", out)
	}
}

// TestFormatRemovesRedundantTrailingPlaceMatchIndexOut: trailing annotation on a
// subject-only place-match is removed.
func TestFormatRemovesRedundantTrailingPlaceMatchIndexOut(t *testing.T) {
	input := "f = () {\n" +
		"    state [4]i64 = [1, 2, 3, 4]\n" +
		"    state[10]: {\n" +
		"        ok(v) -> it = v\n" +
		"        -> _\n" +
		"    } #{index-out=zero}\n" +
		"    _ = state[1]\n" +
		"}\n"
	program := parseSugarForFmt(t, input)
	out := FormatProgramWithOverflow(program, input, nil, nil)
	if strings.Contains(out, "index-out") {
		t.Errorf("redundant trailing #{index-out} on place-match subject must be removed:\n%s", out)
	}
}

// TestFormatKeepsIndexOutForInnerRead: when an arm-body read depends on the
// annotation's default (safe-gate fails), the annotation must be preserved.
func TestFormatKeepsIndexOutForInnerRead(t *testing.T) {
	input := "f = () {\n" +
		"    state [4]i64 = [1, 2, 3, 4]\n" +
		"    i i64 = 5\n" +
		"    #{index-out=zero}\n" +
		"    state[10]: {\n" +
		"        ok -> it = state[i]\n" +
		"        -> _\n" +
		"    }\n" +
		"    _ = state[1]\n" +
		"}\n"
	program := parseSugarForFmt(t, input)
	out := FormatProgramWithOverflow(program, input, nil, nil)
	if !strings.Contains(out, "index-out") {
		t.Errorf("non-redundant #{index-out} (inner read depends on it) must be kept:\n%s", out)
	}
}
