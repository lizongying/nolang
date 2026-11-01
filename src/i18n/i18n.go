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
	"strings"
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
	// DocLoad returns the DOC catalog for a resolved tag — the translations of
	// `; ...` source comments marked with a trailing Markdown link. It is a
	// separate loader from Load because the two tables obey different rules:
	// a message's inline default is English (so "en" needs no table), while a
	// doc comment is written in whatever language its author used (so "en" is
	// a translation target and DOES need one). A missing doc catalog is not an
	// error: nil, nil means "no doc translations for this tag".
	DocLoad func(tag string) (map[string]string, error)
	// AliasLoad returns the ALIAS catalog for a resolved tag — the localized
	// display names (别名) of std functions/methods, keyed by the same doc
	// anchors Doc uses. Like DocLoad, a missing table is not an error:
	// nil, nil means "no aliases for this tag" and Alias falls back to the
	// identifier itself.
	AliasLoad func(tag string) (map[string]string, error)
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
	// docMsgs is the catalog Doc() reads. It is a SEPARATE map from msgs
	// because the two have opposite rules for English: msgs must stay empty
	// under "en" (T's no-lookup fast path), while docMsgs must be populated
	// under "en" (English is a translation target for source comments, which
	// are authored in whatever language their writer used).
	docMsgs map[string]string
	// aliasMsgs is the catalog Alias() reads: anchor -> localized display
	// name. Same English rule as docMsgs — "en" has a table too.
	aliasMsgs map[string]string

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
	docMsgs = nil
	aliasMsgs = nil
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

// A doc link needs no ID derivation: the author writes the key in the source as
// the anchor of a Markdown link (`[get](#args.get)`), so producer and consumer
// both read it instead of computing it. That is deliberate — a content-addressed
// key would require the .no scanner and the LSP to agree byte for byte on where a
// doc block starts and ends, and any disagreement would silently orphan the
// translation.

// SetDocCatalog installs the table Doc reads. It is independent of SetCatalog:
// it is installed under every language, including "en".
//
// A nil or empty table means "no doc translations", so Doc falls back to the
// comment text found in the source.
func SetDocCatalog(table map[string]string) {
	if len(table) == 0 {
		msgMu.Lock()
		docMsgs = nil
		msgMu.Unlock()
		return
	}
	cp := make(map[string]string, len(table))
	for k, v := range table {
		cp[k] = v
	}
	msgMu.Lock()
	docMsgs = cp
	msgMu.Unlock()
}

// Doc resolves a documentation string — the body of a source comment that ends
// with a Markdown link anchor — and returns it verbatim. key is the anchor
// (`args.get` for `[get](#args.get)`).
//
// It differs from T in exactly two ways, both deliberate:
//
//  1. It consults the catalog under "en" too. T's no-lookup fast path is sound
//     only because the inline default of a *message* is written in English; a
//     doc comment is written in whatever language its author used (the shipped
//     std sources are Chinese), so for docs English is a translation TARGET.
//  2. It never formats. Doc text routinely contains a literal `%` ("50% of the
//     buffer") and there are no arguments to interpolate; running Sprintf would
//     turn that into `%!(NOVERB)`.
//
// A missing translation falls back to def, so a hover is always readable and
// never empty, and an untranslated comment degrades to the source text instead
// of disappearing.
func Doc(key, def string) string {
	if key == "" {
		return def
	}
	msgMu.RLock()
	t := docMsgs[key]
	msgMu.RUnlock()
	if t != "" {
		return t
	}
	recordMissing(key)
	return def
}

// Errorf translates id and returns it as an error carrying that ID. It is the
// only helper that may be given a %w verb, because it delegates to fmt.Errorf on
// the resolved format instead of Sprintf.
//
// The returned error is an *Error, so Error() renders "[id] message" and the ID
// survives %w wrapping (see Error).
func Errorf(id, def string, args ...any) error {
	format := lookup(id)
	if format == "" {
		recordMissing(id)
		format = def
	}
	return NewError(id, newError(format, args))
}

// SetAliasCatalog installs the table Alias reads. Like SetDocCatalog it is
// installed under every language including "en"; a nil or empty table means
// "no aliases", so Alias falls back to the identifier.
func SetAliasCatalog(table map[string]string) {
	if len(table) == 0 {
		msgMu.Lock()
		aliasMsgs = nil
		msgMu.Unlock()
		return
	}
	cp := make(map[string]string, len(table))
	for k, v := range table {
		cp[k] = v
	}
	msgMu.Lock()
	aliasMsgs = cp
	msgMu.Unlock()
}

// Alias returns the localized display name for a std function/method keyed by
// its doc anchor (`args.get` -> 获取参数 under zh-CN).
//
// The fallback when the anchor has no translation is the anchor's final dotted
// segment — the identifier itself (`get`) — which is always readable and is
// exactly what the source comment's link label already shows. Missing keys are
// not recorded in the fallback log: unlike Doc, a missing alias has no
// author-language text to be "untranslated" from; the identifier IS English.
func Alias(anchor string) string {
	if anchor == "" {
		return ""
	}
	msgMu.RLock()
	t := aliasMsgs[anchor]
	msgMu.RUnlock()
	if t != "" {
		return t
	}
	return IdentifierOf(anchor)
}

// IdentifierOf extracts the final dotted segment of a doc anchor — the raw
// function/method identifier the author wrote in the link label
// (`arr.arr-stack-pop` -> `arr-stack-pop`). Alias uses it as the fallback
// display name; callers use it to tell a real translation apart from the
// fallback.
func IdentifierOf(anchor string) string {
	if i := strings.LastIndexByte(anchor, '.'); i >= 0 {
		return anchor[i+1:]
	}
	return anchor
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

	err := installMessages(o, tag)
	// Docs come second on purpose: every message installer calls SetLang, which
	// clears both tables, so installing docs first would wipe them immediately.
	installDocs(o, tag)
	installAliases(o, tag)
	return err
}

// installMessages resolves the message catalog. English is the "no catalog"
// language by definition, so it never loads one.
func installMessages(o Options, tag string) error {
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

// installDocs resolves the doc catalog. Unlike messages this runs for every
// language including English, and a failure is never fatal: a doc translation is
// a presentation nicety, so a malformed doc file degrades to "show the source
// text" instead of stopping the compiler.
func installDocs(o Options, tag string) {
	if o.DocLoad == nil {
		return
	}
	table, err := o.DocLoad(tag)
	if err != nil {
		if o.Warn != nil {
			o.Warn(fmt.Sprintf("doc translations for %q unavailable, comments will show source text: %v", tag, err))
		}
		return
	}
	SetDocCatalog(table)
}

// installAliases resolves the alias catalog. Same non-fatal rule as
// installDocs: an alias is a presentation nicety, so a malformed alias file
// degrades to showing the identifier instead of stopping the toolchain.
func installAliases(o Options, tag string) {
	if o.AliasLoad == nil {
		return
	}
	table, err := o.AliasLoad(tag)
	if err != nil {
		if o.Warn != nil {
			o.Warn(fmt.Sprintf("alias names for %q unavailable, identifiers will be shown: %v", tag, err))
		}
		return
	}
	SetAliasCatalog(table)
}

// WithLang runs fn with tag active and restores the previous language and
// catalog afterwards. Tests must use it instead of bare SetLang so that a
// forgotten restore cannot leak into a sibling test.
func WithLang(tag string, fn func()) {
	prevLang := Lang()
	msgMu.RLock()
	prevMsgs := msgs
	prevDocs := docMsgs
	prevAliases := aliasMsgs
	msgMu.RUnlock()
	defer func() {
		langVal.Store(prevLang)
		msgMu.Lock()
		msgs = prevMsgs
		docMsgs = prevDocs
		aliasMsgs = prevAliases
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
