package parser

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
)

func reservedWordDiagnostics(input string) []Diagnostic {
	p := New(lexer.New(input))
	_ = p.ParseProgram()
	var out []Diagnostic
	for _, d := range p.Diagnostics() {
		if d.Code == reservedWordCode {
			out = append(out, d)
		}
	}
	return out
}

func TestReservedWordAsIdentifierWarnsAtKeyword(t *testing.T) {
	tests := []struct {
		name string
		// wantLine / wantCol point at the keyword itself, not at a later token.
		input    string
		wantLine int
		wantCol  int
	}{
		{"variable declaration", "co = 1\n", 1, 1},
		{"unwrap assign declaration", "f = () (result ?i64) {\n    result = nil\n    co ?= g()\n}\n", 3, 5},
		{"parameter name", "f = (chan i64) {}\n", 1, 6},
		{"function name", "co = () {}\n", 1, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := reservedWordDiagnostics(tt.input)
			if len(got) == 0 {
				t.Fatalf("expected a %s diagnostic, got none", reservedWordCode)
			}
			// One keyword occurrence should yield exactly one diagnostic —
			// layered detection points must not double-report it.
			if len(got) != 1 {
				t.Errorf("diagnostic count = %d, want 1: %v", len(got), got)
			}
			d := got[0]
			if d.Severity != SeverityWarning {
				t.Errorf("severity = %s, want warning", d.Severity)
			}
			if d.ID != reservedWordID {
				t.Errorf("ID = %q, want %q", d.ID, reservedWordID)
			}
			if d.Pos.Line != tt.wantLine || d.Pos.Column != tt.wantCol {
				t.Errorf("position = %d:%d, want %d:%d", d.Pos.Line, d.Pos.Column, tt.wantLine, tt.wantCol)
			}
		})
	}
}

func TestReservedWordWarningDoesNotFireOnLegitimateKeywords(t *testing.T) {
	inputs := []struct {
		name  string
		input string
	}{
		{"match as identifier", "match = true\n"},
		{"co spawn", "co worker()\n"},
		{"awy group", "awy { co worker() }\n"},
		{"return statement", "f = () {\n    return\n}\n"},
		{"in as parameter name", "f = (in i64) {}\n"},
	}
	for _, tt := range inputs {
		t.Run(tt.name, func(t *testing.T) {
			if got := reservedWordDiagnostics(tt.input); len(got) != 0 {
				t.Errorf("unexpected %s diagnostics: %v", reservedWordCode, got)
			}
		})
	}
}
