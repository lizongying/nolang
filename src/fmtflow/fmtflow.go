// Package fmtflow 是 `no fmt` 与 LSP format-on-save 共用的**唯一**主动改写管线。
//
// 两条入口过去各有一套编排：
//   - cmd/no：先按 byte 区间改源码字串（移除冗余标注）→ 重新 parse →
//     stringConcat 翻转 → overflow 相关性 → FormatProgramWithOverflow；
//   - lsp：parse（或复用快照 AST 的副本）→ 节点表标记该省略哪些标注 →
//     stringConcat → overflow 相关性 → FormatProgramWithRewrites → 产物回验。
//
// 两套编排的差异不只是代码重复：判定的防禦条件一度也不同（LSP 多挡了 slice /
// map 型别），于是同一份档案「存档自动格式化」与「no fmt -w」会写出不同内容。
// 这里把编排收成一份，防禦判定则统一由 fmt.RedundantTypeAnnotationSafe 提供。
//
// 分層：本套件同时依赖 checker 与 fmt（fmt 刻意不依赖 checker，避免
// StdSigKey 缺失时被 checker 的 os.Exit(1) 带走），因此编排只能放在兩者之上。
package fmtflow

import (
	"github.com/lizongying/nolang/checker"
	nfmt "github.com/lizongying/nolang/fmt"
	"github.com/lizongying/nolang/parser"
)

// Options 是一次格式化的改写开关与渲染参数。
//
// 零值等于「只排版」：三个改写开关全关、LoopStyle 为零值（prefix）。
// 需要 CLI 的默认行为请用 DefaultOptions()。
type Options struct {
	LoopStyle nfmt.LoopStyle
	// 三类主动改写。任何一个为 true 都会走完整的 parse + 判定管线。
	RedundantTypes      bool
	StringConcat        bool
	IneffectiveOverflow bool
	// CrossModuleSignatures 为 false 时只清掉三张签名表（见 NarrowConfig），
	// 保留 Filename / EnumVariants / AllowAnonymousFnType —— 后两者影响
	// 「能否解析」与 match desugar，清掉会让渲染视野与调用端的诊断视野不一致。
	CrossModuleSignatures bool
	// Base 是可选的「已解析快照 AST」（LSP 的 parseSnapshot.ast）。仅在
	// CrossModuleSignatures 为 true 时被复用，且一定先深拷贝，绝不就地改写。
	Base *parser.Program
}

// DefaultOptions 是 `no fmt` 的默认行为：三类改写全开、跨模组签名全开。
func DefaultOptions(loopStyle nfmt.LoopStyle) Options {
	return Options{
		LoopStyle:             loopStyle,
		RedundantTypes:        true,
		StringConcat:          true,
		IneffectiveOverflow:   true,
		CrossModuleSignatures: true,
	}
}

// NarrowConfig 依「關閉跨模組簽名」收斂解析配置：只移除函式 / 方法 / struct
// 三張簽名表，其餘欄位原樣保留。
func NarrowConfig(cfg nfmt.ParseConfig) nfmt.ParseConfig {
	out := cfg
	out.FuncSigs = nil
	out.MethodSigs = nil
	out.StructFields = nil
	return out
}

// Format 對 src 執行一次格式化。回傳值可直接作為輸出 / TextEdit.NewText。
//
// perrs 只在「解析失敗、退回純排版」時非空：CLI 用它把錯誤報給使用者並拒絕
// 寫檔，LSP 則直接忽略（存檔格式化不該因為檔案有錯就整個失敗）。
func Format(src string, cfg nfmt.ParseConfig, opt Options) (string, []string) {
	effective := cfg
	if !opt.CrossModuleSignatures {
		effective = NarrowConfig(cfg)
	}
	fallback := func() (string, []string) {
		return nfmt.FormatFileWithConfig(src, effective, opt.LoopStyle)
	}

	// 三類改寫全關：純排版，不解析。
	if !opt.RedundantTypes && !opt.StringConcat && !opt.IneffectiveOverflow {
		return fallback()
	}

	var program *parser.Program
	if opt.Base != nil && opt.CrossModuleSignatures {
		// 快照 AST 與 effective 同視野：深拷貝後直接用，省一次 lex/parse。
		// CloneProgramForFormat 會補回 CloneProgram 漏掉的 ExternStructFields。
		program = nfmt.CloneProgramForFormat(opt.Base)
	} else {
		var perrs []string
		program, perrs = nfmt.ParseSourceWithConfig(src, effective)
		if len(perrs) > 0 {
			return fallback()
		}
	}
	if program == nil || len(program.Statements) == 0 {
		return fallback()
	}

	ext := checker.ExternSigTablesFromConfig(effective)

	var redundant map[*parser.LetStatement]bool
	if opt.RedundantTypes {
		redundant = redundantLetTypes(program, ext)
	}
	if opt.StringConcat {
		checker.FixStringConcatOperators(program, checker.BuildTHIRView(program))
	}
	var relevant map[parser.Statement]bool
	var governed map[*parser.AnnotationStatement]parser.Statement
	if opt.IneffectiveOverflow {
		relevant, governed = checker.OverflowAnnotationRelevanceWithExtern(program, ext)
	}

	out := nfmt.FormatProgramWithRewritesAndLoopStyle(program, src, opt.LoopStyle, redundant, relevant, governed)

	// 關卡：省略標註後的產物必須仍能解析（空字面量等推不回型別的形態）。
	// 失敗就用同一棵樹重渲染一次，只保留 stringConcat / overflow 兩類不改語法
	// 結構的改寫。
	if len(redundant) > 0 {
		if _, errs := nfmt.ParseSourceWithConfig(out, effective); len(errs) > 0 {
			return nfmt.FormatProgramWithRewritesAndLoopStyle(program, src, opt.LoopStyle, nil, relevant, governed), nil
		}
	}
	return out, nil
}

// redundantLetTypes 把 checker 的報告（位置）換成節點表，並套用共用的
// 「這個標註能不能省」判定。報告的 Line/Column 取自 s.Type.Pos()，與
// parser.FindLetByTypePos 的定位同源。
func redundantLetTypes(program *parser.Program, ext checker.ExternSigTables) map[*parser.LetStatement]bool {
	results := checker.ValidateRedundantTypeAnnotationWithExtern(program, ext)
	if len(results) == 0 {
		return nil
	}
	out := make(map[*parser.LetStatement]bool, len(results))
	for _, r := range results {
		n := parser.FindLetByTypePos(program, r.Line, r.Column)
		if n == nil || !nfmt.RedundantTypeAnnotationSafe(n) {
			continue
		}
		out[n] = true
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
