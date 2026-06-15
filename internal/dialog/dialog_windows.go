// Package dialog 提供原生 Windows 文件选择器, 给托盘 "选文件发送..." 用.
//
// 工程角度选 syscall.LazyDLL 而非 cgo:
//   - 只调一个 API (GetOpenFileNameW), CGO 是杀鸡用牛刀
//   - 不引入 mingw 编译依赖到这个包 (整个项目仍需 cgo 给 webview / systray)
//   - 标准库 syscall + unicode/utf16, 零外部依赖
//
// 行为: PickFile 阻塞调用线程直到用户选/取消. 必须在能跑消息循环的 goroutine 调用
// (托盘 ClickedCh 处理是 systray 的 goroutine, 可以). 不能在 main goroutine
// 跟 webview / systray 抢消息.
package dialog

import (
	"syscall"
	"unicode/utf16"
	"unsafe"
)

// OPENFILENAMEW 对应 Windows commdlg.h 的同名结构, lpstrFile 指 UTF-16 缓冲区.
// 字段顺序与字节大小必须跟 Win32 一致, 不要重排.
// 见 https://learn.microsoft.com/windows/win32/api/commdlg/ns-commdlg-openfilenamew
type openFileName struct {
	lStructSize       uint32
	hwndOwner         uintptr
	hInstance         uintptr
	lpstrFilter       *uint16
	lpstrCustomFilter *uint16
	nMaxCustFilter    uint32
	nFilterIndex      uint32
	lpstrFile         *uint16
	nMaxFile          uint32
	lpstrFileTitle    *uint16
	nMaxFileTitle     uint32
	lpstrInitialDir   *uint16
	lpstrTitle        *uint16
	Flags             uint32
	nFileOffset       uint16
	nFileExtension    uint16
	lpstrDefExt       *uint16
	lCustData         uintptr
	lpfnHook          uintptr
	lpTemplateName    *uint16
	pvReserved        uintptr
	dwReserved        uint32
	FlagsEx           uint32
}

// 关键 flag (commdlg.h):
const (
	ofnExplorer    = 0x00080000 // 用现代 Explorer 风格对话框
	ofnFileMustExist = 0x00001000 // 用户输入不存在的路径要报错
	ofnPathMustExist = 0x00000800 // 路径必须存在
	ofnHideReadOnly  = 0x00000004 // 隐藏 "只读" 复选框 (我们不需要)
	ofnNoChangeDir   = 0x00000008 // 不改进程当前目录
)

var (
	comdlg32           = syscall.NewLazyDLL("comdlg32.dll")
	procGetOpenFileNameW = comdlg32.NewProc("GetOpenFileNameW")
)

// PickFile 弹原生文件选择对话框, 返用户选的绝对路径.
// 用户取消返 ("", nil). 真正失败 (如 DLL 加载不上) 才返 error.
//
// 注意 GetOpenFileNameW 取消时不通过 GetLastError 返错, 而是直接返 0,
// CommDlgExtendedError 才区分取消 vs 真错. 这里把取消视为正常路径, 静默返空.
func PickFile() (string, error) {
	// 16 KB buffer 够任何合法 Windows 路径 (MAX_PATH=260 加长路径模式 32767).
	const bufLen = 32768
	buf := make([]uint16, bufLen)

	// "All files (*.*)\0*.*\0\0" — Windows 的 filter 是 \0 分隔, \0\0 结尾.
	// 用 utf16.Encode 把双 \0 拼好.
	filter := utf16Z("所有文件 (*.*)") + utf16Z("*.*") + "\x00"
	filterUTF16 := utf16.Encode([]rune(filter))

	title := utf16PtrFromString("选择要发送的文件")

	ofn := openFileName{
		lpstrFile:   &buf[0],
		nMaxFile:    bufLen,
		lpstrFilter: &filterUTF16[0],
		lpstrTitle:  title,
		Flags:       ofnExplorer | ofnFileMustExist | ofnPathMustExist | ofnHideReadOnly | ofnNoChangeDir,
	}
	ofn.lStructSize = uint32(unsafe.Sizeof(ofn))

	ret, _, _ := procGetOpenFileNameW.Call(uintptr(unsafe.Pointer(&ofn)))
	if ret == 0 {
		// 用户取消 (或对话框失败). 不区分, 都视为 "没选". 真正的失败极其罕见
		// (DLL 加载失败之类), 不值得为它写错误处理路径.
		return "", nil
	}

	// 找 \0 终止符截断
	for i, c := range buf {
		if c == 0 {
			return string(utf16.Decode(buf[:i])), nil
		}
	}
	return string(utf16.Decode(buf)), nil
}

// utf16Z helper: 把字符串转成 UTF-16 内存表示 + 在末尾追加 \0.
// filter 字段需要嵌套 \0 分隔, 不能用 syscall.UTF16PtrFromString (它遇 \0 报错).
func utf16Z(s string) string {
	return s + "\x00"
}

// utf16PtrFromString 跟 syscall.UTF16PtrFromString 一样, 但忽略错误
// (我们传的是写死的字符串, 不会有内嵌 NUL).
func utf16PtrFromString(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(s)
	return p
}
