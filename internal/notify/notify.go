// Package notify 弹 Windows 10/11 toast 通知 (ADR-19).
//
// 用 go-toast 包: 它生成 ToastNotification XML 通过 PowerShell 调 WinRT API.
// 不需要 cgo, 也不需要 manifest 注册 (相比直接 winrt 调用简单很多).
//
// Toast actions 用 protocol activation: 按钮触发 Windows 启动 quickdrop://accept?token=xxx,
// 由 install 时注册的 URL scheme handler 把 URL 转成 quickdrop.exe 调用,
// main.go 的 url-action 子命令解析出 token 调 IPC /internal/peer-decide.
package notify

import (
	"fmt"
	"log"

	"github.com/go-toast/toast"
)

const appID = "QuickDrop"

// Incoming 弹一条 "X 想发给你 Y" 的 toast, 含 [接受] [拒绝] 两个按钮.
//
// fromName: 对端显示名 (来自 mDNS TXT 的 name).
// fileName: 文件名.
// fileSize: 字节数 (用于人类可读大小展示).
// token:    transferID, 按钮 URL 里带, 决策时发回 daemon.
//
// 失败只打日志, 调用方继续 (有红点 fallback, 见 2.5e).
func Incoming(fromName, fileName string, fileSize int64, token string) {
	n := toast.Notification{
		AppID:    appID,
		Title:    fmt.Sprintf("%s 想发文件给你", fromName),
		Message:  fmt.Sprintf("%s (%s)", fileName, humanSize(fileSize)),
		Duration: toast.Long,
		Audio:    toast.IM,
		Actions: []toast.Action{
			{
				Type:      "protocol",
				Label:     "接受",
				Arguments: "quickdrop://accept?token=" + token,
			},
			{
				Type:      "protocol",
				Label:     "拒绝",
				Arguments: "quickdrop://reject?token=" + token,
			},
		},
	}
	if err := n.Push(); err != nil {
		log.Printf("toast 推送失败: %v", err)
	}
}

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}
