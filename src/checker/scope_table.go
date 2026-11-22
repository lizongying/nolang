package checker

// scopeTable 是 checker 主链的詞法作用域表。
//
// flat 是唯一權威的「名字 → 型別」視圖，永遠不替換指標：newScopeTable 直接
// 引用呼叫方的頂層 map，所以頂層 let 的 Bind 會寫回呼叫方，後續頂層陳述照常
// 可見。這與原本「同一份 varTypes map 遞迴傳遞」的語意完全一致，未遷移到
// resolve 介面的 helper 可持續唯讀消費 st.Flat()。
//
// frames 只記錄「本幀綁定過哪些名字，以及綁定前的舊值」，供 Pop 還原。
// frames[0] 是頂層（不 Pop）；Push 開函式幀；結果參數在同一函式幀內 Bind，
// 位於最內，天然優先於外層同名。
//
// 本次範圍固定三層：頂層 → 函式局部 → 結果參數。刻意不引入塊級 scope：
// Block/For/BlockMatch 主體沿用同一函式幀（checker.go:11466-11467 既有語意）。
type scopeFrame struct {
	old   map[string]string // name → 綁定前舊值
	had   map[string]bool   // name → 綁定前是否存在（區分舊值為空字串）
	order []string          // 綁定順序，Pop 時逆序還原
}

type scopeTable struct {
	frames []*scopeFrame
	flat   map[string]string
}

func newScopeTable(top map[string]string) *scopeTable {
	if top == nil {
		top = map[string]string{}
	}
	return &scopeTable{
		frames: []*scopeFrame{newScopeFrame()},
		flat:   top,
	}
}

func newScopeFrame() *scopeFrame {
	return &scopeFrame{
		old:   map[string]string{},
		had:   map[string]bool{},
		order: nil,
	}
}

// Push 開一層新幀。
func (s *scopeTable) Push() {
	s.frames = append(s.frames, newScopeFrame())
}

// Pop 丟掉最內層幀，並把該幀遮蔽的名字還原成綁定前的值。
func (s *scopeTable) Pop() {
	if len(s.frames) <= 1 {
		return
	}
	f := s.frames[len(s.frames)-1]
	for i := len(f.order) - 1; i >= 0; i-- {
		name := f.order[i]
		if f.had[name] {
			s.flat[name] = f.old[name]
		} else {
			delete(s.flat, name)
		}
	}
	s.frames = s.frames[:len(s.frames)-1]
}

// Bind 在目前幀宣告 name，同步寫入 flat（遮蔽外層同名）。
// 這就是 declare 語意：不看外層是否已有同名，直接覆寫並記錄舊值。
func (s *scopeTable) Bind(name, typ string) {
	if name == "" || len(s.frames) == 0 {
		return
	}
	f := s.frames[len(s.frames)-1]
	if !f.had[name] {
		f.had[name] = true
		f.old[name] = s.flat[name]
		f.order = append(f.order, name)
	}
	s.flat[name] = typ
}

// BoundHere 只問「目前幀是否宣告過 name」，用於显式 let 是否該登記的判定。
// 外層（含頂層）同名不算，因此函式內 `h0 u64` 能遮蔽頂層 `h0 u32`；
// 但同函式的結果參數已在該幀 Bind，仍會被攔住、不被 let 覆寫。
func (s *scopeTable) BoundHere(name string) bool {
	if len(s.frames) == 0 {
		return false
	}
	return s.frames[len(s.frames)-1].had[name]
}

// Resolve 由 flat 查最近可見綁定（flat 已由 Bind/Pop  maintains 遮蔽關係）。
func (s *scopeTable) Resolve(name string) (string, bool) {
	v, ok := s.flat[name]
	return v, ok
}

// Unbind 從目前幀與 flat 移除 name（對應原本 delete(varTypes, name)）。
func (s *scopeTable) Unbind(name string) {
	if len(s.frames) > 0 {
		f := s.frames[len(s.frames)-1]
		if !f.had[name] {
			f.had[name] = true
			f.old[name] = s.flat[name]
			f.order = append(f.order, name)
		}
	}
	delete(s.flat, name)
}

// Flat 回傳穩定的展平視圖（永不替換指標），供唯讀 helper 與遞迴使用。
func (s *scopeTable) Flat() map[string]string {
	return s.flat
}
