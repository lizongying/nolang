package checker

import (
	"testing"
)

// REGRESSION GUARD: a `#{index-out = ...}` annotation governing a place-match
// (`state[1]: { ... }`) whose subject is a container index read is reported as a
// redundant HINT (idxoutrd) only under the safe-gate: no arm-body container
// index read depends on the annotation's default. If an inner read (e.g.
// `state[5]`) needs the default, the annotation is kept and no idxoutrd is
// emitted (D7 three-way consistency: fmt / vet / codegen must agree).

// parseProgSurface uses the sugar AST (SkipLowering), matching the `no vet` path.

func TestRedundantIndexOutHintEmitted(t *testing.T) {
	src := `f = () {
    state [4]i64 = [1, 2, 3, 4]
    #{index-out=zero}
    state[10]: {
        ok(v) -> it = v
        -> _
    }
    _ = state[1]
}`
	prog := parseProgSurface(t, src)
	results := RunAllLints(prog, LintOptions{SourcePath: "src/app.no"})
	found := false
	for _, r := range results {
		if r.TraceID == "idxoutrd" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected idxoutrd HINT for redundant place-match index-out")
	}
}

func TestNoRedundantIndexOutHintForInnerRead(t *testing.T) {
	src := `f = () {
    state [4]i64 = [1, 2, 3, 4]
    i i64 = 5
    #{index-out=zero}
    state[10]: {
        ok -> it = state[i]
        -> _
    }
    _ = state[1]
}`
	prog := parseProgSurface(t, src)
	results := RunAllLints(prog, LintOptions{SourcePath: "src/app.no"})
	for _, r := range results {
		if r.TraceID == "idxoutrd" {
			t.Fatalf("idxoutrd must NOT fire when an inner read depends on the default")
		}
	}
}

func TestNoRedundantIndexOutHintForNonPlaceMatch(t *testing.T) {
	// A standalone #{index-out} on a plain container read (not a place-match
	// subject) must NOT be flagged redundant — it carries DEF-match consumption.
	src := `f = () {
    a [4]i64 = [1, 2, 3, 4]
    #{index-out=zero}
    x = a[2]
}`
	prog := parseProgSurface(t, src)
	results := RunAllLints(prog, LintOptions{SourcePath: "src/app.no"})
	for _, r := range results {
		if r.TraceID == "idxoutrd" {
			t.Fatalf("idxoutrd must NOT fire for a non-place-match index read")
		}
	}
}
