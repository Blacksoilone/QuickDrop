// QuickDrop 入口: 多种模式
//
//  1. window 子命令:    子进程, 只跑 webview 加载指定 URL (供 daemon fork)
//  2. install 子命令:   写注册表注册 Windows 右键菜单 "通过 QuickDrop 发送"
//  3. uninstall 子命令: 删注册表条目
//  4. status 子命令:    打印当前 daemon / 右键菜单注册状态
//  5. send <path> 模式:
//     - 端口 8443 空闲 → 起 daemon (HTTP server + 托盘) + 弹发送窗
//     - 已被 QuickDrop daemon 占用 → POST /internal/send 通知切换, 客户端退出
//  6. recv 模式:
//     - 端口 8443 空闲 → 起 daemon (无文件) + 弹接收窗
//     - 已被 QuickDrop daemon 占用 → POST /internal/receive on, 客户端退出
//
// 解决重复 `quickdrop send X` 端口冲突的无感问题 (QuickDrop.md §6 2.2 + 2.3).
// 接收模式独立入口 + 安全分离 (2.13, ADR-17).
// Windows 右键菜单 (2.4, 真正的核心交互).
//
// 不再像 Phase 1 早期那样自动开浏览器 (ADR-11: 浏览器破坏"无感").
package main

import (
	"context"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"quickdrop/internal/config"
	"quickdrop/internal/devices"
	"quickdrop/internal/dialog"
	"quickdrop/internal/discovery"
	"quickdrop/internal/receive"
	"quickdrop/internal/identity"
	"quickdrop/internal/installer"
	"quickdrop/internal/notify"
	"quickdrop/internal/peer"
	"quickdrop/internal/progress"
	"quickdrop/internal/server"
	"quickdrop/internal/tray"
	"quickdrop/internal/window"
)

// webDist 嵌入 Vite 构建产物. embed 路径不允许 ../, 所以 build.ps1 会先把
// 仓库根 web/dist/ 复制到 cmd/quickdrop/web/, 再 go build.
//
//go:embed all:web
var webDist embed.FS

// distFS 是剥掉 "web/" 前缀后的 fs.FS, 直接以 dist 内容为根.
var distFS fs.FS

func init() {
	sub, err := fs.Sub(webDist, "web")
	if err != nil {
		// 这只在 build 前没复制 web/ 时发生, 提示开发者
		panic("embed web/ failed; build.ps1 应当先把 web/dist 复制到 cmd/quickdrop/web/: " + err.Error())
	}
	distFS = sub
}

const (
	// envWindowMode: 控制 daemon 弹 webview 窗口的策略.
	//   replace    (默认): 切换文件时先杀旧窗再开新窗, 屏幕上始终 1 个窗
	//   keep       : 切换文件时旧窗保留, 屏幕上可能并存多个窗
	//   first-only : 只在 daemon 首次启动时弹窗, 切换文件不开新窗
	envWindowMode = "QUICKDROP_WINDOW_MODE"

	// envPort: 覆盖 daemon HTTP 端口 (默认 8443). 已迁移到 internal/config (applyEnv).
	// 保留常量名只为文档目的; 实际读取在 config.Load() 内. 不要在这个文件里直接读.
	envPort = "QUICKDROP_PORT"

	// version 软件版本, 写入 mDNS TXT 给对端做兼容性判断.
	version = "0.8.0"
)

// daemonURL 返回客户端模式 (send/recv 子进程) 要 POST 给本机 daemon 的 base URL.
// 读 config + env, 让用户改 config.json 的 port 后 send 命令也能找对端口.
func daemonURL() string {
	return fmt.Sprintf("http://127.0.0.1:%d", clientPort())
}

// clientPort 客户端模式 (send/recv) 探测 daemon 监听端口.
// 优先级 env > config.json > 8443. 与 runDaemon 完全一致.
func clientPort() int {
	if m, err := config.Load(); err == nil {
		p := m.Get().Server.Port
		if p > 0 && p < 65536 {
			return p
		}
	}
	return 8443
}

func usage() {
	fmt.Fprintf(os.Stderr, `QuickDrop - 局域网快速发送/接收

用法:
  %s send <文件路径>      发送模式
  %s <文件路径>           同上 (拖拽到 .exe 也走此分支)
  %s recv                 接收模式 (从手机往电脑传)
  %s install              安装右键菜单 "通过 QuickDrop 发送"
  %s uninstall            卸载右键菜单
  %s status               打印当前程序 / 右键菜单状态
  %s window <url>         内部: webview 子进程入口, 不要手动调

环境变量:
  %s=replace|keep|first-only  控制发送窗口策略 (默认 replace)

例:
  quickdrop.exe install
  右键任意文件 → "通过 QuickDrop 发送"
`, os.Args[0], os.Args[0], os.Args[0], os.Args[0], os.Args[0], os.Args[0], os.Args[0], envWindowMode)
}

func main() {
	// window 子命令是 daemon fork 出来的内部入口. 不走 flag.Parse,
	// 因为 webview 不能被 flag 干扰. 必须最先判断.
	// 用法:
	//   quickdrop window <url>                  默认 264x316 (mini dashboard)
	//   quickdrop window <url> <width> <height> 自定义尺寸 (config / devices 大窗)
	//   quickdrop window <url> [width height] [borderless]
	//   borderless 标志可以出现在任何位置（最后一个参数）.
	if len(os.Args) >= 2 && os.Args[1] == "window" {
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: quickdrop window <url> [width height] [borderless]")
			os.Exit(1)
		}
		setupLogging()
		w, h := 0, 0
		borderless := false
		// 扫描所有参数: 数字 → 尺寸, "borderless" → 无边框标志
		nums := []int{}
		for i := 3; i < len(os.Args); i++ {
			arg := os.Args[i]
			if arg == "borderless" {
				borderless = true
			} else if n, err := strconv.Atoi(arg); err == nil {
				nums = append(nums, n)
			}
		}
		if len(nums) >= 2 {
			w = nums[0]
			h = nums[1]
		}
		log.Printf("--- window 子进程 pid=%d, url=%s, size=%dx%d, borderless=%v ---", os.Getpid(), os.Args[2], w, h, borderless)
		if borderless {
			window.RunBorderless(os.Args[2], "QuickDrop", w, h)
		} else {
			window.Run(os.Args[2], "QuickDrop", w, h)
		}
		return
	}

	// url-action 子命令: Windows shell 把 quickdrop:// URL 启动转给我们 (toast 按钮点击).
	// 格式: quickdrop.exe url-action "quickdrop://accept?token=xxx"
	// 我们解析 URL → POST /internal/peer-decide → 退出.
	if len(os.Args) >= 2 && os.Args[1] == "url-action" {
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: quickdrop url-action <quickdrop://...>")
			os.Exit(1)
		}
		setupLogging()
		runURLAction(os.Args[2])
		return
	}

	// install / uninstall / status 子命令: 直接看 os.Args, 不走 flag.Parse,
	// 避免 -q 被 flag 包当成未定义 flag 拒绝.
	if len(os.Args) >= 2 {
		quiet := len(os.Args) >= 3 && os.Args[2] == "-q"
		switch os.Args[1] {
		case "install":
			setupLogging()
			runInstall(quiet)
			return
		case "uninstall":
			setupLogging()
			runUninstall(quiet)
			return
		case "status":
			setupLogging()
			runStatus(quiet)
			return
		}
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
		// 无参数时启动纯后台模式（不带文件）
		if len(args) == 0 {
			log.Print("无参数启动, 进入后台模式（不带初始文件）")
			runDaemon("")
			return
		}
		usage()
		os.Exit(1)
	}
	runSend(rawPath)
}

// runURLAction 处理 quickdrop:// URL scheme 启动 (toast action 点击触发).
// URL: quickdrop://accept?token=xxx 或 quickdrop://reject?token=xxx
// 行为: POST /internal/peer-decide → 立即退出, daemon 那边异步 Pull 文件.
func runURLAction(raw string) {
	u, err := url.Parse(raw)
	if err != nil {
		log.Fatalf("URL 解析失败: %v (raw=%s)", err, raw)
	}
	if u.Scheme != "quickdrop" {
		log.Fatalf("非 quickdrop:// URL: %s", raw)
	}
	decision := u.Host // "accept" 或 "reject"
	if decision != "accept" && decision != "reject" {
		log.Fatalf("未知 URL action: %s (应为 accept 或 reject)", decision)
	}
	token := u.Query().Get("token")
	if token == "" {
		log.Fatal("URL 缺 token 参数")
	}
	log.Printf("URL action: decision=%s token=%s", decision, token[:8])

	// POST /internal/peer-decide
	body := fmt.Sprintf(`{"token":%q,"decision":%q}`, token, decision)
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Post(daemonURL()+"/internal/peer-decide", "application/json", strings.NewReader(body))
	if err != nil {
		log.Fatalf("通知后台服务失败: %v (程序没在运行?)", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		rb, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		log.Fatalf("后台服务返回 %d: %s", resp.StatusCode, strings.TrimSpace(string(rb)))
	}
	log.Printf("后台服务已确认 %s, 退出", decision)
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
		log.Printf("发现已运行的程序, 走客户端模式切换文件: %s", absPath)
		if err := notifyDaemonSend(absPath); err != nil {
			log.Fatalf("通知后台服务失败: %v", err)
		}
		log.Print("后台服务已切换, 客户端退出")
		return
	}
	log.Print("未发现运行中的程序, 启动新实例 (发送文件)")
	runDaemon(absPath)
}

func runRecv() {
	if probeDaemon() {
		log.Print("发现已运行的程序, 走客户端模式开启接收")
		if err := notifyDaemonReceive("on"); err != nil {
			log.Fatalf("通知后台服务失败: %v", err)
		}
		log.Print("后台服务已开启接收, 客户端退出")
		return
	}
	log.Print("未发现运行中的程序, 启动新实例")
	// recv 命令明确表达"我要接收", 即使 config.Receive.DefaultOn=false 也强制开启.
	// 通过环境变量 QUICKDROP_FORCE_RECEIVE 传给 runDaemon (临时方案, 后续可改函数参数).
	os.Setenv("QUICKDROP_FORCE_RECEIVE", "1")
	runDaemon("")
}

// runInstall 写入右键菜单注册表 + 弹 MessageBox 反馈.
// 用户从 Explorer 双击或命令行调 `quickdrop install` 都走这.
// 第二参 quiet=true 时不弹 MessageBox (脚本测试用).
func runInstall(quiet bool) {
	exe, err := os.Executable()
	if err != nil {
		if !quiet {
			installer.MessageBox("QuickDrop 安装失败", "拿不到自身路径: "+err.Error(), installer.MBIconError)
		}
		log.Fatalf("os.Executable: %v", err)
	}
	if err := installer.Install(exe); err != nil {
		if !quiet {
			installer.MessageBox("QuickDrop 安装失败", err.Error(), installer.MBIconError)
		}
		log.Fatalf("Install: %v", err)
	}
	if !quiet {
		msg := fmt.Sprintf("已注册右键菜单 \"通过 QuickDrop 发送\"\n\n"+
			"使用: 右键任意文件 → \"通过 QuickDrop 发送\"\n"+
			"(Win11 可能需要 Shift+右键 或 \"显示更多选项\")\n\n"+
			"exe: %s", exe)
		installer.MessageBox("QuickDrop 安装成功", msg, installer.MBIconInfo)
	}
	log.Printf("安装成功, exe=%s", exe)
}

func runUninstall(quiet bool) {
	if err := installer.Uninstall(); err != nil {
		if !quiet {
			installer.MessageBox("QuickDrop 卸载失败", err.Error(), installer.MBIconError)
		}
		log.Fatalf("Uninstall: %v", err)
	}
	if !quiet {
		installer.MessageBox("QuickDrop 卸载成功", "已从注册表删除右键菜单条目", installer.MBIconInfo)
	}
	log.Print("卸载成功")
}

func runStatus(quiet bool) {
	exe, _ := os.Executable()
	installed, cmd := installer.IsInstalled()
	daemonAlive := probeDaemon()

	var b strings.Builder
	fmt.Fprintf(&b, "当前 exe: %s\n\n", exe)
	if installed {
		fmt.Fprintf(&b, "右键菜单: 已注册\n  command = %s\n", cmd)
	} else {
		fmt.Fprintf(&b, "右键菜单: 未注册 (跑 quickdrop install 注册)\n")
	}
	fmt.Fprintf(&b, "\nDaemon: ")
	if daemonAlive {
		fmt.Fprintf(&b, "运行中 (127.0.0.1:8443)\n")
	} else {
		fmt.Fprintf(&b, "未运行\n")
	}
	if !quiet {
		installer.MessageBox("QuickDrop 状态", b.String(), installer.MBIconInfo)
	}
	log.Print(b.String())
}

// probeDaemon GET /internal/health, 判断本机指定端口是不是 QuickDrop daemon.
func probeDaemon() bool {
	client := &http.Client{Timeout: 500 * time.Millisecond}
	resp, err := client.Get(daemonURL() + "/internal/health")
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
	resp, err := client.Post(daemonURL()+path, "text/plain", strings.NewReader(body))
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
// initialPath 为空表示纯 daemon 启动 (不发送文件), 可通过托盘/IPC 后续操作.
// 接收状态由 config.Receive.DefaultOn 决定, quickdrop recv 命令会强制开启.
func runDaemon(initialPath string) {
	// 全局 panic recover: 防 daemon 主 goroutine 任何一个 handler / goroutine
	// panic 推倒整个进程. recover 后 log + 标准退出, 让 mDNS 下播 / 子窗清理跑到.
	// 不补回归: panic 是真 bug, 应该当场修, 这里只是兜底防全局推倒.
	defer func() {
		if r := recover(); r != nil {
			log.Printf("daemon panic: %v", r)
			log.Printf("正在尝试优雅退出...")
			tray.Quit()
		}
	}()
	// 加载配置 (~/.quickdrop/config.json + env 覆盖). 失败用默认.
	cfgMgr, err := config.Load()
	if err != nil {
		log.Printf("加载 config 失败 (用默认): %v", err)
		// 兜底再来一遍, 这次绝对成功 (默认值不依赖磁盘)
		cfgMgr, _ = config.Load()
	}
	cfg := cfgMgr.Get()

	// port 优先级: env (cfgMgr 已合并) > config.json > 8443
	port := cfg.Server.Port
	if port <= 0 {
		port = 8443
	}

	// 加载/生成本机身份 (UUID + 显示名)
	ident, err := identity.Load()
	if err != nil {
		log.Fatalf("加载 identity: %v", err)
	}
	log.Printf("身份: %s (%s)", ident.Name, ident.UUID[:8])
	log.Printf("config: port=%d dl=%q conflict=%s maxSize=%d toast=%v reveal=%v mdns=%v autostart=%v",
		port,
		cfg.Download.Dir,
		cfg.Download.Conflict,
		cfg.Receive.MaxFileSize,
		cfg.UI.ToastsEnabled,
		cfg.UI.RevealOnDone,
		cfg.Server.MdnsEnabled,
		cfg.System.Autostart,
	)

	// 同步 autostart 到 HKCU\Run (cfg 真源, 注册表跟着)
	if err := installer.SyncAutostart(cfg.System.Autostart); err != nil {
		log.Printf("同步 autostart 注册表失败: %v", err)
	}

	// 自检: URL scheme 未注册时 toast 按钮点了会报 "没有应用能打开此链接".
	// 不致命 (用户可能只想发送), 但 log warn 提示用户跑 install 修复.
	if !installer.IsURLSchemeInstalled() {
		log.Print("WARN: quickdrop:// URL scheme 未注册, toast 接收按钮将无法工作. " +
			"运行 `quickdrop install` 修复 (右键菜单 + URL scheme 一起装)")
	}

	srv, err := server.New(port, &receive.Config{
		DefaultOn: cfg.Receive.DefaultOn,
	})
	if err != nil {
		log.Fatal(err)
	}
	srv.SetDist(distFS)
	srv.SetIdentity(ident.UUID, ident.Name)
	srv.SetPeerManager(peerMgrAdapter{peer.NewManager()})
	srv.SetProgressHub(progressHubAdapter{progress.NewHub()})
	srv.SetConfig(configAdapter{cfgMgr})
	srv.SetInstaller(installerAdapter{})

	// 如果启动时指定了文件, 动态设置 (不在构造时传)
	if initialPath != "" {
		if err := srv.SwapFile(initialPath); err != nil {
			log.Fatalf("设置初始发送文件失败: %v", err)
		}
	}

	// 加载设备信任表 (ADR-20). 失败不致命, 退回到"所有设备 ask".
	if devStore, err := devices.Load(); err == nil {
		srv.SetDeviceStore(deviceStoreAdapter{devStore})
		log.Printf("已加载设备信任表: %d 条记录", len(devStore.All()))
	} else {
		log.Printf("加载设备信任表失败 (退回到全 ask): %v", err)
	}

	// toast 总开关守卫: cfg.UI.ToastsEnabled=false 时所有 notify.* 静音.
	// 用闭包 capture cfgMgr, 每次回调实时读最新值 (热配置生效).
	toastOK := func() bool { return cfgMgr.Get().UI.ToastsEnabled }

	srv.SetOnPeerIncoming(func(fromName, fileName string, fileSize int64, token string) {
		if !toastOK() {
			return
		}
		notify.Incoming(fromName, fileName, fileSize, token)
	})
	srv.SetOnPeerAccepted(func(fromName, fileName string, fileSize int64) {
		if !toastOK() {
			return
		}
		notify.IncomingSilent(fromName, fileName, fileSize)
	})
	srv.SetOnPeerSent(func(toName, fileName string, fileSize int64) {
		if !toastOK() {
			return
		}
		notify.PeerSent(toName, fileName, fileSize)
	})
	srv.SetOnPeerReceived(func(fromName, fileName string, fileSize int64) {
		if !toastOK() {
			return
		}
		notify.PeerReceived(fromName, fileName, fileSize)
	})
	srv.SetOnUploadDone(func(count int) {
		if !toastOK() {
			return
		}
		notify.UploadDone(count)
	})
	srv.SetOnFileSaved(func(absPath string) {
		// 文件落盘后: 如果配置 RevealOnDone, 起 explorer /select,<path>
		// (Windows-only; 其他平台 noop)
		if !cfgMgr.Get().UI.RevealOnDone {
			return
		}
		revealInExplorer(absPath)
	})
	srv.SetOnPendingChange(func(count int) {
		tray.SetPendingCount(count)
	})

	// mDNS: 广播自己 + 发现局域网内其他 QuickDrop. 失败不致命.
	// cfg.Server.MdnsEnabled=false 时彻底不启 (隐身模式).
	var disc *discovery.Discovery
	if cfg.Server.MdnsEnabled {
		d, err := discovery.Start(ident.UUID, ident.Name, version, port)
		if err != nil {
			log.Printf("mDNS 启动失败 (PC→PC 发现不可用, 但其他功能正常): %v", err)
		} else {
			disc = d
			srv.SetPeerSource(peerAdapter{disc})
		}
	} else {
		log.Print("mDNS 已在 config 中禁用 (隐身), 跳过广播 + 发现")
	}

	selfExe, err := os.Executable()
	if err != nil {
		log.Fatalf("拿不到自身可执行路径: %v", err)
	}
	mode := window.ParseMode(os.Getenv(envWindowMode))
	borderless := cfgMgr.Get().UI.BorderlessWindows
	winMgr := window.NewManager(mode, selfExe, borderless)
	log.Printf("窗口策略: %s (env %s=%s), 无边框: %v", mode, envWindowMode, os.Getenv(envWindowMode), borderless)

	// 文件切换时: 刷托盘 tooltip + 弹发送窗 (按 mode 策略)
	srv.SetOnSwap(func(name string) {
		tray.UpdateTooltip(name)
		winMgr.OpenForFile(srv.HomeURL())
	})

	srv.Start()

	// daemon 启动行为分支
	if srv.HasFile() {
		winMgr.OpenForFile(srv.HomeURL())
	}
	// 接收状态已在 Server.New 时由 config.Receive.DefaultOn 初始化.
	// quickdrop recv 命令通过 QUICKDROP_FORCE_RECEIVE=1 强制开启 (即使 config 默认关闭).
	if os.Getenv("QUICKDROP_FORCE_RECEIVE") == "1" {
		srv.EnableReceive(true)
	}

	// 托盘 "待处理 (N)" 菜单点击 → 起 pending dashboard 子窗 (单实例).
	// 用 LocalURL (127.0.0.1) 而非 HomeURL (LAN IP), 因为子窗需要调 /internal/*
	// 路由 (如 /internal/peer-decide), 这些路由有 requireLocal 中间件 (LAN IP 会 404).
	pendingURL := srv.LocalURL() + "/p"
	onTrayPending := func() {
		winMgr.OpenPendingWindow(pendingURL)
	}

	// 托盘 "设置" 菜单点击 → 起配置中心子窗 (单实例, 960×640).
	// 配置页里包含设备管理 section, 是设备管理的唯一入口.
	// 用 LocalURL: 子窗调 /internal/config-save + /internal/device-trust, 需要 127.0.0.1.
	configURL := srv.LocalURL() + "/c"
	onTrayConfig := func() {
		winMgr.OpenConfigWindow(configURL)
	}

	// 托盘 "选文件发送..." 菜单点击 → 弹原生选择器 → 选中后 SwapFile + 弹 QR 窗.
	// 跟 `quickdrop send <path>` / 右键 / 拖拽路径完全等价, 只是入口换成系统对话框.
	// 用户取消选择 = noop. 选了无效文件 = log 错误吞掉 (托盘没好途径反馈).
	onTrayPickFile := func() {
		path, err := dialog.PickFile()
		if err != nil {
			log.Printf("打开文件选择器失败: %v", err)
			return
		}
		if path == "" {
			return // 用户取消
		}
		if err := srv.SwapFile(path); err != nil {
			log.Printf("SwapFile %s 失败: %v", path, err)
			return
		}
		// 跟 send 路径一致: 弹发送窗 (replace/keep/first-only 行为由 winMgr 持有)
		winMgr.OpenForFile(srv.HomeURL())
		log.Printf("从托盘选了文件发送: %s", path)
	}

	// 托盘 "显示接收 QR 码" 菜单点击 → 弹窗显示接收页二维码 (手机扫码上传).
	// 需要接收模式已开启才有意义, 否则手机扫了也传不上来.
	onTrayShowRecvQR := func() {
		winMgr.OpenReceiveWindow(srv.ReceiveURL())
	}

	// 监听 HTTP server 异常退出 (listener 被外部杀 / 网络栈炸 / 端口被夺):
	// 触发 systray.Quit() → tray.Run 解阻塞 → onExit 跑标准清理路径.
	// 比 log.Fatal 强: defer 会被执行, mDNS 下播 + 子窗清理 + 设备表 flush 都不会丢.
	go func() {
		<-srv.Done()
		if err := srv.Err(); err != nil {
			log.Printf("HTTP server 异常 (%v), 触发程序优雅退出...", err)
		}
		// 触发 tray 退出 → tray.Run 解阻塞 → onExit 跑清理.
		// 用导出的 tray.Quit() 而非直接 import systray, 包边界更整洁.
		tray.Quit()
	}()

	tray.Run(srv.MobileURL(), srv.ReceiveURL(), srv.CurrentFileName(), onTrayPending, onTrayConfig, onTrayPickFile, onTrayShowRecvQR, func() {
		if disc != nil {
			disc.Close()
		}
		winMgr.Shutdown()
		srv.Shutdown()
	})
}

// portFromEnv 已迁移到 internal/config (applyEnv 内). 这里保留 stub 供老代码路径,
// 实际不再被调用 - 删除时机: 验证全链路通 + 一两个版本后.
// 当前 daemonURL 走 clientPort(), runDaemon 走 cfgMgr.Get().Server.Port.

// peerAdapter 把 internal/discovery.Discovery 适配成 server.PeerSource.
// 拆这层是因为 server 包不能直接 import discovery (避免循环依赖), 也避免
// server 包知道 zeroconf 的存在.
type peerAdapter struct{ d *discovery.Discovery }

func (a peerAdapter) Peers() []*server.Peer {
	src := a.d.Peers()
	out := make([]*server.Peer, len(src))
	for i, p := range src {
		out[i] = &server.Peer{
			UUID:    p.UUID,
			Name:    p.Name,
			Host:    p.Host,
			IPv4:    p.IPv4,
			Port:    p.Port,
			Version: p.Version,
			SeenAt:  p.SeenAt,
		}
	}
	return out
}

// peerMgrAdapter 把 internal/peer.Manager 适配成 server.PeerManager.
// server 接口签名是扁平参数, peer.Manager 内部用 struct, 这里做转换.
// 不在 peer 包加扁平方法是因为 peer 包用 struct 更自然.
type peerMgrAdapter struct{ m *peer.Manager }

func (a peerMgrAdapter) CreateOutgoing(to server.PeerInfo, absPath, fileName string, fileSize int64) (string, error) {
	o, _, err := a.m.CreateOutgoing(peer.PeerInfo{
		UUID: to.UUID, Name: to.Name, Host: to.Host, IPv4: to.IPv4, Port: to.Port,
	}, absPath, fileName, fileSize)
	if err != nil {
		return "", err
	}
	return o.Token, nil
}

func (a peerMgrAdapter) LookupOutgoing(token string) (absPath, fileName, toName string, fileSize int64, ok bool) {
	o, ok := a.m.LookupOutgoing(token)
	if !ok {
		return "", "", "", 0, false
	}
	return o.AbsPath, o.FileName, o.To.Name, o.FileSize, true
}

func (a peerMgrAdapter) MarkDelivered(token string) { a.m.MarkDelivered(token) }

func (a peerMgrAdapter) AddPending(token, fromUUID, fromName, fromHost, fromIPv4 string, fromPort int, fileName string, fileSize int64) error {
	return a.m.AddPending(peer.Incoming{
		Token: token,
		From: peer.PeerInfo{
			UUID: fromUUID, Name: fromName, Host: fromHost, IPv4: fromIPv4, Port: fromPort,
		},
		FileName: fileName,
		FileSize: fileSize,
	})
}

func (a peerMgrAdapter) LookupPending(token string) (fromIPv4 string, fromPort int, fromName, fileName string, fileSize int64, ok bool) {
	p := a.m.LookupPending(token)
	if p == nil {
		return "", 0, "", "", 0, false
	}
	return p.From.IPv4, p.From.Port, p.From.Name, p.FileName, p.FileSize, true
}

func (a peerMgrAdapter) SetPendingState(token, state string) bool {
	return a.m.SetPendingState(token, peer.State(state))
}

func (a peerMgrAdapter) PendingList() []server.PendingEntry {
	src := a.m.PendingList()
	out := make([]server.PendingEntry, len(src))
	for i, p := range src {
		out[i] = server.PendingEntry{
			Token: p.Token,
			State: string(p.State),
			From: server.Peer{
				UUID: p.From.UUID, Name: p.From.Name, Host: p.From.Host,
				IPv4: []string{p.From.IPv4}, Port: p.From.Port,
			},
			FileName: p.FileName,
			FileSize: p.FileSize,
			ArriveAt: p.ArriveAt.Unix(),
		}
	}
	return out
}

func (a peerMgrAdapter) PendingCount() int { return a.m.PendingCount() }

// SetOnChange 转发到底层 peer.Manager. main 用它把 server.emitPendingChange 接到 tray.
func (a peerMgrAdapter) SetOnChange(fn func()) { a.m.SetOnChange(fn) }

// deviceStoreAdapter 把 internal/devices.Store 适配成 server.DeviceStore.
// 抽这一层是为了 server 包不依赖 devices 包 (避免循环), 也屏蔽 Trust 类型转 string.
type deviceStoreAdapter struct{ s *devices.Store }

func (a deviceStoreAdapter) TrustOf(uuid string) string {
	return string(a.s.TrustOf(uuid))
}

func (a deviceStoreAdapter) UpsertSeen(uuid, name string) error {
	return a.s.UpsertSeen(uuid, name)
}

func (a deviceStoreAdapter) SetTrust(uuid, name, trust string) error {
	t := devices.Trust(trust)
	// 兜底: 空串当 ask, 反正 SetTrust 会校验
	if trust == "" {
		t = devices.TrustAsk
	}
	return a.s.SetTrust(uuid, name, t)
}

func (a deviceStoreAdapter) SetAlias(uuid, alias string) error {
	return a.s.SetAlias(uuid, alias)
}

func (a deviceStoreAdapter) Delete(uuid string) error {
	return a.s.Delete(uuid)
}

func (a deviceStoreAdapter) All() []server.DeviceEntry {
	src := a.s.All()
	out := make([]server.DeviceEntry, len(src))
	for i, d := range src {
		out[i] = server.DeviceEntry{
			UUID:      d.UUID,
			Name:      d.Name,
			Alias:     d.Alias,
			Trust:     string(d.Trust),
			FirstSeen: d.FirstSeen,
			LastSeen:  d.LastSeen,
		}
	}
	return out
}

// progressHubAdapter 把 internal/progress.Hub 适配成 server.ProgressPublisher.
type progressHubAdapter struct{ h *progress.Hub }

func (a progressHubAdapter) WrapReader(r io.Reader, id, kind, fileName string, fileSize int64) server.ProgressReader {
	return a.h.WrapReader(r, id, progress.Kind(kind), fileName, fileSize)
}

func (a progressHubAdapter) ServeWS(ctx context.Context, conn server.ProgressConn) {
	a.h.ServeWS(ctx, progressConnAdapter{conn})
}

// progressConnAdapter 把 server.ProgressConn 适配成 progress.Conn.
// 两边接口签名其实相同, 这层只为类型别名.
type progressConnAdapter struct{ c server.ProgressConn }

func (a progressConnAdapter) Write(ctx context.Context, msg []byte) error {
	return a.c.Write(ctx, msg)
}

func (a progressConnAdapter) Close(code int, reason string) error {
	return a.c.Close(code, reason)
}

// configAdapter 把 *config.Manager 适配成 server.ConfigStore.
// 拆 interface 避免 server 直接 import config (虽然不会循环, 但保 server 包纯净).
type configAdapter struct{ m *config.Manager }

// Snapshot /api/config GET 返回的 JSON 对象. 直接给完整 Config struct.
func (a configAdapter) Snapshot() any { return a.m.Get() }

// ApplyJSON /internal/config-save POST. 解 body → 校验 → 持久化 + 热应用.
// 校验/写盘交给 m.Save (它内部已经原子写 + 字段合法性检查).
// 热应用副作用 (autostart 注册表 / mDNS 重启) 也在这里串起来.
func (a configAdapter) ApplyJSON(body []byte) error {
	var next config.Config
	// 默认值兜底: 解到 next 之前先 Default, 这样 partial body 也能合并而不是清空字段
	next = a.m.Get()
	if err := json.Unmarshal(body, &next); err != nil {
		return fmt.Errorf("解析配置 JSON: %w", err)
	}
	// 立刻持久化 + 替换内存副本 (Save 内部加锁)
	if err := a.m.Save(next); err != nil {
		return err
	}
	// autostart 副作用: 同步 HKCU\Run
	if err := installer.SyncAutostart(next.System.Autostart); err != nil {
		log.Printf("ApplyJSON: 同步 autostart 注册表失败: %v", err)
		// 注册表失败不阻塞 config 保存; UI 端会看到状态没改
	}
	log.Printf("config 已热应用: port=%d conflict=%s maxSize=%d toast=%v reveal=%v mdns=%v autostart=%v",
		next.Server.Port, next.Download.Conflict, next.Receive.MaxFileSize,
		next.UI.ToastsEnabled, next.UI.RevealOnDone, next.Server.MdnsEnabled, next.System.Autostart)
	return nil
}

func (a configAdapter) ResolvedDownloadDir() (string, error) { return a.m.ResolvedDownloadDir() }
func (a configAdapter) Conflict() string                     { return string(a.m.Get().Download.Conflict) }
func (a configAdapter) MaxFileSize() int64                   { return a.m.Get().Receive.MaxFileSize }
func (a configAdapter) ToastsEnabled() bool                  { return a.m.Get().UI.ToastsEnabled }
func (a configAdapter) RevealOnDone() bool                   { return a.m.Get().UI.RevealOnDone }
func (a configAdapter) MdnsEnabled() bool                    { return a.m.Get().Server.MdnsEnabled }
func (a configAdapter) Autostart() bool                      { return a.m.Get().System.Autostart }

// installerAdapter 包装 installer 包函数, 适配 server.InstallerInterface.
// 用空 struct, 因为 installer 包的所有函数都是包级别的.
type installerAdapter struct{}

func (installerAdapter) Install(exePath string) error  { return installer.Install(exePath) }
func (installerAdapter) Uninstall() error              { return installer.Uninstall() }
func (installerAdapter) IsInstalled() (bool, string)   { return installer.IsInstalled() }
func (installerAdapter) IsURLSchemeInstalled() bool    { return installer.IsURLSchemeInstalled() }

// revealInExplorer Windows: 启 explorer.exe /select,<absPath> 让 Explorer 高亮该文件.
// 其他平台 noop. 失败只 log, 不致命 (用户至少能去 Downloads 文件夹自己找).
func revealInExplorer(absPath string) {
	if runtime.GOOS != "windows" {
		return
	}
	// 注意 explorer /select 的参数有逗号: "/select,C:\path"
	// 用 cmd /c start "" 也可以, 但直接 explorer 干净.
	cmd := exec.Command("explorer.exe", "/select,"+absPath)
	if err := cmd.Start(); err != nil {
		log.Printf("revealInExplorer %s 失败: %v", absPath, err)
		return
	}
	// 不等它退出 (explorer 会一直驻留)
	go func() { _ = cmd.Wait() }()
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
