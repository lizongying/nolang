package mir

import (
	"fmt"
	"sort"
	"strings"
)

// String renders the whole module in a readable, debuggable textual form. It is
// used by the NOLANG_MIR verification path (dumped to a debug file) and by tests
// as an oracle for the lowering.
func (m *Module) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "module %q\n", m.Name)
	// types
	if len(m.Types) > 1 {
		b.WriteString("types:\n")
		for i := 1; i < len(m.Types); i++ {
			t := &m.Types[i]
			own := ""
			if t.Owned {
				own = " [owned]"
			}
			fmt.Fprintf(&b, "  %%t%d = %s%s\n", t.ID, t.Raw, own)
		}
	}
	// struct layouts with their DEFINITION-SITE field tags. The tag is what
	// decides the field's layout (Owned => pointer for a struct-typed field) and
	// its drop, so printing it here is what makes a layout change auditable.
	// Sorted: map iteration order must not leak into the dump.
	if len(m.StructFields) > 0 {
		names := make([]string, 0, len(m.StructFields))
		for name := range m.StructFields {
			names = append(names, name)
		}
		sort.Strings(names)
		b.WriteString("structs:\n")
		for _, name := range names {
			fmt.Fprintf(&b, "  %s\n", name)
			for _, f := range m.StructFields[name] {
				fmt.Fprintf(&b, "    %s %s [%s]\n", f.Name, f.TypeRaw, f.Tag)
			}
		}
	}
	for fi := range m.Funcs {
		f := &m.Funcs[fi]
		if f.IsExtern {
			fmt.Fprintf(&b, "\nextern %s\n", f.Name)
			continue
		}
		fmt.Fprintf(&b, "\nfn %s\n", f.Name)
		for _, bid := range f.Blocks {
			blk := m.Block(bid)
			if blk == nil {
				continue
			}
			fmt.Fprintf(&b, "block %q\n", blk.Name)
			for _, iid := range blk.Insts {
				inst := m.Inst(iid)
				if inst == nil {
					continue
				}
				b.WriteString("  ")
				b.WriteString(m.instStr(inst))
				b.WriteByte('\n')
			}
			if blk.Term != nil {
				b.WriteString("  ")
				b.WriteString(m.termStr(blk.Term))
				b.WriteByte('\n')
			}
		}
	}
	return b.String()
}

func (m *Module) instStr(inst *Inst) string {
	dst := ""
	if inst.Dst > NoVal {
		name := ""
		if v := m.Value(inst.Dst); v != nil {
			name = v.Name
		}
		if name != "" {
			dst = fmt.Sprintf("%%%d(%s) = ", inst.Dst, name)
		} else {
			dst = fmt.Sprintf("%%%d = ", inst.Dst)
		}
	}
	args := make([]string, 0, len(inst.Args))
	for _, a := range inst.Args {
		args = append(args, fmt.Sprintf("%%%d", a))
	}
	switch inst.Op {
	case OpConst:
		if inst.Type > NoType && m.Type(inst.Type) != nil && m.Type(inst.Type).Kind == KindFloat {
			return fmt.Sprintf("%sconst %s %v", dst, m.typeStr(inst.Type), inst.Flt)
		}
		if inst.Str != "" {
			return fmt.Sprintf("%sconst %s %q", dst, m.typeStr(inst.Type), inst.Str)
		}
		return fmt.Sprintf("%sconst %s %d", dst, m.typeStr(inst.Type), inst.Int)
	case OpCall, OpCallExtern, OpCallFFI:
		return fmt.Sprintf("%scall %s(%s)", dst, inst.Sym, strings.Join(args, ", "))
	case OpMove:
		if len(args) >= 2 {
			return fmt.Sprintf("%smove %s <- %s", dst, args[1], args[0])
		}
		return fmt.Sprintf("%smove %s <-", dst, strings.Join(args, ", "))
	case OpAlloc:
		return fmt.Sprintf("%salloc %s", dst, m.typeStr(inst.Type))
	default:
		sym := ""
		if inst.Sym != "" {
			sym = " " + inst.Sym
		}
		return fmt.Sprintf("%s%s%s(%s)", dst, inst.Op, sym, strings.Join(args, ", "))
	}
}

func (m *Module) termStr(t *Term) string {
	switch t.Op {
	case OpReturn:
		args := make([]string, 0, len(t.Args))
		for _, a := range t.Args {
			args = append(args, fmt.Sprintf("%%%d", a))
		}
		return fmt.Sprintf("return %s", strings.Join(args, ", "))
	case OpBr:
		if len(t.Targets) > 0 {
			return fmt.Sprintf("br block %d", t.Targets[0])
		}
		return "br <none>"
	case OpCondBr:
		args := make([]string, 0, len(t.Args))
		for _, a := range t.Args {
			args = append(args, fmt.Sprintf("%%%d", a))
		}
		if len(t.Targets) >= 2 {
			return fmt.Sprintf("cond-br %s then block %d else block %d", strings.Join(args, ","), t.Targets[0], t.Targets[1])
		}
		return fmt.Sprintf("cond-br %s (malformed targets)", strings.Join(args, ","))
	case OpSwitch:
		return fmt.Sprintf("switch %s", t.Sym)
	}
	return "terminator?"
}

func (m *Module) typeStr(t TypeID) string {
	if t <= NoType {
		return "< void >"
	}
	ty := m.Type(t)
	if ty == nil {
		return fmt.Sprintf("%%t%d?", t)
	}
	return ty.Raw
}
