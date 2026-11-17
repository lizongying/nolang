package mir

import "fmt"

// codegen_chan.go — `chan[T]` 的运行时代码生成。
//
// 句柄是一个不透明的 i8*：指向堆上的控制块 %chancb。元素宽度存在控制块里
// （elemsize），所以 LLVM 侧的 chan 类型与 T 无关，send/recv 一律按字节搬。
//
// 控制块（48 字节）：
//
//	field 0  len       当前排队元素数
//	field 1  cap       当前缓冲容量（初始容量按需倍增；send 永不阻塞）
//	field 2  head      环形缓冲读游标
//	field 3  elemsize  元素字节宽度
//	field 4  buf       环形缓冲（i8*，cap*elemsize 字节）
//	field 5  closed    关闭标志（1 = 已关闭；close 幂等）
//
// 读语义（三态，codegen 直接折成 ?T 的 tag）：
//
//	0 = ok        取到一个元素
//	1 = closed    已关闭且排空
//	2 = empty     打开但空（单线程顺序模型下等价于"暂时没有数据"）
//	              未初始化的通道（零值句柄）也折成 2
//
// 没有阻塞、没有等待队列：`co`/`awy` 目前是"组尾顺序跑完"的模型，
// 无法挂起协程，通道因此定义为可增长队列（见 docs/docs/lang/syntax.md §chan）。

// chanControlBlock 字段偏移（字节），供 codegen 与测试共用。
const (
	chanFieldLen      = 0
	chanFieldCap      = 1
	chanFieldHead     = 2
	chanFieldElemSize = 3
	chanFieldBuf      = 4
	chanFieldClosed   = 5
)

// chanStatus* 是 @nolang_chan_recv 的返回码。它们与 ?T 的 tag 取值一致
// （0 ok / 1 nil / 2 err），codegen 因此可以把返回值直接当判别式写入 option，
// 而「未初始化的通道」折进 err（打开但空）—— 单线程顺序模型下两者对使用者
// 是同一件事：没有值可取。
const (
	chanStatusOK     = 0
	chanStatusClosed = 1
	chanStatusEmpty  = 2
)

// chanElemLTOf 返回通道载荷的 LLVM 类型（`chan[i64]` → i64）。
func (c *codegen) chanElemLTOf(v ValueID) string {
	if t := c.mirTypeOfValue(v); t != nil && t.Kind == KindChan && t.Elem != NoType {
		if et := c.mod.Type(t.Elem); et != nil {
			return c.llvmTypeOf(et)
		}
	}
	return "i64"
}

// chanHandle 把 MIR 值和它的 i8* 句柄取出来。
//
// MIR 侧的通道型别是 i64（见 llvmTypeOf 的 KindChan 分支），指針只在调用运行期
// 函数的这一处出现，所以这里做 inttoptr。
func (c *codegen) chanHandle(v ValueID) string {
	_, val := c.loadVal(v)
	c.loadSeq++
	h := fmt.Sprintf("%%chh.%d", c.loadSeq)
	fmt.Fprintf(c.sb, "  %s = inttoptr i64 %s to i8*\n", h, val)
	return h
}

// emitChanMake lowers OpChanMake: allocate a control block with the requested
// initial capacity (grown later on demand) and store the handle.
func (c *codegen) emitChanMake(inst *Inst) error {
	_, capV := c.loadVal(inst.Args[0])
	return c.emitChanMakeCap(inst, capV)
}

// emitChanMakeCap is the shared body, for the `with-cap(n)` builtin whose size
// operand has already been peeled out of a possible %option.
func (c *codegen) emitChanMakeCap(inst *Inst, capV string) error {
	slot := c.valSlot[inst.Dst]
	if slot == "" {
		return fmt.Errorf("chan-make: no result slot")
	}
	elemLT := c.chanElemLTOf(inst.Dst)
	elemSize := c.typeSizeOperand(elemLT)
	c.loadSeq++
	h := fmt.Sprintf("%%chnew.%d", c.loadSeq)
	fmt.Fprintf(c.sb, "  %s = call i8* @nolang_chan_new(i64 %s, i64 %s)\n", h, elemSize, capV)
	c.loadSeq++
	hi := fmt.Sprintf("%%chnewi.%d", c.loadSeq)
	fmt.Fprintf(c.sb, "  %s = ptrtoint i8* %s to i64\n", hi, h)
	fmt.Fprintf(c.sb, "  store i64 %s, i64* %s\n", hi, slot)
	return nil
}

// emitChanSend lowers OpChanSend: enqueue a copy of Args[1] into Args[0].
//
// The copy is DEEP, and it is made HERE rather than in the lowerer. A `str` /
// `[]T` descriptor owns its buffer, so the ring cannot just hold a second
// descriptor over the sender's bytes — the sender drops its own value at scope
// exit and the ring would then point at freed memory (measured: `c2 <- 'a'`
// followed by a receive printed the replacement character). Cloning in the
// lowerer instead does not work either: the clone is an ordinary owned local of
// that block, so the scope-exit drop frees the very buffer the ring kept (the
// emitted IR showed `%cl24 = call @str_clone` immediately followed by
// `call void @str_free(... %v17.s)` before the loop even started).
//
// Cloning inside codegen keeps the ownership split clean: the sender owns and
// drops its value, the ring owns an independent copy, and the receiver takes
// that copy over on recv.
func (c *codegen) emitChanSend(inst *Inst) error {
	ch := c.chanHandle(inst.Args[0])
	lt, val := c.loadVal(inst.Args[1])
	if lt == "" || lt == "void" {
		return fmt.Errorf("chan-send: the payload has no value (type %q)", lt)
	}
	c.loadSeq++
	tmp := fmt.Sprintf("%%chsd.%d", c.loadSeq)
	fmt.Fprintf(c.sb, "  %s = alloca %s\n", tmp, lt)
	switch {
	case lt == "%str-long":
		c.loadSeq++
		cl := fmt.Sprintf("%%chsc.%d", c.loadSeq)
		fmt.Fprintf(c.sb, "  %s = call %%str-long @str_clone(%%str-long %s)\n", cl, val)
		val = cl
	case lt == "%vec":
		// The element type decides how deep the copy must go (a []str needs a
		// per-element @str_clone), so the helper is specialised per element.
		elemT := NoType
		if t := c.mirTypeOfValue(inst.Args[1]); t != nil {
			elemT = t.Elem
		}
		if elemT != NoType {
			if fn := c.vecDeepClone(elemT, 0); fn != "" {
				c.loadSeq++
				cl := fmt.Sprintf("%%chvc.%d", c.loadSeq)
				fmt.Fprintf(c.sb, "  %s = call %%vec %s(%%vec %s)\n", cl, fn, val)
				val = cl
			}
		}
	}
	// Scalars, POD structs and enums have no heap to share: the descriptor copy
	// emitted below is the whole transfer.
	fmt.Fprintf(c.sb, "  store %s %s, %s* %s\n", lt, val, lt, tmp)
	c.loadSeq++
	addr := fmt.Sprintf("%%chsa.%d", c.loadSeq)
	fmt.Fprintf(c.sb, "  %s = bitcast %s* %s to i8*\n", addr, lt, tmp)
	fmt.Fprintf(c.sb, "  call void @nolang_chan_send(i8* %s, i8* %s)\n", ch, addr)
	return nil
}

// emitChanRecv lowers OpChanRecv.
//
//	inst.Dst       = the ?T the limb binds (KindOption over the element type)
//	inst.Args[0]   = the channel handle
//	inst.Results[1] (optional) = a bare i64 that receives the raw status
//
// The two results exist because the loop back-edge needs the RAW status
// ("did this round produce a value?") while the body needs the three-state
// option. Folding both into one instruction keeps the element scratch private
// to codegen: the payload is memcpy'd out of the ring into a stack temporary and
// then moved into the option, so a `?str` pays exactly one copy of the
// descriptor and no clone — the ring slot is dead after recv, which makes the
// transfer a move rather than an aliasing bug.
func (c *codegen) emitChanRecv(inst *Inst, allocaFor func(ValueID) string) error {
	optSlot := c.valSlot[inst.Dst]
	if optSlot == "" {
		return fmt.Errorf("chan-recv: no result slot")
	}
	// Results[1] is a second result value, so the prologue's Dst sweep does not
	// see it — its slot must be requested explicitly before it is stored into.
	// Without this the store is skipped and the loop back-edge reads undef.
	var statusSlot string
	if len(inst.Results) > 1 && inst.Results[1] > NoVal {
		statusSlot = allocaFor(inst.Results[1])
	}
	payloadLT := c.optPayloadLTOf(inst.Dst)
	if payloadLT == "" {
		return fmt.Errorf("chan-recv: cannot resolve the payload type of the destination option")
	}
	ch := c.chanHandle(inst.Args[0])
	c.loadSeq++
	scratch := fmt.Sprintf("%%chsc.%d", c.loadSeq)
	fmt.Fprintf(c.sb, "  %s = alloca %s\n", scratch, payloadLT)
	c.loadSeq++
	scratchI8 := fmt.Sprintf("%%chscp.%d", c.loadSeq)
	fmt.Fprintf(c.sb, "  %s = bitcast %s* %s to i8*\n", scratchI8, payloadLT, scratch)
	c.loadSeq++
	st := fmt.Sprintf("%%chst.%d", c.loadSeq)
	fmt.Fprintf(c.sb, "  %s = call i64 @nolang_chan_recv(i8* %s, i8* %s)\n", st, ch, scratchI8)

	// Discriminant: the runtime's status IS the option tag (0 ok, 1 closed,
	// 2 open-but-empty). Zero the payload area first so a `nil` / `err` option
	// never exposes the previous round's bytes, then overwrite the tag.
	c.optStoreTag(optSlot, payloadLT, 0)
	c.optStoreTagValue(optSlot, payloadLT, st)

	c.loadSeq++
	isok := fmt.Sprintf("%%chok.%d", c.loadSeq)
	fmt.Fprintf(c.sb, "  %s = icmp eq i64 %s, 0\n", isok, st)
	c.loadSeq++
	okBlk := fmt.Sprintf("chrok.%d", c.loadSeq)
	c.loadSeq++
	doneBlk := fmt.Sprintf("chrdn.%d", c.loadSeq)
	fmt.Fprintf(c.sb, "  br i1 %s, label %%%s, label %%%s\n", isok, okBlk, doneBlk)
	fmt.Fprintf(c.sb, "%s:\n", okBlk)
	if c.optionPayloadInline(payloadLT) {
		c.loadSeq++
		v := fmt.Sprintf("%%chv.%d", c.loadSeq)
		fmt.Fprintf(c.sb, "  %s = load %s, %s* %s\n", v, payloadLT, payloadLT, scratch)
		c.optStoreInlinePayload(optSlot, payloadLT, payloadLT, v)
	} else {
		// Payload bigger than the inline slot: box it. NOTE there is deliberately
		// no deep clone here (unlike optStoreBoxedPayload): the bytes in the ring
		// slot are being MOVED out, so the box takes over the references the ring
		// held. Cloning would leak the originals.
		sz := c.typeSizeOperand(payloadLT)
		c.loadSeq++
		box := fmt.Sprintf("%%chbox.%d", c.loadSeq)
		fmt.Fprintf(c.sb, "  %s = call i8* @nolang_rc_alloc(i64 %s)\n", box, sz)
		c.emitMemcpy(box, scratchI8, sz)
		c.loadSeq++
		pi := fmt.Sprintf("%%chboxi.%d", c.loadSeq)
		fmt.Fprintf(c.sb, "  %s = ptrtoint i8* %s to i64\n", pi, box)
		c.loadSeq++
		p0 := fmt.Sprintf("%%chbox0.%d", c.loadSeq)
		fmt.Fprintf(c.sb, "  %s = bitcast i8* %s to i64*\n", p0, c.optPayloadI8(optSlot, payloadLT))
		fmt.Fprintf(c.sb, "  store i64 %s, i64* %s\n", pi, p0)
	}
	fmt.Fprintf(c.sb, "  br label %%%s\n", doneBlk)
	fmt.Fprintf(c.sb, "%s:\n", doneBlk)

	if statusSlot != "" {
		fmt.Fprintf(c.sb, "  store i64 %s, i64* %s\n", st, statusSlot)
	}
	return nil
}

// emitChanClose lowers OpChanClose (idempotent).
func (c *codegen) emitChanClose(inst *Inst) error {
	ch := c.chanHandle(inst.Args[0])
	fmt.Fprintf(c.sb, "  call void @nolang_chan_close(i8* %s)\n", ch)
	return nil
}

// emitBuiltinChanMethod lowers the three channel methods registered in
// src/builtin/chan.go. They are ForwardFunc builtins (not opcodes) because
// `.len()` / `.cap()` / `.close()` are ordinary method calls in the surface
// syntax; routing them through the same table as str/vec methods keeps the
// argument checking, LSP signature and result typing in one place.
func (c *codegen) emitBuiltinChanMethod(inst *Inst, ff string) error {
	if len(inst.Args) == 0 {
		return fmt.Errorf("%s: missing receiver", ff)
	}
	ch := c.chanHandle(inst.Args[0])
	if ff == "chan-close" {
		fmt.Fprintf(c.sb, "  call void @nolang_chan_close(i8* %s)\n", ch)
		return nil
	}
	fn := "nolang_chan_len"
	if ff == "chan-cap" {
		fn = "nolang_chan_cap"
	}
	slot := c.valSlot[inst.Dst]
	if slot == "" {
		return fmt.Errorf("%s: no result slot", ff)
	}
	c.loadSeq++
	r := fmt.Sprintf("%%chm.%d", c.loadSeq)
	fmt.Fprintf(c.sb, "  %s = call i64 @%s(i8* %s)\n", r, fn, ch)
	fmt.Fprintf(c.sb, "  store i64 %s, i64* %s\n", r, slot)
	return nil
}

// emitChanRuntime appends the channel runtime: control block, panic helper, and
// the new/send/recv/close/free/len/cap entry points. Mirrors emitAsyncScheduler
// (hand-written textual IR appended to extraGlobals).
func (c *codegen) emitChanRuntime() {
	var b string

	b += `
; ---- channel runtime (chan[T]) ----
; control block: len, cap, head, elemsize, buf, closed  (48 bytes)
%chancb = type { i64, i64, i64, i64, i8*, i64 }

@nolang_chan_msg_send = private unnamed_addr constant [41 x i8] c"nolang: send on a closed or nil channel\0A\00"

declare void @exit(i32)

; nolang_chan_panic(): write the fixed diagnostic to stderr and exit(1).
define void @nolang_chan_panic() {
entry:
  %p = getelementptr inbounds [41 x i8], [41 x i8]* @nolang_chan_msg_send, i64 0, i64 0
  %n = call i64 @write(i32 2, i8* %p, i64 40)
  call void @exit(i32 1)
  unreachable
}

define i8* @nolang_chan_new(i64 %elemsize, i64 %capin) {
entry:
  %raw = call i8* @nolang_rc_alloc(i64 48)
  %cb = bitcast i8* %raw to %chancb*
  %f0 = getelementptr inbounds %chancb, %chancb* %cb, i32 0, i32 0
  store i64 0, i64* %f0
  %f1 = getelementptr inbounds %chancb, %chancb* %cb, i32 0, i32 1
  store i64 %capin, i64* %f1
  %f2 = getelementptr inbounds %chancb, %chancb* %cb, i32 0, i32 2
  store i64 0, i64* %f2
  %f3 = getelementptr inbounds %chancb, %chancb* %cb, i32 0, i32 3
  store i64 %elemsize, i64* %f3
  %f4 = getelementptr inbounds %chancb, %chancb* %cb, i32 0, i32 4
  store i8* null, i8** %f4
  %f5 = getelementptr inbounds %chancb, %chancb* %cb, i32 0, i32 5
  store i64 0, i64* %f5
  %has = icmp sgt i64 %capin, 0
  br i1 %has, label %alloc, label %done
alloc:
  %bytes = mul i64 %capin, %elemsize
  %buf = call i8* @nolang_rc_alloc(i64 %bytes)
  store i8* %buf, i8** %f4
  br label %done
done:
  ret i8* %raw
}

; nolang_chan_grow(cb): double the ring (4 on first use), linearising it so
; head becomes 0. A single wrap suffices: i < len <= cap implies head+i < 2*cap.
define void @nolang_chan_grow(%chancb* %cb) {
entry:
  %pc = getelementptr inbounds %chancb, %chancb* %cb, i32 0, i32 1
  %cap = load i64, i64* %pc
  %pl = getelementptr inbounds %chancb, %chancb* %cb, i32 0, i32 0
  %len = load i64, i64* %pl
  %ph = getelementptr inbounds %chancb, %chancb* %cb, i32 0, i32 2
  %head = load i64, i64* %ph
  %pe = getelementptr inbounds %chancb, %chancb* %cb, i32 0, i32 3
  %esz = load i64, i64* %pe
  %pb = getelementptr inbounds %chancb, %chancb* %cb, i32 0, i32 4
  %oldbuf = load i8*, i8** %pb
  %is0 = icmp eq i64 %cap, 0
  %dbl = mul i64 %cap, 2
  %newcap = select i1 %is0, i64 4, i64 %dbl
  %newbytes = mul i64 %newcap, %esz
  %newbuf = call i8* @nolang_rc_alloc(i64 %newbytes)
  br label %loop
loop:
  %i = phi i64 [ 0, %entry ], [ %i1, %body ]
  %more = icmp slt i64 %i, %len
  br i1 %more, label %body, label %fin
body:
  %idx0 = add i64 %head, %i
  %wrap = icmp sge i64 %idx0, %cap
  %idx = select i1 %wrap, i64 0, i64 %idx0
  %soff = mul i64 %idx, %esz
  %src = getelementptr i8, i8* %oldbuf, i64 %soff
  %doff = mul i64 %i, %esz
  %dst = getelementptr i8, i8* %newbuf, i64 %doff
  call void @llvm.memcpy.p0i8.p0i8.i64(i8* %dst, i8* %src, i64 %esz, i1 false)
  %i1 = add i64 %i, 1
  br label %loop
fin:
  %oldnull = icmp eq i8* %oldbuf, null
  br i1 %oldnull, label %store, label %freeold
freeold:
  call void @nolang_free(i8* %oldbuf)
  br label %store
store:
  store i8* %newbuf, i8** %pb
  store i64 %newcap, i64* %pc
  store i64 0, i64* %ph
  ret void
}

define void @nolang_chan_send(i8* %raw, i8* %val) {
entry:
  %isnull = icmp eq i8* %raw, null
  br i1 %isnull, label %bad, label %chk
chk:
  %cb = bitcast i8* %raw to %chancb*
  %pf = getelementptr inbounds %chancb, %chancb* %cb, i32 0, i32 5
  %closed = load i64, i64* %pf
  %isc = icmp ne i64 %closed, 0
  br i1 %isc, label %bad, label %room
bad:
  call void @nolang_chan_panic()
  unreachable
room:
  %pl = getelementptr inbounds %chancb, %chancb* %cb, i32 0, i32 0
  %pc = getelementptr inbounds %chancb, %chancb* %cb, i32 0, i32 1
  %len0 = load i64, i64* %pl
  %cap0 = load i64, i64* %pc
  %full = icmp sge i64 %len0, %cap0
  br i1 %full, label %grow, label %store
grow:
  call void @nolang_chan_grow(%chancb* %cb)
  br label %store
store:
  %len1 = load i64, i64* %pl
  %cap1 = load i64, i64* %pc
  %ph = getelementptr inbounds %chancb, %chancb* %cb, i32 0, i32 2
  %head = load i64, i64* %ph
  %pe = getelementptr inbounds %chancb, %chancb* %cb, i32 0, i32 3
  %esz = load i64, i64* %pe
  %pb = getelementptr inbounds %chancb, %chancb* %cb, i32 0, i32 4
  %buf = load i8*, i8** %pb
  %idx0 = add i64 %head, %len1
  %wrap = icmp sge i64 %idx0, %cap1
  %idx1 = select i1 %wrap, i64 0, i64 %idx0
  %off = mul i64 %idx1, %esz
  %dst = getelementptr i8, i8* %buf, i64 %off
  call void @llvm.memcpy.p0i8.p0i8.i64(i8* %dst, i8* %val, i64 %esz, i1 false)
  %len2 = add i64 %len1, 1
  store i64 %len2, i64* %pl
  ret void
}

define i64 @nolang_chan_recv(i8* %raw, i8* %out) {
entry:
  %isnull = icmp eq i8* %raw, null
  br i1 %isnull, label %nilch, label %chk
nilch:
  ret i64 2
chk:
  %cb = bitcast i8* %raw to %chancb*
  %pl = getelementptr inbounds %chancb, %chancb* %cb, i32 0, i32 0
  %len0 = load i64, i64* %pl
  %iszero = icmp eq i64 %len0, 0
  br i1 %iszero, label %empty, label %take
empty:
  %pf = getelementptr inbounds %chancb, %chancb* %cb, i32 0, i32 5
  %closed = load i64, i64* %pf
  %isc = icmp ne i64 %closed, 0
  %r = select i1 %isc, i64 1, i64 2
  ret i64 %r
take:
  %pc = getelementptr inbounds %chancb, %chancb* %cb, i32 0, i32 1
  %cap0 = load i64, i64* %pc
  %ph = getelementptr inbounds %chancb, %chancb* %cb, i32 0, i32 2
  %head = load i64, i64* %ph
  %pe = getelementptr inbounds %chancb, %chancb* %cb, i32 0, i32 3
  %esz = load i64, i64* %pe
  %pb = getelementptr inbounds %chancb, %chancb* %cb, i32 0, i32 4
  %buf = load i8*, i8** %pb
  %off = mul i64 %head, %esz
  %src = getelementptr i8, i8* %buf, i64 %off
  call void @llvm.memcpy.p0i8.p0i8.i64(i8* %out, i8* %src, i64 %esz, i1 false)
  %head1 = add i64 %head, 1
  %wrap = icmp sge i64 %head1, %cap0
  %head2 = select i1 %wrap, i64 0, i64 %head1
  store i64 %head2, i64* %ph
  %len1 = sub i64 %len0, 1
  store i64 %len1, i64* %pl
  ret i64 0
}

define void @nolang_chan_close(i8* %raw) {
entry:
  %isnull = icmp eq i8* %raw, null
  br i1 %isnull, label %done, label %set
set:
  %cb = bitcast i8* %raw to %chancb*
  %pf = getelementptr inbounds %chancb, %chancb* %cb, i32 0, i32 5
  store i64 1, i64* %pf
  br label %done
done:
  ret void
}

define i64 @nolang_chan_len(i8* %raw) {
entry:
  %isnull = icmp eq i8* %raw, null
  br i1 %isnull, label %zero, label %load
load:
  %cb = bitcast i8* %raw to %chancb*
  %pl = getelementptr inbounds %chancb, %chancb* %cb, i32 0, i32 0
  %len = load i64, i64* %pl
  ret i64 %len
zero:
  ret i64 0
}

define i64 @nolang_chan_cap(i8* %raw) {
entry:
  %isnull = icmp eq i8* %raw, null
  br i1 %isnull, label %zero, label %load
load:
  %cb = bitcast i8* %raw to %chancb*
  %pc = getelementptr inbounds %chancb, %chancb* %cb, i32 0, i32 1
  %cap = load i64, i64* %pc
  ret i64 %cap
zero:
  ret i64 0
}

; nolang_chan_free(cb): release the ring. Elements still queued keep their own
; heap (they were cloned on send) and are intentionally not walked — dropping a
; non-empty channel leaks them (documented limitation, see syntax.md §chan).
define void @nolang_chan_free(i8* %raw) {
entry:
  %isnull = icmp eq i8* %raw, null
  br i1 %isnull, label %done, label %freebuf
freebuf:
  %cb = bitcast i8* %raw to %chancb*
  %pb = getelementptr inbounds %chancb, %chancb* %cb, i32 0, i32 4
  %buf = load i8*, i8** %pb
  %bufnull = icmp eq i8* %buf, null
  br i1 %bufnull, label %freeraw, label %freebuf2
freebuf2:
  call void @nolang_free(i8* %buf)
  br label %freeraw
freeraw:
  call void @nolang_free(i8* %raw)
  br label %done
done:
  ret void
}
`

	c.extraGlobals = append(c.extraGlobals, b)
}
