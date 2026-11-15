package lsp

// fmt_config.go — LSP 格式化的細粒度設定。
//
// 預設值全部為 true，也就是「與 CLI `no fmt` 完全一致的主動改寫」：不只排版，
// 還會移除冗餘型別標註、移除無效 overflow 註解、翻轉字串拼接運算子。
// 想把 format-on-save 退回純排版時，設 fmt.rewrite=false 即可；也可以只關掉
// 其中一項（例如文件專案不希望動註解，就設 ineffectiveOverflow=false）。
//
// 設定來源有兩個，後者覆蓋前者（只覆蓋明確出現的欄位）：
//  1. initialize.initializationOptions.fmt
//  2. workspace/didChangeConfiguration.settings 裡的 fmt / nolang.fmt
//
// 用 *bool 而非 bool，才能區分「用戶沒設」與「用戶明確設成 false」——
// 否则零值會把預設的 true 悄悄關掉。

import (
	"encoding/json"
	"sync"
)

// FmtConfig 是一次 format 請求實際採用的行為開關。
type FmtConfig struct {
	// Rewrite 是總開關。false 時所有主動改寫都不做，只排版（等同舊 LSP 行為，
	// 且可繼續直接複用 doc.AST，不產生額外 parse）。
	Rewrite bool
	// RedundantTypes 移除「標註型別 == 推斷型別」的顯式標註（`x str = ''` → `x = ''`）。
	RedundantTypes bool
	// IneffectiveOverflow 移除管轄陳述不含整數運算的 #{overflow = ...} 註解。
	IneffectiveOverflow bool
	// StringConcat 把字串拼接的 `+` 就地翻轉為 Nolang 的 `-`（formatter 重渲染時生效）。
	StringConcat bool
	// CrossModuleSignatures 決定**格式化渲染**是否注入 std / 本地模組簽名。
	// 關掉後渲染回到單檔視野，依賴跨模組回傳型別的 bool 比較化簡與冗餘標註
	// 判定都會收斂（更保守）。即時診斷與 format 閘門不受此設定影響：它們讀
	// 的是 ParseDocument 的完整簽名視野，紅線存在就一定擋下格式化。
	CrossModuleSignatures bool
}

// DefaultFmtConfig 與 CLI `no fmt` 的預設行為一致。
func DefaultFmtConfig() FmtConfig {
	return FmtConfig{
		Rewrite:               true,
		RedundantTypes:        true,
		IneffectiveOverflow:   true,
		StringConcat:          true,
		CrossModuleSignatures: true,
	}
}

// fmtConfigPatch 用指標欄位保留「未指定」這個狀態。
type fmtConfigPatch struct {
	Rewrite               *bool `json:"rewrite,omitempty"`
	RedundantTypes        *bool `json:"redundantTypes,omitempty"`
	IneffectiveOverflow   *bool `json:"ineffectiveOverflow,omitempty"`
	StringConcat          *bool `json:"stringConcat,omitempty"`
	CrossModuleSignatures *bool `json:"crossModuleSignatures,omitempty"`
}

// fmtOptionsEnvelope 相容三種客戶端形狀：
//
//	{"fmt": {...}}                          initializationOptions（本專案擴充）
//	{"nolang": {"fmt": {...}}}              VS Code contributes.configuration
//	{"settings": {...上述任一...}}           workspace/didChangeConfiguration
type fmtOptionsEnvelope struct {
	Fmt      *fmtConfigPatch     `json:"fmt,omitempty"`
	Nolang   *fmtOptionsEnvelope `json:"nolang,omitempty"`
	Settings *fmtOptionsEnvelope `json:"settings,omitempty"`
}

// extractFmtPatch 從任意 JSON 形狀的 options 中找出 fmt patch。
// 找不到或格式錯誤時回傳 nil，呼叫端保留既有設定（絕不因壞設定重置成預設）。
func extractFmtPatch(options any) *fmtConfigPatch {
	if options == nil {
		return nil
	}
	raw, err := json.Marshal(options)
	if err != nil {
		return nil
	}
	var env fmtOptionsEnvelope
	if err := json.Unmarshal(raw, &env); err == nil {
		for i := 0; i < 4 && env.Settings != nil; i++ {
			env = *env.Settings
		}
		if env.Fmt != nil {
			return env.Fmt
		}
		if env.Nolang != nil && env.Nolang.Fmt != nil {
			return env.Nolang.Fmt
		}
	}
	// 也接受「initializationOptions 本身就是 patch」的精簡寫法：
	// {"rewrite": false}。全部欄位皆 nil 時視為沒有設定。
	var direct fmtConfigPatch
	if err := json.Unmarshal(raw, &direct); err != nil {
		return nil
	}
	if direct.Rewrite != nil || direct.RedundantTypes != nil ||
		direct.IneffectiveOverflow != nil || direct.StringConcat != nil ||
		direct.CrossModuleSignatures != nil {
		return &direct
	}
	return nil
}

func applyFmtPatch(base FmtConfig, p *fmtConfigPatch) FmtConfig {
	if p == nil {
		return base
	}
	out := base
	if p.Rewrite != nil {
		out.Rewrite = *p.Rewrite
	}
	if p.RedundantTypes != nil {
		out.RedundantTypes = *p.RedundantTypes
	}
	if p.IneffectiveOverflow != nil {
		out.IneffectiveOverflow = *p.IneffectiveOverflow
	}
	if p.StringConcat != nil {
		out.StringConcat = *p.StringConcat
	}
	if p.CrossModuleSignatures != nil {
		out.CrossModuleSignatures = *p.CrossModuleSignatures
	}
	return out
}

// DidChangeConfigurationParams 已定義於 lsp.go（settings 為 any），
// workspace/didChangeConfiguration 直接沿用即可。

// fmtConfigMu / fmtConfig 由 Server 持有。放在這裡是為了讓 Server struct
// 只多兩個欄位，且所有讀取都走底下這個加鎖的快照方法。
type fmtConfigState struct {
	mu  sync.Mutex
	cfg FmtConfig
}

func newFmtConfigState() *fmtConfigState {
	return &fmtConfigState{cfg: DefaultFmtConfig()}
}

func (st *fmtConfigState) get() FmtConfig {
	if st == nil {
		return DefaultFmtConfig()
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.cfg
}

// apply 把 options 裡明確指定的欄位合進目前設定。
func (st *fmtConfigState) apply(options any) {
	if st == nil {
		return
	}
	p := extractFmtPatch(options)
	if p == nil {
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	st.cfg = applyFmtPatch(st.cfg, p)
}
