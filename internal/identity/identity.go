// Package identity 管理本机 QuickDrop 设备身份: UUID + 显示名.
//
// UUID 持久化到 ~/.quickdrop/device-id, 首次启动随机生成, 后续读取.
// 用于 mDNS TXT 字段, 让对端能区分 "同 IP 不同 daemon" 以及 "改名后仍是同一设备".
//
// 显示名优先级: env QUICKDROP_DEVICE_NAME > 主机名 (os.Hostname).
// 配置页 (Phase 2 后期) 做完后会把环境变量升级到 ~/.quickdrop/config.json.
package identity

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	// envDeviceName 用户起的设备显示名 (mDNS TXT name 字段).
	envDeviceName = "QUICKDROP_DEVICE_NAME"
)

// Identity 当前进程的设备身份.
type Identity struct {
	UUID string // 16 字节随机 hex, 持久化到磁盘
	Name string // 显示名 (mDNS TXT 字段)
}

// Load 加载/生成本机 identity.
// UUID: ~/.quickdrop/device-id 文件 (无则生成并写入).
// Name: env QUICKDROP_DEVICE_NAME > os.Hostname() > "Unknown PC".
func Load() (*Identity, error) {
	uuid, err := loadOrCreateUUID()
	if err != nil {
		return nil, err
	}
	return &Identity{
		UUID: uuid,
		Name: deviceName(),
	}, nil
}

func deviceName() string {
	if n := strings.TrimSpace(os.Getenv(envDeviceName)); n != "" {
		return n
	}
	if h, err := os.Hostname(); err == nil && h != "" {
		return h
	}
	return "Unknown PC"
}

// loadOrCreateUUID 读取 ~/.quickdrop/device-id, 不存在就生成 32 字符 hex (16 字节随机)
// 写进去再返回.
func loadOrCreateUUID() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "device-id")

	// 已存在 → 读
	if data, err := os.ReadFile(path); err == nil {
		uuid := strings.TrimSpace(string(data))
		if len(uuid) == 32 {
			return uuid, nil
		}
		// 格式不对, 当作不存在重新生成 (不打扰用户)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("读 %s: %w", path, err)
	}

	// 不存在 → 生成
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成 UUID: %w", err)
	}
	uuid := hex.EncodeToString(buf)
	if err := os.WriteFile(path, []byte(uuid), 0o644); err != nil {
		return "", fmt.Errorf("写 %s: %w", path, err)
	}
	return uuid, nil
}

// configDir 返回 ~/.quickdrop/ (创建若不存在).
// 同时也是 Phase 2.6 devices.json 的存储位置.
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
