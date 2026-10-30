package lsp

// lsp/ty.go — structural type queries for the editor features.
//
// Hover and the document walker present types as spellings, but the questions
// they ask are structural: is this an option, and what does it wrap? Answering
// them by scanning the leading character was the same predicate as
// ty.Kind == Option, but silently wrong the moment the type had already been
// unwrapped somewhere upstream (double marker, or a marker that is not one).

import "github.com/lizongying/nolang/ty"

var tyPool = ty.NewPool(nil)

// optionInner drops one option layer: `?i64` -> `i64`, `i64` -> `i64`.
func optionInner(raw string) string { return tyPool.Parse(raw).OptionInner().Raw() }

// unwrapType drops option and view wrappers: `?&point` -> `point`.
func unwrapType(raw string) string { return tyPool.Parse(raw).Unwrap().Raw() }

// isOptionType reports whether the spelling is an option (`?T`).
func isOptionType(raw string) bool { return tyPool.Parse(raw).Kind == ty.Option }
