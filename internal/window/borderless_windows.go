package window

import (
	"syscall"
	"unsafe"
)

var (
	user32                  = syscall.NewLazyDLL("user32.dll")
	procGetWindowLongPtrW   = user32.NewProc("GetWindowLongPtrW")
	procSetWindowLongPtrW   = user32.NewProc("SetWindowLongPtrW")
	procSetWindowPos        = user32.NewProc("SetWindowPos")
	procGetSystemMetrics    = user32.NewProc("GetSystemMetrics")
	procSendMessageW        = user32.NewProc("SendMessageW")
)

const (
	GWL_STYLE      = ^uintptr(16 - 1) // -16
	GWL_EXSTYLE    = ^uintptr(20 - 1) // -20
	WS_CAPTION     = 0x00C00000
	WS_THICKFRAME  = 0x00040000
	WS_MINIMIZEBOX = 0x00020000
	WS_MAXIMIZEBOX = 0x00010000
	WS_SYSMENU     = 0x00080000
	WS_VISIBLE     = 0x10000000
	WS_POPUP       = 0x80000000
	WS_BORDER      = 0x00800000
	
	WS_EX_APPWINDOW    = 0x00040000
	WS_EX_WINDOWEDGE   = 0x00000100
	WS_EX_DLGMODALFRAME = 0x00000001
	
	SWP_FRAMECHANGED = 0x0020
	SWP_NOMOVE       = 0x0002
	SWP_NOSIZE       = 0x0001
	SWP_NOZORDER     = 0x0004
	
	HWND_TOP = 0
	
	WM_NCCALCSIZE = 0x0083
	
	SW_MINIMIZE = 6
)

var (
	procShowWindow      = user32.NewProc("ShowWindow")
	procReleaseCapture  = user32.NewProc("ReleaseCapture")
)

// 拖动窗口相关常量
const (
	WM_NCLBUTTONDOWN = 0x00A1
	HTCAPTION        = 2
)

// MinimizeWindow 最小化窗口
func MinimizeWindow(hwnd uintptr) {
	procShowWindow.Call(hwnd, SW_MINIMIZE)
}

// StartWindowDrag 启动窗口拖动 (用于无边框窗口的拖动手势)
// 调用顺序: ReleaseCapture → SendMessage(WM_NCLBUTTONDOWN, HTCAPTION)
// 这让 Windows 把当前鼠标按下事件视为"拖动标题栏"
func StartWindowDrag(hwnd uintptr) {
	procReleaseCapture.Call()
	procSendMessageW.Call(hwnd, WM_NCLBUTTONDOWN, HTCAPTION, 0)
}

// MakeBorderless 移除窗口标题栏和边框，创建无边框窗口。
// hwnd 是 Windows 窗口句柄。
func MakeBorderless(hwnd uintptr) error {
	// 获取当前窗口样式
	style, _, _ := procGetWindowLongPtrW.Call(hwnd, GWL_STYLE)
	
	// 移除标题栏、边框、系统菜单，保留弹出窗口样式
	newStyle := style & ^uintptr(WS_CAPTION|WS_THICKFRAME|WS_SYSMENU)
	newStyle |= WS_POPUP
	
	// 应用新样式
	procSetWindowLongPtrW.Call(hwnd, GWL_STYLE, newStyle)
	
	// 获取扩展样式
	exStyle, _, _ := procGetWindowLongPtrW.Call(hwnd, GWL_EXSTYLE)
	
	// 移除对话框边框
	newExStyle := exStyle & ^uintptr(WS_EX_DLGMODALFRAME|WS_EX_WINDOWEDGE)
	
	// 应用新扩展样式
	procSetWindowLongPtrW.Call(hwnd, GWL_EXSTYLE, newExStyle)
	
	// 刷新窗口框架
	procSetWindowPos.Call(
		hwnd,
		HWND_TOP,
		0, 0, 0, 0,
		SWP_FRAMECHANGED|SWP_NOMOVE|SWP_NOSIZE|SWP_NOZORDER,
	)
	
	return nil
}

// GetHwndFromWebview 从 webview 实例获取窗口句柄。
// webview_go 提供了 Window() 方法返回 unsafe.Pointer，
// 在 Windows 上这就是 HWND。
func GetHwndFromWebview(w interface{}) (uintptr, error) {
	// 类型断言获取 WebView 接口
	type windowGetter interface {
		Window() unsafe.Pointer
	}
	
	if wg, ok := w.(windowGetter); ok {
		ptr := wg.Window()
		return uintptr(ptr), nil
	}
	
	return 0, nil
}
