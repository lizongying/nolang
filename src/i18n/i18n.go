// Package i18n provides message translation for the Nolang toolchain.
//
// Every user-facing message carries an 8-character base36 ID (see the message
// registry convention in CONTRIBUTING.md). The English text is written inline at
// the call site and is ALSO the fallback, so a missing or stale translation can
// never produce an empty string, a bare ID, or a failed build:
//
//	Message: i18n.T("j7dzteja", "function '%s' expects %d argument(s), got %d", name, want, got)
//
// Two properties are load-bearing and must not be broken:
//
//  1. With lang == "en" T is byte-identical to fmt.Sprintf(def, args...). It does
//     not even consult the catalog. This is what makes "--lang=en is unchanged"
//     true by construction rather than by testing, and it is why the migration
//     of ~1000 call sites cannot change English output.
//  2. With zero args neither T nor Errorf calls Sprintf. fmt.Sprintf("100%")
//     renders "100%!(NOVERB)", so a literal percent sign in a help line or a
//     message would be corrupted by a needless format pass.
//
// T forbids %w (Sprintf renders it as %!w(...)); Errorf is the only entry point
// that may see one, because it calls fmt.Errorf on the resolved format.
package i18n

import (
	"errors"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
)

// Options configures Init. Zero values are usable: Env defaults to os.Getenv and
// Warn defaults to writing nothing.
type Options struct {
	// Lang is the explicitly requested tag (from --lang or NO_LANG). Empty means
	// "not requested" unless LangSet is true.
	Lang    string
	LangSet bool
	// Available lists the tags that actually have a catalog, in preference order
	// (project-local first, then embedded). The resolver uses it to canonicalise
	// case and to decide whether an explicit request is satisfiable.
	Available []string
	// Load returns the catalog for a resolved tag. A nil Load means "English
	// only": the resolver still reports the tag, but no catalog is installed.
	Load func(tag string) (map[string]string, error)
	// Canonical maps an alias onto the tag whose catalog file actually exists
	// (e.g. "zh" -> "zh-CN"). Nil means identity. It is applied after Resolve
	// and before Load, so the reported language is always the file's own tag.
	Canonical func(tag string) string
	// Env reads environment variables. Nil means os.Getenv.
	Env func(string) string
	// Warn receives non-fatal notices (unknown explicit tag, rejected entries).
	// Nil means discard.
	Warn func(msg string)
}

// defaultLang is the tag used when nothing else resolves, and the tag whose
// behaviour is defined to be "no lookup at all".
const defaultLang = "en"

var (
	langVal atomic.Value // string

	msgMu sync.RWMutex
	msgs  map[string]string // nil means "no catalog": every lookup falls back

	missMu sync.Mutex
	missed map[string]struct{}
)

func init() {
	langVal.Store(defaultLang)
}

// Lang reports the resolved language tag (never empty; "en" when unset).
func Lang() string {
	if v, ok := langVal.Load().(string); ok && v != "" {
		return v
	}
	return defaultLang
}

// SetLang installs tag as the active language and drops any installed catalog.
// Callers that have a catalog must follow with SetCatalog. It is intended for
// Init and for tests; the CLI sets the language exactly once, before any worker
// goroutine starts, so reads stay lock-free.
func SetLang(tag string) {
	if tag == "" {
		tag = defaultLang
	}
	langVal.Store(tag)
	msgMu.Lock()
	msgs = nil
	msgMu.Unlock()
}

// SetCatalog installs the translation table for tag. A nil or empty table is
// equivalent to "no translations", which makes every lookup fall back to the
// inline English default.
func SetCatalog(tag string, table map[string]string) {
	SetLang(tag)
	if len(table) == 0 {
		return
	}
	cp := make(map[string]string, len(table))
	for k, v := range table {
		cp[k] = v
	}
	msgMu.Lock()
	msgs = cp
	msgMu.Unlock()
}

// T translates id and formats the result with args.
//
// The English default def is used verbatim when the active language is "en",
// when no catalog is installed, or when id is absent from the catalog. args are
// interpolated with the same verbs as def, so a translation must keep the verb
// sequence (or use explicit indexes like %[2]s) — the catalog gate enforces it.
func T(id, def string, args ...any) string {
	if format := lookup(id); format != "" {
		return sprintf(format, args)
	}
	recordMissing(id)
	return sprintf(def, args)
}

// P is T for text that takes no arguments (help lines, headings, summaries).
// It exists to make the intent obvious at the call site and to keep the
// generator's extraction uniform.
func P(id, def string) string {
	return T(id, def)
}

// Translate is T for FORWARDING helpers, where the ID is a runtime value rather
// than a literal at the call site (e.g. the parser's emit(), which every
// saveErrorf funnels through).
//
// It is deliberately a different name from T: the catalog generator requires a
// literal ID and treats a non-literal as an error, because a computed message
// cannot be translated. Giving the forwarder its own name lets the real call
// sites — the ones carrying the literals — be the only things scanned.
func Translate(id, def string, args ...any) string {
	return T(id, def, args...)
}

// Errorf translates id and returns it as an error. It is the only helper that
// may be given a %w verb, because it delegates to fmt.Errorf on the resolved
// format instead of Sprintf.
func Errorf(id, def string, args ...any) error {
	if format := lookup(id); format != "" {
		return newError(format, args)
	}
	recordMissing(id)
	return newError(def, args)
}

// Missing reports the IDs that fell back to English, sorted. It is diagnostic
// only (surfaced under -v); a missing translation never fails a build.
func Missing() []string {
	missMu.Lock()
	defer missMu.Unlock()
	out := make([]string, 0, len(missed))
	for id := range missed {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// ResetMissing clears the fallback log. Tests use it to assert per-case behaviour.
func ResetMissing() {
	missMu.Lock()
	missed = nil
	missMu.Unlock()
}

// Init resolves the language and installs its catalog. It is safe to call once
// per process; the CLI does so in main before any subcommand runs.
func Init(o Options) error {
	env := o.Env
	if env == nil {
		env = defaultEnv
	}
	tag, explicit, raw := Resolve(env, o.Lang, o.LangSet, o.Available)

	if explicit && raw != "" && tag == defaultLang && !containsTag(o.Available, raw) && !isPlainEnglish(raw) {
		if o.Warn != nil {
			o.Warn(fmt.Sprintf("unknown language %q, using %s (available: %s)",
				raw, defaultLang, joinTags(distinctTags(o.Available, o.Canonical))))
		}
	}

	if o.Canonical != nil {
		if c := o.Canonical(tag); c != "" {
			tag = c
		}
	}

	if tag == defaultLang || o.Load == nil {
		SetLang(tag)
		return nil
	}
	table, err := o.Load(tag)
	if err != nil {
		return err
	}
	SetCatalog(tag, table)
	return nil
}

// WithLang runs fn with tag active and restores the previous language and
// catalog afterwards. Tests must use it instead of bare SetLang so that a
// forgotten restore cannot leak into a sibling test.
func WithLang(tag string, fn func()) {
	prevLang := Lang()
	msgMu.RLock()
	prevMsgs := msgs
	msgMu.RUnlock()
	defer func() {
		langVal.Store(prevLang)
		msgMu.Lock()
		msgs = prevMsgs
		msgMu.Unlock()
	}()
	SetLang(tag)
	fn()
}

// lookup returns the translation for id, or "" when the active language has no
// usable entry. The "en" fast path is what guarantees English output never
// depends on the catalog.
func lookup(id string) string {
	if id == "" || Lang() == defaultLang {
		return ""
	}
	msgMu.RLock()
	defer msgMu.RUnlock()
	if msgs == nil {
		return ""
	}
	return msgs[id]
}

func sprintf(format string, args []any) string {
	if len(args) == 0 {
		return format
	}
	return fmt.Sprintf(format, args...)
}

func newError(format string, args []any) error {
	if len(args) == 0 {
		return errors.New(format)
	}
	return fmt.Errorf(format, args...)
}

func recordMissing(id string) {
	if id == "" || Lang() == defaultLang {
		return
	}
	missMu.Lock()
	if missed == nil {
		missed = make(map[string]struct{})
	}
	missed[id] = struct{}{}
	missMu.Unlock()
}
