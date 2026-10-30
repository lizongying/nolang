package ty

import "sync"

// Resolver lets a compilation stage teach the pool about names the pool cannot
// know on its own: std aliases (`fd = i64`), struct layouts, enum names. The
// stage keeps owning that registry; ty only asks.
//
// Both methods must be cheap and safe for concurrent readers: a Pool parses
// under RLock and may be shared by the goroutines of one compilation unit.
type Resolver interface {
	// Alias reports the target spelling of a type alias (`fd` -> `i64`).
	// Returning false leaves the name as an opaque Named type.
	Alias(name string) (target string, ok bool)

	// IsTypeName reports whether name denotes a type that can be used as a
	// container key: a builtin scalar, a registered struct/enum/newtype or an
	// alias. It resolves the `[fd]i64` (map) vs `[32]byte` (array) ambiguity.
	IsTypeName(name string) bool
}

// Pool interns Types by their raw spelling.
//
// One Pool belongs to one compilation unit: build/builder.go compiles targets
// concurrently, so there is deliberately no global pool. Interning makes
// pointer identity agree with spelling identity, which is what the existing
// defaultPool backs the package-level Parse helper. It has no resolver: the
// package-level entry point answers syntax-only questions, and a stage that
// needs alias resolution builds its own Pool with a Resolver.
var defaultPool = NewPool(nil)

// Parse is the package-level convenience form of (*Pool).Parse: it parses raw
// with the shared resolver-free pool. Total — never nil, and String() returns
// the spelling it was given.
func Parse(raw string) *Type { return defaultPool.Parse(raw) }

// `TypeMap[raw]` tables rely on.
type Pool struct {
	mu sync.RWMutex
	m  map[string]*Type
	r  Resolver
}

// NewPool returns an empty pool. A nil Resolver is allowed: every unresolved
// name then stays an opaque Named type.
func NewPool(r Resolver) *Pool {
	return &Pool{m: make(map[string]*Type, 64), r: r}
}

// SetResolver installs (or replaces) the resolver. It is meant to be called
// once, before the pool is shared between goroutines; it still takes the write
// lock so a late install cannot race with readers.
func (p *Pool) SetResolver(r Resolver) {
	p.mu.Lock()
	p.r = r
	p.mu.Unlock()
}

// Resolver returns the installed resolver (nil when none).
func (p *Pool) Resolver() Resolver {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.r
}

// Len reports how many distinct spellings have been interned.
func (p *Pool) Len() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.m)
}

// All returns a snapshot of every interned type (diagnostics and tests).
func (p *Pool) All() []*Type {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]*Type, 0, len(p.m))
	for _, t := range p.m {
		out = append(out, t)
	}
	return out
}

// Parse returns the interned Type for a nolang type spelling.
//
// Parse is total: it never panics and never returns nil. A spelling it cannot
// classify becomes Kind Invalid carrying the original text, and the invariant
// Parse(s).String() == s always holds, so a Pool can be dropped into any place
// that used to pass a raw string around.
func (p *Pool) Parse(raw string) *Type {
	if t := p.lookup(raw); t != nil {
		return t
	}
	built := p.build(raw, 0)
	if built == nil {
		built = &Type{Kind: Invalid, raw: raw}
	}
	built.raw = raw
	return p.intern(built)
}

// MakeOn interns a caller-built Type under raw. Later Parse(raw) calls return
// the same instance, so a stage can publish a precise structure (e.g. a
// registry-derived struct layout) without the pool having to rediscover it.
//
// When t conflicts with the spelling already inferred for raw, MakeOn keeps the
// first result — callers that need to detect the conflict use Checked.
func (p *Pool) MakeOn(raw string, t Type) *Type {
	if existing := p.lookup(raw); existing != nil {
		return existing
	}
	t.raw = raw
	return p.intern(&t)
}

// Checked interns a caller-built Type and reports whether the structure agrees
// with what Parse(raw) infers. A false result means the caller's precise
// knowledge contradicts the spelling (the silent inconsistency that used to
// surface as a bogus struct layout); the returned Type is the caller's, because
// precise knowledge wins.
func (p *Pool) Checked(raw string, t Type) (*Type, bool) {
	want := p.Parse(raw)
	if want.Kind == t.Kind {
		return want, true
	}
	t.raw = raw
	return p.intern(&t), false
}

// Int returns the interned canonical spelling for a fixed-width integer.
func (p *Pool) Int(bits int16, signed bool) *Type {
	name := "i"
	if !signed {
		name = "u"
	}
	return p.Parse(name + itoa(int(bits)))
}

// Float returns the interned canonical spelling for a float width.
func (p *Pool) Float(bits int16) *Type {
	if bits == 32 {
		return p.Parse("f32")
	}
	return p.Parse("f64")
}

func (p *Pool) lookup(raw string) *Type {
	p.mu.RLock()
	t := p.m[raw]
	p.mu.RUnlock()
	return t
}

func (p *Pool) intern(t *Type) *Type {
	p.mu.Lock()
	if existing, ok := p.m[t.raw]; ok {
		p.mu.Unlock()
		return existing
	}
	p.m[t.raw] = t
	p.mu.Unlock()
	return t
}

// itoa renders a small non-negative int without pulling strconv into every
// caller's build graph.
func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var buf [8]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
