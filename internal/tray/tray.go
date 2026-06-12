// Package tray hosts the QuickDrop system tray UI.
//
// systray.Run 会阻塞主 goroutine 且必须在 main goroutine 上执行,
// 所以 HTTP server 必须先在另一个 goroutine 起好, tray.Run 放在 main 最后.
package tray

import (
	_ "embed"
	"fmt"
	"log"
	"sync"

	"github.com/atotto/clipboard"
	"github.com/getlantern/systray"
)

// Windows systray.SetIcon 要求 .ico 格式 (PNG-in-ICO 容器, Vista+).
// 把 PNG 直接传进去会得到 "Unable to set icon: The operation completed successfully."
//
//go:embed icon.ico
var iconNormalBytes []byte

// 待处理 incoming > 0 时切换的红点图标. 蓝底右上角小红方块.
//
//go:embed icon-alert.ico
var iconAlertBytes []byte

// receiveItem: 模块级保存接收菜单项, 给 SetReceiveChecked 外部同步用.
// pendingItem: 模块级保存 "待处理 (N)" 菜单项, 给 SetPendingCount 改文本+显隐用.
// systray 没有暴露按 id 拿菜单项的 API, 只能这样.
// 用 mu 保护避免 Run 还没建好菜单时外部 setter 拿到 nil.
var (
	stateMu      sync.Mutex
	receiveItem  *systray.MenuItem
	pendingItem  *systray.MenuItem
	currentName  string // 当前文件名 (tooltip 用)
	pendingCount int    // 当前待处理数 (tooltip 用)
)

// Run 启动托盘 (阻塞), 直到用户点 "退出" 或 systray.Quit() 被外部触发.
//
// shareURL:    给朋友的链接 (mobileURL, 指向 /d 手机端发送页). "复制扫码链接" 复制这个.
// initialName: 初始发送的文件名, 显示在 tooltip 上.
// onReceive:   用户点 "接收文件" / "停止接收" 时调 (传入新状态 on/off).
// onPending:   用户点 "待处理 (N)" 时调 (main 起 pending webview 子窗加载 /p).
// onExit:      用户点退出时调用 (典型用法: server.Shutdown()).
//              onExit 在 systray.Quit() 之后, 进程返回前执行.
func Run(shareURL, initialName string, onReceive func(on bool), onPending func(), onExit func()) {
	onReady := func() {
		systray.SetIcon(iconNormalBytes)
		systray.SetTitle("QuickDrop")
		stateMu.Lock()
		currentName = initialName
		stateMu.Unlock()
		systray.SetTooltip(buildTooltip())

		mCopy := systray.AddMenuItem("复制扫码链接", "把 "+shareURL+" 写到剪贴板")
		mRecv := systray.AddMenuItemCheckbox("接收文件", "开启接收模式, 弹接收 QR 窗", false)
		mPend := systray.AddMenuItem("待处理 (0)", "查看待接受/拒绝的文件传入")
		mPend.Hide() // 默认隐藏, 有 pending 时显示

		stateMu.Lock()
		receiveItem = mRecv
		pendingItem = mPend
		stateMu.Unlock()

		systray.AddSeparator()
		mQuit := systray.AddMenuItem("退出", "停止 server 并退出")

		go func() {
			for {
				select {
				case <-mCopy.ClickedCh:
					if err := clipboard.WriteAll(shareURL); err != nil {
						log.Printf("复制到剪贴板失败: %v", err)
					} else {
						log.Printf("已复制: %s", shareURL)
					}
				case <-mRecv.ClickedCh:
					if mRecv.Checked() {
						mRecv.Uncheck()
						if onReceive != nil {
							onReceive(false)
						}
					} else {
						mRecv.Check()
						if onReceive != nil {
							onReceive(true)
						}
					}
				case <-mPend.ClickedCh:
					if onPending != nil {
						onPending()
					}
				case <-mQuit.ClickedCh:
					systray.Quit()
					return
				}
			}
		}()
	}

	systray.Run(onReady, onExit)
}

// SetReceiveChecked 让外部 (server.EnableReceive 回调) 同步菜单勾选状态.
// 用于 "停止接收" 按钮 / IPC 关接收模式时, 菜单也跟着取消勾.
//
// 在 systray.Run 起来之前调是 no-op (没事, 因为初值就是 false).
func SetReceiveChecked(checked bool) {
	stateMu.Lock()
	defer stateMu.Unlock()
	if receiveItem == nil {
		return
	}
	if checked {
		receiveItem.Check()
	} else {
		receiveItem.Uncheck()
	}
}

// UpdateTooltip 给外部 (server SwapFile 回调) 调用, 刷新当前发送的文件名.
// systray.SetTooltip 是包级函数, 可在任意 goroutine 调用, 但必须在 systray.Run
// 起来之后才会生效. SwapFile 由 HTTP handler 触发, 此时 systray 必已 onReady.
func UpdateTooltip(fileName string) {
	stateMu.Lock()
	currentName = fileName
	stateMu.Unlock()
	systray.SetTooltip(buildTooltip())
}

// SetPendingCount 让外部 (server peer 状态机回调) 同步待处理数.
// n > 0 时: tooltip 加 "N 个待处理", 菜单项显示并改文字, 图标切红点版.
// n = 0 时: tooltip 恢复, 菜单项隐藏, 图标恢复正常.
func SetPendingCount(n int) {
	stateMu.Lock()
	pendingCount = n
	mItem := pendingItem
	stateMu.Unlock()

	systray.SetTooltip(buildTooltip())

	if n > 0 {
		systray.SetIcon(iconAlertBytes)
		if mItem != nil {
			mItem.SetTitle(fmt.Sprintf("待处理 (%d)", n))
			mItem.Show()
		}
	} else {
		systray.SetIcon(iconNormalBytes)
		if mItem != nil {
			mItem.Hide()
		}
	}
}

// buildTooltip 拼装 tooltip 文本. 持锁外读 state, 调用者保证.
func buildTooltip() string {
	stateMu.Lock()
	name := currentName
	n := pendingCount
	stateMu.Unlock()

	base := "QuickDrop"
	if name != "" {
		base = "QuickDrop - 正在发送: " + name
	} else {
		base = "QuickDrop - 局域网文件传输"
	}
	if n > 0 {
		base += fmt.Sprintf(" · %d 个待处理", n)
	}
	return base
}
