// Package server hosts the QuickDrop HTTP routes (/, /qr, /file, /upload)
// plus internal IPC routes (/internal/health, /internal/send).
//
// 单进程 daemon 模式: 第一次 quickdrop send X 起 daemon, 后续 send Y 走 IPC
// 切换当前发送文件, 命令行立即退出. 见 cmd/quickdrop/main.go.
package server

import (
	"context"
	"fmt"
	"html"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"quickdrop/internal/qr"
)

// Server 持有 daemon 状态 + 底层 http.Server.
// 一次进程一个 Server, 通过 New + Start/Shutdown 管理生命周期.
//
// 当前发送文件 (absPath/fileName/fileSize) 用 mu 保护, SwapFile 可在运行中替换.
type Server struct {
	mu       sync.RWMutex
	absPath  string // 待发送文件的绝对路径
	fileName string // 文件名 (展示+下载头用)
	fileSize string // 人类可读大小

	homeURL string // 主页 URL (含 LAN IP), Start 后不再变
	baseURL string // http://<lan>:8443
	httpSrv *http.Server

	onSwap func(newName string) // 文件被 SwapFile 切换后回调 (tray tooltip 用)
}

// New 验证文件可读, 探测 LAN IP, 装配 Server.
// 不启动 listener.
func New(rawPath string) (*Server, error) {
	absPath, name, size, err := validateFile(rawPath)
	if err != nil {
		return nil, err
	}

	lanIP := getLANIP()
	if lanIP == "" {
		return nil, fmt.Errorf("没找到对外 LAN IP, 检查网卡和路由")
	}

	baseURL := fmt.Sprintf("http://%s:8443", lanIP)
	return &Server{
		absPath:  absPath,
		fileName: name,
		fileSize: size,
		homeURL:  baseURL + "/",
		baseURL:  baseURL,
	}, nil
}

// HomeURL 暴露给托盘菜单 "复制扫码链接" 用.
func (s *Server) HomeURL() string { return s.homeURL }

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
	cleanupStaleTmp()

	mux := http.NewServeMux()
	mux.HandleFunc("/file", s.handleFile)
	mux.HandleFunc("/qr", s.handleQR)
	mux.HandleFunc("/upload", handleUpload)
	mux.HandleFunc("/internal/health", requireLocal(s.handleInternalHealth))
	mux.HandleFunc("/internal/send", requireLocal(s.handleInternalSend))
	mux.HandleFunc("/", s.handleIndex)

	s.httpSrv = &http.Server{Addr: ":8443", Handler: mux}

	log.Printf("发送文件: %s (%s)", s.absPath, s.fileSize)
	log.Printf("主页:     %s", s.homeURL)
	log.Printf("直链:     %s/file", s.baseURL)
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
	w.Header().Set("Content-Disposition", contentDisposition(name))
	http.ServeFile(w, r, absPath)
}

// handleQR 把 homeURL 渲染成 PNG, 给 / 页面 <img src="/qr"> 内嵌用.
// QR 编码主页而不是 /file, 这样手机扫码进的是 dashboard, 能同时下载和反向上传.
func (s *Server) handleQR(w http.ResponseWriter, r *http.Request) {
	png, err := qr.Render(s.homeURL)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(png)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	s.mu.RLock()
	name, size := s.fileName, s.fileSize
	s.mu.RUnlock()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// 主页禁缓存, 切换文件后手机刷新立刻看到新内容
	w.Header().Set("Cache-Control", "no-store")
	fmt.Fprintf(w, indexHTMLTpl, html.EscapeString(name), html.EscapeString(size))
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

func handleUpload(w http.ResponseWriter, r *http.Request) {
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
	fmt.Fprintf(w, uploadDoneHTMLTpl, count)
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
