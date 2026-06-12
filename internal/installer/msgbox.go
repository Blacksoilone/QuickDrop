package installer

import (
	"syscall"
	"unsafe"
)

// MessageBox 弹一个原生 Win32 MessageBox.
// windowsgui 模式下 stderr/stdout 失效, install/uninstall 需要可视反馈.
//
// flags 常用值:
//   0x00000040 MB_ICONINFORMATION
//   0x00000010 MB_ICONERROR
//   0x00000030 MB_ICONWARNING
const (
	MBIconInfo    = 0x00000040
	MBIconError   = 0x00000010
	MBIconWarning = 0x00000030
)

var (
	user32           = syscall.NewLazyDLL("user32.dll")
	procMessageBoxW  = user32.NewProc("MessageBoxW")
)

// MessageBox 弹窗显示 text + title. 阻塞到用户点 OK.
// 失败静默 (没有比这更兜底的反馈渠道了).
func MessageBox(title, text string, icon uintptr) {
	titlePtr, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		return
	}
	textPtr, err := syscall.UTF16PtrFromString(text)
	if err != nil {
		return
	}
	procMessageBoxW.Call(
		0, // hWnd
		uintptr(unsafe.Pointer(textPtr)),
		uintptr(unsafe.Pointer(titlePtr)),
		icon,
	)
}
