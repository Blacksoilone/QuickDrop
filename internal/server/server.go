// Package server hosts the QuickDrop HTTP routes
// (/, /d, /r, /u, /file, /qr, /qr-recv, /upload) plus internal IPC routes
// (/internal/health, /internal/send, /internal/receive) and the JSON API
// (/api/info) for the Vue 前端 (2.7).
//
// ADR-17 路由职责:
//   /        电脑端发送 dashboard (Vue 渲染, GET 拉 /api/info), 只渲 QR + 文件名/大小 + 关闭键
//   /d       手机端发送目标页 (Vue), 文件图标 + 信息 + 下载
//   /qr      PNG QR, 编码 baseURL + /d (发送模式)
//   /file    实际文件下载, http.ServeFile
//   /r       电脑端接收 dashboard (Vue), QR + 提示 + 停止接收键
//   /u       手机端上传表单 (Vue, 受 receiveMode 门禁)
//   /qr-recv PNG QR, 编码 baseURL + /u (接收模式)
//   /upload  实际接收上传, 默认 404 (ADR-17 安全约束), 由 EnableReceive(true) 开启
//   /api/info JSON 服务状态, Vue 前端拉取
//   /assets/* embed.FS 静态资源 (Vue chunk + CSS)
//
// 单进程 daemon 模式: 第一次 quickdrop send X 起 daemon, 后续 send Y 走 IPC
// 切换当前发送文件, 命令行立即退出. 见 cmd/quickdrop/main.go.
package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"quickdrop/internal/qr"
)

// Server 持有 daemon 状态 + 底层 http.Server.
// 一次进程一个 Server, 通过 New + Start/Shutdown 管理生命周期.
//
// 当前发送文件 (absPath/fileName/fileSize) 用 mu 保护, SwapFile 可在运行中替换.
// 纯接收模式启动时 absPath 为空, 发送类路由返回 404.
type Server struct {
	mu       sync.RWMutex
	absPath  string // 待发送文件的绝对路径; "" 表示纯接收模式
	fileName string // 文件名 (展示+下载头用)
	fileSize string // 人类可读大小

	// receiveMode atomic.Bool: 是否开启接收模式. 默认 false → /upload 返回 404.
	// 由 EnableReceive(bool) 切换. ADR-17 安全约束: 任何陌生人扫到发送 QR 都不能
	// 往本机塞文件, 上传仅在接收模式开启时可用.
	receiveMode atomic.Bool

	port       int    // HTTP 端口, 默认 8443; QUICKDROP_PORT env 可覆盖 (测试用)
	homeURL    string // 电脑端发送 dashboard URL
	receiveURL string // 电脑端接收 dashboard URL
	mobileURL  string // 手机端发送目标 URL (QR 编码 + 托盘 "复制扫码链接" 复制)
	uploadURL  string // 手机端上传 URL (接收模式 QR 编码)
	baseURL    string // http://<lan>:<port>
	httpSrv    *http.Server

	// dist 是 Vite 构建产物的 fs (含 index.html / d.html / r.html / u.html / assets/*).
	// 由 cmd/quickdrop 通过 SetDist 注入 (因为 //go:embed 必须在 main 包里相对仓库根).
	dist fs.FS

	// peerSource 提供局域网内发现的 PC 列表 (来自 internal/discovery).
	// nil 时 /api/peers 返回空数组. 由 SetPeerSource 注入.
	peerSource PeerSource

	// peers 管理 PC↔PC 文件传输状态 (Outgoing + Pending).
	// 由 SetPeerManager 注入. nil 时 /peer/* 路由全 404.
	peers PeerManager

	// deviceStore 设备信任表 (ADR-20). 由 SetDeviceStore 注入.
	// nil 时所有设备视为 ask, /api/devices 返回空.
	deviceStore DeviceStore

	// progress 推实时传输进度给 Vue (/ws). 由 SetProgressHub 注入. nil 时不推.
	progress ProgressPublisher

	// cfg 运行时配置 (~/.quickdrop/config.json). 由 SetConfig 注入.
	// nil 时全用硬编码默认值 (向后兼容).
	cfg ConfigStore

	// myIdentity 本机身份 (UUID + 显示名), 给 /peer/incoming 时填 from 字段用.
	// 由 SetIdentity 注入.
	myUUID string
	myName string

	onSwap          func(newName string)                                            // 文件被 SwapFile 切换后回调 (tray tooltip 用)
	onReceive       func(on bool)                                                   // 接收模式切换后回调 (main 起/关 receive webview)
	onPeerIncoming  func(fromName, fileName string, fileSize int64, token string)   // peer incoming ask 时弹 toast (按钮)
	onPeerAccepted  func(fromName, fileName string, fileSize int64)                 // peer incoming trusted 自动接受时弹纯通知
	onPeerSent      func(toName, fileName string, fileSize int64)                   // 我作为 sender, 对端 Pull 完成时
	onPeerReceived  func(fromName, fileName string, fileSize int64)                 // 我作为 receiver (ask 路径), Pull 完成时. trusted 路径不调 (已被 onPeerAccepted 告知)
	onUploadDone    func(count int)                                                 // 手机 /upload 完成时
	onFileSaved     func(absPath string)                                            // 任意接收路径文件落盘后. main 用它做 RevealOnDone
	onPendingChange func(count int)                                                 // 待处理数变化后回调 (tray 红点+菜单显隐)
}

// PeerSource 注入接口, server 通过它拉发现到的对端列表 (避免 server 直接依赖 discovery 包).
type PeerSource interface {
	Peers() []*Peer
}

// PeerManager 注入接口, server 通过它操作 PC↔PC 传输状态 (Outgoing/Pending).
// 实际实现是 internal/peer.Manager, 这里抽 interface 避免引入循环依赖 +
// 让 server.go 不依赖具体实现.
type PeerManager interface {
	// Sender 端
	CreateOutgoing(to PeerInfo, absPath, fileName string, fileSize int64) (token string, err error)
	LookupOutgoing(token string) (absPath, fileName, toName string, fileSize int64, ok bool)
	MarkDelivered(token string)
	// Receiver 端
	AddPending(token, fromUUID, fromName, fromHost, fromIPv4 string, fromPort int, fileName string, fileSize int64) error
	LookupPending(token string) (fromIPv4 string, fromPort int, fromName, fileName string, fileSize int64, ok bool)
	SetPendingState(token, state string) bool
	PendingList() []PendingEntry
	PendingCount() int
	// 通知层
	SetOnChange(fn func()) // pending/outgoing 变化时触发 (gc 也算), 给 server 用于 emitPendingChange
}

// PeerInfo 与 internal/peer.PeerInfo 同结构, server 包独立定义避免循环.
type PeerInfo struct {
	UUID string
	Name string
	Host string
	IPv4 string
	Port int
}

// PendingEntry 给 /api/pending 用的快照.
type PendingEntry struct {
	Token    string `json:"token"`
	State    string `json:"state"`
	From     Peer   `json:"from"`
	FileName string `json:"fileName"`
	FileSize int64  `json:"fileSize"`
	ArriveAt int64  `json:"arriveAt"` // Unix 秒
	Trust    string `json:"trust"`    // 该发送方设备当前 trust (ask/trusted/blocked), 给 Vue 显示徽章用
}

// DeviceStore 信任表接口. 实现是 internal/devices.Store, 抽 interface 避免循环 import.
type DeviceStore interface {
	TrustOf(uuid string) string                       // 返 "ask" / "trusted" / "blocked", 未知设备返 "ask"
	UpsertSeen(uuid, name string) error               // 收到 incoming 时更新最近见到
	SetTrust(uuid, name, trust string) error          // 用户操作 /v 管理页或 pending 决策时调
	All() []DeviceEntry                               // 列所有已知设备
}

// DeviceEntry 给 /api/devices 用的快照.
type DeviceEntry struct {
	UUID      string `json:"uuid"`
	Name      string `json:"name"`
	Trust     string `json:"trust"`
	FirstSeen int64  `json:"firstSeen"`
	LastSeen  int64  `json:"lastSeen"`
}

// ProgressPublisher 注入接口, server 通过它发布传输进度事件.
// 实际实现是 internal/progress.Hub. 抽 interface 避免 server 直接依赖 progress 包.
type ProgressPublisher interface {
	// WrapReader 包装一个 io.Reader 让其按字节计数 + 节流推进度.
	// fileSize=-1 表示未知.
	// 返回的 reader 实现 io.Reader, 同时有一个 Done(err) 方法可以在结束时强制推最终一帧.
	WrapReader(r io.Reader, id, kind, fileName string, fileSize int64) ProgressReader
	// ServeWS 给一个 ws 连接跑订阅 → 写循环, 阻塞到 ctx done 或 client 断开.
	ServeWS(ctx context.Context, conn ProgressConn)
}

// ProgressReader 由 WrapReader 返回. 透传 Read, 额外有 Done().
type ProgressReader interface {
	io.Reader
	Done(err error)
}

// ProgressConn 由 ServeWS 用. websocket conn 实现.
type ProgressConn interface {
	Write(ctx context.Context, msg []byte) error
	Close(code int, reason string) error
}

// ConfigStore 注入接口, server 通过它读/写运行时配置.
// 实际实现是 internal/config.Manager. 拆 interface 避免循环.
type ConfigStore interface {
	// Snapshot 返回完整 config 的 JSON-serializable 副本. 给 /api/config GET 用.
	Snapshot() any
	// ApplyJSON 接 /internal/config-save POST 的原始 body, 校验合法 + 持久化 + 热应用.
	// 返回 error 时 daemon 端不变.
	ApplyJSON(body []byte) error
	// ResolvedDownloadDir 取当前生效的下载目录 (空 → 默认), 同时 MkdirAll.
	ResolvedDownloadDir() (string, error)
	// Conflict 当前同名文件冲突策略 (rename/overwrite/reject).
	Conflict() string
	// MaxFileSize 接收上限 (字节). 0 表示不限.
	MaxFileSize() int64
	// ToastsEnabled toast 总开关.
	ToastsEnabled() bool
	// RevealOnDone 接收完成自动 Explorer reveal.
	RevealOnDone() bool
	// MdnsEnabled mDNS 广播开关.
	MdnsEnabled() bool
	// Autostart 开机自启 (Windows Run 注册表).
	Autostart() bool
}

// Peer 与 internal/discovery.Peer 字段同名同义, 为了 server → JSON 转换不引入循环依赖,
// 这里独立定义一份. JSON 字段保持小写驼峰与前端一致.
type Peer struct {
	UUID    string   `json:"uuid"`
	Name    string   `json:"name"`
	Host    string   `json:"host"`
	IPv4    []string `json:"ipv4"`
	Port    int      `json:"port"`
	Version string   `json:"version"`
	SeenAt  int64    `json:"seenAt"`
}

// New 装配 Server. rawPath 为空表示纯接收模式启动 (daemon 启动时无文件可发).
// port 通常是 8443; 为支持多 daemon 同机测试可用 QUICKDROP_PORT env 覆盖.
// 不启动 listener.
func New(rawPath string, port int) (*Server, error) {
	if port <= 0 {
		port = 8443
	}
	lanIP := getLANIP()
	if lanIP == "" {
		return nil, fmt.Errorf("没找到对外 LAN IP, 检查网卡和路由")
	}

	baseURL := fmt.Sprintf("http://%s:%d", lanIP, port)
	s := &Server{
		port:       port,
		homeURL:    baseURL + "/",
		receiveURL: baseURL + "/r",
		mobileURL:  baseURL + "/d",
		uploadURL:  baseURL + "/u",
		baseURL:    baseURL,
	}

	if rawPath != "" {
		absPath, name, size, err := validateFile(rawPath)
		if err != nil {
			return nil, err
		}
		s.absPath = absPath
		s.fileName = name
		s.fileSize = size
	}
	return s, nil
}

// HomeURL 电脑端发送模式 dashboard URL. webview 加载用.
func (s *Server) HomeURL() string { return s.homeURL }

// ReceiveURL 电脑端接收模式 dashboard URL. 接收 webview 加载用.
func (s *Server) ReceiveURL() string { return s.receiveURL }

// MobileURL 手机端发送目标 URL. 托盘 "复制扫码链接" 复制这个 (给朋友扫/打开).
func (s *Server) MobileURL() string { return s.mobileURL }

// HasFile 当前是否有可发送的文件. 用于 main 决定要不要开发送窗.
func (s *Server) HasFile() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.absPath != ""
}

// EnableReceive 开/关接收模式 (是否真正接受 /upload 上传). ADR-17 安全约束.
// 触发 onReceive 回调让 main 起/关 receive webview.
func (s *Server) EnableReceive(on bool) {
	prev := s.receiveMode.Swap(on)
	if prev != on {
		log.Printf("接收模式: %v → %v", prev, on)
		if s.onReceive != nil {
			s.onReceive(on)
		}
	}
}

// IsReceiving 当前是否在接收模式.
func (s *Server) IsReceiving() bool { return s.receiveMode.Load() }

// CurrentFileName 返回当前发送的文件名 (并发安全).
func (s *Server) CurrentFileName() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.fileName
}

// SetOnSwap 注册文件切换回调, 用于 tray 更新 tooltip.
// 必须在 Start 之前调用.
func (s *Server) SetOnSwap(fn func(newName string)) {
	s.onSwap = fn
}

// SetOnReceive 注册接收模式切换回调, 用于 main 起/关 receive webview.
// 必须在 Start 之前调用.
func (s *Server) SetOnReceive(fn func(on bool)) {
	s.onReceive = fn
}

// SetDist 注入 Vite 构建产物 fs (含 index.html / d.html / r.html / u.html / assets/*).
// 必须在 Start 之前调用. 由 cmd/quickdrop 提供, 因为 //go:embed 路径必须在
// 引用它的包内相对存在, 而 web/dist 在仓库根.
func (s *Server) SetDist(dist fs.FS) {
	s.dist = dist
}

// SetPeerSource 注入对端发现源 (来自 internal/discovery).
// 必须在 Start 之前调用. 不调用时 /api/peers 返回空数组.
func (s *Server) SetPeerSource(src PeerSource) {
	s.peerSource = src
}

// SetPeerManager 注入 PC↔PC 传输状态机 (来自 internal/peer.Manager).
// 必须在 Start 之前调用. 不调用时 /peer/* 路由全 404.
func (s *Server) SetPeerManager(pm PeerManager) {
	s.peers = pm
}

// SetIdentity 注入本机身份, 用于 POST /peer/incoming 时填 from 字段.
// 必须在 Start 之前调用.
func (s *Server) SetIdentity(uuid, name string) {
	s.myUUID = uuid
	s.myName = name
}

// SetOnPeerIncoming 注册 peer incoming (ask) 到达回调 (典型用法: 弹 toast 按钮).
// 必须在 Start 之前调用.
func (s *Server) SetOnPeerIncoming(fn func(fromName, fileName string, fileSize int64, token string)) {
	s.onPeerIncoming = fn
}

// SetOnPeerAccepted 注册 peer incoming 因信任而自动接受时的回调 (典型: 弹纯通知 toast).
// 必须在 Start 之前调用.
func (s *Server) SetOnPeerAccepted(fn func(fromName, fileName string, fileSize int64)) {
	s.onPeerAccepted = fn
}

// SetOnPeerSent 注册"我作为发送方, 对端 Pull 完成"回调 (典型: 弹"已发送给 X" toast).
// 必须在 Start 之前调用.
func (s *Server) SetOnPeerSent(fn func(toName, fileName string, fileSize int64)) {
	s.onPeerSent = fn
}

// SetOnPeerReceived 注册"我作为接收方 (ask 路径), Pull 完成"回调.
// trusted 路径不调 (已由 onPeerAccepted 通知, 避免重复 toast).
// 必须在 Start 之前调用.
func (s *Server) SetOnPeerReceived(fn func(fromName, fileName string, fileSize int64)) {
	s.onPeerReceived = fn
}

// SetOnUploadDone 注册"手机 /upload 完成"回调 (典型: 弹"已收到 N 个文件" toast).
// 必须在 Start 之前调用.
func (s *Server) SetOnUploadDone(fn func(count int)) {
	s.onUploadDone = fn
}

// SetOnFileSaved 注册"任意接收路径文件落盘"回调.
// 典型用法: main 据 RevealOnDone 配置决定是否 explorer /select.
// 必须在 Start 之前调用.
func (s *Server) SetOnFileSaved(fn func(absPath string)) {
	s.onFileSaved = fn
}

// SetOnPendingChange 注册待处理数变化回调 (典型用法: tray.SetPendingCount).
// AddPending / SetPendingState / GC expire 都会触发.
// 必须在 Start 之前调用.
func (s *Server) SetOnPendingChange(fn func(count int)) {
	s.onPendingChange = fn
}

// SetDeviceStore 注入设备信任表 (来自 internal/devices.Store).
// 必须在 Start 之前调用. nil 时所有设备视为 ask.
func (s *Server) SetDeviceStore(d DeviceStore) {
	s.deviceStore = d
}

// SetProgressHub 注入进度发布器 (来自 internal/progress.Hub).
// 必须在 Start 之前调用. nil 时 /ws 返回 404 + 文件传输不推进度.
func (s *Server) SetProgressHub(p ProgressPublisher) {
	s.progress = p
}

// SetConfig 注入运行时配置 (来自 internal/config.Manager).
// 必须在 Start 之前调用. nil 时所有字段走硬编码默认值.
func (s *Server) SetConfig(c ConfigStore) {
	s.cfg = c
}

// emitPendingChange 在 peer manager 状态可能变化后调用.
// 拉最新 pending count 传给回调. 由 handlePeerIncoming / handleInternalPeerDecide 等触发.
func (s *Server) emitPendingChange() {
	if s.onPendingChange == nil || s.peers == nil {
		return
	}
	s.onPendingChange(s.peers.PendingCount())
}

// SwapFile 把当前发送文件切换成 rawPath. 并发安全.
// 用于 IPC: 第二次 quickdrop send Y 不重启进程, 直接更新这里.
func (s *Server) SwapFile(rawPath string) error {
	absPath, name, size, err := validateFile(rawPath)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.absPath = absPath
	s.fileName = name
	s.fileSize = size
	s.mu.Unlock()
	log.Printf("切换发送文件: %s (%s)", absPath, size)
	if s.onSwap != nil {
		s.onSwap(name)
	}
	return nil
}

// Start 在当前 goroutine 之外起 listener, 立刻返回. 出错通过 log.Fatalf 兜底.
// 调用者通常: go server.Start() 然后让 main goroutine 跑 systray.
func (s *Server) Start() {
	if s.dist == nil {
		log.Fatal("Server.Start: dist 未注入, 调用方必须先 SetDist")
	}
	s.cleanupStaleTmp()

	// 把 peer manager 的变化也桥到 emitPendingChange (gc 过期项时也要刷新 tray)
	if s.peers != nil {
		s.peers.SetOnChange(s.emitPendingChange)
	}

	mux := http.NewServeMux()
	// 发送侧路由 (无文件时 404)
	mux.HandleFunc("/file", s.handleFile)
	mux.HandleFunc("/qr", s.handleQR)
	mux.HandleFunc("/d", s.handleDownload)
	mux.HandleFunc("/", s.handleIndex)
	// 接收侧路由 (始终注册, 但 /u 和 /upload 受 receiveMode 门禁)
	mux.HandleFunc("/qr-recv", s.handleQRRecv)
	mux.HandleFunc("/r", s.handleReceiveDashboard)
	mux.HandleFunc("/u", s.handleUploadForm)
	mux.HandleFunc("/upload", s.handleUpload)
	// Peer 待处理列表 dashboard (Vue), 不受 receiveMode 门禁 (PC→PC 走另一套路)
	mux.HandleFunc("/p", s.handlePendingDashboard)
	mux.HandleFunc("/v", s.handleDevicesDashboard)
	mux.HandleFunc("/c", s.handleConfigDashboard)
	// JSON API (Vue 前端拉服务状态)
	mux.HandleFunc("/api/info", s.handleAPIInfo)
	mux.HandleFunc("/api/peers", s.handleAPIPeers)
	mux.HandleFunc("/api/pending", s.handleAPIPending)
	mux.HandleFunc("/api/devices", s.handleAPIDevices)
	mux.HandleFunc("/api/config", s.handleAPIConfig)
	// PC↔PC peer 传输 (来自其他 daemon)
	mux.HandleFunc("/peer/incoming", s.handlePeerIncoming)
	mux.HandleFunc("/peer/file", s.handlePeerFile)
	// IPC (仅 127.0.0.1)
	mux.HandleFunc("/internal/health", requireLocal(s.handleInternalHealth))
	mux.HandleFunc("/internal/send", requireLocal(s.handleInternalSend))
	mux.HandleFunc("/internal/receive", requireLocal(s.handleInternalReceive))
	mux.HandleFunc("/internal/peer-send", requireLocal(s.handleInternalPeerSend))
	mux.HandleFunc("/internal/peer-decide", requireLocal(s.handleInternalPeerDecide))
	mux.HandleFunc("/internal/device-trust", requireLocal(s.handleInternalDeviceTrust))
	mux.HandleFunc("/internal/config-save", requireLocal(s.handleInternalConfigSave))
	// WebSocket: 推实时传输进度给 Vue. 任意 LAN 都能连 (只读, 不暴露敏感信息).
	mux.HandleFunc("/ws", s.handleWS)
	// 静态资源 (Vue chunks / CSS)
	mux.Handle("/assets/", http.FileServer(http.FS(s.dist)))

	s.httpSrv = &http.Server{Addr: fmt.Sprintf(":%d", s.port), Handler: mux}

	if s.absPath != "" {
		log.Printf("发送文件: %s (%s)", s.absPath, s.fileSize)
		log.Printf("电脑端:   %s", s.homeURL)
		log.Printf("手机端:   %s", s.mobileURL)
		log.Printf("直链:     %s/file", s.baseURL)
	} else {
		log.Print("未指定文件, 仅启 daemon (可通过托盘/CLI 进入接收模式或后续 send)")
	}
	log.Printf("监听:     0.0.0.0:%d", s.port)

	go func() {
		if err := s.httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server 退出: %v", err)
		}
	}()
}

// Shutdown 给 5 秒 graceful 窗口结束当前传输, 超时强杀.
// 用于托盘 "退出" 菜单点击之后 systray.Quit 之前.
func (s *Server) Shutdown() {
	if s.httpSrv == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.httpSrv.Shutdown(ctx); err != nil {
		log.Printf("HTTP server 关闭失败: %v", err)
	}
}

func (s *Server) handleFile(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	absPath, name := s.absPath, s.fileName
	s.mu.RUnlock()
	if absPath == "" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Disposition", contentDisposition(name))
	http.ServeFile(w, r, absPath)
}

// handleQR 把 mobileURL 渲染成 PNG, 给 / dashboard 内嵌 <img src="/qr"> 用.
// QR 编码 /d (手机扫码进手机端发送页), 不是电脑端 dashboard.
// 无文件时 404 (纯接收模式下没什么可扫的发送 QR).
func (s *Server) handleQR(w http.ResponseWriter, r *http.Request) {
	if !s.HasFile() {
		http.NotFound(w, r)
		return
	}
	png, err := qr.Render(s.mobileURL)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(png)
}

// handleQRRecv: /qr-recv PNG QR 编码接收 URL (/u). 接收 dashboard 用.
// 不受 receiveMode 门禁 (用户从托盘点 "接收文件" 时窗口要立刻显示 QR).
func (s *Server) handleQRRecv(w http.ResponseWriter, r *http.Request) {
	png, err := qr.Render(s.uploadURL)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(png)
}

// handleIndex / : 电脑端发送模式 webview 弹窗加载.
// 极简 dashboard: 只渲 QR + 当前文件名 + 大小 + 关闭按钮 (ADR-17).
// 无文件时 404 (纯接收模式 daemon 没东西可展示).
// handleIndex / : 电脑端发送模式 webview 弹窗加载.
// 服务 web/dist/index.html (Vue 渲染骨架, 内容靠 JS 拉 /api/info).
// 无文件时 404 (纯接收模式 daemon 没东西可展示).
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if !s.HasFile() {
		http.NotFound(w, r)
		return
	}
	s.serveDistFile(w, r, "index.html")
}

// handleDownload /d : 手机端发送目标页 (扫码进的就是这里).
// 服务 web/dist/d.html.
// 无文件时 404.
func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	if !s.HasFile() {
		http.NotFound(w, r)
		return
	}
	s.serveDistFile(w, r, "d.html")
}

// handleReceiveDashboard /r : 电脑端接收模式 webview 弹窗加载.
// 服务 web/dist/r.html. 始终可达, 不受 receiveMode 门禁.
func (s *Server) handleReceiveDashboard(w http.ResponseWriter, r *http.Request) {
	s.serveDistFile(w, r, "r.html")
}

// handleUploadForm /u : 手机端上传表单. 服务 web/dist/u.html.
// 受 receiveMode 门禁: 关时 404.
func (s *Server) handleUploadForm(w http.ResponseWriter, r *http.Request) {
	if !s.receiveMode.Load() {
		http.NotFound(w, r)
		return
	}
	s.serveDistFile(w, r, "u.html")
}

// handlePendingDashboard /p : 待处理 incoming 列表页 (Vue).
// daemon 弹独立 webview 子窗加载这里. 不受 receiveMode 门禁
// (待处理跟接收模式独立, PC→PC 是单独的安全模型).
func (s *Server) handlePendingDashboard(w http.ResponseWriter, r *http.Request) {
	s.serveDistFile(w, r, "p.html")
}

// handleDevicesDashboard /v : 设备管理页 (Vue). 列出所有已知设备,
// 用户可设/撤 trust/block (ADR-20). 始终可达.
//
// v0.11.0 起设备管理已合并进 /c 配置中心. /v 保留向后兼容: 还能用,
// 但内置一条提示让用户改去 /c#devices. 后续版本会重定向 / 删除.
func (s *Server) handleDevicesDashboard(w http.ResponseWriter, r *http.Request) {
	s.serveDistFile(w, r, "v.html")
}

// handleConfigDashboard /c : 配置中心页 (Vue). 包含所有可调项 + 设备管理.
// 给 webview 子窗用, 也支持浏览器直接访问 (127.0.0.1:port/c).
func (s *Server) handleConfigDashboard(w http.ResponseWriter, r *http.Request) {
	s.serveDistFile(w, r, "c.html")
}

// serveDistFile 从嵌入的 web/dist 读取文件, 加 no-store 防手机缓存看到旧文件名.
func (s *Server) serveDistFile(w http.ResponseWriter, r *http.Request, name string) {
	data, err := fs.ReadFile(s.dist, name)
	if err != nil {
		http.Error(w, "web 资源缺失: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(data)
}

// serverInfo /api/info 返回的 JSON. 必须与 web/src/api.ts 的 ServerInfo 保持一致.
type serverInfo struct {
	FileName  string `json:"fileName"`
	FileSize  string `json:"fileSize"`
	HasFile   bool   `json:"hasFile"`
	Receiving bool   `json:"receiving"`
}

// handleAPIInfo /api/info : Vue 前端拉服务状态 (当前文件 + 接收开关).
func (s *Server) handleAPIInfo(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	name, size, has := s.fileName, s.fileSize, s.absPath != ""
	s.mu.RUnlock()
	info := serverInfo{
		FileName:  name,
		FileSize:  size,
		HasFile:   has,
		Receiving: s.receiveMode.Load(),
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(info)
}

// handleAPIPeers /api/peers : Vue 前端 + 托盘 "发送到..." 菜单拉局域网 PC 列表.
// 没有 peerSource 时返回 [] 而不是 null, 前端可以无脑 .map().
func (s *Server) handleAPIPeers(w http.ResponseWriter, r *http.Request) {
	peers := []*Peer{}
	if s.peerSource != nil {
		peers = s.peerSource.Peers()
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(peers)
}

// handleAPIPending /api/pending : Vue 前端拉待决策的 incoming 列表.
// 给 2.5e 的红点 UI / pending 列表页用. 每条 join device store 的 trust 字段.
func (s *Server) handleAPIPending(w http.ResponseWriter, r *http.Request) {
	list := []PendingEntry{}
	if s.peers != nil {
		list = s.peers.PendingList()
	}
	// join trust
	if s.deviceStore != nil {
		for i := range list {
			list[i].Trust = s.deviceStore.TrustOf(list[i].From.UUID)
		}
	} else {
		for i := range list {
			list[i].Trust = "ask"
		}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(list)
}

// handleAPIDevices /api/devices : Vue 设备管理页 (/v) 拉所有已知设备.
func (s *Server) handleAPIDevices(w http.ResponseWriter, r *http.Request) {
	list := []DeviceEntry{}
	if s.deviceStore != nil {
		list = s.deviceStore.All()
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(list)
}

// handleAPIConfig /api/config : Vue 配置页拉当前 config 完整快照.
// cfg 未注入时返回空 object.
func (s *Server) handleAPIConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if s.cfg == nil {
		w.Write([]byte("{}"))
		return
	}
	_ = json.NewEncoder(w).Encode(s.cfg.Snapshot())
}

// handleInternalConfigSave POST /internal/config-save : Vue 配置页保存按钮.
// body = 完整 Config JSON. 校验合法 + 持久化 + 热应用.
// 仅 127.0.0.1.
func (s *Server) handleInternalConfigSave(w http.ResponseWriter, r *http.Request) {
	if s.cfg == nil {
		http.Error(w, "config 未注入", http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "需要 POST", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 32*1024))
	if err != nil {
		http.Error(w, "读 body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.cfg.ApplyJSON(body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	log.Print("config 已保存 + 热应用")
	resp200JSON(w, map[string]string{"ok": "saved"})
}

// handleWS /ws : 给 Vue 推实时传输进度. 任意 LAN 都能连 (内容只读, 不含敏感信息).
// progress hub 不在则 404.
func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	if s.progress == nil {
		http.NotFound(w, r)
		return
	}
	// InsecureSkipVerify: 我们的 LAN 场景没 Origin 强校验需求 (浏览器/webview/手机都行)
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		log.Printf("ws accept 失败: %v", err)
		return
	}
	// Adapter: coder/websocket.Conn → progress.ProgressConn
	conn := wsConnAdapter{c}
	s.progress.ServeWS(r.Context(), conn)
}

// wsConnAdapter 把 coder/websocket.Conn 适配成 server.ProgressConn (= progress.Conn).
type wsConnAdapter struct{ c *websocket.Conn }

func (a wsConnAdapter) Write(ctx context.Context, msg []byte) error {
	return a.c.Write(ctx, websocket.MessageText, msg)
}

func (a wsConnAdapter) Close(code int, reason string) error {
	return a.c.Close(websocket.StatusCode(code), reason)
}

// handlePeerIncoming POST /peer/incoming : 来自其他 daemon 的发送邀请.
// body = JSON { token, from: {uuid,name,host,ipv4,port}, fileName, fileSize }
//
// ADR-20 信任分支:
//   blocked → 静默 reject, 不入 pending, 不弹 toast (但记录到 device store 以便 UI 看到)
//   trusted → 入 pending 立即 SetPendingState(accepted), 启异步 Pull, 弹纯通知 toast
//   ask    → 入 pending 等用户决策, 弹 toast 按钮
func (s *Server) handlePeerIncoming(w http.ResponseWriter, r *http.Request) {
	if s.peers == nil {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "需要 POST", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Token string `json:"token"`
		From  struct {
			UUID string `json:"uuid"`
			Name string `json:"name"`
			Host string `json:"host"`
			IPv4 string `json:"ipv4"`
			Port int    `json:"port"`
		} `json:"from"`
		FileName string `json:"fileName"`
		FileSize int64  `json:"fileSize"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4*1024)).Decode(&body); err != nil {
		http.Error(w, "解析 body 失败: "+err.Error(), http.StatusBadRequest)
		return
	}
	if body.Token == "" || body.From.UUID == "" || body.FileName == "" {
		http.Error(w, "token/from.uuid/fileName 不能为空", http.StatusBadRequest)
		return
	}

	// maxFileSize 守卫: 配置非 0 时, 超出大小直接拒收 (不入 pending, 不弹 toast).
	if s.cfg != nil {
		if max := s.cfg.MaxFileSize(); max > 0 && body.FileSize > max {
			log.Printf("peer incoming 超大小上限 %d > %d, 拒收: %s", body.FileSize, max, body.FileName)
			http.Error(w, fmt.Sprintf("文件 %d 字节超出本端配置上限 %d", body.FileSize, max), http.StatusRequestEntityTooLarge)
			return
		}
	}

	// 更新设备表 lastSeen (即使被 block 也记, 让用户看到"有人想发但被你拉黑")
	trust := "ask"
	if s.deviceStore != nil {
		_ = s.deviceStore.UpsertSeen(body.From.UUID, body.From.Name)
		trust = s.deviceStore.TrustOf(body.From.UUID)
	}

	// blocked: 直接 reject, 不入 pending. 对端会等 TTL 自然清理.
	if trust == "blocked" {
		log.Printf("blocked peer 邀请: %s (%s) → 静默拒绝", body.From.Name, body.From.UUID[:8])
		// 返回 200 让对端 IPC 看起来 OK (避免对端用错误判断重试), 但实际啥也不做
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("queued"))
		return
	}

	if err := s.peers.AddPending(body.Token, body.From.UUID, body.From.Name, body.From.Host, body.From.IPv4, body.From.Port, body.FileName, body.FileSize); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	log.Printf("收到 peer 邀请: %s 想发 %s (%d 字节) token=%s trust=%s", body.From.Name, body.FileName, body.FileSize, body.Token[:8], trust)

	if trust == "trusted" {
		// 信任设备: 立刻 accept + Pull. 弹纯通知 toast 告知 (无按钮).
		s.peers.SetPendingState(body.Token, "accepted")
		go s.pullPeerFile(body.Token, body.From.IPv4, body.From.Port, body.From.Name, body.FileName, body.FileSize, true)
		if s.onPeerAccepted != nil {
			go s.onPeerAccepted(body.From.Name, body.FileName, body.FileSize)
		}
	} else {
		// ask: 弹按钮 toast 等用户
		if s.onPeerIncoming != nil {
			go s.onPeerIncoming(body.From.Name, body.FileName, body.FileSize, body.Token)
		}
	}

	// 通知 tray 更新红点 + 菜单 + tooltip (2.5e fallback)
	s.emitPendingChange()

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("queued"))
}

// handlePeerFile GET /peer/file?token=xxx : Bob 接受邀请后 Pull 文件.
// token 验证: 匹配 outgoing 才回, 否则 404 (不暴露存在性, ADR-17 安全约束延伸到 peer).
// 一次性: ServeFile 完成后 MarkDelivered, 再来同 token 会 404.
func (s *Server) handlePeerFile(w http.ResponseWriter, r *http.Request) {
	if s.peers == nil {
		http.NotFound(w, r)
		return
	}
	token := r.URL.Query().Get("token")
	if token == "" {
		http.NotFound(w, r)
		return
	}
	absPath, fileName, toName, fileSize, ok := s.peers.LookupOutgoing(token)
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Disposition", contentDisposition(fileName))
	http.ServeFile(w, r, absPath)
	// ServeFile 完成 (或 client 提前断开) 后标记交付; 下次同 token 失效.
	// 真正"交付完成"无法 100% 知 (HTTP 不可靠), 但 token 一次性即可防重放.
	s.peers.MarkDelivered(token)
	log.Printf("peer Pull 完成: token=%s file=%s → %s", token[:8], fileName, toName)
	if s.onPeerSent != nil {
		go s.onPeerSent(toName, fileName, fileSize)
	}
}

// handleInternalPeerSend POST /internal/peer-send : Alice 端 CLI/Vue 触发发文件给指定对端.
// body = JSON { toUUID, filePath } (mDNS 路径), 或 { toIPv4, toPort, filePath } (直连旁路)
//
// filePath 为空时用当前 daemon 的 absPath (dashboard 调用: 用户已通过 send X 设定了当前文件,
// 不想 Vue 拿到绝对路径).
//
// 步骤: 1) 找对端坐标  2) CreateOutgoing 拿 token  3) POST 到对端 /peer/incoming
//       4) 返回 token 给客户端
//
// 直连旁路: 单机测试时 mDNS 不工作 (grandcat/zeroconf 默认不走 loopback),
// 允许直接传 toIPv4+toPort 跳过对端发现. 此时 toUUID 不必填.
// 仅 127.0.0.1.
func (s *Server) handleInternalPeerSend(w http.ResponseWriter, r *http.Request) {
	if s.peers == nil {
		http.Error(w, "peer 子系统未启用", http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "需要 POST", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		ToUUID   string `json:"toUUID"`
		ToIPv4   string `json:"toIPv4"`
		ToPort   int    `json:"toPort"`
		FilePath string `json:"filePath"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 32*1024)).Decode(&body); err != nil {
		http.Error(w, "解析 body 失败: "+err.Error(), http.StatusBadRequest)
		return
	}

	// filePath 为空时用当前 daemon 的发送文件 (Vue dashboard "发送到 X" 走这条)
	if body.FilePath == "" {
		s.mu.RLock()
		body.FilePath = s.absPath
		s.mu.RUnlock()
		if body.FilePath == "" {
			http.Error(w, "filePath 未指定且 daemon 当前无发送文件", http.StatusBadRequest)
			return
		}
	}

	// 解析对端坐标: 优先 mDNS UUID 查找, 失败回退到 toIPv4/toPort
	var (
		targetIPv4 string
		targetPort int
		targetUUID string
		targetName string
		targetHost string
	)
	if body.ToUUID != "" && s.peerSource != nil {
		for _, p := range s.peerSource.Peers() {
			if p.UUID == body.ToUUID && len(p.IPv4) > 0 {
				targetIPv4 = p.IPv4[0]
				targetPort = p.Port
				targetUUID = p.UUID
				targetName = p.Name
				targetHost = p.Host
				break
			}
		}
	}
	if targetIPv4 == "" {
		// 回退: 直连参数
		if body.ToIPv4 == "" || body.ToPort == 0 {
			http.Error(w, "对端未发现 (mDNS) 且未提供 toIPv4/toPort 直连参数", http.StatusNotFound)
			return
		}
		targetIPv4 = body.ToIPv4
		targetPort = body.ToPort
		targetUUID = body.ToUUID // 可能空
		targetName = fmt.Sprintf("%s:%d", body.ToIPv4, body.ToPort)
	}

	// 验证本地文件
	absPath, fileName, _, err := validateFile(body.FilePath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	info, _ := os.Stat(absPath)

	// 创建 outgoing + 拿 token
	token, err := s.peers.CreateOutgoing(PeerInfo{
		UUID: targetUUID, Name: targetName, Host: targetHost,
		IPv4: targetIPv4, Port: targetPort,
	}, absPath, fileName, info.Size())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// POST 给对端 /peer/incoming
	incomingBody, _ := json.Marshal(map[string]any{
		"token": token,
		"from": map[string]any{
			"uuid": s.myUUID, "name": s.myName,
			"host": "",
			"ipv4": getLANIP(),
			"port": s.port,
		},
		"fileName": fileName,
		"fileSize": info.Size(),
	})
	peerURL := fmt.Sprintf("http://%s:%d/peer/incoming", targetIPv4, targetPort)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Post(peerURL, "application/json", strings.NewReader(string(incomingBody)))
	if err != nil {
		http.Error(w, "通知对端失败: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		rb, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		http.Error(w, fmt.Sprintf("对端拒绝 %d: %s", resp.StatusCode, strings.TrimSpace(string(rb))), http.StatusBadGateway)
		return
	}

	log.Printf("已通知 %s 准备接收 %s (token=%s)", targetName, fileName, token[:8])
	resp200JSON(w, map[string]string{"token": token, "to": targetName})
}

// handleInternalPeerDecide POST /internal/peer-decide : Bob 端 toast/CLI/Vue 决策.
// body = JSON { token, decision: "accept"|"reject", trust?: "trusted"|"blocked" }
// 可选 trust: accept 时同时设设备 trust=trusted (信任), reject 时同时设 trust=blocked (永不信任).
// 不传 trust 字段则只决策本次, 不动 device 表.
//
// accept → 异步 GET http://from/peer/file?token=xxx 流式存到 Downloads/QuickDrop/
// reject → 改 pending 状态, 不通知对端 (对端等 outgoing TTL 自然清掉)
// 仅 127.0.0.1.
func (s *Server) handleInternalPeerDecide(w http.ResponseWriter, r *http.Request) {
	if s.peers == nil {
		http.Error(w, "peer 子系统未启用", http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "需要 POST", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Token    string `json:"token"`
		Decision string `json:"decision"`
		Trust    string `json:"trust,omitempty"` // 可选: trusted (accept 时) / blocked (reject 时)
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&body); err != nil {
		http.Error(w, "解析 body 失败: "+err.Error(), http.StatusBadRequest)
		return
	}
	if body.Decision != "accept" && body.Decision != "reject" {
		http.Error(w, `decision 必须是 "accept" 或 "reject"`, http.StatusBadRequest)
		return
	}

	fromIPv4, fromPort, fromName, fileName, fileSize, ok := s.peers.LookupPending(body.Token)
	if !ok {
		http.Error(w, "token 未找到 (可能已过期)", http.StatusNotFound)
		return
	}

	// 如果带了 trust 参数 + 我们能查到设备, 一并更新 device store
	if body.Trust != "" && s.deviceStore != nil {
		// 从 pending 拿到的 fromUUID 在内部状态机里, 这里 Lookup 一下完整 entry
		// 走 PendingList 简化 (本来就 join 了 trust, 找出对应 entry)
		for _, p := range s.peers.PendingList() {
			if p.Token == body.Token {
				if err := s.deviceStore.SetTrust(p.From.UUID, p.From.Name, body.Trust); err != nil {
					log.Printf("设 trust 失败: %v", err)
				}
				break
			}
		}
	}

	if body.Decision == "reject" {
		s.peers.SetPendingState(body.Token, "rejected")
		log.Printf("拒绝 peer 邀请 token=%s file=%s trust=%s", body.Token[:8], fileName, body.Trust)
		s.emitPendingChange()
		resp200JSON(w, map[string]string{"decision": "rejected"})
		return
	}

	// accept: 异步 Pull (ask 路径, silent=false → 完成时弹 PeerReceived toast)
	s.peers.SetPendingState(body.Token, "accepted")
	go s.pullPeerFile(body.Token, fromIPv4, fromPort, fromName, fileName, fileSize, false)
	s.emitPendingChange()
	resp200JSON(w, map[string]string{"decision": "accepted", "pulling": "started"})
}

// handleInternalDeviceTrust POST /internal/device-trust : 设备管理页 (Vue /v) 用.
// body = JSON { uuid, name?, trust }
// 设备不存在会新建 (允许预先信任/拉黑还没见过的设备).
// 仅 127.0.0.1.
func (s *Server) handleInternalDeviceTrust(w http.ResponseWriter, r *http.Request) {
	if s.deviceStore == nil {
		http.Error(w, "device store 未启用", http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "需要 POST", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		UUID  string `json:"uuid"`
		Name  string `json:"name"`
		Trust string `json:"trust"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4*1024)).Decode(&body); err != nil {
		http.Error(w, "解析 body 失败: "+err.Error(), http.StatusBadRequest)
		return
	}
	if body.UUID == "" {
		http.Error(w, "uuid 不能为空", http.StatusBadRequest)
		return
	}
	if err := s.deviceStore.SetTrust(body.UUID, body.Name, body.Trust); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	log.Printf("设备 trust 已更新: %s (%s) → %s", body.Name, body.UUID[:8], body.Trust)
	resp200JSON(w, map[string]string{"uuid": body.UUID, "trust": body.Trust})
}

// pullPeerFile 主动 GET 发送方的 /peer/file?token=xxx, 流式写到 Downloads/QuickDrop/.
// 失败只打日志, 不重试 (用户可以再次 send).
//
// silent=true 表示这是 trusted 设备触发的自动 accept (由 onPeerAccepted 通知),
// 完成时不再弹 onPeerReceived toast 防重复.
// silent=false (ask 路径用户手动接受) 完成时弹 onPeerReceived.
func (s *Server) pullPeerFile(token, fromIPv4 string, fromPort int, fromName, fileName string, fileSize int64, silent bool) {
	url := fmt.Sprintf("http://%s:%d/peer/file?token=%s", fromIPv4, fromPort, token)
	client := &http.Client{Timeout: 30 * time.Minute} // 大文件慢传可能久
	resp, err := client.Get(url)
	if err != nil {
		log.Printf("Pull peer 文件失败 (token=%s): %v", token[:8], err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Printf("Pull peer 文件返回 %d (token=%s)", resp.StatusCode, token[:8])
		return
	}

	dir, err := s.downloadsDir()
	if err != nil {
		log.Printf("Pull 失败 - 创建目录: %v", err)
		return
	}
	safeName := filepath.Base(fileName)
	finalPath, err := s.resolveSavePath(dir, safeName)
	if err != nil {
		log.Printf("Pull 拒绝 (conflict reject): %v", err)
		return
	}
	tmpPath := finalPath + ".tmp"

	// 包 progress reader 推实时进度给 Vue. nil 时直接用原始 body.
	var src io.Reader = resp.Body
	var pr ProgressReader
	if s.progress != nil {
		pr = s.progress.WrapReader(resp.Body, token, "receive", fileName, fileSize)
		src = pr
	}
	saveErr := saveStream(tmpPath, finalPath, src)
	if pr != nil {
		pr.Done(saveErr)
	}
	if saveErr != nil {
		log.Printf("Pull 写入失败 (token=%s): %v", token[:8], saveErr)
		return
	}
	log.Printf("Pull peer 完成: %s (%d 字节) silent=%v", finalPath, fileSize, silent)
	if !silent && s.onPeerReceived != nil {
		go s.onPeerReceived(fromName, fileName, fileSize)
	}
	// reveal 在主流程: 让 main 决定 (它知道 RevealOnDone 配置 + 平台限制)
	if s.onFileSaved != nil {
		go s.onFileSaved(finalPath)
	}
}

func resp200JSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(v)
}

// handleInternalHealth: GET /internal/health
// 客户端模式探测 daemon 是否在跑. 200 + X-QuickDrop: 1 表示 "我是 QuickDrop".
// 仅 127.0.0.1 可访问 (requireLocal 中间件).
func (s *Server) handleInternalHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-QuickDrop", "1")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

// handleInternalSend: POST /internal/send body=<绝对路径>
// 把 daemon 当前发送的文件切换成 body 指定的路径.
// 仅 127.0.0.1 可访问.
func (s *Server) handleInternalSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "需要 POST", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 32*1024))
	if err != nil {
		http.Error(w, "读 body 失败: "+err.Error(), http.StatusBadRequest)
		return
	}
	path := strings.TrimSpace(string(body))
	if path == "" {
		http.Error(w, "body 为空", http.StatusBadRequest)
		return
	}
	if err := s.SwapFile(path); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("switched"))
}

// handleInternalReceive: POST /internal/receive body=on|off
// 开/关接收模式. 用于 `quickdrop recv` 客户端模式让 daemon 切到接收状态.
// 仅 127.0.0.1 可访问.
func (s *Server) handleInternalReceive(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "需要 POST", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 32))
	if err != nil {
		http.Error(w, "读 body 失败: "+err.Error(), http.StatusBadRequest)
		return
	}
	cmd := strings.TrimSpace(string(body))
	switch cmd {
	case "on":
		s.EnableReceive(true)
	case "off":
		s.EnableReceive(false)
	default:
		http.Error(w, `body 必须是 "on" 或 "off"`, http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(cmd))
}

// requireLocal 拒绝非 127.0.0.1 的访问, 直接 404 (不暴露存在性).
// /internal/* 用. LAN 上的手机走 /file /upload, 走不到这.
func requireLocal(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			http.NotFound(w, r)
			return
		}
		next(w, r)
	}
}

// handleUpload /upload : 接收端上传, 默认 404 (ADR-17 安全约束).
// 只在 EnableReceive(true) 后真正接收 multipart. 路由本身始终注册 (避免重新挂 mux),
// 关闭模式时返回 404 假装不存在, 不暴露 "QuickDrop 在跑但接收关了" 这条信息.
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	if !s.receiveMode.Load() {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "需要 POST", http.StatusMethodNotAllowed)
		return
	}

	reader, err := r.MultipartReader()
	if err != nil {
		http.Error(w, "解析 multipart 失败: "+err.Error(), http.StatusBadRequest)
		return
	}

	dir, err := s.downloadsDir()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	count := 0
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			http.Error(w, "读 part 失败: "+err.Error(), http.StatusBadRequest)
			return
		}

		if part.FileName() == "" {
			part.Close()
			continue
		}

		// 取 Base 防路径穿越
		safeName := filepath.Base(part.FileName())
		finalPath, err := s.resolveSavePath(dir, safeName)
		if err != nil {
			part.Close()
			log.Printf("Upload 拒绝 %s (conflict): %v", safeName, err)
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		tmpPath := finalPath + ".tmp"

		// 包 progress reader. multipart part 不知道精确大小, 用 r.ContentLength 当上限
		// (会包括其他字段, 但近似够用; 前端拿 bytes / fileSize 算百分比时会少于 100).
		var src io.Reader = part
		var pr ProgressReader
		if s.progress != nil {
			uid := uploadProgressID()
			pr = s.progress.WrapReader(part, uid, "upload", safeName, r.ContentLength)
			src = pr
		}

		// maxFileSize 守卫: 用 io.LimitReader 包一层, 超出直接返 EOF 截断;
		// 但更友好的是先用 ContentLength 拦. ContentLength 是 multipart 整包大小, 不精确,
		// 所以 LimitReader 是兜底.
		if s.cfg != nil {
			if max := s.cfg.MaxFileSize(); max > 0 {
				if r.ContentLength > 0 && r.ContentLength > max {
					part.Close()
					http.Error(w, fmt.Sprintf("文件超出配置上限 (%d > %d 字节)", r.ContentLength, max), http.StatusRequestEntityTooLarge)
					return
				}
				src = io.LimitReader(src, max+1) // +1 用来检测越界, 见下方校验
			}
		}

		saveErr := saveStream(tmpPath, finalPath, src)
		if pr != nil {
			pr.Done(saveErr)
		}
		if saveErr != nil {
			part.Close()
			log.Printf("接收 %s 失败: %v", safeName, saveErr)
			http.Error(w, saveErr.Error(), http.StatusInternalServerError)
			return
		}
		// 越界检测: LimitReader 兜底, 文件大小 == max+1 时认为越界
		if s.cfg != nil {
			if max := s.cfg.MaxFileSize(); max > 0 {
				if info, err := os.Stat(finalPath); err == nil && info.Size() > max {
					_ = os.Remove(finalPath)
					part.Close()
					http.Error(w, "文件超出配置上限", http.StatusRequestEntityTooLarge)
					return
				}
			}
		}
		part.Close()
		log.Printf("接收完成: %s", finalPath)
		if s.onFileSaved != nil {
			go s.onFileSaved(finalPath)
		}
		count++
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Vue 的 Upload.vue 解析 "已收到 N 个文件" 拿数字, 保持文字契约.
	// 后续可以换成纯 JSON, 现在用 HTML 让浏览器直接打开 /upload (非 Vue 场景) 也能看.
	fmt.Fprintf(w, `<!doctype html><html lang="zh-CN"><meta charset="utf-8">`+
		`<title>QuickDrop</title><body style="font-family:system-ui;text-align:center;padding:40px;">`+
		`<h1>已收到 %d 个文件</h1><p>保存到 ~/Downloads/QuickDrop/</p></body></html>`, count)

	// 弹电脑端 toast 让用户知道 (用户可能在浏览器之外, 没看 daemon 日志)
	if count > 0 && s.onUploadDone != nil {
		go s.onUploadDone(count)
	}
}

// validateFile 把用户给的路径 (相对/绝对都行) 检查 + 描述化.
// 返回 (绝对路径, 文件名, 人类可读大小, error).
// 共用于 New (初次启动) 和 SwapFile (运行中切换).
func validateFile(rawPath string) (abs, name, size string, err error) {
	abs, err = filepath.Abs(rawPath)
	if err != nil {
		return "", "", "", fmt.Errorf("路径解析失败: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", "", "", fmt.Errorf("无法访问 %s: %w", abs, err)
	}
	if info.IsDir() {
		return "", "", "", fmt.Errorf("%s 是目录, Phase 1 只支持单文件", abs)
	}
	return abs, filepath.Base(abs), humanSize(info.Size()), nil
}

// getLANIP 借 UDP 拨号让 OS 按默认路由选源 IP (不真正发包).
// 在 WSL/Docker 多网卡环境下也能拿到对外的那张.
func getLANIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err == nil {
		defer conn.Close()
		return conn.LocalAddr().(*net.UDPAddr).IP.String()
	}
	// 兜底: UDP 拨号失败时遍历网卡, 跳过回环和 APIPA
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok || ipnet.IP.IsLoopback() || ipnet.IP.IsLinkLocalUnicast() {
			continue
		}
		if v4 := ipnet.IP.To4(); v4 != nil {
			return v4.String()
		}
	}
	return ""
}

// contentDisposition 生成同时携带 ASCII fallback 和 RFC 5987 UTF-8 的下载头.
// 现代浏览器优先认 filename*=, iOS Safari / Chrome / Edge 都支持.
func contentDisposition(filename string) string {
	asciiSafe := strings.ReplaceAll(filename, `"`, `\"`)
	return fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`,
		asciiSafe, url.PathEscape(filename))
}

// downloadsDir 接收文件的根目录. 优先 cfg.ResolvedDownloadDir, 否则回退 ~/Downloads/QuickDrop/.
// 调用者拿到目录路径已含 MkdirAll, 直接 filepath.Join 用即可.
func (s *Server) downloadsDir() (string, error) {
	if s.cfg != nil {
		return s.cfg.ResolvedDownloadDir()
	}
	// fallback (cfg 未注入, 比如旧代码路径): 跟之前一致
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("拿不到用户家目录: %w", err)
	}
	dir := filepath.Join(home, "Downloads", "QuickDrop")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("创建 %s: %w", dir, err)
	}
	return dir, nil
}

// resolveSavePath 根据 conflict 策略决定最终落盘路径.
// dir + name → finalPath. 返回 error 表示 "reject" 拒绝接收.
//
//	rename    final 已存在 → name (1).ext, (2).ext...; 找空位返回
//	overwrite 直接覆盖 (沿用旧行为)
//	reject    final 已存在 → 返 error
func (s *Server) resolveSavePath(dir, name string) (string, error) {
	final := filepath.Join(dir, name)
	policy := "rename" // 默认
	if s.cfg != nil {
		policy = s.cfg.Conflict()
	}
	if _, err := os.Stat(final); os.IsNotExist(err) {
		return final, nil
	}
	switch policy {
	case "overwrite":
		return final, nil
	case "reject":
		return "", fmt.Errorf("%s 已存在, 配置为 reject", name)
	case "rename", "":
		// 拆 base + ext, 在中间插 (N)
		ext := filepath.Ext(name)
		base := strings.TrimSuffix(name, ext)
		for i := 1; i < 10000; i++ {
			cand := filepath.Join(dir, fmt.Sprintf("%s (%d)%s", base, i, ext))
			if _, err := os.Stat(cand); os.IsNotExist(err) {
				return cand, nil
			}
		}
		return "", fmt.Errorf("%s 重命名尝试 10000 次仍冲突", name)
	default:
		// 未知 policy 走 rename 兜底
		return final, nil
	}
}

// cleanupStaleTmp 删除 Downloads/QuickDrop 下上次进程异常退出留下的 *.tmp.
// 正常路径 saveStream 写完会 rename, 不会留 tmp; 但 -F 强杀或 OS 崩溃会留半截文件.
// 启动时清扫一次, 避免越攒越多 + 用户疑惑. 失败不致命, 打日志继续.
func (s *Server) cleanupStaleTmp() {
	dir, err := s.downloadsDir()
	if err != nil {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(e.Name(), ".tmp") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		if err := os.Remove(path); err != nil {
			log.Printf("清理残留 tmp %s 失败: %v", path, err)
			continue
		}
		n++
	}
	if n > 0 {
		log.Printf("启动清理: 删除 %d 个残留 *.tmp", n)
	}
}

// uploadProgressID 给每个手机 upload part 生成一个进度 ID (用于 Vue 区分多个并行 upload).
// 16 字符 hex.
func uploadProgressID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "upload-" + hex.EncodeToString(b)
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

// saveStream 把 r 流式写入 tmpPath, 完整写完后原子 rename 到 finalPath.
// Windows 上 os.Rename 不会覆盖已有文件, 所以先 Remove finalPath.
// 任何阶段失败都清掉 tmp, 不在磁盘上留半截文件.
func saveStream(tmpPath, finalPath string, r io.Reader) error {
	tmpFile, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("创建临时文件: %w", err)
	}
	if _, err := io.Copy(tmpFile, r); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("写入数据: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("关闭临时文件: %w", err)
	}
	os.Remove(finalPath)
	if err := os.Rename(tmpPath, finalPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}
