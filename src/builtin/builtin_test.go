package builtin

import (
	"fmt"
	"testing"
)

func TestBuiltinMethodListNotEmpty(t *testing.T) {
	if len(BuiltinMethodList) == 0 {
		t.Error("BuiltinMethodList should have at least one entry")
	}
}

func TestBuiltinMethodListEntries(t *testing.T) {
	seen := make(map[string]bool)
	for _, m := range BuiltinMethodList {
		key := fmt.Sprintf("%s.%s(%v)", m.ReceiverType, m.MethodName, m.Params)
		if seen[key] {
			t.Errorf("duplicate builtin method: %s", key)
		}
		seen[key] = true

		if m.MethodName == "" {
			t.Error("builtin method has empty MethodName")
		}
		// ForwardFunc or LLVMIntrinsic must be set
		if m.ForwardFunc == "" && m.LLVMIntrinsic == "" {
			t.Errorf("builtin %s has neither ForwardFunc nor LLVMIntrinsic", key)
		}
	}
}

func TestReceiverKindString(t *testing.T) {
	tests := []struct {
		kind ReceiverKind
		want string
	}{
		{ReceiverGlobal, ""},
		{ReceiverStr, "str"},
		{ReceiverF32, "f32"},
		{ReceiverF64, "f64"},
		{ReceiverI64, "i64"},
		{ReceiverVec, "[]t"},
		{ReceiverArr, "[n]t"},
	}
	for _, tt := range tests {
		if got := tt.kind.String(); got != tt.want {
			t.Errorf("ReceiverKind(%d).String() = %q, want %q", tt.kind, got, tt.want)
		}
	}
}
