package builtin

import (
	"github.com/lizongying/nolang/parser"
)

func init() {
	// async-cancel: 取消一个正在运行（或排队中）的异步任务。
	// 参数 task 是 `run` 返回的不透明句柄（底层为 i8*，指向堆上的 %task 结构）。
	// 将 %task.cancelled（field 3）置为 true；事件循环调度该任务时，
	// async_wrapper 在入口检查 cancelled 标志，若已置位则跳过目标函数执行并标记 done=true
	// （优雅中止，不执行用户代码），使等待方（awy）能立即返回。
	// 返回 void。
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "async-cancel",
		Params:       []parser.Type{&parser.PointerType{Type: parser.TypeI8}},
		Doc:          "Cancel an async task by its run() handle. Sets the task's cancelled flag so the event loop skips execution",
		ForwardFunc:  "async-cancel",
	})

	// async-cancelled: 协作式自我取消检查。
	// 读取当前正在执行的任务（@nolang_current_task 全局，由事件循环在调度时写入）
	// 的 cancelled 标志（%task field 3），返回 bool。
	// 异步函数（如 Agent Loop）可在每个步骤的开头调用本函数，
	// 若返回 true 则提前返回，从而实现“被外部 async-cancel 后及时退出”。
	// 这是协作式取消：长阻塞调用（如 LLM 请求）无法被强制中断，
	// 但函数可在阻塞返回后、下一个步骤开始前检查本标志。
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "async-cancelled",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeBool},
		Doc:          "Returns true if the currently running async task has been cancelled via async-cancel",
		ForwardFunc:  "async-cancelled",
	})

	// async-yield: 协作式让出（re-enqueue 当前任务并返回事件循环）。
	// 用于非阻塞轮询循环：在 WS 处理器的 recv-nb 返回“would-block”时调用，
	// 让事件循环有机会运行其他任务（例如正在执行的 Agent Loop），
	// 从而避免单线程协程因忙等而饿死其它任务。
	// 返回 void。
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "async-yield",
		Params:       []parser.Type{},
		Doc:          "Cooperatively yield: re-enqueue the current task and return to the event loop",
		ForwardFunc:  "async-yield",
	})
}
