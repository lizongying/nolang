package fmt

// signatures.go — `no fmt` 的跨模組簽名注入入口。
//
// 過去 fmt 只有「裸 parser.New + SkipLowering」的單檔解析（見 api.go 的
// formatProgramWithLoopStyle 與 cmd/no 的 parseProgramForFmt），拿不到
// build / vet 透過 SemRebuildConfig.Apply → SetExternSignatures 注入的跨模組
// 簽名；expr.go 裡 boolComparisonFacts 因此只能靠本檔證據。本檔案補上這條
// 入口，但刻意不 import checker / build：
//
//   - 簽名表由呼叫端（CLI 的 FmtSigLoader）準備成 parser.SemRebuildConfig；
//   - fmt 只負責在 fresh parse 之前 cfg.Apply(p)；
//   - 零值 cfg 不會觸發任何注入，行為與舊路徑逐位元組一致。
//
// 這樣 fmt 保持「不碰 checker」的分層，也避開 CollectStdModuleSignatures
// 在 StdSigKey 為空（非 make 建構）時 os.Exit(1) 的坑。

import (
	"strings"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// ParseConfig 是一次格式化解析中「原始碼以外」的全部輸入。直接沿用 parser
// 的 SemRebuildConfig，讓 fmt / build / vet / LSP 共用同一份注入協定；新增
// parser 輸入時只需要在 SemRebuildConfig 加欄位，四條管道不會各自漂移。
type ParseConfig = parser.SemRebuildConfig

// ExternSignatures 是一次解析實際看到的跨模組簽名快照，供 LSP / 工具鏈
// 讀取（例如 hover 顯示 mod.fn 的回傳型別）。回傳的是只讀視圖。
type ExternSignatures struct {
	FuncSigs     map[string][]string
	MethodSigs   map[string][]string
	StructFields map[string]map[string]string
}

// ProgramExternSignatures 取出 program 語義副表裡的跨模組簽名。未注入配置
// 的單檔解析會得到三張空表——這正是「有沒有拿到跨模組簽名」的可觀測判據。
func ProgramExternSignatures(program *parser.Program) ExternSignatures {
	if program == nil || program.Sem == nil {
		return ExternSignatures{}
	}
	return ExternSignatures{
		FuncSigs:     program.Sem.ExternFuncSigs,
		MethodSigs:   program.Sem.ExternMethodSigs,
		StructFields: program.Sem.ExternStructFields,
	}
}

// ParseSourceWithConfig 以與 fmt 內部解析完全相同的選項（S4 糖 AST：
// SkipLowering）解析 source，並在 ParseProgram 之前套用 cfg。
//
// 回傳 nil program 代表「空輸入或解析失敗」，errs 帶 parser 錯誤；呼叫端
// 應退回原樣輸出。cfg 為零值時 Apply 不會注入任何表，與舊的單檔解析等價。
func ParseSourceWithConfig(code string, cfg ParseConfig) (*parser.Program, []string) {
	if strings.TrimSpace(code) == "" {
		return nil, nil
	}
	l := lexer.New(code)
	p := parser.New(l)
	p.SkipLowering = true
	cfg.Apply(p)
	program := p.ParseProgram()
	if len(p.Errors()) > 0 {
		return nil, p.Errors()
	}
	return program, nil
}

// FormatFileWithConfig 是 FormatFileWithErrorsAndLoopStyle 的跨模組版本：
// 先用 cfg 解析，再交給既有的 AST 渲染器（api.go），保證排版邏輯只有一份。
// 解析失敗時原樣回傳 source 並帶回錯誤，語意與舊 API 一致。
func FormatFileWithConfig(code string, cfg ParseConfig, style LoopStyle) (out string, errs []string) {
	program, perrs := ParseSourceWithConfig(code, cfg)
	if program == nil {
		return code, perrs
	}
	o, ok, perrs := formatProgramASTWithLoopStyle(program, code, style)
	if !ok {
		return code, perrs
	}
	return ensureTrailingNewline(o), nil
}
