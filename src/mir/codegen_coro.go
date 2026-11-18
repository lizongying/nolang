package mir

import (
	"fmt"
	"strings"

	"github.com/lizongying/nolang/i18n"
)

// Coroutine codegen.
//
// A function marked IsCoro (see coro.go) is emitted very differently from an
// ordinary one:
//
//	define void @f_async(i8* %task_ptr) {       ; the scheduler's signature
//	f_async.bb0:                                 ; the DISPATCH block
//	  %cs.raw = load i8*, i8** (<task>.frame)
//	  %cs     = bitcast i8* %cs.raw to %frame.N*
//	  %res.t  = ...                              ; the caller's result buffer
//	  %cs.0   = getelementptr %frame.N, %frame.N* %cs, i32 0, i32 2
//	  ...
//	  %cs.statep = getelementptr %frame.N, %frame.N* %cs, i32 0, i32 0
//	  %cs.st  = load i32, i32* %cs.statep
//	  %cs.z   = icmp eq i32 %cs.st, 0
//	  br i1 %cs.z, label %cs.init, label %cs.d0
//	cs.init:                                     ; zero the slots ONCE
//	  ...
//	  br label %f_async.bb1                      ; the original entry (state 0)
//	cs.d0:
//	  %cs.e0 = icmp eq i32 %cs.st, 1
//	  br i1 %cs.e0, label %<cont of state 1>, label %cs.d1
//	...
//
// Every block that ends at a suspension point terminates with OpCoroPark: an
// await stores the state, registers itself on the awaited task and returns;
// a yield stores the state, re-enqueues itself and returns. The continuation
// block then re-executes the OpAwait as "read the result that is now ready".
//
// The frame layout is { i32 __state, i32 __pad, <slot 0>, ... }, so slot i is
// field index i+CoroFrameHeader.

// emitCoroPrologue writes the frame pointer / result pointer setup and registers
// the frame type declaration. It runs right after the entry label, so `%cs`
// dominates every block of the function.
func (c *codegen) emitCoroPrologue(f *Function) {
	ci := f.Coro
	c.coroInit.Reset()
	c.coroFrameReg = "%cs"
	c.coroTaskReg = "%task_ptr"
	if !c.coroFrameDone[f.ID] {
		c.coroFrameDone[f.ID] = true
		fields := make([]string, 0, len(ci.Slots)+CoroFrameHeader)
		fields = append(fields, "i32", "i32")
		for _, v := range ci.Slots {
			lt, _ := c.ptypeIn(f.ID, v)
			if lt == "" || lt == "void" {
				// A void value never reaches allocaFor (it has no slot), but the
				// frame's field count must stay in step with the slot ordinals,
				// or the GEP indices computed below would land on the wrong
				// field. A byte of padding is the cheapest way to keep them
				// aligned.
				lt = "i8"
			}
			fields = append(fields, lt)
		}
		c.coroFrameDecls = append(c.coroFrameDecls, fmt.Sprintf("%%%s = type { %s }\n",
			ci.FrameName, strings.Join(fields, ", ")))
	}
	// %cs.raw = load i8*, i8** (task.frame)
	c.loadSeq++
	frameGep := fmt.Sprintf("%%cs.fg.%d", c.loadSeq)
	fmt.Fprintf(c.sb, "  %s = getelementptr inbounds %%task, %%task* %%task_ptr, i32 0, i32 5\n", frameGep)
	c.loadSeq++
	frameRaw := fmt.Sprintf("%%cs.raw.%d", c.loadSeq)
	fmt.Fprintf(c.sb, "  %s = load i8*, i8** %s\n", frameRaw, frameGep)
	fmt.Fprintf(c.sb, "  %s = bitcast i8* %s to %%%s*\n", c.coroFrameReg, frameRaw, ci.FrameName)
	// %cs.statep = &frame.__state
	fmt.Fprintf(c.sb, "  %s = getelementptr %%%s, %%%s* %s, i32 0, i32 0\n",
		"%cs.statep", ci.FrameName, ci.FrameName, c.coroFrameReg)
	c.coroStateReg = "%cs.statep"
	// The result pointer: the spawn site put it in the args struct's field 0,
	// and emitReturn writes it back through paramPtr exactly as for a normal
	// out-param.
	if len(f.ResultParams) > 0 {
		resLT, _ := c.ptypeIn(f.ID, f.ResultParams[0])
		if resLT == "" || resLT == "void" {
			resLT = "i64"
		}
		c.loadSeq++
		dg := fmt.Sprintf("%%cs.rdg.%d", c.loadSeq)
		fmt.Fprintf(c.sb, "  %s = getelementptr inbounds %%task, %%task* %%task_ptr, i32 0, i32 1\n", dg)
		c.loadSeq++
		di := fmt.Sprintf("%%cs.rd.%d", c.loadSeq)
		fmt.Fprintf(c.sb, "  %s = load i64, i64* %s\n", di, dg)
		c.loadSeq++
		dp := fmt.Sprintf("%%cs.rdp.%d", c.loadSeq)
		fmt.Fprintf(c.sb, "  %s = inttoptr i64 %s to i8*\n", dp, di)
		c.loadSeq++
		at := fmt.Sprintf("%%cs.rat.%d", c.loadSeq)
		fmt.Fprintf(c.sb, "  %s = bitcast i8* %s to { i8* }*\n", at, dp)
		c.loadSeq++
		pg := fmt.Sprintf("%%cs.rpg.%d", c.loadSeq)
		fmt.Fprintf(c.sb, "  %s = getelementptr inbounds { i8* }, { i8* }* %s, i32 0, i32 0\n", pg, at)
		c.loadSeq++
		rp := fmt.Sprintf("%%cs.rp.%d", c.loadSeq)
		fmt.Fprintf(c.sb, "  %s = load i8*, i8** %s\n", rp, pg)
		resReg := "%cs.res.t"
		fmt.Fprintf(c.sb, "  %s = bitcast i8* %s to %s*\n", resReg, rp, resLT)
		for _, rp2 := range f.ResultParams {
			c.paramPtr[rp2] = resReg
		}
		// A method's `self` IS its first result param: it keeps aliasing the
		// result buffer so in-body mutations propagate, exactly like the
		// non-coroutine path aliases it to the caller's out-pointer.
		if f.IsMethod && len(f.ResultParams) > 0 {
			c.valSlot[f.ResultParams[0]] = resReg
		}
	}
}

// emitCoroDispatch writes the state comparison chain that replaced the entry
// block's terminator.
func (c *codegen) emitCoroDispatch(f *Function) error {
	ci := f.Coro
	st := "%cs.st"
	c.loadSeq++
	stReg := fmt.Sprintf("%%cs.st.%d", c.loadSeq)
	fmt.Fprintf(c.sb, "  %s = load i32, i32* %s\n", stReg, c.coroStateReg)
	st = stReg

	initLabel := fmt.Sprintf("cs.init.%d", f.ID)
	c.loadSeq++
	zero := fmt.Sprintf("%%cs.z.%d", c.loadSeq)
	fmt.Fprintf(c.sb, "  %s = icmp eq i32 %s, 0\n", zero, st)
	fmt.Fprintf(c.sb, "  br i1 %s, label %%%s, label %%%s\n", zero, initLabel,
		fmt.Sprintf("cs.d0.%d", f.ID))

	// cs.init: the zero-initialisation of every frame slot. State 0 is the only
	// path that reaches a FRESH frame, so this is where it belongs: repeating
	// it on a resume would clobber the values the park saved.
	fmt.Fprintf(c.sb, "%s:\n", initLabel)
	if c.coroInit.Len() > 0 {
		c.sb.WriteString(c.coroInit.String())
	}
	fmt.Fprintf(c.sb, "  br label %%%s\n", c.labelFor[ci.Body])

	for i, p := range ci.States {
		fmt.Fprintf(c.sb, "cs.d%d.%d:\n", i, f.ID)
		c.loadSeq++
		eq := fmt.Sprintf("%%cs.e.%d", c.loadSeq)
		fmt.Fprintf(c.sb, "  %s = icmp eq i32 %s, %d\n", eq, st, p.State)
		fmt.Fprintf(c.sb, "  br i1 %s, label %%%s, label %%%s\n", eq,
			c.labelFor[p.Resume], fmt.Sprintf("cs.d%d.%d", i+1, f.ID))
	}
	// Fallthrough: an unknown state means a corrupt frame. Restart the body
	// rather than jumping nowhere — a wrong answer is easier to debug than
	// invalid IR.
	fmt.Fprintf(c.sb, "cs.d%d.%d:\n", len(ci.States), f.ID)
	fmt.Fprintf(c.sb, "  br label %%%s\n", c.labelFor[ci.Body])
	return nil
}

// emitCoroPark terminates a parking block.
//
// await:  if the child is already done (or the handle is null) fall through to
// the continuation; otherwise record the state, register this task as the
// child's waiter and return to whoever resumed us.
// yield:  record the state, re-enqueue ourselves and return, which is exactly
// "let another ready task run".
// chan:   if the channel is readable (or null, or closed, or already holding an
// element) fall through to the receive; otherwise record the state, link this
// task into the channel's waiter chain and return. The resume re-enters this
// very block rather than the receive, because a wake-up only means "something
// changed" — the element may have been taken by a task that was woken first.
func (c *codegen) emitCoroPark(f *Function, p CoroPark) error {
	cont := c.labelFor[p.Cont]
	park := fmt.Sprintf("cs.park%d.%d", p.State, f.ID)
	if p.Kind == CoroParkYield {
		fmt.Fprintf(c.sb, "  store i32 %d, i32* %s\n", p.State, c.coroStateReg)
		c.sb.WriteString("  call void @nolang_async_yield()\n")
		c.sb.WriteString("  ret void\n")
		return nil
	}
	if p.Kind == CoroParkChan {
		ch := "null"
		if p.Handle > NoVal {
			ch = c.chanHandle(p.Handle)
		}
		c.loadSeq++
		ready := fmt.Sprintf("%%cs.chr.%d", c.loadSeq)
		fmt.Fprintf(c.sb, "  %s = call i1 @nolang_chan_ready(i8* %s)\n", ready, ch)
		fmt.Fprintf(c.sb, "  br i1 %s, label %%%s, label %%%s\n", ready, cont, park)
		fmt.Fprintf(c.sb, "%s:\n", park)
		fmt.Fprintf(c.sb, "  store i32 %d, i32* %s\n", p.State, c.coroStateReg)
		fmt.Fprintf(c.sb, "  call void @nolang_chan_park(i8* %s)\n", ch)
		c.sb.WriteString("  ret void\n")
		return nil
	}
	// await
	chk := fmt.Sprintf("cs.chk%d.%d", p.State, f.ID)
	hT, hReg := "i64", "0"
	if p.Handle > NoVal {
		if lt, reg := c.loadVal(p.Handle); lt != "" && lt != "void" {
			hT, hReg = lt, reg
		}
	}
	if hT != "i64" {
		c.loadSeq++
		conv := fmt.Sprintf("%%cs.hcast.%d", c.loadSeq)
		fmt.Fprintf(c.sb, "  %s = ptrtoint %s %s to i64\n", conv, hT, hReg)
		hReg = conv
	}
	c.loadSeq++
	isNull := fmt.Sprintf("%%cs.hnull.%d", c.loadSeq)
	fmt.Fprintf(c.sb, "  %s = icmp eq i64 %s, 0\n", isNull, hReg)
	fmt.Fprintf(c.sb, "  br i1 %s, label %%%s, label %%%s\n", isNull, cont, chk)
	fmt.Fprintf(c.sb, "%s:\n", chk)
	c.loadSeq++
	taskI8 := fmt.Sprintf("%%cs.ht.%d", c.loadSeq)
	fmt.Fprintf(c.sb, "  %s = inttoptr i64 %s to i8*\n", taskI8, hReg)
	c.loadSeq++
	taskT := fmt.Sprintf("%%cs.htt.%d", c.loadSeq)
	fmt.Fprintf(c.sb, "  %s = bitcast i8* %s to %%task*\n", taskT, taskI8)
	c.loadSeq++
	doneGep := fmt.Sprintf("%%cs.hdg.%d", c.loadSeq)
	fmt.Fprintf(c.sb, "  %s = getelementptr inbounds %%task, %%task* %s, i32 0, i32 2\n", doneGep, taskT)
	c.loadSeq++
	doneVal := fmt.Sprintf("%%cs.hdv.%d", c.loadSeq)
	fmt.Fprintf(c.sb, "  %s = load i1, i1* %s\n", doneVal, doneGep)
	fmt.Fprintf(c.sb, "  br i1 %s, label %%%s, label %%%s\n", doneVal, cont, park)
	fmt.Fprintf(c.sb, "%s:\n", park)
	fmt.Fprintf(c.sb, "  store i32 %d, i32* %s\n", p.State, c.coroStateReg)
	fmt.Fprintf(c.sb, "  call void @nolang_async_wait(i8* %s)\n", taskI8)
	c.sb.WriteString("  ret void\n")
	return nil
}

// emitCoroAwaitRead emits OpAwait inside a coroutine: the parking (if any)
// already happened at the terminator of the preceding block, so all that is
// left is to lift the result out of the child's result buffer.
//
// A null handle is the "already awaited / never assigned" case; it yields a zero
// result instead of dereferencing null, mirroring the synchronous path's
// consumed-handle guard.
func (c *codegen) emitCoroAwaitRead(f *Function, inst *Inst) error {
	resLT, _ := c.ptype(inst.Dst)
	slot := c.valSlot[inst.Dst]
	if slot == "" {
		c.fail("OpAwait: no slot for destination")
		return i18n.Errorf("zcelvmmy", "OpAwait: no slot")
	}
	if len(inst.Args) == 0 {
		fmt.Fprintf(c.sb, "  store %s zeroinitializer, %s* %s\n", resLT, resLT, slot)
		return nil
	}
	_, hReg := c.loadVal(inst.Args[0])
	nullLbl := fmt.Sprintf("cs.awn%d.%d", inst.ID, f.ID)
	read := fmt.Sprintf("cs.awr%d.%d", inst.ID, f.ID)
	after := fmt.Sprintf("cs.awa%d.%d", inst.ID, f.ID)
	c.loadSeq++
	isNull := fmt.Sprintf("%%cs.awnul.%d", c.loadSeq)
	fmt.Fprintf(c.sb, "  %s = icmp eq i64 %s, 0\n", isNull, hReg)
	fmt.Fprintf(c.sb, "  br i1 %s, label %%%s, label %%%s\n", isNull, nullLbl, read)
	fmt.Fprintf(c.sb, "%s:\n", nullLbl)
	fmt.Fprintf(c.sb, "  store %s zeroinitializer, %s* %s\n", resLT, resLT, slot)
	fmt.Fprintf(c.sb, "  br label %%%s\n", after)
	fmt.Fprintf(c.sb, "%s:\n", read)
	c.loadSeq++
	taskI8 := fmt.Sprintf("%%cs.awt.%d", c.loadSeq)
	fmt.Fprintf(c.sb, "  %s = inttoptr i64 %s to i8*\n", taskI8, hReg)
	c.loadSeq++
	taskT := fmt.Sprintf("%%cs.awtt.%d", c.loadSeq)
	fmt.Fprintf(c.sb, "  %s = bitcast i8* %s to %%task*\n", taskT, taskI8)
	c.loadSeq++
	dataGep := fmt.Sprintf("%%cs.awdg.%d", c.loadSeq)
	fmt.Fprintf(c.sb, "  %s = getelementptr inbounds %%task, %%task* %s, i32 0, i32 1\n", dataGep, taskT)
	c.loadSeq++
	dataI64 := fmt.Sprintf("%%cs.awdi.%d", c.loadSeq)
	fmt.Fprintf(c.sb, "  %s = load i64, i64* %s\n", dataI64, dataGep)
	c.loadSeq++
	dataI8 := fmt.Sprintf("%%cs.awdp.%d", c.loadSeq)
	fmt.Fprintf(c.sb, "  %s = inttoptr i64 %s to i8*\n", dataI8, dataI64)
	c.loadSeq++
	argsT := fmt.Sprintf("%%cs.awat.%d", c.loadSeq)
	fmt.Fprintf(c.sb, "  %s = bitcast i8* %s to { i8* }*\n", argsT, dataI8)
	c.loadSeq++
	rpGep := fmt.Sprintf("%%cs.awrg.%d", c.loadSeq)
	fmt.Fprintf(c.sb, "  %s = getelementptr inbounds { i8* }, { i8* }* %s, i32 0, i32 0\n", rpGep, argsT)
	c.loadSeq++
	rp := fmt.Sprintf("%%cs.awrp.%d", c.loadSeq)
	fmt.Fprintf(c.sb, "  %s = load i8*, i8** %s\n", rp, rpGep)
	c.loadSeq++
	resT := fmt.Sprintf("%%cs.awrs.%d", c.loadSeq)
	fmt.Fprintf(c.sb, "  %s = bitcast i8* %s to %s*\n", resT, rp, resLT)
	c.loadSeq++
	resV := fmt.Sprintf("%%cs.awv.%d", c.loadSeq)
	fmt.Fprintf(c.sb, "  %s = load %s, %s* %s\n", resV, resLT, resLT, resT)
	fmt.Fprintf(c.sb, "  store %s %s, %s* %s\n", resLT, resV, resLT, slot)
	// Release the handle's reference. Done BEFORE the null branch closes so both
	// paths converge on `after` with the slot already written.
	fmt.Fprintf(c.sb, "  call void @nolang_task_retire(i8* %s)\n", taskI8)
	fmt.Fprintf(c.sb, "  br label %%%s\n", after)
	fmt.Fprintf(c.sb, "%s:\n", after)
	return nil
}

// emitCoroReturn closes a coroutine: free the parameter payloads the spawn
// boundary handed to the frame, mark the task done and wake its waiter.
//
// The parameter free must mirror the wrapper's w_free exactly — every argument
// that arrived with a classified kind is owned by the frame after the spawn
// (moved arguments transferred ownership, retained ones took a second
// reference, plain ones were deep-copied), so all three are released here and
// nowhere else.
func (c *codegen) emitCoroReturn(f *Function) error {
	kinds, _, _ := c.asyncArgKinds(f.ID, f)
	ki := 0
	for _, p := range f.Params {
		if c.resultParam[p] {
			continue
		}
		kind := ""
		if ki < len(kinds) {
			kind = kinds[ki]
		}
		ki++
		if kind == "" {
			continue
		}
		slot := c.valSlot[p]
		if slot == "" {
			continue
		}
		lt, _ := c.ptypeIn(f.ID, p)
		switch kind {
		case "str", "vec":
			if (kind == "str" && lt != "%str-long") || (kind == "vec" && lt != "%vec") {
				continue
			}
			c.loadSeq++
			desc := fmt.Sprintf("%%cs.pd.%d", c.loadSeq)
			fmt.Fprintf(c.sb, "  %s = load %s, %s* %s\n", desc, lt, lt, slot)
			fmt.Fprintf(c.sb, "  call void @%s_free(%s %s)\n", kind, lt, desc)
		case "struct":
			key := c.structKeyOfLLVM(lt)
			if key == "" || (!c.mod.StructHasPtrFields(key) && !c.mod.StructHasOwnedLeafFields(key)) {
				continue
			}
			c.emitStructDropHelper(lt, key)
			fmt.Fprintf(c.sb, "  call void @%s(%s* %s)\n", structDropName(lt), lt, slot)
		}
	}
	// done = true, then wake whoever is blocked on us.
	c.loadSeq++
	doneGep := fmt.Sprintf("%%cs.doneg.%d", c.loadSeq)
	fmt.Fprintf(c.sb, "  %s = getelementptr inbounds %%task, %%task* %%task_ptr, i32 0, i32 2\n", doneGep)
	fmt.Fprintf(c.sb, "  store i1 true, i1* %s\n", doneGep)
	c.sb.WriteString("  call void @nolang_async_done(i8* %task_ptr)\n")
	c.sb.WriteString("  ret void\n")
	return nil
}

// emitCoroRun is emitAsyncRun's coroutine branch: allocate the frame, fill its
// parameter slots with the (already coerced and deep-copied) arguments, install
// the body itself as the task's resume function and enqueue.
//
// There is no wrapper: the body IS the state machine, and it returns as soon as
// it parks, so the scheduler can call it again to resume.
func (c *codegen) emitCoroRun(f *Function, inst *Inst, cid FuncID, cf *Function) error {
	calleeName := inst.Sym
	targetName := c.fname[cid]
	ci := cf.Coro

	isResult := map[ValueID]bool{}
	for _, rp := range cf.ResultParams {
		isResult[rp] = true
	}
	var argTypes []string
	var argParams []ValueID
	for _, p := range cf.Params {
		if isResult[p] {
			continue
		}
		lt, _ := c.ptypeIn(cid, p)
		argTypes = append(argTypes, lt)
		argParams = append(argParams, p)
	}
	argKinds, argElems, argKeys := c.asyncArgKinds(cid, cf)
	resLT := "i64"
	if len(cf.ResultParams) > 0 {
		if lt, _ := c.ptypeIn(cid, cf.ResultParams[0]); lt != "" {
			resLT = lt
		}
	}

	// 1. result buffer (heap), zero-initialized so an owned payload slot is valid.
	c.loadSeq++
	resBuf := fmt.Sprintf("%%cr.resbuf.%d", c.loadSeq)
	fmt.Fprintf(c.sb, "  %s = call i8* @nolang_rc_alloc(i64 %d)\n", resBuf, c.mallocBytesFor(resLT))
	c.loadSeq++
	resBufT := fmt.Sprintf("%%cr.resbuf.t.%d", c.loadSeq)
	fmt.Fprintf(c.sb, "  %s = bitcast i8* %s to %s*\n", resBufT, resBuf, resLT)
	fmt.Fprintf(c.sb, "  store %s zeroinitializer, %s* %s\n", resLT, resLT, resBufT)

	// 2. args struct: ONE field, the result pointer. The parameters do not go
	// through it any more — the frame owns them.
	c.loadSeq++
	argsStruct := fmt.Sprintf("%%cr.args.%d", c.loadSeq)
	fmt.Fprintf(c.sb, "  %s = call i8* @nolang_rc_alloc(i64 8)\n", argsStruct)
	c.loadSeq++
	argsT := fmt.Sprintf("%%cr.args.t.%d", c.loadSeq)
	fmt.Fprintf(c.sb, "  %s = bitcast i8* %s to { i8* }*\n", argsT, argsStruct)
	c.loadSeq++
	resI8 := fmt.Sprintf("%%cr.resi8.%d", c.loadSeq)
	fmt.Fprintf(c.sb, "  %s = bitcast %s* %s to i8*\n", resI8, resLT, resBufT)
	c.loadSeq++
	f0 := fmt.Sprintf("%%cr.f0.%d", c.loadSeq)
	fmt.Fprintf(c.sb, "  %s = getelementptr inbounds { i8* }, { i8* }* %s, i32 0, i32 0\n", f0, argsT)
	fmt.Fprintf(c.sb, "  store i8* %s, i8** %s\n", resI8, f0)
	c.loadSeq++
	argsI64 := fmt.Sprintf("%%cr.argsi64.%d", c.loadSeq)
	fmt.Fprintf(c.sb, "  %s = ptrtoint i8* %s to i64\n", argsI64, argsStruct)

	// 3. task (64 bytes: resume_fn, data, done, cancelled, waiters, frame,
	// next, waitnext, depth, pad). depth inherits from @nolang_current_task —
	// a recursively spawning coroutine must abort before it allocates another
	// frame, not after it has exhausted the heap.
	c.loadSeq++
	depthVal := c.emitSpawnDepthCheck()
	c.loadSeq++
	taskBuf := fmt.Sprintf("%%cr.task.%d", c.loadSeq)
	c.sb.WriteString("  " + taskBuf + " = call i8* @nolang_rc_alloc(i64 72)\n")
	c.loadSeq++
	taskT := fmt.Sprintf("%%cr.task.t.%d", c.loadSeq)
	fmt.Fprintf(c.sb, "  %s = bitcast i8* %s to %%task*\n", taskT, taskBuf)

	// 4. frame (heap), __state = 0.
	c.loadSeq++
	frameSize := fmt.Sprintf("%%cr.fsz.%d", c.loadSeq)
	fmt.Fprintf(c.sb, "  %s = ptrtoint %%%s* getelementptr (%%%s, %%%s* null, i32 1) to i64\n",
		frameSize, ci.FrameName, ci.FrameName, ci.FrameName)
	c.loadSeq++
	frameRaw := fmt.Sprintf("%%cr.frame.%d", c.loadSeq)
	fmt.Fprintf(c.sb, "  %s = call i8* @nolang_rc_alloc(i64 %s)\n", frameRaw, frameSize)
	c.loadSeq++
	frameT := fmt.Sprintf("%%cr.frame.t.%d", c.loadSeq)
	fmt.Fprintf(c.sb, "  %s = bitcast i8* %s to %%%s*\n", frameT, frameRaw, ci.FrameName)
	c.loadSeq++
	stateGep := fmt.Sprintf("%%cr.state.%d", c.loadSeq)
	fmt.Fprintf(c.sb, "  %s = getelementptr %%%s, %%%s* %s, i32 0, i32 0\n", stateGep, ci.FrameName, ci.FrameName, frameT)
	fmt.Fprintf(c.sb, "  store i32 0, i32* %s\n", stateGep)
	// task.frame = frame
	c.loadSeq++
	ffGep := fmt.Sprintf("%%cr.ff.%d", c.loadSeq)
	fmt.Fprintf(c.sb, "  %s = getelementptr inbounds %%task, %%task* %s, i32 0, i32 5\n", ffGep, taskT)
	fmt.Fprintf(c.sb, "  store i8* %s, i8** %s\n", frameRaw, ffGep)
	// task.resume_fn = the body itself
	c.loadSeq++
	fnGep := fmt.Sprintf("%%cr.fn.%d", c.loadSeq)
	fmt.Fprintf(c.sb, "  %s = getelementptr inbounds %%task, %%task* %s, i32 0, i32 0\n", fnGep, taskT)
	fmt.Fprintf(c.sb, "  store void (i8*)* @%s, void (i8*)** %s\n", targetName, fnGep)
	// task.data = args struct
	c.loadSeq++
	dGep := fmt.Sprintf("%%cr.dt.%d", c.loadSeq)
	fmt.Fprintf(c.sb, "  %s = getelementptr inbounds %%task, %%task* %s, i32 0, i32 1\n", dGep, taskT)
	fmt.Fprintf(c.sb, "  store i64 %s, i64* %s\n", argsI64, dGep)
	// done / cancelled / waiters / next / waitnext. Field 7 must be nulled here
	// rather than left to the first push: @nolang_async_done walks that link
	// when this task is awaited, and @nolang_rc_alloc does not zero.
	for _, idx := range []struct {
		n int
		v string
		t string
	}{{2, "false", "i1"}, {3, "false", "i1"}, {4, "null", "i8*"}, {6, "null", "i8*"}, {7, "null", "i8*"}} {
		c.loadSeq++
		g := fmt.Sprintf("%%cr.f%d.%d", idx.n, c.loadSeq)
		fmt.Fprintf(c.sb, "  %s = getelementptr inbounds %%task, %%task* %s, i32 0, i32 %d\n", g, taskT, idx.n)
		fmt.Fprintf(c.sb, "  store %s %s, %s* %s\n", idx.t, idx.v, idx.t, g)
	}
	// field 8 = inherited logical depth, field 9 = explicit pad (rc_alloc
	// does not zero).
	c.loadSeq++
	dg := fmt.Sprintf("%%cr.fdepth.%d", c.loadSeq)
	fmt.Fprintf(c.sb, "  %s = getelementptr inbounds %%task, %%task* %s, i32 0, i32 8\n", dg, taskT)
	fmt.Fprintf(c.sb, "  store i32 %s, i32* %s\n", depthVal, dg)
	c.loadSeq++
	pg := fmt.Sprintf("%%cr.fpad.%d", c.loadSeq)
	fmt.Fprintf(c.sb, "  %s = getelementptr inbounds %%task, %%task* %s, i32 0, i32 9\n", pg, taskT)
	fmt.Fprintf(c.sb, "  store i32 0, i32* %s\n", pg)
	// field 10 = join_top: a freshly spawned task owns no join-set frame yet.
	c.loadSeq++
	jg := fmt.Sprintf("%%cr.fjoin.%d", c.loadSeq)
	fmt.Fprintf(c.sb, "  %s = getelementptr inbounds %%task, %%task* %s, i32 0, i32 10\n", jg, taskT)
	fmt.Fprintf(c.sb, "  store i8* null, i8** %s\n", jg)

	// 5. parameter slots. The deep-copy protocol is the same one emitAsyncRun
	// applies to its per-argument heap buffers; only the destination changed.
	for i, av := range inst.Args {
		if i >= len(argParams) {
			break
		}
		plt := argTypes[i]
		alt, areg, owned := c.coerceAsyncArg(av, plt)
		if alt == "void" {
			c.fail("OpRun: argument %d of %s lowered to void in func %s", i, calleeName, f.Name)
			return i18n.Errorf("x57xvqny", "unsupported value type %s in func %s", alt, f.Name)
		}
		kind := ""
		if i < len(argKinds) {
			kind = argKinds[i]
		}
		moved := c.mod.spawnArgMoves[av] && alt == plt && kind != ""
		retained := c.mod.spawnArgRetains[av] && alt == plt && kind != ""
		share := moved || retained
		if retained {
			c.emitSpawnArgRetain(kind, alt, areg)
		}
		structKey := ""
		if i < len(argKeys) {
			structKey = argKeys[i]
		}
		if !share && kind != "" && kind != "struct" && !owned {
			wantLT := "%str-long"
			if kind == "vec" {
				wantLT = "%vec"
			}
			if alt == wantLT {
				elem := NoType
				if i < len(argElems) {
					elem = argElems[i]
				}
				if cp := c.asyncArgOwnedCopy(kind, areg, elem); cp != "" {
					areg = cp
				}
			}
		}
		srcSlot := ""
		if !share && structKey != "" && alt == plt {
			c.loadSeq++
			srcSlot = fmt.Sprintf("%%cr.srcslot.%d_%d", c.loadSeq, i)
			fmt.Fprintf(c.sb, "  %s = alloca %s\n", srcSlot, alt)
			fmt.Fprintf(c.sb, "  store %s %s, %s* %s\n", alt, areg, alt, srcSlot)
		}
		// destination: the frame slot of parameter i
		ord := ci.SlotIdx[argParams[i]]
		c.loadSeq++
		dst := fmt.Sprintf("%%cr.pdst.%d_%d", c.loadSeq, i)
		fmt.Fprintf(c.sb, "  %s = getelementptr %%%s, %%%s* %s, i32 0, i32 %d\n",
			dst, ci.FrameName, ci.FrameName, frameT, ord+CoroFrameHeader)
		fmt.Fprintf(c.sb, "  store %s %s, %s* %s\n", alt, areg, alt, dst)
		if srcSlot != "" {
			if c.mod.StructHasPtrFields(structKey) {
				c.emitPtrFieldsClone(dst, srcSlot, alt, structKey, map[string]bool{})
			}
			if c.mod.StructHasOwnedLeafFields(structKey) {
				c.emitLeafFieldsClone(dst, alt, structKey)
			}
		}
	}

	// 6. enqueue: the scheduler is the only thing that ever runs this task, and
	// enqueue takes its own reference so a queued task cannot be freed by an
	// awaiter that gets there first.
	c.loadSeq++
	taskI8 := fmt.Sprintf("%%cr.ti8.%d", c.loadSeq)
	fmt.Fprintf(c.sb, "  %s = bitcast %%task* %s to i8*\n", taskI8, taskT)
	fmt.Fprintf(c.sb, "  call void @nolang_async_enqueue(i8* %s)\n", taskI8)

	slot := c.valSlot[inst.Dst]
	c.loadSeq++
	handleI64 := fmt.Sprintf("%%cr.handle.%d", c.loadSeq)
	fmt.Fprintf(c.sb, "  %s = ptrtoint i8* %s to i64\n", handleI64, taskI8)
	if slot == "" {
		return i18n.Errorf("coronowl", "OpRun: no slot")
	}
	fmt.Fprintf(c.sb, "  store i64 %s, i64* %s\n", handleI64, slot)
	return nil
}
