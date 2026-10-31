package main

import (
	"reflect"
	"strings"
	"testing"
)

// TestParseLangFlag pins the three spellings plus the two hazards: a dangling
// --lang (no value) must be an error, and a bare `--` must stop parsing so the
// flag is forwarded to the program under test.
func TestParseLangFlag(t *testing.T) {
	cases := []struct {
		name      string
		in        []string
		wantArgs  []string
		wantLang  string
		wantSet   bool
		wantError bool
	}{
		{
			name:     "double dash equals",
			in:       []string{"no", "vet", "--lang=zh-CN", "src"},
			wantArgs: []string{"no", "vet", "src"},
			wantLang: "zh-CN",
			wantSet:  true,
		},
		{
			name:     "single dash equals",
			in:       []string{"no", "build", "-lang=en", "main.no"},
			wantArgs: []string{"no", "build", "main.no"},
			wantLang: "en",
			wantSet:  true,
		},
		{
			name:     "space separated",
			in:       []string{"no", "vet", "--lang", "zh-CN", "src"},
			wantArgs: []string{"no", "vet", "src"},
			wantLang: "zh-CN",
			wantSet:  true,
		},
		{
			name:     "flag after the file argument",
			in:       []string{"no", "build", "main.no", "--lang=zh-CN"},
			wantArgs: []string{"no", "build", "main.no"},
			wantLang: "zh-CN",
			wantSet:  true,
		},
		{
			name:     "no flag",
			in:       []string{"no", "vet", "src"},
			wantArgs: []string{"no", "vet", "src"},
		},
		{
			name:      "dangling flag",
			in:        []string{"no", "vet", "--lang"},
			wantError: true,
		},
		{
			name:      "empty value",
			in:        []string{"no", "vet", "--lang="},
			wantError: true,
		},
		{
			name:     "double dash terminates parsing",
			in:       []string{"no", "run", "main.no", "--", "--lang=zh-CN"},
			wantArgs: []string{"no", "run", "main.no", "--", "--lang=zh-CN"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			args, lang, set, err := parseLangFlag(c.in)
			if c.wantError {
				if err == nil {
					t.Fatalf("want error, got nil (args=%v lang=%q)", args, lang)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(args, c.wantArgs) {
				t.Errorf("args = %v, want %v", args, c.wantArgs)
			}
			if lang != c.wantLang || set != c.wantSet {
				t.Errorf("lang = %q set = %v, want %q / %v", lang, set, c.wantLang, c.wantSet)
			}
		})
	}
}

// TestParseLangFlagErrorMentionsSpelling keeps the message actionable.
func TestParseLangFlagErrorMentionsSpelling(t *testing.T) {
	_, _, _, err := parseLangFlag([]string{"no", "--lang"})
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), "--lang") {
		t.Errorf("error %q should name the flag", err)
	}
}
