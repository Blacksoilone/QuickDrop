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
// homeURL: 用于 "复制扫码链接" 菜单项写入剪贴板.
// onExit:  用户点退出时调用 (典型用法: server.Shutdown()).
//          onExit 在 systray.Quit() 之后, 进程返回前执行.
func Run(homeURL string, onExit func()) {
	onReady := func() {
		systray.SetIcon(iconBytes)
		systray.SetTitle("QuickDrop")
		systray.SetTooltip("QuickDrop - 局域网文件传输")

		mCopy := systray.AddMenuItem("复制扫码链接", "把 "+homeURL+" 写到剪贴板")
		systray.AddSeparator()
		mQuit := systray.AddMenuItem("退出", "停止 server 并退出")

		go func() {
			for {
				select {
				case <-mCopy.ClickedCh:
					if err := clipboard.WriteAll(homeURL); err != nil {
						log.Printf("复制到剪贴板失败: %v", err)
					} else {
						log.Printf("已复制: %s", homeURL)
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
