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
	m := FindBuiltinMethod("cbrt")
	if m == nil {
		t.Fatal("FindBuiltinMethod(cbrt) returned nil")
	}
	if m.CLibCall == nil || m.CLibCall.FuncName != "cbrt" {
		t.Errorf("cbrt CLibCall = %v, want cbrt", m.CLibCall)
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

func TestCLibCallMethods(t *testing.T) {
	clibMethods := []struct {
		name     string
		funcName string
		argCount int
	}{
		{"cbrt", "cbrt", 1},
		{"hypot", "hypot", 2},
		{"asin", "asin", 1},
		{"acos", "acos", 1},
		{"atan", "atan", 1},
		{"atan2", "atan2", 2},
		{"sinh", "sinh", 1},
		{"cosh", "cosh", 1},
		{"tanh", "tanh", 1},
		{"fmod", "fmod", 2},
	}
	for _, tt := range clibMethods {
		m := FindBuiltinMethod(tt.name)
		if m == nil {
			t.Errorf("FindBuiltinMethod(%s) returned nil", tt.name)
			continue
		}
		if m.CLibCall == nil {
			t.Errorf("%s CLibCall is nil", tt.name)
			continue
		}
		if m.CLibCall.FuncName != tt.funcName {
			t.Errorf("%s CLibCall.FuncName = %q, want %q", tt.name, m.CLibCall.FuncName, tt.funcName)
		}
		if len(m.CLibCall.ArgTypes) != tt.argCount {
			t.Errorf("%s CLibCall.ArgTypes len = %d, want %d", tt.name, len(m.CLibCall.ArgTypes), tt.argCount)
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
		{"is-file", "stat"},
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

func TestStrconvCLibCallMethods(t *testing.T) {
	tests := []struct {
		name     string
		funcName string
	}{
		{"str-to-i64", "atoi"},
		{"str-to-f64", "strtod"},
		{"str-to-f32", "strtod"},
		{"str-to-u64", "strtoull"},
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
	tests := []struct {
		name string
		fmt  string
	}{
		{"i8-to-str", "%hhd"},
		{"i16-to-str", "%hd"},
		{"i32-to-str", "%d"},
		{"i64-to-str", "%lld"},
		{"u8-to-str", "%hhu"},
		{"u16-to-str", "%hu"},
		{"u32-to-str", "%u"},
		{"u64-to-str", "%llu"},
		{"f32-to-str", "%g"},
		{"f64-to-str", "%g"},
		{"byte-to-str", "%hhu"},
		{"char-to-str", "%c"},
	}
	for _, tt := range tests {
		found := false
		for i := range BuiltinMethodList {
			m := &BuiltinMethodList[i]
			if m.MethodName == tt.name && m.CLibCall != nil && m.CLibCall.SprintfFmt == tt.fmt {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("no BuiltinMethod with name=%s and SprintfFmt=%s", tt.name, tt.fmt)
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
