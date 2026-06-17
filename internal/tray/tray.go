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

// pendingItem: 模块级保存 "待处理 (N)" 菜单项, 给 SetPendingCount 改文本+显隐用.
// systray 没有暴露按 id 拿菜单项的 API, 只能这样.
// 用 mu 保护避免 Run 还没建好菜单时外部 setter 拿到 nil.
var (
	stateMu      sync.Mutex
	pendingItem  *systray.MenuItem
	currentName  string // 当前文件名 (tooltip 用)
	pendingCount int    // 当前待处理数 (tooltip 用)
)

// Run 启动托盘 (阻塞), 直到用户点 "退出" 或 systray.Quit() 被外部触发.
//
// shareURL:    给朋友的链接 (mobileURL, 指向 /d 手机端发送页). "复制扫码链接" 复制这个.
// receiveURL:  接收页链接 (指向 /r 手机端接收页). "显示接收 QR 码" 打开这个.
// initialName: 初始发送的文件名, 显示在 tooltip 上.
// onPending:   用户点 "待处理 (N)" 时调 (main 起 pending webview 子窗加载 /p).
// onConfig:    用户点 "设置" 时调 (main 起 config webview 子窗加载 /c).
// onPickFile:  用户点 "选文件发送..." 时调 (main 弹原生选择器 + SwapFile).
// onShowRecvQR: 用户点 "显示接收 QR 码" 时调 (main 起 webview 显示 receiveURL 的二维码).
// onExit:      用户点退出时调用 (典型用法: server.Shutdown()).
//              onExit 在 systray.Quit() 之后, 进程返回前执行.
func Run(shareURL, receiveURL, initialName string, onPending func(), onConfig func(), onPickFile func(), onShowRecvQR func(), onExit func()) {
	onReady := func() {
		systray.SetIcon(iconNormalBytes)
		systray.SetTitle("QuickDrop")
		stateMu.Lock()
		currentName = initialName
		stateMu.Unlock()
		systray.SetTooltip(buildTooltip())

		mCopy := systray.AddMenuItem("复制扫码链接", "把 "+shareURL+" 写到剪贴板")
		mPick := systray.AddMenuItem("选文件发送...", "弹原生选择器选一个文件直接发送 (替代右键/拖拽)")
		mRecvQR := systray.AddMenuItem("显示接收 QR 码", "弹窗显示手机扫码上传的二维码 (需先在设置中开启接收)")
		mPend := systray.AddMenuItem("待处理 (0)", "查看待接受/拒绝的文件传入")
		mPend.Hide() // 默认隐藏, 有 pending 时显示
		mCfg := systray.AddMenuItem("设置", "打开配置中心 (包含设备管理)")

		stateMu.Lock()
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
				case <-mPick.ClickedCh:
					if onPickFile != nil {
						// onPickFile 内部弹 GetOpenFileNameW (阻塞), 在 systray goroutine
						// 跑没问题 - systray 的菜单消息循环在 onReady 已起好.
						go onPickFile() // 用 goroutine 防对话框阻塞菜单消息派发
					}
				case <-mRecvQR.ClickedCh:
					if onShowRecvQR != nil {
						onShowRecvQR()
					}
				case <-mPend.ClickedCh:
					if onPending != nil {
						onPending()
					}
				case <-mCfg.ClickedCh:
					if onConfig != nil {
						onConfig()
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

// Quit 让外部触发托盘退出 (从而 tray.Run 解阻塞 + onExit 被执行).
// 用于 daemon 异常场景: HTTP server 自己挂了, 需要拉 daemon 一起跑标准 cleanup.
// 多次调用安全 (systray 内部幂等).
func Quit() {
	systray.Quit()
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
