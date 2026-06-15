// Package receive 管理接收状态 + 策略.
//
// 设计原则: 接收状态不是 Server 的字段, 是独立模块.
// 好处: 未来加 auto_accept_trusted / conflict_strategy 时不改 Server.
//
// 当前实现: 单一 enabled 开关 + 配置驱动初值.
// 未来扩展:
//   - AutoAcceptTrusted bool (信任设备免确认)
//   - ConflictStrategy string (rename/overwrite/ask)
//   - MaxFileSize int64 (已在 config, 待迁移进来)
package receive

import (
	"sync/atomic"
)

// Config 接收策略配置.
// 当前: 只有 DefaultOn (启动时是否开启接收).
// 未来扩展: AutoAcceptTrusted / ConflictStrategy / MaxFileSize.
type Config struct {
	DefaultOn bool
	// 未来扩展:
	// AutoAcceptTrusted bool
	// ConflictStrategy  string  // "rename" / "overwrite" / "ask"
	// MaxFileSize       int64
}

// Manager 管理接收状态.
type Manager struct {
	enabled  atomic.Bool
	config   *Config
	onToggle func(bool) // 状态切换回调 (通知 tray 更新 checkbox)
}

// NewManager 创建接收管理器.
// cfg: 从 config.json 读的配置 (决定 DefaultOn).
// onToggle: 状态变化回调 (可选, 用于同步 tray checkbox).
func NewManager(cfg *Config, onToggle func(bool)) *Manager {
	m := &Manager{
		config:   cfg,
		onToggle: onToggle,
	}
	m.enabled.Store(cfg.DefaultOn) // 初始化状态
	return m
}

// Enable 开启/关闭接收.
// 触发 onToggle 回调 (如果状态确实变了).
func (m *Manager) Enable(on bool) {
	prev := m.enabled.Swap(on)
	if prev != on && m.onToggle != nil {
		m.onToggle(on)
	}
}

// IsEnabled 返回当前接收状态.
func (m *Manager) IsEnabled() bool {
	return m.enabled.Load()
}

// Config 返回当前配置 (只读).
func (m *Manager) Config() *Config {
	return m.config
}
