package mir

import "fmt"

// ValidationError describes a structural defect in the MIR (independent of the
// memory-safety diagnostics produced by Analyze).
type ValidationError struct {
	Msg string
}

func (e ValidationError) Error() string { return e.Msg }

// Validate performs structural checks: every block has exactly one terminator,
// every operand references a valid value, every instruction belongs to a known
// block, and no value is defined twice. It returns a nil slice when the module
// is well formed.
func (m *Module) Validate() []ValidationError {
	var errs []ValidationError
	dstDef := map[ValueID]InstID{}

	for _, f := range m.Funcs {
		if f.IsExtern {
			continue
		}
		for _, bid := range f.Blocks {
			blk := m.Block(bid)
			if blk == nil {
				errs = append(errs, ValidationError{fmt.Sprintf("func %s: block %d missing", f.Name, bid)})
				continue
			}
			if blk.Term == nil {
				errs = append(errs, ValidationError{fmt.Sprintf("func %s: block %q has no terminator", f.Name, blk.Name)})
			}
			for _, iid := range blk.Insts {
				inst := m.Inst(iid)
				if inst == nil {
					errs = append(errs, ValidationError{fmt.Sprintf("func %s: block %q references missing inst %d", f.Name, blk.Name, iid)})
					continue
				}
				if inst.Block != bid {
					errs = append(errs, ValidationError{fmt.Sprintf("func %s: inst %d claims block %d but lives in %d", f.Name, iid, inst.Block, bid)})
				}
				for _, a := range inst.Args {
					if a < 0 || int(a) >= len(m.Values) {
						errs = append(errs, ValidationError{fmt.Sprintf("func %s: inst %d references invalid value %d", f.Name, iid, a)})
					}
				}
				if inst.Dst > NoVal {
					if prev, ok := dstDef[inst.Dst]; ok {
						errs = append(errs, ValidationError{fmt.Sprintf("func %s: value %d defined twice (inst %d and %d)", f.Name, inst.Dst, prev, iid)})
					}
					dstDef[inst.Dst] = iid
				}
			}
		}
	}
	return errs
}
