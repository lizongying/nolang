package build

import (
	"strings"
	"testing"
)

// TestOptionStoreToScalarElem guards the optionPayloadOf destructuring reversal
// in emitIndexStore / emitSetField. optionPayloadOf returns (value, type), but
// three callers wrote `plt, pl :=` (i.e. treating the FIRST result as the type),
// so `coerce(plt, pl, elemT)` received the value as srcTy and the type as srcVal.
// coerce matched no case, returned "", and the fallback `valV = pl` stored the
// TYPE STRING as the value — emitting `store i8 i64` (a type where a value
// belongs), which opt-verify rejected with "expected value token". It surfaced
// as `no build` failing on noimg's tga-load-rle (?i64 stored into a byte).
func TestOptionStoreToScalarElem(t *testing.T) {
	src := `main = () () {
  px [4]byte
  v ?i64 = 200
  px[0] = v
  print(px[0])
}
`
	t2 := NewTranspiler(nil)
	_, err := t2.Compile(src)
	if err != nil {
		if strings.Contains(err.Error(), "expected value token") ||
			strings.Contains(err.Error(), "store i8 i64") {
			t.Fatalf("regression: storing a ?i64 option into an i8 element emitted a "+
				"type-as-value store (optionPayloadOf arg-order reversal): %v", err)
		}
		t.Fatalf("unexpected compile error: %v", err)
	}
}
