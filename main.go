package main

import (
	"flag"
	"fmt"
	"html"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/skip2/go-qrcode"
)

// 临时方案: 网页渲染 QR + 自动开浏览器
// 最终方案 (Phase 2 ADR-12): WebView2 无边框小窗加载同一个 / 路由

// indexHTMLTpl 主页模板, 占位符顺序:
//
//	%s = 文件名 (已 HTML 转义)
//	%s = 文件大小描述 (已 HTML 转义)
const indexHTMLTpl = `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>QuickDrop</title>
  <style>
    :root { color-scheme: light dark; }
    * { box-sizing: border-box; }
    body {
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", system-ui, sans-serif;
      max-width: 520px; margin: 24px auto; padding: 16px;
      color: #1a1a1a; background: #fafafa;
    }
    h1 { font-size: 1.3em; margin: 0 0 16px; }
    section {
      padding: 18px; margin-bottom: 14px;
      background: #fff; border: 1px solid #e5e5e5; border-radius: 10px;
    }
    section h2 { font-size: 1em; margin: 0 0 12px; color: #555; font-weight: 600; }
    .qr-card { text-align: center; }
    .qr-card img { width: 220px; height: 220px; display: block; margin: 0 auto 8px; }
    .qr-card p { color: #888; font-size: .9em; margin: 0; }
    .file-card .name { font-weight: 600; word-break: break-all; margin-bottom: 4px; }
    .btn {
      display: inline-block; padding: 10px 18px; font-size: 1em;
      background: #0066ff; color: #fff; border: 0; border-radius: 6px;
      text-decoration: none; cursor: pointer; -webkit-appearance: none;
    }
    input[type=file] {
      width: 100%%; margin-bottom: 12px; padding: 8px;
      background: #fafafa; border: 1px dashed #ccc; border-radius: 6px;
    }
    .hint { color: #888; font-size: .85em; margin: 8px 0 12px; }
    @media (prefers-color-scheme: dark) {
      body { color: #eee; background: #1a1a1a; }
      section { background: #262626; border-color: #333; }
      section h2 { color: #aaa; }
      .qr-card img { background: #fff; padding: 6px; border-radius: 4px; }
      input[type=file] { background: #1a1a1a; border-color: #444; color: #eee; }
    }
  </style>
</head>
<body>
  <h1>QuickDrop</h1>

  <section class="qr-card">
    <h2>手机扫码</h2>
    <img src="/qr" alt="扫码进入此页面">
    <p>同 WiFi 的手机用系统相机扫上方二维码</p>
  </section>

  <section class="file-card">
    <h2>下载到此设备</h2>
    <p class="name">%s</p>
    <p class="hint">%s</p>
    <a class="btn" href="/file">下载</a>
  </section>

  <section>
    <h2>上传到电脑</h2>
    <form action="/upload" method="post" enctype="multipart/form-data">
      <input type="file" name="file" multiple required>
      <button class="btn" type="submit">上传</button>
    </form>
    <p class="hint">保存到电脑 ~/Downloads/QuickDrop/</p>
  </section>
</body>
</html>`

const uploadDoneHTMLTpl = `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>QuickDrop - 上传完成</title>
  <style>
    body { font-family: -apple-system, system-ui, sans-serif; max-width: 480px; margin: 40px auto; padding: 16px; text-align: center; color: #1a1a1a; }
    h1 { font-size: 1.4em; }
    a { display: inline-block; padding: 10px 18px; background: #0066ff; color: #fff; border-radius: 6px; text-decoration: none; margin-top: 16px; }
  </style>
</head>
<body>
  <h1>已收到 %d 个文件</h1>
  <p>已保存到电脑 ~/Downloads/QuickDrop/</p>
  <a href="/">返回</a>
</body>
</html>`

// getLANIP 借 UDP 拨号让 OS 按默认路由选源 IP (不真正发包)
// 在 WSL/Docker 多网卡环境下也能拿到对外的那张
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

// contentDisposition 生成同时携带 ASCII fallback 和 RFC 5987 UTF-8 的下载头
// 现代浏览器优先认 filename*=, iOS Safari / Chrome / Edge 都支持
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

// saveStream 把 r 流式写入 tmpPath, 完整写完后原子 rename 到 finalPath
// Windows 上 os.Rename 不会覆盖已有文件, 所以先 Remove finalPath
// 任何阶段失败都清掉 tmp, 不在磁盘上留半截文件
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

	rawPath, err := parseArgs()
	if err != nil {
		usage()
		os.Exit(1)
	}

	absPath, err := filepath.Abs(rawPath)
	if err != nil {
		log.Fatalf("路径解析失败: %v", err)
	}
	info, err := os.Stat(absPath)
	if err != nil {
		log.Fatalf("无法访问 %s: %v", absPath, err)
	}
	if info.IsDir() {
		log.Fatalf("%s 是目录, Phase 1 只支持单文件", absPath)
	}
	fileName := filepath.Base(absPath)
	fileSize := humanSize(info.Size())

	lanIP := getLANIP()
	if lanIP == "" {
		log.Fatal("没找到对外 LAN IP, 检查网卡和路由")
	}
	baseURL := fmt.Sprintf("http://%s:8443", lanIP)
	homeURL := baseURL + "/"

	http.HandleFunc("/file", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", contentDisposition(fileName))
		http.ServeFile(w, r, absPath)
	})

	// /qr: 把 homeURL 渲染成 PNG, 给 / 页面 <img src="/qr"> 内嵌用
	// QR 编码主页而不是 /file, 这样手机扫码进的是 dashboard, 能同时下载和反向上传
	http.HandleFunc("/qr", func(w http.ResponseWriter, r *http.Request) {
		png, err := qrcode.Encode(homeURL, qrcode.Medium, 320)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(png)
	})

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, indexHTMLTpl, html.EscapeString(fileName), html.EscapeString(fileSize))
	})

	http.HandleFunc("/upload", handleUpload)

	log.Printf("发送文件: %s (%s)", absPath, fileSize)
	log.Printf("主页:     %s", homeURL)
	log.Printf("直链:     %s/file", baseURL)
	log.Printf("监听:     0.0.0.0:8443")

	// 自动开浏览器到主页 (临时方案, Phase 2 ADR-12 改 WebView2 无边框小窗)
	// 失败不影响 server, 用户也能手动访问
	if err := exec.Command("cmd", "/c", "start", homeURL).Start(); err != nil {
		log.Printf("自动开浏览器失败: %v (手动访问 %s)", err, homeURL)
	}

	log.Fatal(http.ListenAndServe(":8443", nil))
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
