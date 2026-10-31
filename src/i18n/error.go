package i18n

import "regexp"

// embeddedID matches a rendered ID token anywhere in a message. It exists
// because some messages embed a diagnostic as TEXT rather than as a wrapped
// error — build's "parser errors: %s" interpolates
// parser.FormatDiagnostics, whose output already ends in " [xxxxxxxx]". In that
// case IDOf cannot see the inner ID (there is no error chain to walk), so the
// only way to avoid printing two IDs is to notice the one already in the text.
var embeddedID = regexp.MustCompile(`\[[0-9a-z]{8}\]`)

// Error is an error that carries the 8-character base36 message ID of the
// message it was built from.
//
// The ID is the translation index, and it is also the token a user pastes into
// a bug report, so it is rendered in the TRAILING slot:
//
//	member "a" is an empty object: annotate it with #{type = '<struct>'} [abc12def]
//
// Trailing rather than leading is the repo-wide convention: `no vet` prints
// "file:line:col: [SEVERITY] rule: message [id]", parser.FormatDiagnostics prints
// "line L, column C: message [id]", and build.splitCompileErrParts recovers the ID
// by scanning for it there. A leading slot would have to be parsed differently by
// every consumer, and would break the "noml:"-style prefix checks in tests.
type Error struct {
	id  string
	err error
}

// NewError attaches id to err. It is exported for call sites that already have
// a formatted error and only need the ID attached; prefer Errorf, which also
// handles translation.
func NewError(id string, err error) error {
	if id == "" || err == nil {
		return err
	}
	return &Error{id: id, err: err}
}

// Error implements error. The bracketed ID is omitted when the message has none,
// so a partially migrated call site still renders exactly as before.
//
// It is also omitted when the wrapped error already renders an ID. A build
// failure is a chain — "compilation error" wraps "parser errors" wraps the
// diagnostic — and every frame has its own ID, so the naive rule would print
// three of them on one line:
//
//	compilation error [s8gh6opt]: parser errors [au6g7ji7]: … [ew5qg77j]
//
// Suppressing the outer ID whenever an inner one is already visible keeps exactly
// one ID per chain, and the one that survives is the innermost — the frame that
// actually names the problem rather than the frame that only supplies context.
func (e *Error) Error() string {
	if e.id == "" {
		return e.err.Error()
	}
	msg := e.err.Error()
	if IDOf(e.err) != "" || embeddedID.MatchString(msg) {
		return msg
	}
	return msg + " [" + e.id + "]"
}

// Unwrap exposes the underlying error so errors.Is / errors.As keep working
// through the ID wrapper (including through a %w chain it may itself hold).
func (e *Error) Unwrap() error { return e.err }

// ID reports the message ID carried by this error.
func (e *Error) ID() string { return e.id }

// IDOf reports the message ID that err renders, or "" when it renders none.
//
// It walks the whole %w chain and keeps the DEEPEST (innermost) ID, because that
// is the one Error() actually prints: every outer frame suppresses its own ID
// once an inner one is visible, so the innermost frame is the survivor. Returning
// the outermost ID here would disagree with what the user sees.
//
// A joined error (errors.Join) is not traversed: its children are alternatives,
// not a chain, and there is no single frame being rendered.
func IDOf(err error) string {
	last := ""
	for err != nil {
		if e, ok := err.(*Error); ok && e.id != "" {
			last = e.id
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			break
		}
		err = u.Unwrap()
	}
	return last
}
