// Package server hosts the QuickDrop HTTP routes (/, /qr, /file, /upload).
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
	"time"

	"quickdrop/internal/qr"
)

// Server 持有当前发送会话的状态 + 底层 http.Server.
// 一次进程一个 Server, 通过 New + Start/Shutdown 管理生命周期.
type Server struct {
	absPath  string // 待发送文件的绝对路径
	fileName string // 文件名 (展示+下载头用)
	fileSize string // 人类可读大小
	homeURL  string // 主页 URL (含 LAN IP)
	baseURL  string // http://<lan>:8443
	httpSrv  *http.Server
}

// New 验证文件可读, 探测 LAN IP, 装配 Server.
// 不启动 listener.
func New(rawPath string) (*Server, error) {
	absPath, err := filepath.Abs(rawPath)
	if err != nil {
		return nil, fmt.Errorf("路径解析失败: %w", err)
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return nil, fmt.Errorf("无法访问 %s: %w", absPath, err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("%s 是目录, Phase 1 只支持单文件", absPath)
	}

	lanIP := getLANIP()
	if lanIP == "" {
		return nil, fmt.Errorf("没找到对外 LAN IP, 检查网卡和路由")
	}

	baseURL := fmt.Sprintf("http://%s:8443", lanIP)
	return &Server{
		absPath:  absPath,
		fileName: filepath.Base(absPath),
		fileSize: humanSize(info.Size()),
		homeURL:  baseURL + "/",
		baseURL:  baseURL,
	}, nil
}

// HomeURL 暴露给托盘菜单 "复制扫码链接" 用.
func (s *Server) HomeURL() string { return s.homeURL }

// Start 在当前 goroutine 之外起 listener, 立刻返回. 出错通过 log.Fatalf 兜底.
// 调用者通常: go server.Start() 然后让 main goroutine 跑 systray.
func (s *Server) Start() {
	cleanupStaleTmp()

	mux := http.NewServeMux()
	mux.HandleFunc("/file", s.handleFile)
	mux.HandleFunc("/qr", s.handleQR)
	mux.HandleFunc("/upload", handleUpload)
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
	w.Header().Set("Content-Disposition", contentDisposition(s.fileName))
	http.ServeFile(w, r, s.absPath)
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
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, indexHTMLTpl, html.EscapeString(s.fileName), html.EscapeString(s.fileSize))
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
