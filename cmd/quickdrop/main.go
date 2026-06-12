// QuickDrop 入口: 解析参数, 起 HTTP server, 进托盘主循环.
//
// 不再像 Phase 1 早期那样自动开浏览器 (ADR-11: 浏览器破坏"无感").
// 用户从托盘菜单 "复制扫码链接" 拿 URL, 或者直接给同 WiFi 的手机扫码.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"quickdrop/internal/server"
	"quickdrop/internal/tray"
)

func usage() {
	fmt.Fprintf(os.Stderr, `QuickDrop - 局域网快速发送

用法:
  %s send <文件路径>
  %s <文件路径>           # 拖拽文件到 .exe 也走此分支

例:
  quickdrop.exe send C:\Users\you\Pictures\test.jpg
`, os.Args[0], os.Args[0])
}

// parseArgs 支持两种形态:
//
//	quickdrop send <path>   显式语法
//	quickdrop <path>        拖拽到 .exe 时 Windows 直接把路径作为 args[0]
func parseArgs() (string, error) {
	args := flag.Args()
	if len(args) >= 1 && args[0] == "send" {
		args = args[1:]
	}
	if len(args) != 1 {
		return "", fmt.Errorf("参数数量不对")
	}
	return args[0], nil
}

func main() {
	flag.Usage = usage
	flag.Parse()

	// build 加 -ldflags=-H=windowsgui 后双击/拖拽不会弹黑窗, 但 stderr/stdout 也丢了.
	// 同时往 stderr 和 %TEMP%\quickdrop.log 写, 保证两种启动方式都能看日志.
	setupLogging()

	rawPath, err := parseArgs()
	if err != nil {
		usage()
		os.Exit(1)
	}

	srv, err := server.New(rawPath)
	if err != nil {
		log.Fatal(err)
	}
	srv.Start()

	// systray.Run 阻塞 + 必须在 main goroutine.
	// 用户点 "退出" → onExit 关 HTTP server → systray.Run 返回 → 进程退出.
	tray.Run(srv.HomeURL(), func() {
		srv.Shutdown()
	})
}

func setupLogging() {
	logPath := filepath.Join(os.TempDir(), "quickdrop.log")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		// 文件打不开就只用 stderr (windowsgui 下 stderr 可能是无效 handle, 写入静默失败也无伤大雅).
		log.Printf("打开日志文件 %s 失败: %v", logPath, err)
		return
	}
	// 不能用 io.MultiWriter(os.Stderr, f): -H=windowsgui 下 stderr 是无效 handle,
	// MultiWriter 第一个 Writer 报错就 short-circuit, 文件那段永远写不到.
	// 直接只写文件; 调试模式 (不带 windowsgui) 可以 tail -f 这个文件.
	log.SetOutput(f)
	log.Printf("--- quickdrop 启动, 日志写到 %s ---", logPath)
}
