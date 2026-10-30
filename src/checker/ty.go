package checker

// checker/ty.go — the checker's view of the type pool.
//
// The checker owns two registries the ty package cannot know about: the std /
// per-program concrete aliases (`fd = i64`) and the set of names that denote a
// real type (builtin scalar, registered struct/enum, alias). They are exposed
// here as a ty.Resolver, so a query like "what is the underlying scalar of this
// spelling?" has one answer for the whole package instead of the hand-written
// alias recursion each validator used to carry.

import (
	"github.com/lizongying/nolang/parser"
	"github.com/lizongying/nolang/ty"
)

// checkerTypeResolver teaches a ty.Pool the checker's registries.
//
// Readers note: both methods read package-level maps that a validation run
// patches (validationConcreteTypeAliases / validationStructFields). They are
// read-only here, exactly like every other consumer in this package, and the
// maps are only replaced between runs — never mutated while a run is in flight.
type checkerTypeResolver struct{}

// Alias reports the target spelling of a concrete type alias. Module-qualified
// spellings are retried with the prefix stripped, because std aliases are
// registered under their bare name (`fs.fd` -> `fd` -> `i64`).
func (checkerTypeResolver) Alias(name string) (string, bool) {
	if name == "" || validationConcreteTypeAliases == nil {
		return "", false
	}
	if t, ok := validationConcreteTypeAliases[name]; ok && t != "" {
		return t, true
	}
	if short := stripModulePrefix(name); short != name {
		if t, ok := validationConcreteTypeAliases[short]; ok && t != "" {
			return t, true
		}
	}
	return "", false
}

// IsTypeName reports whether name can name a type on its own. It decides the
// `[fd]i64` (map) versus `[32]byte` (array) split, so it must know aliases and
// registered struct/enum names, not just the builtin vocabulary.
func (r checkerTypeResolver) IsTypeName(name string) bool {
	if name == "" {
		return false
	}
	if parser.IsBuiltinTypeName(name) {
		return true
	}
	if _, ok := r.Alias(name); ok {
		return true
	}
	if validationStructFields != nil {
		if _, ok := validationStructFields[name]; ok {
			return true
		}
		if short := stripModulePrefix(name); short != name {
			if _, ok := validationStructFields[short]; ok {
				return true
			}
		}
	}
	return false
}

// checkerType parses a spelling with the checker's resolver installed.
//
// The pool is created per call on purpose: the alias registry is republished by
// every validation run, and an interned Type carries the resolution it was built
// with, so a shared pool would answer later runs with earlier runs' aliases
// (and `no vet` validates files concurrently). Queries here are one-shot
// classification, so the allocation is the cheaper mistake.
func checkerType(raw string) *ty.Type {
	return ty.NewPool(checkerTypeResolver{}).Parse(raw)
}

// checkerCanonicalRaw follows the alias chain to the first non-alias spelling.
// It returns raw unchanged when nothing resolves, so callers can use it as a
// drop-in for "the same spelling, but with aliases flattened".
func checkerCanonicalRaw(raw string) string {
	return checkerType(raw).Canonical().Raw()
}
