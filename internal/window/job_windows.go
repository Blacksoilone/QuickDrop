// JobObject 让 webview 子进程跟 daemon 同生共死.
//
// 问题: daemon 用 exec.Cmd.Start() fork 出 webview 子进程, 进程之间在 OS 层是平等的.
// daemon 被任务管理器强杀 / panic / kill -9 时, webview 子进程不会跟着死, 用户看到的
// 现象就是 "前台还在但所有 API 全部 404 / 拒连". 还有可能用户重启 daemon 后旧 webview
// 仍连着旧端口失败.
//
// Windows 解法: JobObject. 把所有子进程绑到一个 Job, 设 KILL_ON_JOB_CLOSE flag.
// 当 daemon 进程退出 (任何理由), OS 自动关 Job → 所有成员进程被 SIGKILL 等价处理.
// 这是 OS 级保证, 比 daemon 自己装 atexit handler 可靠多了 (defer 在 -F 强杀时不跑).
//
// macOS / Linux 等价方案: Pdeathsig (Linux) / kqueue PROC_EXIT (mac), 此处仅 Windows.
// 当前项目只支持 Windows, 见 ADR-6.
package window

import (
	"fmt"
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// jobHandle 进程级 JobObject 句柄. 唯一. NewManager 时建好, daemon 退出时 OS 自动清.
// 不需要显式 Close: 进程结束时内核引用计数归零 → Job 被销毁 → 触发 KILL_ON_JOB_CLOSE.
var jobHandle windows.Handle

// initJobObject 建一个全进程唯一的 JobObject 并设 KILL_ON_JOB_CLOSE.
// 失败返 error: 调用方应只 log 不致命 (没 Job 也能跑, 只是失去同生共死保证).
//
// 多次调用幂等: 已建过直接返 nil.
func initJobObject() error {
	if jobHandle != 0 {
		return nil
	}
	h, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return fmt.Errorf("CreateJobObject: %w", err)
	}

	// 关键 flag: JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE.
	// daemon 进程死时, 它持有的 Job 句柄被关闭, OS 检查这个 flag → 干掉所有成员进程.
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	if _, err := windows.SetInformationJobObject(
		h,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		windows.CloseHandle(h)
		return fmt.Errorf("SetInformationJobObject: %w", err)
	}

	jobHandle = h
	return nil
}

// assignToJob 把已起的子进程加进 Job.
// 必须在 cmd.Start() 之后调; cmd.Start 才有 Process.Pid + 内核句柄.
//
// 失败只 log: 子进程已经在跑, 加 Job 失败仅意味着它不会在 daemon 死时同步死,
// 不影响功能正确性, 只丢一层防御.
func assignToJob(cmd *exec.Cmd) error {
	if jobHandle == 0 {
		// initJobObject 失败过 / 没调过. 静默放行, 退化成无 Job 行为.
		return nil
	}
	if cmd.Process == nil {
		return fmt.Errorf("cmd.Process 为 nil, 还没 Start")
	}
	// exec.Cmd 没暴露 Windows 进程 handle, 自己 OpenProcess.
	// PROCESS_SET_QUOTA + PROCESS_TERMINATE 是 AssignProcessToJobObject 文档要求的最小权限.
	const access = windows.PROCESS_SET_QUOTA | windows.PROCESS_TERMINATE
	hProc, err := windows.OpenProcess(access, false, uint32(cmd.Process.Pid))
	if err != nil {
		return fmt.Errorf("OpenProcess pid=%d: %w", cmd.Process.Pid, err)
	}
	defer windows.CloseHandle(hProc)

	if err := windows.AssignProcessToJobObject(jobHandle, hProc); err != nil {
		// ERROR_ACCESS_DENIED (5) 在子进程已经隐式属于另一个 Job 时会发生
		// (如 Process Explorer / ConHost / Win11 Terminal 行为). Win8+ 后 nested Job
		// 已经允许, 但仍偶发 — 此时退化成无 Job, 不致命.
		if errno, ok := err.(syscall.Errno); ok && errno == windows.ERROR_ACCESS_DENIED {
			return fmt.Errorf("AssignProcessToJobObject 拒绝 (子进程可能已属其他 Job): %w", err)
		}
		return fmt.Errorf("AssignProcessToJobObject: %w", err)
	}
	return nil
}
