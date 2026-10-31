package i18n

import (
	"os"
	"sort"
	"strings"
)

// defaultEnv is the environment reader used when Options.Env is nil.
func defaultEnv(key string) string { return os.Getenv(key) }

// Resolve picks the language tag to activate.
//
// Precedence, highest first:
//
//	--lang=<tag>  >  NO_LANG  >  LC_ALL  >  LANG  >  "en"
//
// The default is therefore the SYSTEM language, not English: a user with
// LANG=zh_CN.UTF-8 gets Chinese without passing any flag. English is only the
// fallback for when nothing else resolves to a tag we have a catalog for.
//
// wasExplicit reports whether the tag came from --lang or NO_LANG (a deliberate
// request), as opposed to the ambient system locale. Only an explicit request
// that cannot be satisfied is worth a warning — otherwise every command on a
// host with LANG=de_DE would print one.
//
// raw is the untouched input, so the caller can echo it back in that warning.
func Resolve(env func(string) string, explicit string, explicitSet bool, available []string) (tag string, wasExplicit bool, raw string) {
	if env == nil {
		env = defaultEnv
	}

	wasExplicit = explicitSet
	raw = strings.TrimSpace(explicit)
	if !explicitSet || raw == "" {
		if v := strings.TrimSpace(env("NO_LANG")); v != "" {
			raw, wasExplicit = v, true
		}
	}
	if raw == "" {
		raw = strings.TrimSpace(env("LC_ALL"))
	}
	if raw == "" {
		raw = strings.TrimSpace(env("LANG"))
	}
	if raw == "" {
		return defaultLang, wasExplicit, ""
	}

	norm := Normalize(raw)
	if norm == "" {
		return defaultLang, wasExplicit, raw
	}

	for _, cand := range fallbackChain(norm) {
		if cand == defaultLang {
			return defaultLang, wasExplicit, raw
		}
		if got := canonical(cand, available); got != "" {
			return got, wasExplicit, raw
		}
	}
	return defaultLang, wasExplicit, raw
}

// Normalize turns a locale string into a comparable BCP-47-ish tag.
//
//	zh_CN.UTF-8  -> zh-cn
//	sr_RS@latin  -> sr-rs
//	zh-Hans      -> zh-cn
//	C / POSIX    -> ""   (means "no usable locale")
//
// The result is lower-cased on purpose: matching is case-insensitive, and
// canonical() restores the spelling the catalog actually uses.
func Normalize(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if i := strings.IndexAny(s, ".@"); i >= 0 {
		s = s[:i]
	}
	s = strings.ToLower(strings.ReplaceAll(s, "_", "-"))
	switch s {
	case "", "c", "posix":
		return ""
	}
	// Script subtags: only Simplified Chinese has a catalog, and a Hans request
	// is exactly a zh-CN request.
	if s == "zh-hans" || strings.HasPrefix(s, "zh-hans-") {
		return "zh-cn"
	}
	return s
}

// fallbackChain expands a normalised tag into the tags to try, most specific
// first, ending with the primary language subtag:
//
//	zh-cn -> [zh-cn, zh]
//	zh    -> [zh]
//	en-us -> [en-us, en]
func fallbackChain(norm string) []string {
	chain := []string{norm}
	if i := strings.IndexByte(norm, '-'); i > 0 {
		chain = append(chain, norm[:i])
	}
	return chain
}

// canonical maps norm onto the spelling used by one of the available tags,
// comparing case-insensitively. It returns "" when no tag matches.
func canonical(norm string, available []string) string {
	for _, a := range available {
		if strings.EqualFold(a, norm) {
			return a
		}
	}
	return ""
}

// containsTag reports whether tag is present in available, case-insensitively.
func containsTag(available []string, tag string) bool {
	return canonical(strings.ToLower(strings.ReplaceAll(tag, "_", "-")), available) != ""
}

// isPlainEnglish reports whether raw denotes English, which always resolves
// even though it has no catalog file (the inline defaults are the catalog).
func isPlainEnglish(raw string) bool {
	norm := Normalize(raw)
	return norm == defaultLang || strings.HasPrefix(norm, defaultLang+"-")
}

// joinTags renders available as a comma-separated list for warnings.
func joinTags(available []string) string {
	if len(available) == 0 {
		return "none"
	}
	return strings.Join(available, ", ")
}

// distinctTags collapses alias tags onto their canonical form so a warning lists
// "zh-CN" once instead of "zh-CN, zh, zh-hans, zh-hant, ...".
func distinctTags(available []string, canon func(string) string) []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range available {
		c := t
		if canon != nil {
			if mapped := canon(t); mapped != "" {
				c = mapped
			}
		}
		if seen[c] {
			continue
		}
		seen[c] = true
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}
