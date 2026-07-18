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
		// ForwardFunc, LLVMIntrinsic, CLibCall, or LLVMConv must be set
		if m.ForwardFunc == "" && m.LLVMIntrinsic == "" && m.CLibCall == nil && m.LLVMConv == nil {
			t.Errorf("builtin %s has neither ForwardFunc, LLVMIntrinsic, CLibCall, nor LLVMConv", key)
		}
	}
}

func TestFindBuiltinMethod(t *testing.T) {
	m := FindBuiltinMethod("get-pid")
	if m == nil {
		t.Fatal("FindBuiltinMethod(get-pid) returned nil")
	}
	if m.CLibCall == nil || m.CLibCall.FuncName != "getpid" {
		t.Errorf("get-pid CLibCall = %v, want getpid", m.CLibCall)
	}

	m = FindBuiltinMethod("nonexistent")
	if m != nil {
		t.Errorf("FindBuiltinMethod(nonexistent) = %v, want nil", m)
	}
}

func TestLLVMIntrinsicMethods(t *testing.T) {
	intrinsicMethods := []struct {
		name      string
		intrinsic string
		paramType string
	}{
		{"abs", "llvm.fabs.f64", "f64"},
		{"max", "llvm.maxnum.f64", "f64f64"},
		{"min", "llvm.minnum.f64", "f64f64"},
		{"sqrt", "llvm.sqrt.f64", "f64"},
		{"sin", "llvm.sin.f64", "f64"},
		{"cos", "llvm.cos.f64", "f64"},
		{"pow", "llvm.pow.f64", "f64f64"},
		{"ceil", "llvm.ceil.f64", "f64"},
		{"floor", "llvm.floor.f64", "f64"},
		{"round", "llvm.round.f64", "f64"},
		{"trunc", "llvm.trunc.f64", "f64"},
		{"exp", "llvm.exp.f64", "f64"},
		{"log", "llvm.log.f64", "f64"},
		{"log10", "llvm.log10.f64", "f64"},
		{"log2", "llvm.log2.f64", "f64"},
		{"asin", "llvm.asin.f64", "f64"},
		{"acos", "llvm.acos.f64", "f64"},
		{"atan", "llvm.atan.f64", "f64"},
		{"atan2", "llvm.atan2.f64", "f64f64"},
		{"sinh", "llvm.sinh.f64", "f64"},
		{"cosh", "llvm.cosh.f64", "f64"},
		{"tanh", "llvm.tanh.f64", "f64"},
	}
	for _, tt := range intrinsicMethods {
		found := false
		for i := range BuiltinMethodList {
			m := &BuiltinMethodList[i]
			if m.MethodName == tt.name && m.LLVMIntrinsic == tt.intrinsic {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("no BuiltinMethod with name=%s and LLVMIntrinsic=%s", tt.name, tt.intrinsic)
		}
	}
}

func TestLLVMConvMethods(t *testing.T) {
	tests := []struct {
		name string
		conv LLVMConvKind
	}{
		{"i64-to-f64", LLVMConvI64ToFP},
		{"f64-to-i64", LLVMConvFPToI64},
		{"f64-to-f32", LLVMConvF64ToF32},
		{"f32-to-f64", LLVMConvF32ToF64},
	}
	for _, tt := range tests {
		found := false
		for i := range BuiltinMethodList {
			m := &BuiltinMethodList[i]
			if m.MethodName == tt.name && m.LLVMConv != nil && *m.LLVMConv == tt.conv {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("no BuiltinMethod with name=%s and LLVMConv=%d", tt.name, tt.conv)
		}
	}
}

func TestOSCLibCallMethods(t *testing.T) {
	tests := []struct {
		name     string
		funcName string
	}{
		{"get-env", "getenv"},
		{"ch-dir", "chdir"},
		{"remove", "unlink"},
		{"rename", "rename"},
		{"get-pid", "getpid"},
		{"now", "time"},
		{"sleep", "sleep"},
		{"open-read", "open"},
		{"open-write", "open"},
		{"close", "close"},
	}
	for _, tt := range tests {
		found := false
		for i := range BuiltinMethodList {
			m := &BuiltinMethodList[i]
			if m.MethodName == tt.name && m.CLibCall != nil && m.CLibCall.FuncName == tt.funcName {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("no BuiltinMethod with name=%s and CLibCall.FuncName=%s", tt.name, tt.funcName)
		}
	}
}

func TestStrconvSprintfMethods(t *testing.T) {
	// All to-str methods (i8/i16/i32/i64/u8/u16/u32/u64/byte.to-str,
	// char-to-str, f64.to-str, f32.to-str) are now implemented in Nolang
	// (src/std/number.no). str.to-f64 / str.to-f32 are implemented in
	// Nolang (src/std/str.no) using str-to-f64 + f64-to-f32.
	// No sprintf CLibCall builtins remain for strconv.
	for i := range BuiltinMethodList {
		m := &BuiltinMethodList[i]
		if m.CLibCall != nil && m.CLibCall.FuncName == "sprintf" && m.CLibCall.SprintfFmt != "" {
			t.Errorf("builtin %s still uses sprintf CLibCall with fmt %q (should be Nolang impl)",
				m.MethodName, m.CLibCall.SprintfFmt)
		}
	}
}

func TestSprintfBuiltin(t *testing.T) {
	m := FindBuiltinMethod("sprintf")
	if m == nil {
		t.Fatal("FindBuiltinMethod(sprintf) returned nil")
	}
	if m.ForwardFunc != "sprintf" {
		t.Errorf("sprintf ForwardFunc = %q, want \"sprintf\"", m.ForwardFunc)
	}
	if len(m.Return) != 1 || m.Return[0].String() != "str" {
		t.Errorf("sprintf Return = %v, want [str]", m.Return)
	}
	if len(m.Params) != 1 || m.Params[0].String() != "str" {
		t.Errorf("sprintf Params = %v, want [str]", m.Params)
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
