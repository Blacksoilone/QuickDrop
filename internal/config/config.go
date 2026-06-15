// Package config 持久化 QuickDrop 运行时配置.
//
// 存 ~/.quickdrop/config.json. 跟 devices.json 共目录, 原子写 (tmp + rename),
// 进程崩溃不会留半截 JSON.
//
// 三层加载顺序: 默认 < ~/.quickdrop/config.json < env (启动时一次性 merge).
// env 不写回 JSON, 只覆盖当次启动. 这样运维改 env 不会污染用户磁盘配置.
//
// 字段命名规约:
//   - 嵌套 struct: 按域分组 (Download / Server / UI / System / Receive)
//   - JSON tag 用 snake_case 让手工编辑 config.json 时易读
//   - Go 字段 export 大写
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// 环境变量名 (与 cmd/quickdrop/main.go 的现有约定保持一致).
const (
	envPort        = "QUICKDROP_PORT"
	envDeviceName  = "QUICKDROP_DEVICE_NAME"
	envWindowMode  = "QUICKDROP_WINDOW_MODE"
	envDownloadDir = "QUICKDROP_DOWNLOAD_DIR"
)

// ConflictPolicy 接收文件同名冲突策略.
type ConflictPolicy string

const (
	ConflictRename    ConflictPolicy = "rename"    // 默认: 加 (1), (2) 后缀
	ConflictOverwrite ConflictPolicy = "overwrite" // 覆盖
	ConflictReject    ConflictPolicy = "reject"    // 拒绝接收
)

// IsValid 返回 true 表示是合法的 conflict policy 值.
func (p ConflictPolicy) IsValid() bool {
	switch p {
	case ConflictRename, ConflictOverwrite, ConflictReject:
		return true
	}
	return false
}

// Config 所有运行时可调项. 注意 receive/network 等域内字段后续会扩.
type Config struct {
	Download DownloadConfig `json:"download"`
	Server   ServerConfig   `json:"server"`
	Receive  ReceiveConfig  `json:"receive"`
	UI       UIConfig       `json:"ui"`
	System   SystemConfig   `json:"system"`
}

type DownloadConfig struct {
	// Dir 接收文件保存位置. 空串视为默认 (~/Downloads/QuickDrop/).
	Dir string `json:"dir"`
	// Conflict 同名文件冲突策略. 空串视为默认 (rename).
	Conflict ConflictPolicy `json:"conflict"`
}

type ServerConfig struct {
	// Port HTTP 监听端口. 0 视为默认 8443.
	Port int `json:"port"`
	// MdnsEnabled 是否广播 mDNS 让局域网其他 QuickDrop 看到.
	// 关掉就完全隐身, 只能通过扫码用. 默认 true.
	MdnsEnabled bool `json:"mdns_enabled"`
}

type ReceiveConfig struct {
	// MaxFileSize 接收文件大小上限 (字节). 0 = 不限.
	// 来自 peer 的 incoming 或手机 upload 超过都拒.
	MaxFileSize int64 `json:"max_file_size"`
	// DefaultOn daemon 启动时是否默认开启接收.
	// true: 启动即可接收 (开机自启场景). false: 需手动开启 (安全模式).
	// 默认 true. 注意: quickdrop recv 命令会强制开启 (即使配置为 false).
	DefaultOn bool `json:"default_on"`
}

type UIConfig struct {
	// ToastsEnabled toast 总开关. false 时所有 notify.* 都静音.
	ToastsEnabled bool `json:"toasts_enabled"`
	// RevealOnDone 接收完文件自动 Explorer 高亮新文件.
	RevealOnDone bool `json:"reveal_on_done"`
}

type SystemConfig struct {
	// Autostart Windows 启动时是否自动跑 daemon.
	// 切换时同步写 HKCU\Software\Microsoft\Windows\CurrentVersion\Run.
	Autostart bool `json:"autostart"`
}

// Default 返回不带任何 env / 磁盘文件的全默认配置. Load 失败 fallback 用.
func Default() Config {
	return Config{
		Download: DownloadConfig{
			Dir:      "", // 空串 = 用 ~/Downloads/QuickDrop/
			Conflict: ConflictRename,
		},
		Server: ServerConfig{
			Port:        8443,
			MdnsEnabled: true,
		},
		Receive: ReceiveConfig{
			MaxFileSize: 0,    // 不限
			DefaultOn:   true, // 默认开启接收
		},
		UI: UIConfig{
			ToastsEnabled: true,
			RevealOnDone:  true,
		},
		System: SystemConfig{
			Autostart: false,
		},
	}
}

// Manager 持有当前 config 的副本 + 序列化保护. server 注入它, handler 通过它读/写.
// Save 会立刻 flush 到磁盘.
type Manager struct {
	mu   sync.RWMutex
	path string
	cur  Config
}

// Load 从 ~/.quickdrop/config.json 加载 + 应用 env 覆盖.
// 文件不存在: 用默认 + env. 文件损坏: 返 error (调用方决定是否退回默认).
func Load() (*Manager, error) {
	dir, err := configDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "config.json")
	cur := Default()

	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		// 首次启动: 用默认, 不报错
	case err != nil:
		return nil, fmt.Errorf("读 %s: %w", path, err)
	default:
		if err := json.Unmarshal(data, &cur); err != nil {
			return nil, fmt.Errorf("解析 %s: %w", path, err)
		}
	}

	// 合法性兜底: 不合法的 conflict 值回退默认
	if !cur.Download.Conflict.IsValid() {
		cur.Download.Conflict = ConflictRename
	}
	if cur.Server.Port <= 0 || cur.Server.Port > 65535 {
		cur.Server.Port = 8443
	}

	// 应用 env 覆盖 (不写回磁盘, 仅本次启动)
	applyEnv(&cur)

	return &Manager{path: path, cur: cur}, nil
}

// Get 返回当前 config 的副本 (并发安全).
func (m *Manager) Get() Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cur
}

// Save 替换整份 config 并立刻 flush 磁盘. 调用方应先 Get 拿副本改字段再 Save.
// 字段不合法 (port 越界 / conflict 非法) 返 error 拒写, 防垃圾进 JSON.
func (m *Manager) Save(c Config) error {
	if c.Server.Port <= 0 || c.Server.Port > 65535 {
		return fmt.Errorf("port 越界: %d", c.Server.Port)
	}
	if !c.Download.Conflict.IsValid() {
		return fmt.Errorf("conflict 非法: %s", c.Download.Conflict)
	}
	if c.Receive.MaxFileSize < 0 {
		return fmt.Errorf("max_file_size 负值: %d", c.Receive.MaxFileSize)
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化 config: %w", err)
	}

	m.mu.Lock()
	tmp := m.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		m.mu.Unlock()
		return fmt.Errorf("写 %s: %w", tmp, err)
	}
	// Windows os.Rename 不覆盖, 先 Remove
	_ = os.Remove(m.path)
	if err := os.Rename(tmp, m.path); err != nil {
		_ = os.Remove(tmp)
		m.mu.Unlock()
		return fmt.Errorf("rename %s → %s: %w", tmp, m.path, err)
	}
	m.cur = c
	m.mu.Unlock()
	return nil
}

// ResolvedDownloadDir 把空字符串 Dir 替换成 ~/Downloads/QuickDrop/ 默认值后返回.
// 同时确保目录存在 (MkdirAll). 失败返 error, 调用方应优雅降级 (落到默认目录).
func (m *Manager) ResolvedDownloadDir() (string, error) {
	c := m.Get()
	if c.Download.Dir != "" {
		if err := os.MkdirAll(c.Download.Dir, 0o755); err != nil {
			return "", fmt.Errorf("创建 %s: %w", c.Download.Dir, err)
		}
		return c.Download.Dir, nil
	}
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

// Path 返回 config.json 完整路径. 测试用.
func (m *Manager) Path() string { return m.path }

// applyEnv 把环境变量按优先级覆盖到 c 上. 不写回磁盘.
func applyEnv(c *Config) {
	if v := strings.TrimSpace(os.Getenv(envPort)); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n < 65536 {
			c.Server.Port = n
		}
	}
	if v := strings.TrimSpace(os.Getenv(envDownloadDir)); v != "" {
		c.Download.Dir = v
	}
	// QUICKDROP_DEVICE_NAME 与 QUICKDROP_WINDOW_MODE 还由 identity / main 直接读 env,
	// 暂不进 config (它们是启动期 once-only, 没有热改语义).
}

// configDir 返回 ~/.quickdrop/ (创建若不存在).
// 与 internal/identity / internal/devices 一致, 故意三处不复用避免循环 import.
func configDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("拿不到用户家目录: %w", err)
	}
	dir := filepath.Join(home, ".quickdrop")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("创建 %s: %w", dir, err)
	}
	return dir, nil
}
