// Package tray hosts the QuickDrop system tray UI.
//
// systray.Run 会阻塞主 goroutine 且必须在 main goroutine 上执行,
// 所以 HTTP server 必须先在另一个 goroutine 起好, tray.Run 放在 main 最后.
package tray

import (
	_ "embed"
	"log"

	"github.com/atotto/clipboard"
	"github.com/getlantern/systray"
)

// Windows systray.SetIcon 要求 .ico 格式 (PNG-in-ICO 容器, Vista+).
// 把 PNG 直接传进去会得到 "Unable to set icon: The operation completed successfully."
//
//go:embed icon.ico
var iconBytes []byte

// Run 启动托盘 (阻塞), 直到用户点 "退出" 或 systray.Quit() 被外部触发.
//
// shareURL:    给朋友的链接 (mobileURL, 指向 /d 手机端发送页). "复制扫码链接" 复制这个.
// initialName: 初始发送的文件名, 显示在 tooltip 上.
// onExit:      用户点退出时调用 (典型用法: server.Shutdown()).
//              onExit 在 systray.Quit() 之后, 进程返回前执行.
func Run(shareURL, initialName string, onExit func()) {
	onReady := func() {
		systray.SetIcon(iconBytes)
		systray.SetTitle("QuickDrop")
		systray.SetTooltip(tooltipFor(initialName))

		mCopy := systray.AddMenuItem("复制扫码链接", "把 "+shareURL+" 写到剪贴板")
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
				case <-mQuit.ClickedCh:
					systray.Quit()
					return
				}
			}
		}()
	}

	systray.Run(onReady, onExit)
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
