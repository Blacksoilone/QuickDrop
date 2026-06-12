package window

import (
	"log"

	webview "github.com/webview/webview_go"
)

// runWebview: 子进程实际起 webview 的内部实现. main goroutine 阻塞调用.
//
// 极简策略 (ADR-17): 默认固定尺寸 264×316, 不允许用户拉伸 (mini dashboard 用).
// 大窗 (config 等) 用 width/height >= 600 时, 改用 HintNone 让用户可拉伸.
// width/height 0 用默认 264×316 (恰好容下 240px QR + 文件名 + 大小, 无滚动条).
//
// 真"无边框"需要原生 WIN32 API 去掉 WS_CAPTION, webview_go 不直接暴露,
// 工程量大, 先用最小尺寸的标题栏窗口. 后续若必要再走 cgo 调 SetWindowLong.
//
// 关闭按钮: webview2 里 JS 的 window.close() 只能关 JS 自己 open 的窗口,
// host 创建的窗口它管不到. 用 Bind 注册一个 Go 函数, HTML 调它走 w.Terminate().
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

	// HTML 关闭按钮的 onclick=quickdropClose() 走这, 触发主循环退出.
	// Bind 必须在 Navigate 之前 (Navigate 后再 Bind 当次加载用不上).
	if err := w.Bind("quickdropClose", func() {
		w.Terminate()
	}); err != nil {
		log.Printf("绑定 quickdropClose 失败: %v", err)
	}

	w.SetTitle(title)
	// 大窗 (>= 600 宽) 用 HintNone 让用户拉伸; 小窗保持 HintFixed 防破布局
	var hint webview.Hint = webview.HintFixed
	if width >= 600 {
		hint = webview.HintNone
	}
	w.SetSize(width, height, hint)
	w.Navigate(url)
	w.Run() // 阻塞主消息循环
}
