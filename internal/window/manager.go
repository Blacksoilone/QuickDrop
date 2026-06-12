// Package window 同时是 daemon 持有的子进程管理器 + 子进程入口.
//
// 架构:
//   - Run(): 子进程入口, daemon fork 出来的 `quickdrop window <url>` 进程跑这个
//   - Manager: daemon 进程持有, 负责 fork/kill webview 子进程, 实现三种 mode
package window

import (
	"errors"
	"log"
	"os"
	"os/exec"
	"sync"
)

// Mode 控制 SwapFile 之后窗口怎么处理.
type Mode string

const (
	// ModeReplace: 默认. SwapFile 时先 kill 旧窗再 fork 新窗.
	// 任何时刻最多 1 个 webview 子进程. 主流使用场景: 一次发一个文件.
	ModeReplace Mode = "replace"

	// ModeKeep: SwapFile 时旧窗保留, fork 新窗. 屏幕上可能并存多个窗.
	// 场景: 用户要把不同文件发给不同人, 想留着旧 QR 给前一个人扫.
	ModeKeep Mode = "keep"

	// ModeFirstOnly: 只首次启动 daemon 弹窗, SwapFile 时不开新窗.
	// 旧窗如果还在 (用户没关), 主页内容会自动跟着 daemon 切换 -- 但旧窗 URL 是 /qr,
	// 不会自动 reload, 需要用户手动刷新. 场景: 用户嫌窗口烦, 只在第一次见一下.
	ModeFirstOnly Mode = "first-only"
)

// ParseMode 把字符串解析成 Mode. 空串或未知值 → ModeReplace.
func ParseMode(s string) Mode {
	switch Mode(s) {
	case ModeKeep:
		return ModeKeep
	case ModeFirstOnly:
		return ModeFirstOnly
	default:
		return ModeReplace
	}
}

// Manager 由 daemon 进程持有. fork/kill webview 子进程.
type Manager struct {
	mu        sync.Mutex
	mode      Mode
	selfExe   string      // os.Executable() 的结果, fork 用
	cur       *exec.Cmd   // ModeReplace 持有当前发送窗子进程
	keepCmds  []*exec.Cmd // ModeKeep 持有所有发送窗子进程 (退出时一并清理)
	firstUsed bool        // ModeFirstOnly 记是否已经开过发送窗
	recvCmd   *exec.Cmd   // 接收窗子进程 (独立于 mode, 单实例)
	pendCmd   *exec.Cmd   // pending dashboard 子进程 (单实例; 用户从托盘点开)
	devCmd    *exec.Cmd   // devices dashboard 子进程 (单实例; 用户从托盘 "设备管理" 点开)
}

// NewManager 创建一个 Manager. mode 决定 OpenForFile 的行为.
// selfExe 必须是当前进程的可执行路径 (os.Executable()).
func NewManager(mode Mode, selfExe string) *Manager {
	return &Manager{
		mode:    mode,
		selfExe: selfExe,
	}
}

// OpenForFile 在 daemon 起 webview 子进程加载 url.
// 行为视 mode 不同. 失败只打日志不致命 (用户至少还能从托盘菜单复制 URL).
func (m *Manager) OpenForFile(url string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	switch m.mode {
	case ModeFirstOnly:
		if m.firstUsed {
			return
		}
		cmd, err := spawn(m.selfExe, url)
		if err != nil {
			log.Printf("打开 webview 子进程失败: %v", err)
			return
		}
		m.cur = cmd
		m.firstUsed = true

	case ModeKeep:
		cmd, err := spawn(m.selfExe, url)
		if err != nil {
			log.Printf("打开 webview 子进程失败: %v", err)
			return
		}
		m.keepCmds = append(m.keepCmds, cmd)

	default: // ModeReplace
		if m.cur != nil && m.cur.Process != nil {
			if err := m.cur.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
				log.Printf("杀旧 webview 子进程 (PID %d) 失败: %v", m.cur.Process.Pid, err)
			}
		}
		cmd, err := spawn(m.selfExe, url)
		if err != nil {
			log.Printf("打开 webview 子进程失败: %v", err)
			return
		}
		m.cur = cmd
	}
}

// Shutdown 关掉所有当前持有的子进程. daemon 退出时调.
func (m *Manager) Shutdown() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cur != nil && m.cur.Process != nil {
		_ = m.cur.Process.Kill()
	}
	for _, c := range m.keepCmds {
		if c.Process != nil {
			_ = c.Process.Kill()
		}
	}
	if m.recvCmd != nil && m.recvCmd.Process != nil {
		_ = m.recvCmd.Process.Kill()
	}
	if m.pendCmd != nil && m.pendCmd.Process != nil {
		_ = m.pendCmd.Process.Kill()
	}
	if m.devCmd != nil && m.devCmd.Process != nil {
		_ = m.devCmd.Process.Kill()
	}
}

// OpenPendingWindow 起一个 pending dashboard 子进程 (单实例).
// 用户从托盘点 "待处理 (N)" 时调. 重复点会先杀旧的避免堆积.
func (m *Manager) OpenPendingWindow(url string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pendCmd != nil && m.pendCmd.Process != nil {
		if err := m.pendCmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			log.Printf("杀旧 pending 窗 (PID %d) 失败: %v", m.pendCmd.Process.Pid, err)
		}
	}
	cmd, err := spawn(m.selfExe, url)
	if err != nil {
		log.Printf("打开 pending webview 子进程失败: %v", err)
		return
	}
	m.pendCmd = cmd
}

// OpenDevicesWindow 起一个 devices dashboard 子进程 (单实例).
// 用户从托盘点 "设备管理" 时调.
func (m *Manager) OpenDevicesWindow(url string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.devCmd != nil && m.devCmd.Process != nil {
		if err := m.devCmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			log.Printf("杀旧 devices 窗 (PID %d) 失败: %v", m.devCmd.Process.Pid, err)
		}
	}
	cmd, err := spawn(m.selfExe, url)
	if err != nil {
		log.Printf("打开 devices webview 子进程失败: %v", err)
		return
	}
	m.devCmd = cmd
}

// OpenReceiveWindow 起一个接收 dashboard 子进程. 单实例.
// 若已存在 (用户连续多次点"接收文件"), 先杀旧的.
func (m *Manager) OpenReceiveWindow(url string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.recvCmd != nil && m.recvCmd.Process != nil {
		if err := m.recvCmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			log.Printf("杀旧接收窗 (PID %d) 失败: %v", m.recvCmd.Process.Pid, err)
		}
	}
	cmd, err := spawn(m.selfExe, url)
	if err != nil {
		log.Printf("打开接收 webview 子进程失败: %v", err)
		return
	}
	m.recvCmd = cmd
}

// CloseReceiveWindow 关掉接收窗子进程 (如果在跑).
// 由 server.EnableReceive(false) 回调触发.
func (m *Manager) CloseReceiveWindow() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.recvCmd != nil && m.recvCmd.Process != nil {
		if err := m.recvCmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			log.Printf("关闭接收窗 (PID %d) 失败: %v", m.recvCmd.Process.Pid, err)
		}
	}
	m.recvCmd = nil
}

// spawn fork 一个 `<selfExe> window <url>` 子进程, 立刻返回不等它结束.
// 子进程退出由 OS 回收, 父进程不 Wait (Wait 会阻塞)... reaper goroutine 兜底.
func spawn(selfExe, url string) (*exec.Cmd, error) {
	cmd := exec.Command(selfExe, "window", url)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	log.Printf("打开 webview 子进程 PID %d 加载 %s", cmd.Process.Pid, url)
	// reaper: 等子进程退出, 防止僵尸 + 记录退出时间, 不阻塞 OpenForFile
	go func() {
		if err := cmd.Wait(); err != nil {
			log.Printf("webview 子进程 PID %d 退出: %v", cmd.Process.Pid, err)
		} else {
			log.Printf("webview 子进程 PID %d 正常退出", cmd.Process.Pid)
		}
	}()
	return cmd, nil
}

// Run 是子进程入口: 起 webview 加载 url 阻塞到关窗.
// 必须在子进程的 main goroutine 调用.
func Run(url, title string, width, height int) {
	runWebview(url, title, width, height)
}
