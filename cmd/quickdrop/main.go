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
	"embed"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"quickdrop/internal/devices"
	"quickdrop/internal/discovery"
	"quickdrop/internal/identity"
	"quickdrop/internal/installer"
	"quickdrop/internal/notify"
	"quickdrop/internal/peer"
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

	// envPort: 覆盖 daemon HTTP 端口 (默认 8443).
	// 主要用于同机起多个 daemon 做 PC→PC 联调测试 (test-discovery.ps1).
	// probeDaemon / notifyDaemon 都用同一端口, 这样 "QUICKDROP_PORT=8444 quickdrop send X"
	// 只跟 8444 上的 daemon 对话, 不影响 8443.
	envPort = "QUICKDROP_PORT"

	// version 软件版本, 写入 mDNS TXT 给对端做兼容性判断.
	version = "0.8.0"
)

// daemonURL 返回客户端模式要 POST 给本机 daemon 的 base URL.
// 用 envPort, 这样多端口测试时各自找各自的 daemon.
func daemonURL() string {
	return fmt.Sprintf("http://127.0.0.1:%d", portFromEnv())
}

func usage() {
	fmt.Fprintf(os.Stderr, `QuickDrop - 局域网快速发送/接收

用法:
  %s send <文件路径>      发送模式
  %s <文件路径>           同上 (拖拽到 .exe 也走此分支)
  %s recv                 接收模式 (从手机往电脑传)
  %s install              安装右键菜单 "通过 QuickDrop 发送"
  %s uninstall            卸载右键菜单
  %s status               打印当前 daemon / 右键菜单状态
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
		log.Fatalf("通知 daemon 失败: %v (daemon 没在跑?)", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		rb, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		log.Fatalf("daemon 返回 %d: %s", resp.StatusCode, strings.TrimSpace(string(rb)))
	}
	log.Printf("daemon 已确认 %s, 退出", decision)
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
// initialPath 为空表示纯接收模式启动 (此时 initialReceive 必须 true, 否则 daemon 啥也不干).
// initialReceive 表示 daemon 启动后立刻开启接收模式 + 弹接收窗.
func runDaemon(initialPath string, initialReceive bool) {
	port := portFromEnv()

	// 加载/生成本机身份 (UUID + 显示名)
	ident, err := identity.Load()
	if err != nil {
		log.Fatalf("加载 identity: %v", err)
	}
	log.Printf("身份: %s (%s)", ident.Name, ident.UUID[:8])

	// 自检: URL scheme 未注册时 toast 按钮点了会报 "没有应用能打开此链接".
	// 不致命 (用户可能只想发送), 但 log warn 提示用户跑 install 修复.
	if !installer.IsURLSchemeInstalled() {
		log.Print("WARN: quickdrop:// URL scheme 未注册, toast 接收按钮将无法工作. " +
			"运行 `quickdrop install` 修复 (右键菜单 + URL scheme 一起装)")
	}

	srv, err := server.New(initialPath, port)
	if err != nil {
		log.Fatal(err)
	}
	srv.SetDist(distFS)
	srv.SetIdentity(ident.UUID, ident.Name)
	srv.SetPeerManager(peerMgrAdapter{peer.NewManager()})

	// 加载设备信任表 (ADR-20). 失败不致命, 退回到"所有设备 ask".
	if devStore, err := devices.Load(); err == nil {
		srv.SetDeviceStore(deviceStoreAdapter{devStore})
		log.Printf("已加载设备信任表: %d 条记录", len(devStore.All()))
	} else {
		log.Printf("加载设备信任表失败 (退回到全 ask): %v", err)
	}

	srv.SetOnPeerIncoming(func(fromName, fileName string, fileSize int64, token string) {
		notify.Incoming(fromName, fileName, fileSize, token)
	})
	srv.SetOnPeerAccepted(func(fromName, fileName string, fileSize int64) {
		notify.IncomingSilent(fromName, fileName, fileSize)
	})
	srv.SetOnPeerSent(func(toName, fileName string, fileSize int64) {
		notify.PeerSent(toName, fileName, fileSize)
	})
	srv.SetOnPeerReceived(func(fromName, fileName string, fileSize int64) {
		notify.PeerReceived(fromName, fileName, fileSize)
	})
	srv.SetOnUploadDone(func(count int) {
		notify.UploadDone(count)
	})
	srv.SetOnPendingChange(func(count int) {
		tray.SetPendingCount(count)
	})

	// mDNS: 广播自己 + 发现局域网内其他 QuickDrop. 失败不致命.
	disc, err := discovery.Start(ident.UUID, ident.Name, version, port)
	if err != nil {
		log.Printf("mDNS 启动失败 (PC→PC 发现不可用, 但其他功能正常): %v", err)
	} else {
		srv.SetPeerSource(peerAdapter{disc})
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

	// 托盘 "待处理 (N)" 菜单点击 → 起 pending dashboard 子窗 (单实例)
	pendingURL := srv.HomeURL()[:len(srv.HomeURL())-1] + "/p" // baseURL/p
	onTrayPending := func() {
		winMgr.OpenPendingWindow(pendingURL)
	}

	// 托盘 "设备管理" 菜单点击 → 起 devices dashboard 子窗 (单实例)
	devicesURL := srv.HomeURL()[:len(srv.HomeURL())-1] + "/v" // baseURL/v
	onTrayDevices := func() {
		winMgr.OpenDevicesWindow(devicesURL)
	}

	tray.Run(srv.MobileURL(), srv.CurrentFileName(), onTrayReceive, onTrayPending, onTrayDevices, func() {
		if disc != nil {
			disc.Close()
		}
		winMgr.Shutdown()
		srv.Shutdown()
	})
}

// portFromEnv 读 QUICKDROP_PORT, 解析失败/空回 8443. 测试支持同机多 daemon.
func portFromEnv() int {
	if v := strings.TrimSpace(os.Getenv(envPort)); v != "" {
		n, err := strconv.Atoi(v)
		if err == nil && n > 0 && n < 65536 {
			return n
		}
		log.Printf("无效的 %s=%q, 用默认 8443", envPort, v)
	}
	return 8443
}

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

func (a deviceStoreAdapter) All() []server.DeviceEntry {
	src := a.s.All()
	out := make([]server.DeviceEntry, len(src))
	for i, d := range src {
		out[i] = server.DeviceEntry{
			UUID:      d.UUID,
			Name:      d.Name,
			Trust:     string(d.Trust),
			FirstSeen: d.FirstSeen,
			LastSeen:  d.LastSeen,
		}
	}
	return out
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
