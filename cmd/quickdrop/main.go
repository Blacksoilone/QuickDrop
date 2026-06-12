// QuickDrop 入口: 三种模式
//
//  1. window 子命令: 子进程, 只跑 webview 加载指定 URL (供 daemon fork)
//  2. Daemon 模式:   端口 8443 空闲 → 起 HTTP server + 托盘, 常驻
//  3. Client 模式:   端口已被 QuickDrop daemon 占用 → POST /internal/send 通知
//                    daemon 切换发送文件, 自己立刻退出
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
	"quickdrop/internal/window"
)

const (
	daemonURL = "http://127.0.0.1:8443"

	// envWindowMode: 控制 daemon 弹 webview 窗口的策略.
	//   replace    (默认): 切换文件时先杀旧窗再开新窗, 屏幕上始终 1 个窗
	//   keep       : 切换文件时旧窗保留, 屏幕上可能并存多个窗
	//   first-only : 只在 daemon 首次启动时弹窗, 切换文件不开新窗
	envWindowMode = "QUICKDROP_WINDOW_MODE"
)

func usage() {
	fmt.Fprintf(os.Stderr, `QuickDrop - 局域网快速发送

用法:
  %s send <文件路径>
  %s <文件路径>             # 拖拽文件到 .exe 也走此分支
  %s window <url>           # 内部: webview 子进程入口, 不要手动调

环境变量:
  %s=replace|keep|first-only  控制窗口策略 (默认 replace)

例:
  quickdrop.exe send C:\Users\you\Pictures\test.jpg

如果 daemon 已经在跑, 会切换 daemon 当前发送的文件, 不重启进程.
`, os.Args[0], os.Args[0], os.Args[0], envWindowMode)
}

// parseSendArgs 支持两种形态:
//
//	quickdrop send <path>   显式语法
//	quickdrop <path>        拖拽到 .exe 时 Windows 直接把路径作为 args[0]
func parseSendArgs() (string, error) {
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
	// window 子命令是 daemon fork 出来的内部入口. 不走 flag.Parse,
	// 因为 webview 不能被 flag 干扰. 必须最先判断.
	if len(os.Args) >= 2 && os.Args[1] == "window" {
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: quickdrop window <url>")
			os.Exit(1)
		}
		setupLogging() // 子进程也写同一个日志, 便于排查
		log.Printf("--- window 子进程 pid=%d, url=%s ---", os.Getpid(), os.Args[2])
		window.Run(os.Args[2], "QuickDrop", 0, 0)
		return
	}

	flag.Usage = usage
	flag.Parse()

	// build 加 -ldflags=-H=windowsgui 后双击/拖拽不会弹黑窗, 但 stderr/stdout 也丢了.
	// 统一写 %TEMP%\quickdrop.log.
	setupLogging()

	rawPath, err := parseSendArgs()
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

// runDaemon: 起 HTTP server + 托盘 + webview 子进程管理, 阻塞直到用户点 "退出".
func runDaemon(absPath string) {
	srv, err := server.New(absPath)
	if err != nil {
		log.Fatal(err)
	}

	// webview 子进程管理器
	selfExe, err := os.Executable()
	if err != nil {
		log.Fatalf("拿不到自身可执行路径: %v", err)
	}
	mode := window.ParseMode(os.Getenv(envWindowMode))
	winMgr := window.NewManager(mode, selfExe)
	log.Printf("窗口策略: %s (env %s=%s)", mode, envWindowMode, os.Getenv(envWindowMode))

	// 文件切换时: 刷托盘 tooltip + 弹窗 (按 mode 策略)
	srv.SetOnSwap(func(name string) {
		tray.UpdateTooltip(name)
		winMgr.OpenForFile(srv.HomeURL())
	})
	srv.Start()

	// daemon 首次启动也开一个窗 (显示初始文件的 QR)
	winMgr.OpenForFile(srv.HomeURL())

	// systray.Run 阻塞 + 必须在 main goroutine.
	// 用户点 "退出" → onExit 关 HTTP server + 杀所有 webview 子进程 → systray.Run 返回 → 进程退出.
	// shareURL 用 mobileURL (指向 /d 手机端发送页), 给朋友复制走的就该是这个.
	tray.Run(srv.MobileURL(), srv.CurrentFileName(), func() {
		winMgr.Shutdown()
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
