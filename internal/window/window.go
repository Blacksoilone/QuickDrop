package window

import (
	"log"

	webview "github.com/webview/webview_go"
)

// runWebview: 子进程实际起 webview 的内部实现. main goroutine 阻塞调用.
//
// 极简策略 (ADR-17): 固定尺寸, 不允许用户拉伸. 标题用空字符串保持最简.
// width/height 0 用默认 264×316 (恰好容下 240px QR + 文件名 + 大小, 无滚动条).
//
// 真"无边框"需要原生 WIN32 API 去掉 WS_CAPTION, webview_go 不直接暴露,
// 工程量大, 先用最小尺寸的标题栏窗口. 后续若必要再走 cgo 调 SetWindowLong.
func runWebview(url, title string, width, height int) {
	if width == 0 {
		width = 264
	}
	if height == 0 {
		height = 316
	}

	w := webview.New(false) // false: 非 debug 模式
	if w == nil {
		log.Fatal("webview.New 返回 nil, WebView2 Runtime 没装?")
	}
	defer w.Destroy()

	w.SetTitle(title)
	w.SetSize(width, height, webview.HintFixed) // 不让用户改大小
	w.Navigate(url)
	w.Run() // 阻塞主消息循环
}
