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

	homeURL    string // 电脑端发送 dashboard URL
	receiveURL string // 电脑端接收 dashboard URL
	mobileURL  string // 手机端发送目标 URL (QR 编码 + 托盘 "复制扫码链接" 复制)
	uploadURL  string // 手机端上传 URL (接收模式 QR 编码)
	baseURL    string // http://<lan>:8443
	httpSrv    *http.Server

	// dist 是 Vite 构建产物的 fs (含 index.html / d.html / r.html / u.html / assets/*).
	// 由 cmd/quickdrop 通过 SetDist 注入 (因为 //go:embed 必须在 main 包里相对仓库根).
	dist fs.FS

	onSwap    func(newName string) // 文件被 SwapFile 切换后回调 (tray tooltip 用)
	onReceive func(on bool)        // 接收模式切换后回调 (main 起/关 receive webview)
}

// New 装配 Server. rawPath 为空表示纯接收模式启动 (daemon 启动时无文件可发).
// 不启动 listener.
func New(rawPath string) (*Server, error) {
	lanIP := getLANIP()
	if lanIP == "" {
		return nil, fmt.Errorf("没找到对外 LAN IP, 检查网卡和路由")
	}

	baseURL := fmt.Sprintf("http://%s:8443", lanIP)
	s := &Server{
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
	cleanupStaleTmp()

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
	// JSON API (Vue 前端拉服务状态)
	mux.HandleFunc("/api/info", s.handleAPIInfo)
	// IPC (仅 127.0.0.1)
	mux.HandleFunc("/internal/health", requireLocal(s.handleInternalHealth))
	mux.HandleFunc("/internal/send", requireLocal(s.handleInternalSend))
	mux.HandleFunc("/internal/receive", requireLocal(s.handleInternalReceive))
	// 静态资源 (Vue chunks / CSS)
	mux.Handle("/assets/", http.FileServer(http.FS(s.dist)))

	s.httpSrv = &http.Server{Addr: ":8443", Handler: mux}

	if s.absPath != "" {
		log.Printf("发送文件: %s (%s)", s.absPath, s.fileSize)
		log.Printf("电脑端:   %s", s.homeURL)
		log.Printf("手机端:   %s", s.mobileURL)
		log.Printf("直链:     %s/file", s.baseURL)
	} else {
		log.Print("未指定文件, 仅启 daemon (可通过托盘/CLI 进入接收模式或后续 send)")
	}
	log.Printf("监听:     0.0.0.0:8443")

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

	dir, err := downloadsDir()
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
		finalPath := filepath.Join(dir, safeName)
		tmpPath := finalPath + ".tmp"

		if err := saveStream(tmpPath, finalPath, part); err != nil {
			part.Close()
			log.Printf("接收 %s 失败: %v", safeName, err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		part.Close()
		log.Printf("接收完成: %s", finalPath)
		count++
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Vue 的 Upload.vue 解析 "已收到 N 个文件" 拿数字, 保持文字契约.
	// 后续可以换成纯 JSON, 现在用 HTML 让浏览器直接打开 /upload (非 Vue 场景) 也能看.
	fmt.Fprintf(w, `<!doctype html><html lang="zh-CN"><meta charset="utf-8">`+
		`<title>QuickDrop</title><body style="font-family:system-ui;text-align:center;padding:40px;">`+
		`<h1>已收到 %d 个文件</h1><p>保存到 ~/Downloads/QuickDrop/</p></body></html>`, count)
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

func downloadsDir() (string, error) {
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

// cleanupStaleTmp 删除 Downloads/QuickDrop 下上次进程异常退出留下的 *.tmp.
// 正常路径 saveStream 写完会 rename, 不会留 tmp; 但 -F 强杀或 OS 崩溃会留半截文件.
// 启动时清扫一次, 避免越攒越多 + 用户疑惑. 失败不致命, 打日志继续.
func cleanupStaleTmp() {
	dir, err := downloadsDir()
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
