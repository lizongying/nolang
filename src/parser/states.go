package parser

// ParseContext represents the current parsing context, replacing boolean flags.
type ParseContext int

const (
	CTX_GLOBAL     ParseContext = iota
	CTX_FOR_COND                // inside for condition — skip struct literal parsing
	CTX_MATCH_COND              // inside match condition — skip struct literal parsing
	CTX_MATCH_ARM               // inside match arm — prevent | as binary OR
	CTX_EXPR                    // inside expression context (= right side)
	CTX_IF_COND                 // inside if condition
	CTX_FUNC_BODY               // inside function body
)

// contextStack manages nested parsing contexts.
type contextStack []ParseContext

func (s *contextStack) push(ctx ParseContext) {
	*s = append(*s, ctx)
}

func (s *contextStack) pop() ParseContext {
	if len(*s) == 0 {
		return CTX_GLOBAL
	}
	n := len(*s) - 1
	ctx := (*s)[n]
	*s = (*s)[:n]
	return ctx
}

func (s *contextStack) current() ParseContext {
	if len(*s) == 0 {
		return CTX_GLOBAL
	}
	return (*s)[len(*s)-1]
}

func (s *contextStack) contains(ctx ParseContext) bool {
	for _, c := range *s {
		if c == ctx {
			return true
		}
	}
	return false
}

// copy returns a snapshot of the current stack.
func (s *contextStack) copy() contextStack {
	cp := make(contextStack, len(*s))
	copy(cp, *s)
	return cp
}
