package window

import (
	"log"

	webview "github.com/webview/webview_go"
)

// runWebview: 子进程实际起 webview 的内部实现. main goroutine 阻塞调用.
//
// width/height 0 用默认 280×320.
func runWebview(url, title string, width, height int) {
	if width == 0 {
		width = 280
	}
	if height == 0 {
		height = 320
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
