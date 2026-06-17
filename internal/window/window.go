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
// 无边框: 通过 WIN32 API 移除 WS_CAPTION，需要前端实现自定义标题栏（拖动区域）.
//
// 关闭按钮: webview2 里 JS 的 window.close() 只能关 JS 自己 open 的窗口,
// host 创建的窗口它管不到. 用 Bind 注册一个 Go 函数, HTML 调它走 w.Terminate().
func runWebview(url, title string, width, height int) {
	runWebviewWithOptions(url, title, width, height, false)
}

// runWebviewBorderless 创建无边框窗口版本
func runWebviewBorderless(url, title string, width, height int) {
	runWebviewWithOptions(url, title, width, height, true)
}

func runWebviewWithOptions(url, title string, width, height int, borderless bool) {
	if width == 0 {
		width = 264
	}
	if height == 0 {
		// 无边框模式无标题栏, 减少标题栏占用让窗口更紧凑
		if borderless {
			height = 294
		} else {
			height = 316
		}
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
	
	// 无边框窗口：添加最小化和拖动功能
	if borderless {
		if err := w.Bind("quickdropMinimize", func() {
			hwnd, err := GetHwndFromWebview(w)
			if err == nil && hwnd != 0 {
				MinimizeWindow(hwnd)
			}
		}); err != nil {
			log.Printf("绑定 quickdropMinimize 失败: %v", err)
		}
		// 拖动窗口：前端在 mousedown 时调用
		// 用 SendMessage(WM_NCLBUTTONDOWN, HTCAPTION) 让 Windows 启动拖动
		if err := w.Bind("quickdropStartDrag", func() {
			hwnd, err := GetHwndFromWebview(w)
			if err == nil && hwnd != 0 {
				StartWindowDrag(hwnd)
			}
		}); err != nil {
			log.Printf("绑定 quickdropStartDrag 失败: %v", err)
		}
	}

	w.SetTitle(title)
	// 大窗 (>= 600 宽) 用 HintNone 让用户拉伸; 小窗保持 HintFixed 防破布局
	var hint webview.Hint = webview.HintFixed
	if width >= 600 {
		hint = webview.HintNone
	}
	w.SetSize(width, height, hint)
	
	// 应用无边框样式. 通过 Dispatch 确保在 webview 主循环开始后执行,
	// 此时 HWND 已经初始化完毕.
	if borderless {
		w.Dispatch(func() {
			hwnd, err := GetHwndFromWebview(w)
			if err != nil {
				log.Printf("获取 HWND 失败: %v", err)
				return
			}
			if hwnd == 0 {
				log.Printf("HWND 为 0, 无法应用无边框")
				return
			}
			log.Printf("应用无边框样式, hwnd=0x%x", hwnd)
			if err := MakeBorderless(hwnd); err != nil {
				log.Printf("应用无边框样式失败: %v", err)
			} else {
				log.Printf("无边框样式已应用")
			}
		})
	}
	
	w.Navigate(url)
	w.Run() // 阻塞主消息循环
}
