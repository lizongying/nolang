package build

// seminputs_consistency_test.go — 钉住「build 与 fmt / LSP 交给 parser 的 cfg
// 逐字段相等」。
//
// 两个生产者现在共用 parser.NewSemRebuildConfig（字段到 cfg 的映射只在那里
// 发生），但「表从哪来」仍各有判断：build 读 Transpiler 的 extern 字段，
// fmt / LSP 读 FmtSigLoader 的缓存。只要两边对同一个 package、同一份源码交出
// 不同的 cfg，同一次「编辑器保存」与 `no fmt -w` 就会写出不同结果 ——
// compiler.anonymous-fn-type 那次事故正是这一类，当时没有任何测试会红。
//
// 第三个生产者 checker.stdSemConfig 不在此列：std 模块体不属于任何
// package.noml，它的完整性由 parser 包的字段覆盖测试守住。

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestBuildAndFmtLoaderProduceSameConfig(t *testing.T) {
	skipWithoutStdSigKey(t)

	dir := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("workspace.noml", "{\n  lib: '/lib'\n}\n")
	write("package.noml", "{\n  name: 'probe'\n}\n")
	write("lib/helper.no", "is-ok = () (b bool) {\n    b = true\n}\n")

	p, err := LoadPackage(dir)
	if err != nil {
		t.Fatalf("LoadPackage: %v", err)
	}
	if p == nil {
		t.Fatal("LoadPackage returned nil")
	}
	// 在内存里打开解析期开关：两个生产者读的是同一个 *Package，因此这里同时
	// 影响它们 —— 这正是「一个映射点」要保证的性质。
	p.Compiler.AnonymousFnType = true

	src := "use /lib/helper\nf = (cb () ()) () {\n    ok = helper.is-ok()\n}\n"
	entry := filepath.Join(dir, "main.no")

	tr := NewTranspiler(p)
	tr.preloadModuleSignatures(src)
	fromBuild := tr.astParseConfig(entry)
	fromFmt := NewFmtSigLoader(p).ConfigFor(src, entry)

	// 尺子不能是空的：两边都得真的拿到本地模块的签名与 package 级开关。
	if got := fromBuild.FuncSigs["helper.is-ok"]; len(got) == 0 || got[0] != "bool" {
		t.Fatalf("build cfg lost the local module signature: %#v", got)
	}
	if !fromBuild.AllowAnonymousFnType || !fromFmt.AllowAnonymousFnType {
		t.Fatalf("anonymous-fn-type did not reach both producers: build=%v fmt=%v",
			fromBuild.AllowAnonymousFnType, fromFmt.AllowAnonymousFnType)
	}

	if fromBuild.Filename != fromFmt.Filename {
		t.Fatalf("Filename differs: build=%q fmt=%q", fromBuild.Filename, fromFmt.Filename)
	}
	if !reflect.DeepEqual(fromBuild.FuncSigs, fromFmt.FuncSigs) {
		t.Fatalf("FuncSigs differ: build=%d entries, fmt=%d entries",
			len(fromBuild.FuncSigs), len(fromFmt.FuncSigs))
	}
	if !reflect.DeepEqual(fromBuild.MethodSigs, fromFmt.MethodSigs) {
		t.Fatalf("MethodSigs differ: build=%d entries, fmt=%d entries",
			len(fromBuild.MethodSigs), len(fromFmt.MethodSigs))
	}
	if !reflect.DeepEqual(fromBuild.StructFields, fromFmt.StructFields) {
		t.Fatalf("StructFields differ: build=%d entries, fmt=%d entries",
			len(fromBuild.StructFields), len(fromFmt.StructFields))
	}
	if !reflect.DeepEqual(fromBuild.EnumVariants, fromFmt.EnumVariants) {
		t.Fatalf("EnumVariants differ: build=%d entries, fmt=%d entries",
			len(fromBuild.EnumVariants), len(fromFmt.EnumVariants))
	}
}

// TestAstCacheSaltFollowsSignatureTables 钉住 salt 的 memo 不能陈旧。
//
// salt 是 Tier-1 磁盘 AST 缓存的键的一部分，序列化的是上千条签名，所以改成
// 每编译算一次。但 preloadModuleSignatures 的步骤 1 会在函数内部解析模块
// 文件（那时表里只有 std），返回前才把本地模块签名并进去 —— 如果返回前不重算，
// 入口文件就会拿 std-only 的 salt 去命中按另一组表烤出来的 blob，这是静默的。
func TestAstCacheSaltFollowsSignatureTables(t *testing.T) {
	skipWithoutStdSigKey(t)

	tr := NewTranspiler(nil)
	empty := tr.astCacheSalt()
	if empty == "" {
		t.Fatal("salt of an empty table set must still be a value, not the empty string")
	}
	tr.preloadModuleSignatures("")
	loaded := tr.astCacheSalt()
	if loaded == empty {
		t.Fatal("salt stayed stale after preload loaded the signature tables")
	}
	if again := tr.astCacheSalt(); again != loaded {
		t.Fatal("salt must be memoized within one compile target")
	}
}

// TestUnusedUsePathKeepsStdTables 是上面那条的负向补充：解析失败的本地 use
// 不能把 std 表清空，否则 fmt 会对一个写错路径的文件退化成单文件视野，
// 而 build 仍然能编译 —— 又是一次「同一份源码两种结论」。
func TestUnusedUsePathKeepsStdTables(t *testing.T) {
	skipWithoutStdSigKey(t)

	src := "use /definitely/not/here\nf = () {\n    print('x')\n}\n"
	tr := NewTranspiler(nil)
	tr.preloadModuleSignatures(src)
	fromBuild := tr.astParseConfig("")
	fromFmt := NewFmtSigLoader(nil).ConfigFor(src, "")

	if len(fromBuild.FuncSigs) == 0 || len(fromFmt.FuncSigs) == 0 {
		t.Fatal("a bad use path must not wipe the std signature tables")
	}
	if !reflect.DeepEqual(fromBuild.FuncSigs, fromFmt.FuncSigs) {
		t.Fatalf("FuncSigs differ on a bad use path: build=%d entries, fmt=%d entries",
			len(fromBuild.FuncSigs), len(fromFmt.FuncSigs))
	}
}
