// QuickDrop 入口: 多种模式
//
//  1. window 子命令: 子进程, 只跑 webview 加载指定 URL (供 daemon fork)
//  2. send <path> 模式:
//     - 端口 8443 空闲 → 起 daemon (HTTP server + 托盘) + 弹发送窗
//     - 已被 QuickDrop daemon 占用 → POST /internal/send 通知切换, 客户端退出
//  3. recv 模式:
//     - 端口 8443 空闲 → 起 daemon (无文件) + 弹接收窗
//     - 已被 QuickDrop daemon 占用 → POST /internal/receive on, 客户端退出
//
// 解决重复 `quickdrop send X` 端口冲突的无感问题 (QuickDrop.md §6 2.2 + 2.3).
// 接收模式独立入口 + 安全分离 (2.13, ADR-17).
//
// 不再像 Phase 1 早期那样自动开浏览器 (ADR-11: 浏览器破坏"无感").
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

	// envWindowMode: 控制 daemon 弹发送 webview 窗口的策略.
	//   replace    (默认): 切换文件时先杀旧窗再开新窗, 屏幕上始终 1 个窗
	//   keep       : 切换文件时旧窗保留, 屏幕上可能并存多个窗
	//   first-only : 只在 daemon 首次启动时弹窗, 切换文件不开新窗
	envWindowMode = "QUICKDROP_WINDOW_MODE"
)

func usage() {
	fmt.Fprintf(os.Stderr, `QuickDrop - 局域网快速发送/接收

用法:
  %s send <文件路径>      发送模式
  %s <文件路径>           同上 (拖拽到 .exe 也走此分支)
  %s recv                 接收模式 (从手机往电脑传)
  %s window <url>         内部: webview 子进程入口, 不要手动调

环境变量:
  %s=replace|keep|first-only  控制发送窗口策略 (默认 replace)

例:
  quickdrop.exe send C:\Users\you\Pictures\test.jpg
  quickdrop.exe recv

如果 daemon 已经在跑, send 切换发送文件 / recv 开启接收, 不重启进程.
`, os.Args[0], os.Args[0], os.Args[0], os.Args[0], envWindowMode)
}

func main() {
	// window 子命令是 daemon fork 出来的内部入口. 不走 flag.Parse,
	// 因为 webview 不能被 flag 干扰. 必须最先判断.
	if len(os.Args) >= 2 && os.Args[1] == "window" {
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: quickdrop window <url>")
			os.Exit(1)
		}
		setupLogging()
		log.Printf("--- window 子进程 pid=%d, url=%s ---", os.Getpid(), os.Args[2])
		window.Run(os.Args[2], "QuickDrop", 0, 0)
		return
	}

	flag.Usage = usage
	flag.Parse()

	setupLogging()

	args := flag.Args()

	// recv 子命令: 接收模式
	if len(args) >= 1 && args[0] == "recv" {
		runRecv()
		return
	}

	// send <path> 或裸路径
	rawPath, err := parseSendArgs(args)
	if err != nil {
		usage()
		os.Exit(1)
	}
	runSend(rawPath)
}

// parseSendArgs 支持两种形态:
//
//	quickdrop send <path>   显式语法
//	quickdrop <path>        拖拽到 .exe 时 Windows 直接把路径作为 args[0]
func parseSendArgs(args []string) (string, error) {
	if len(args) >= 1 && args[0] == "send" {
		args = args[1:]
	}
	if len(args) != 1 {
		return "", fmt.Errorf("参数数量不对")
	}
	return args[0], nil
}

func runSend(rawPath string) {
	absPath, err := filepath.Abs(rawPath)
	if err != nil {
		log.Fatalf("路径解析失败: %v", err)
	}

	if probeDaemon() {
		log.Printf("发现已运行的 daemon, 走客户端模式切换文件: %s", absPath)
		if err := notifyDaemonSend(absPath); err != nil {
			log.Fatalf("通知 daemon 失败: %v", err)
		}
		log.Print("daemon 已切换, 客户端退出")
		return
	}
	log.Print("未发现 daemon, 启动新的 daemon (发送模式)")
	runDaemon(absPath, false)
}

func runRecv() {
	if probeDaemon() {
		log.Print("发现已运行的 daemon, 走客户端模式开启接收")
		if err := notifyDaemonReceive("on"); err != nil {
			log.Fatalf("通知 daemon 失败: %v", err)
		}
		log.Print("daemon 已切到接收, 客户端退出")
		return
	}
	log.Print("未发现 daemon, 启动新的 daemon (接收模式)")
	runDaemon("", true)
}

// probeDaemon GET /internal/health, 判断本机 8443 是不是 QuickDrop daemon.
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
	return resp.Header.Get("X-QuickDrop") == "1"
}

// notifyDaemonSend POST /internal/send body=<绝对路径>.
func notifyDaemonSend(absPath string) error {
	return postInternal("/internal/send", absPath)
}

// notifyDaemonReceive POST /internal/receive body=on|off.
func notifyDaemonReceive(cmd string) error {
	return postInternal("/internal/receive", cmd)
}

func postInternal(path, body string) error {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Post(daemonURL+path, "text/plain", strings.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		rb, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("daemon 返回 %d: %s", resp.StatusCode, strings.TrimSpace(string(rb)))
	}
	return nil
}

// runDaemon: 起 HTTP server + 托盘 + webview 子进程管理, 阻塞直到用户点 "退出".
// initialPath 为空表示纯接收模式启动 (此时 initialReceive 必须 true, 否则 daemon 啥也不干).
// initialReceive 表示 daemon 启动后立刻开启接收模式 + 弹接收窗.
func runDaemon(initialPath string, initialReceive bool) {
	srv, err := server.New(initialPath)
	if err != nil {
		log.Fatal(err)
	}

	selfExe, err := os.Executable()
	if err != nil {
		log.Fatalf("拿不到自身可执行路径: %v", err)
	}
	mode := window.ParseMode(os.Getenv(envWindowMode))
	winMgr := window.NewManager(mode, selfExe)
	log.Printf("窗口策略: %s (env %s=%s)", mode, envWindowMode, os.Getenv(envWindowMode))

	// 文件切换时: 刷托盘 tooltip + 弹发送窗 (按 mode 策略)
	srv.SetOnSwap(func(name string) {
		tray.UpdateTooltip(name)
		winMgr.OpenForFile(srv.HomeURL())
	})

	// 接收模式切换时: 起/关接收窗 + 同步托盘菜单勾选
	srv.SetOnReceive(func(on bool) {
		tray.SetReceiveChecked(on)
		if on {
			winMgr.OpenReceiveWindow(srv.ReceiveURL())
		} else {
			winMgr.CloseReceiveWindow()
		}
	})

	srv.Start()

	// daemon 启动行为分支
	if srv.HasFile() {
		winMgr.OpenForFile(srv.HomeURL())
	}
	if initialReceive {
		srv.EnableReceive(true) // 会触发 onReceive → 起接收窗 + 勾菜单
	}

	// 托盘 "接收文件" 菜单点击 → 切 server.EnableReceive (会再触发 onReceive 回调)
	onTrayReceive := func(on bool) {
		srv.EnableReceive(on)
	}

	tray.Run(srv.MobileURL(), srv.CurrentFileName(), onTrayReceive, func() {
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
