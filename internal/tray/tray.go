// Package tray hosts the QuickDrop system tray UI.
//
// systray.Run 会阻塞主 goroutine 且必须在 main goroutine 上执行,
// 所以 HTTP server 必须先在另一个 goroutine 起好, tray.Run 放在 main 最后.
package tray

import (
	_ "embed"
	"log"
	"sync"

	"github.com/atotto/clipboard"
	"github.com/getlantern/systray"
)

// Windows systray.SetIcon 要求 .ico 格式 (PNG-in-ICO 容器, Vista+).
// 把 PNG 直接传进去会得到 "Unable to set icon: The operation completed successfully."
//
//go:embed icon.ico
var iconBytes []byte

// receiveItem: 模块级保存接收菜单项, 给 SetReceiveChecked 外部同步用.
// systray 没有暴露按 id 拿菜单项的 API, 只能这样.
// 用 mu 保护避免 Run 还没建好菜单时 SetReceiveChecked 拿到 nil.
var (
	receiveMu   sync.Mutex
	receiveItem *systray.MenuItem
)

// Run 启动托盘 (阻塞), 直到用户点 "退出" 或 systray.Quit() 被外部触发.
//
// shareURL:    给朋友的链接 (mobileURL, 指向 /d 手机端发送页). "复制扫码链接" 复制这个.
// initialName: 初始发送的文件名, 显示在 tooltip 上.
// onReceive:   用户点 "接收文件" / "停止接收" 时调 (传入新状态 on/off).
//              由 main 接到 server.EnableReceive.
// onExit:      用户点退出时调用 (典型用法: server.Shutdown()).
//              onExit 在 systray.Quit() 之后, 进程返回前执行.
func Run(shareURL, initialName string, onReceive func(on bool), onExit func()) {
	onReady := func() {
		systray.SetIcon(iconBytes)
		systray.SetTitle("QuickDrop")
		systray.SetTooltip(tooltipFor(initialName))

		mCopy := systray.AddMenuItem("复制扫码链接", "把 "+shareURL+" 写到剪贴板")
		mRecv := systray.AddMenuItemCheckbox("接收文件", "开启接收模式, 弹接收 QR 窗", false)
		receiveMu.Lock()
		receiveItem = mRecv
		receiveMu.Unlock()
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
	receiveMu.Lock()
	defer receiveMu.Unlock()
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
	systray.SetTooltip(tooltipFor(fileName))
}

func tooltipFor(fileName string) string {
	if fileName == "" {
		return "QuickDrop - 局域网文件传输"
	}
	return "QuickDrop - 正在发送: " + fileName
}
