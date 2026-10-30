package build

// build/ty.go — the structural type pool for the assembly passes.
//
// The transpiler walks a program whose types arrive as spellings (varTypes,
// method receiver types, instantiated names). The passes below ask structural
// questions of those spellings — is the receiver a slice, an array, a map, an
// option? — and used to answer them with prefix scans (`HasPrefix(recv, "[]")`,
// `HasPrefix(recv, "[") && !HasPrefix(recv, "[]")`, `TrimPrefix(recv, "?")`).
// Those scans cannot tell a map from an array (`[str]i64` starts with "[" and is
// not "[]") and they silently disagree with the parser.
//
// A resolver-free pool is right here: the transpiler only needs the syntax, and
// an unresolved name stays Named with its spelling, which is what the callers
// compare against anyway.

import "github.com/lizongying/nolang/ty"

var tyPool = ty.NewPool(nil)

// typeOf is the structural parse of a type spelling. Never nil.
func typeOf(raw string) *ty.Type { return tyPool.Parse(raw) }

// unwrapType drops Option/View wrappers: `?&point` -> `point`.
func unwrapType(raw string) string { return typeOf(raw).Unwrap().Raw() }

// optionInner drops ONE option layer and nothing else: `?&T` -> `&T`.
func optionInner(raw string) string { return typeOf(raw).OptionInner().Raw() }

// isBuiltinContainer reports whether raw denotes a builtin container (slice,
// array or map) — the receivers whose methods are registered as vec./arr./
// hashmap- builtins rather than under a nolang type name.
func isBuiltinContainer(raw string) bool {
	switch typeOf(raw).Kind {
	case ty.Slice, ty.Array, ty.Map:
		return true
	}
	return false
}
