// Package devices 持久化设备身份 + 信任等级.
//
// 存 ~/.quickdrop/devices.json. 写入是原子的 (写临时 + rename),
// 进程崩溃不会留半截 JSON.
//
// 信任模型 (ADR-20):
//   ask     默认. 收到 incoming → 弹 toast 按钮 → 用户决策
//   trusted 信任. 收到 incoming → 直接 accept + Pull, 同时弹纯通知 toast 告知
//   blocked 黑名单. 收到 incoming → 直接 reject, 不弹 toast, 不留 pending
//
// 用户可在设备管理页 (/v) 任意时刻撤回信任或解除黑名单.
package devices

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Trust 信任等级. ADR-20.
type Trust string

const (
	TrustAsk     Trust = "ask"     // 默认: 弹 toast 等用户决策
	TrustTrusted Trust = "trusted" // 自动接受 + 纯通知
	TrustBlocked Trust = "blocked" // 静默拒绝
)

// IsValid 校验 trust 值合法.
func (t Trust) IsValid() bool {
	switch t {
	case TrustAsk, TrustTrusted, TrustBlocked:
		return true
	}
	return false
}

// Device 持久化的一条设备记录.
type Device struct {
	UUID      string `json:"uuid"`
	Name      string `json:"name"`      // 最近一次 mDNS 看到的显示名
	Trust     Trust  `json:"trust"`
	FirstSeen int64  `json:"firstSeen"` // Unix 秒
	LastSeen  int64  `json:"lastSeen"`  // Unix 秒
}

// Store 并发安全的设备表. 操作后立刻 flush 到磁盘.
type Store struct {
	mu      sync.RWMutex
	path    string             // ~/.quickdrop/devices.json
	devices map[string]*Device // key = UUID
}

// Load 从 ~/.quickdrop/devices.json 加载 (不存在则空 store, lazy 创建).
func Load() (*Store, error) {
	dir, err := configDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "devices.json")
	s := &Store{
		path:    path,
		devices: make(map[string]*Device),
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return s, nil
		}
		return nil, fmt.Errorf("读 %s: %w", path, err)
	}
	var on diskFormat
	if err := json.Unmarshal(data, &on); err != nil {
		return nil, fmt.Errorf("解析 %s: %w", path, err)
	}
	for _, d := range on.Devices {
		if d.UUID == "" {
			continue
		}
		if !d.Trust.IsValid() {
			d.Trust = TrustAsk
		}
		clone := d
		s.devices[d.UUID] = &clone
	}
	return s, nil
}

// diskFormat ~/.quickdrop/devices.json 的根 JSON 结构.
// 包一层 "devices" 方便后续加全局配置 (默认 trust / 黑名单策略等).
type diskFormat struct {
	Devices []Device `json:"devices"`
}

// Get 取一条设备记录 (不存在返 nil).
func (s *Store) Get(uuid string) *Device {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d, ok := s.devices[uuid]
	if !ok {
		return nil
	}
	clone := *d
	return &clone
}

// TrustOf 拿设备 trust. 未知设备返 TrustAsk (默认).
func (s *Store) TrustOf(uuid string) Trust {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d, ok := s.devices[uuid]
	if !ok {
		return TrustAsk
	}
	return d.Trust
}

// UpsertSeen 收到 incoming 时调: 更新 name + lastSeen, 没记录就新建.
// 新建时 trust 默认 ask.
func (s *Store) UpsertSeen(uuid, name string) error {
	if uuid == "" {
		return errors.New("uuid 不能为空")
	}
	now := time.Now().Unix()
	s.mu.Lock()
	d, ok := s.devices[uuid]
	if !ok {
		d = &Device{
			UUID:      uuid,
			Name:      name,
			Trust:     TrustAsk,
			FirstSeen: now,
		}
		s.devices[uuid] = d
	}
	d.Name = name
	d.LastSeen = now
	s.mu.Unlock()
	return s.flush()
}

// SetTrust 直接设 trust. 用户在 /v 管理页操作走这.
// 设备不存在会创建一条 (lastSeen=0, 还没见过的设备也能预先信任/拉黑).
func (s *Store) SetTrust(uuid, name string, t Trust) error {
	if uuid == "" {
		return errors.New("uuid 不能为空")
	}
	if !t.IsValid() {
		return fmt.Errorf("无效 trust: %s", t)
	}
	s.mu.Lock()
	d, ok := s.devices[uuid]
	if !ok {
		d = &Device{
			UUID:      uuid,
			Name:      name,
			FirstSeen: time.Now().Unix(),
		}
		s.devices[uuid] = d
	}
	d.Trust = t
	if name != "" {
		d.Name = name
	}
	s.mu.Unlock()
	return s.flush()
}

// All 返回所有设备的快照, 按 lastSeen 倒序 (最近看到的在前).
func (s *Store) All() []Device {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Device, 0, len(s.devices))
	for _, d := range s.devices {
		out = append(out, *d)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].LastSeen > out[j].LastSeen
	})
	return out
}

// flush 原子写磁盘: 写临时文件 + rename. 持锁外调.
func (s *Store) flush() error {
	s.mu.RLock()
	out := diskFormat{Devices: make([]Device, 0, len(s.devices))}
	for _, d := range s.devices {
		out.Devices = append(out.Devices, *d)
	}
	s.mu.RUnlock()

	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化 devices: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("写 %s: %w", tmp, err)
	}
	// Windows 上 os.Rename 不覆盖, 先 Remove
	_ = os.Remove(s.path)
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename %s → %s: %w", tmp, s.path, err)
	}
	return nil
}

// configDir 返回 ~/.quickdrop/ (创建若不存在).
// 与 internal/identity 的一致, 故意不复用 identity 包避免循环 import.
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
