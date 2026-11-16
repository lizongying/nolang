package parser

import (
	"reflect"
	"testing"

	"github.com/lizongying/nolang/lexer"
)

// TestNewSemRebuildConfigCoversEveryField 钉住「cfg 的每个导出字段都由唯一构造
// 点填」。
//
// 这条断言存在的理由是一次真实事故：compiler.anonymous-fn-type 只进了 build 的
// astParseConfig，fmt / LSP 那里没有，于是同一个 package 的同一份源码在两条管道
// 里解析出不同的 AST。当时的代码没有任何东西会因此变红。
//
// 现在给 SemRebuildConfig 加字段而不接进 NewSemRebuildConfig，这里就会失败。
// 如果新字段「本来就不该由构造点填」，也必须在这里显式放行并写明理由 —— 静默
// 通过是不允许的。
func TestNewSemRebuildConfigCoversEveryField(t *testing.T) {
	ext := SigTableSet{
		FuncSigs:     map[string][]string{"mod.fn": {"?bool"}},
		MethodSigs:   map[string][]string{"str.starts-with": {"bool"}},
		StructFields: map[string]map[string]string{"resp": {"status": "i64"}},
		EnumVariants: map[string][]string{"file-mode": {"read", "write"}},
	}
	cfg := NewSemRebuildConfig("probe.no", CompilerSwitches{AnonymousFnType: true}, ext)

	rt := reflect.TypeOf(cfg)
	rv := reflect.ValueOf(cfg)
	if rt.NumField() == 0 {
		t.Fatal("SemRebuildConfig has no fields")
	}
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		if !f.IsExported() {
			continue
		}
		if rv.Field(i).IsZero() {
			t.Fatalf("SemRebuildConfig.%s is zero although every input was non-zero: "+
				"wire it through CompilerSwitches / SigTableSet in NewSemRebuildConfig", f.Name)
		}
	}
}

// TestSemRebuildConfigApplyKeepsInjectionState 钉住 Apply 的「注入」与「未注入」
// 是两种不同状态：ExternSignatureFingerprint 与 Tier-1 salt 都依赖这个区别，
// 把空表当成没注入会让磁盘 AST 缓存串味。
func TestSemRebuildConfigApplyKeepsInjectionState(t *testing.T) {
	bare := New(lexer.New(""))
	NewSemRebuildConfig("", CompilerSwitches{}, SigTableSet{}).Apply(bare)
	if bare.externFuncSigs != nil || bare.externMethodSigs != nil || bare.externStructFields != nil {
		t.Fatal("zero SigTableSet must leave the extern tables unset, not inject empty maps")
	}

	// 显式注入空表（非 nil map）必须真的注入，否则「注入了空表」与「完全没注入」
	// 会被合并成同一种状态。
	explicit := New(lexer.New(""))
	NewSemRebuildConfig("", CompilerSwitches{}, SigTableSet{
		FuncSigs:     map[string][]string{},
		MethodSigs:   map[string][]string{},
		StructFields: map[string]map[string]string{},
	}).Apply(explicit)
	if explicit.externFuncSigs == nil {
		t.Fatal("explicitly injected empty tables must remain distinguishable from no injection")
	}
}
