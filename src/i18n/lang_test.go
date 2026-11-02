package i18n

import (
	"strings"
	"testing"
)

func fakeEnv(kv map[string]string) func(string) string {
	return func(k string) string { return kv[k] }
}

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"zh_CN.UTF-8": "zh-cn",
		"zh-CN":       "zh-cn",
		"ZH_cn":       "zh-cn",
		"sr_RS@latin": "sr-rs",
		"en_US.UTF-8": "en-us",
		"zh-Hans":     "zh-cn",
		"zh-Hans-CN":  "zh-cn",
		"zh-Hant":     "zh-hant",
		"  en  ":      "en",
		"C":           "",
		"POSIX":       "",
		"C.UTF-8":     "",
		"":            "",
	}
	for in, want := range cases {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResolvePrecedence(t *testing.T) {
	available := []string{"zh-CN"}

	cases := []struct {
		name        string
		explicit    string
		explicitSet bool
		env         map[string]string
		wantTag     string
		wantExpl    bool
	}{
		{
			name:    "system locale wins over nothing",
			env:     map[string]string{"LANG": "zh_CN.UTF-8"},
			wantTag: "zh-CN",
		},
		{
			name:    "LC_ALL beats LANG",
			env:     map[string]string{"LC_ALL": "zh_CN.UTF-8", "LANG": "en_US.UTF-8"},
			wantTag: "zh-CN",
		},
		{
			name:     "NO_LANG beats the ambient locale",
			env:      map[string]string{"NO_LANG": "zh_CN.UTF-8", "LANG": "en_US.UTF-8"},
			wantTag:  "zh-CN",
			wantExpl: true,
		},
		{
			name:        "explicit flag beats NO_LANG",
			explicit:    "en",
			explicitSet: true,
			env:         map[string]string{"NO_LANG": "zh_CN.UTF-8"},
			wantTag:     "en",
			wantExpl:    true,
		},
		{
			name:    "unset environment falls back to English",
			env:     map[string]string{},
			wantTag: "en",
		},
		{
			name:    "C locale is not a language",
			env:     map[string]string{"LANG": "C"},
			wantTag: "en",
		},
		{
			name:     "unknown ambient locale falls back silently",
			env:      map[string]string{"LANG": "de_DE.UTF-8"},
			wantTag:  "en",
			wantExpl: false,
		},
		{
			name:        "unknown explicit tag still reports itself",
			explicit:    "klingon",
			explicitSet: true,
			env:         map[string]string{},
			wantTag:     "en",
			wantExpl:    true,
		},
		{
			name:    "english needs no catalog",
			env:     map[string]string{"LANG": "en_US.UTF-8"},
			wantTag: "en",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tag, expl, _ := Resolve(fakeEnv(c.env), c.explicit, c.explicitSet, available)
			if tag != c.wantTag {
				t.Errorf("tag = %q, want %q", tag, c.wantTag)
			}
			if expl != c.wantExpl {
				t.Errorf("wasExplicit = %v, want %v", expl, c.wantExpl)
			}
		})
	}
}

// TestResolvePrimarySubtagFallback covers the zh-CN -> zh -> en chain: a bare
// "zh" request must find the zh-CN catalog when that is all we ship.
func TestResolvePrimarySubtagFallback(t *testing.T) {
	tag, _, _ := Resolve(fakeEnv(map[string]string{"LANG": "zh_TW.UTF-8"}), "", false, []string{"zh-CN", "zh"})
	if tag != "zh" {
		t.Fatalf("tag = %q, want zh (primary subtag match)", tag)
	}

	tag, _, _ = Resolve(fakeEnv(map[string]string{"LANG": "zh_TW.UTF-8"}), "", false, []string{"zh-CN"})
	if tag != "en" {
		t.Fatalf("tag = %q, want en when only zh-CN exists and no zh alias is registered", tag)
	}
}

// TestInitWarnsOnlyForExplicitUnknown pins the "no spam" rule: an explicit
// request that cannot be satisfied warns, an ambient one does not.
func TestInitWarnsOnlyForExplicitUnknown(t *testing.T) {
	reset(t)
	defer reset(t)

	var warnings []string
	warn := func(m string) { warnings = append(warnings, m) }

	err := Init(Options{
		Lang: "klingon", LangSet: true,
		Available: []string{"zh-CN"},
		Env:       fakeEnv(nil),
		Warn:      warn,
	})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want exactly 1", warnings)
	}

	warnings = nil
	err = Init(Options{
		Available: []string{"zh-CN"},
		Env:       fakeEnv(map[string]string{"LANG": "de_DE.UTF-8"}),
		Warn:      warn,
	})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("ambient unknown locale must be silent, got %v", warnings)
	}
}

// TestInitLoadsCatalog exercises the Load hook end to end.
func TestInitLoadsCatalog(t *testing.T) {
	reset(t)
	defer reset(t)

	err := Init(Options{
		Lang: "zh_CN.UTF-8", LangSet: true,
		Available: []string{"zh-CN"},
		Env:       fakeEnv(nil),
		Load: func(tag string) (map[string]string, error) {
			if tag != "zh-CN" {
				t.Fatalf("Load(%q), want zh-CN", tag)
			}
			return map[string]string{"aaaaaaaa": "中文"}, nil
		},
	})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if got := T("aaaaaaaa", "english"); got != "中文" {
		t.Fatalf("T = %q, want 中文", got)
	}
}

// TestProjectLangPrecedence pins where a project's own default — `compiler.lang`
// in package.noml — sits in the chain:
//
//	--lang > NO_LANG > ProjectLang > LC_ALL > LANG > en
//
// It beats the ambient system locale, which is the whole point of pinning it in
// the manifest (a Chinese project should read Chinese on a machine whose LANG is
// de_DE). It loses to --lang and to NO_LANG, because those are deliberate
// per-invocation overrides: without that, a user could not escape the project's
// choice for one command.
func TestProjectLangPrecedence(t *testing.T) {
	available := []string{"en", "zh-cn", "zh-tw", "ja"}

	cases := []struct {
		name        string
		explicit    string
		explicitSet bool
		project     string
		env         map[string]string
		wantTag     string
	}{
		{
			name:    "project default beats the ambient locale",
			project: "zh_TW.UTF-8", // written the way a human would
			env:     map[string]string{"LANG": "de_DE.UTF-8"},
			wantTag: "zh-tw",
		},
		{
			name:        "explicit flag beats the project default",
			explicit:    "en",
			explicitSet: true,
			project:     "zh-tw",
			env:         map[string]string{"LANG": "de_DE.UTF-8"},
			wantTag:     "en",
		},
		{
			name:    "NO_LANG beats the project default",
			project: "zh-tw",
			env:     map[string]string{"NO_LANG": "ja", "LANG": "de_DE.UTF-8"},
			wantTag: "ja",
		},
		{
			name:    "no project default falls back to the ambient locale",
			env:     map[string]string{"LANG": "ja_JP.UTF-8"},
			wantTag: "ja",
		},
		{
			name:    "blank project default is ignored",
			project: "   ",
			env:     map[string]string{"LANG": "ja_JP.UTF-8"},
			wantTag: "ja",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			reset(t)
			defer reset(t)
			err := Init(Options{
				Lang:        c.explicit,
				LangSet:     c.explicitSet,
				ProjectLang: c.project,
				Available:   available,
				Env:         fakeEnv(c.env),
			})
			if err != nil {
				t.Fatalf("Init: %v", err)
			}
			if got := Lang(); got != c.wantTag {
				t.Errorf("Lang() = %q, want %q", got, c.wantTag)
			}
		})
	}
}

// TestProjectLangWarnsWhenUnavailable keeps the project default on the
// "explicit" side of the resolver: a manifest that pins a tag we cannot serve
// must say so once, exactly like --lang does. Staying silent would make a typo
// in package.noml look like "the project simply has no translation".
func TestProjectLangWarnsWhenUnavailable(t *testing.T) {
	reset(t)
	defer reset(t)

	var warnings []string
	err := Init(Options{
		ProjectLang: "zh-tw",
		Available:   []string{"en", "ja"}, // zh-tw not shipped here
		Env:         fakeEnv(nil),
		Warn:        func(m string) { warnings = append(warnings, m) },
	})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want exactly 1", warnings)
	}
	if !strings.Contains(warnings[0], "zh-tw") {
		t.Errorf("warning %q should name the requested tag", warnings[0])
	}
}
