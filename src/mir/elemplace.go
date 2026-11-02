package mir

import "github.com/lizongying/nolang/hir"

// Container-element places: `x <- a` binds x to a[__i], not to a copy.
//
// A container iteration (`x <- a` over an array / vec / slice) reads its
// element ONCE per iteration into a private slot, so every write through the
// loop variable used to land in that dead slot and vanish:
//
//	a []?i64 = [ok(1), ok(2)]
//	x <- a: { x: { ok -> it = 9  -> {} } }     ; a unchanged (silent no-op)
//	i <- [0..2): { a[i]: { ok -> it = 9  -> {} } }   ; a = [ok(9), ok(9)]
//
// The subscript form works because the parser rewrites `it = v` into a store
// through the element (`a[i] = v`, the D9 place-match). The loop form has no
// index expression to rewrite into — the index lives only in MIR — so the
// aliasing is expressed here instead: the loop variable is bound to an
// elemPlace (container + index), a READ of it re-reads the element, and a
// WRITE to it stores into the element.
//
// The same table carries a match arm's identity binding. For an element that
// IS the arm's payload (the parser emits `let it = x` — see the HIR dump of
// `x: { ok -> ... }`), `it` aliases the same place, so `it = v` writes through
// exactly like `a[i]: { ok -> it = v }`. Without that alias `it` would be a
// copy and the write would be lost again — the very bug this fixes.
//
// Deliberately NOT a place:
//   - `c <- s` over a str: the element is a decoded code point, not storage
//     (see lowerStrRangeFor), so a write must stay local.
//   - a loop variable that is the enclosing function's result out-param: that
//     binding drives the out-param slot (see resultOutParamFor) and the
//     write-back to the caller is the whole point of the loop.
type elemPlace struct {
	arr   ValueID // container value the element belongs to
	idx   ValueID // index value (the loop's induction variable)
	elemT TypeID  // element type, as the container read produces it
	name  string  // container's source name ("" when it has none)
}

// placeOf reports whether name currently aliases a container element.
func (l *lowerer) placeOf(name string) (elemPlace, bool) {
	if l.elemPlaces == nil || name == "" {
		return elemPlace{}, false
	}
	p, ok := l.elemPlaces[name]
	return p, ok
}

// readPlace re-reads the element. A read must go through the container every
// time (not once per iteration) so a read AFTER a write in the same iteration
// sees the new value — `x = x + 1` is a read-modify-write of the element.
func (l *lowerer) readPlace(p elemPlace) ValueID {
	if p.arr == NoVal || p.idx == NoVal {
		return NoVal
	}
	return l.b.Emit(OpIndex, p.elemT, []ValueID{p.arr, p.idx}, "")
}

// storePlace writes v into the element, mirroring the `a[i] = v` path in
// lowerAssignNode: an implicit `ok(v)` wrap when the element is a `?T` (the
// container read yields an option, so a bare value assigned back must be
// wrapped), then OpIndexStore. Owned element types are handled by codegen
// (emitIndexStore drops the overwritten element exactly once).
func (l *lowerer) storePlace(p elemPlace, v ValueID) ValueID {
	if p.arr == NoVal || p.idx == NoVal || v == NoVal {
		return v
	}
	if eT := l.indexElemType(p.arr); eT != NoType {
		v = l.wrapOptionIfNeeded(eT, v)
	}
	l.b.EmitVoid(OpIndexStore, []ValueID{p.arr, p.idx, v}, "")
	// The element write mutates the container, so its compile-time aggregate
	// constant is stale — same rule as the `a[i] = v` path in lowerAssignNode
	// (see dropConstAgg). Without it `a []i64 = [0, 0]` + `x <- a: { x = 5 }`
	// followed by `a[0] == 5` folded the read back to the initializer's 0.
	l.dropConstAgg(p.name)
	return v
}

// bindElemPlace registers name as an alias of place p and returns a restore
// function that puts the previous binding (or absence) back. The loop that
// owns the place calls it with `defer` so a same-named binding from an
// enclosing loop is restored on exit.
func (l *lowerer) bindElemPlace(name string, p elemPlace) func() {
	if name == "" {
		return func() {}
	}
	if l.elemPlaces == nil {
		l.elemPlaces = map[string]elemPlace{}
	}
	prev, had := l.elemPlaces[name]
	l.elemPlaces[name] = p
	return func() {
		if had {
			l.elemPlaces[name] = prev
			return
		}
		delete(l.elemPlaces, name)
	}
}

// clearElemPlace drops a place binding for name. A genuine re-declaration
// (`x = ...` introducing a NEW local that shadows the loop variable) must
// stop writing through the element.
func (l *lowerer) clearElemPlace(name string) {
	if l.elemPlaces == nil || name == "" {
		return
	}
	delete(l.elemPlaces, name)
}

// aliasArmPlace turns a synthetic match-arm binding whose initializer is a
// BARE IDENTIFIER (`let it = x`, the identity binding the parser prepends for
// an element that is the arm's payload) into an alias of that identifier's
// element place. Returns true when the binding was handled as an alias.
//
// A synthetic arm binding over a NON-place clears any stale alias for the name
// and returns false, so a place can never outlive the arm that established it.
func (l *lowerer) aliasArmPlace(name string, isArmBinding bool, valueNodeID int32, val ValueID) bool {
	if name == "" || !isArmBinding || val == NoVal || valueNodeID == hir.NoID {
		return false
	}
	cn := l.pkg.Node(valueNodeID)
	if cn == nil || cn.Kind != hir.KIdent {
		return false
	}
	if p, ok := l.placeOf(l.pkg.Str(cn.S)); ok {
		l.pushArmPlace(name, p)
		return true
	}
	l.clearElemPlace(name)
	return false
}

// placePresenceConst folds `<place> == <bare option variant>` to a constant.
//
// A container element reached through an iteration variable is, by
// construction, a PRESENT element: the loop walks 0 .. len-1, so the read can
// never be out of bounds — exactly the case where the subscript form
// `a[i]: { ok -> ... }` has its bounds check proven away and the ok arm always
// runs (parser bounds.go / hir.FlagInBounds). Matching the same variants
// against the loop variable must therefore agree with it:
//
//	x <- a: { x: { ok -> it = 5 -> {} } }        ; ok always runs
//	i <- [0..n): { a[i]: { ok -> it = 5 -> {} } } ; ok always runs
//
// Without this the subject is a plain element (`x` is an i64, not a `?i64`),
// the arm condition degenerates to `x == 0` — the bare `ok` variant's
// discriminant — so the arm fires only for an element whose VALUE happens to
// be 0 and the write is dropped for every other element.
//
// Gated on the element NOT being an option: a container of real `?T` elements
// keeps ordinary tag semantics (`x == ok` tests the tag of the stored option).
// Returns NoVal when the fold does not apply.
func (l *lowerer) placePresenceConst(ln, rn *hir.Node, op Op) ValueID {
	if op != OpEq && op != OpNe {
		return NoVal
	}
	present, ok := l.placeVariantSide(ln, rn)
	if !ok {
		present, ok = l.placeVariantSide(rn, ln)
	}
	if !ok {
		return NoVal
	}
	if op == OpNe {
		present = !present
	}
	v := int64(0)
	if present {
		v = 1
	}
	return l.b.EmitInt(OpConst, l.b.Type("bool"), v, "")
}

// placeVariantSide reports whether `place` is a container-element place whose
// element is not an option, and `variant` is a bare option variant. present is
// true for the success variants (`ok` / `some`), false for `nil` / `err`.
func (l *lowerer) placeVariantSide(place, variant *hir.Node) (present bool, ok bool) {
	if place == nil || variant == nil || place.Kind != hir.KIdent {
		return false, false
	}
	p, isPlace := l.placeOf(l.pkg.Str(place.S))
	if !isPlace || p.elemT == NoType || p.elemT == l.voidType {
		return false, false
	}
	if ty := l.mod.Type(p.elemT); ty != nil && ty.Kind == KindOption {
		return false, false
	}
	if variant.Kind == hir.KNilLit {
		return false, true
	}
	if variant.Kind != hir.KIdent {
		return false, false
	}
	switch l.pkg.Str(variant.S) {
	case "ok", "some":
		return true, true
	case "nil", "err":
		return false, true
	}
	return false, false
}

// armPlaceBind records one match-arm binding that aliased a container element,
// so the alias is dropped when the arm body ends — the counterpart of
// armScopedBind for the `locals` map (see restoreArmScoped). Without it an
// arm's `it` would keep aliasing the element after the match, and a later
// `it = v` would silently store into the container.
type armPlaceBind struct {
	name  string
	saved elemPlace
	had   bool
}

// pushArmPlace aliases name to p for the duration of the current arm.
func (l *lowerer) pushArmPlace(name string, p elemPlace) {
	saved, had := l.elemPlaces[name]
	l.armPlaces = append(l.armPlaces, armPlaceBind{name: name, saved: saved, had: had})
	l.elemPlaces[name] = p
}

// restoreArmPlaces undoes every arm place recorded since `base`, newest first.
func (l *lowerer) restoreArmPlaces(base int) {
	for i := len(l.armPlaces) - 1; i >= base; i-- {
		b := l.armPlaces[i]
		if b.had {
			l.elemPlaces[b.name] = b.saved
		} else {
			delete(l.elemPlaces, b.name)
		}
	}
	l.armPlaces = l.armPlaces[:base]
}
