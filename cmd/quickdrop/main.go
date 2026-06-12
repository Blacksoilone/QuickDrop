// QuickDrop 入口: 两种模式
//
//  1. Daemon 模式: 端口 8443 空闲 → 起 HTTP server + 托盘, 常驻
//  2. Client 模式: 端口已被 QuickDrop daemon 占用 → POST /internal/send 通知
//     daemon 切换发送文件, 自己立刻退出
//
// 解决重复 `quickdrop send X` 端口冲突的无感问题 (QuickDrop.md §6 2.2 + 2.3).
//
// 不再像 Phase 1 早期那样自动开浏览器 (ADR-11: 浏览器破坏"无感").
// 用户从托盘菜单 "复制扫码链接" 拿 URL, 或者直接给同 WiFi 的手机扫码.
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"quickdrop/internal/server"
	"quickdrop/internal/tray"
)

const daemonURL = "http://127.0.0.1:8443"

func usage() {
	fmt.Fprintf(os.Stderr, `QuickDrop - 局域网快速发送

用法:
  %s send <文件路径>
  %s <文件路径>           # 拖拽文件到 .exe 也走此分支

例:
  quickdrop.exe send C:\Users\you\Pictures\test.jpg

如果 daemon 已经在跑, 会切换 daemon 当前发送的文件, 不重启进程.
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
	// 统一写 %TEMP%\quickdrop.log.
	setupLogging()

	rawPath, err := parseArgs()
	if err != nil {
		usage()
		os.Exit(1)
	}

	// 把路径转绝对, 这样客户端模式 POST 给 daemon 时不依赖 daemon 的 cwd.
	absPath, err := filepath.Abs(rawPath)
	if err != nil {
		log.Fatalf("路径解析失败: %v", err)
	}

	if probeDaemon() {
		log.Printf("发现已运行的 daemon, 走客户端模式切换文件: %s", absPath)
		if err := notifyDaemon(absPath); err != nil {
			log.Fatalf("通知 daemon 失败: %v", err)
		}
		log.Print("daemon 已切换, 客户端退出")
		return
	}

	log.Print("未发现 daemon, 启动新的 daemon")
	runDaemon(absPath)
}

// probeDaemon GET /internal/health, 判断本机 8443 是不是 QuickDrop daemon.
// true: 是 daemon, 走客户端模式
// false: 端口空闲, 或被其他东西占用, 走 daemon 模式 (后者会在 ListenAndServe 时报错)
func probeDaemon() bool {
	client := &http.Client{Timeout: 500 * time.Millisecond}
	resp, err := client.Get(daemonURL + "/internal/health")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return false
	}
	// X-QuickDrop: 1 防止某个无关 server 偶然占了 8443 又返回 200
	return resp.Header.Get("X-QuickDrop") == "1"
}

// notifyDaemon POST /internal/send body=<绝对路径>, 让 daemon 切换发送文件.
func notifyDaemon(absPath string) error {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Post(daemonURL+"/internal/send", "text/plain", strings.NewReader(absPath))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("daemon 返回 %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

// runDaemon: 起 HTTP server + 托盘, 阻塞直到用户点 "退出".
func runDaemon(absPath string) {
	srv, err := server.New(absPath)
	if err != nil {
		log.Fatal(err)
	}
	// 文件切换时刷新托盘 tooltip
	srv.SetOnSwap(func(name string) {
		tray.UpdateTooltip(name)
	})
	srv.Start()

	// systray.Run 阻塞 + 必须在 main goroutine.
	// 用户点 "退出" → onExit 关 HTTP server → systray.Run 返回 → 进程退出.
	tray.Run(srv.HomeURL(), srv.CurrentFileName(), func() {
		srv.Shutdown()
	})
}

func setupLogging() {
	logPath := filepath.Join(os.TempDir(), "quickdrop.log")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		log.Printf("打开日志文件 %s 失败: %v", logPath, err)
		return
	}
	log.SetOutput(f)
	log.Printf("--- quickdrop 启动 pid=%d, 日志写到 %s ---", os.Getpid(), logPath)
}
