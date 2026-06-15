// Package transfer 管理文件发送队列.
//
// 当前实现: 单文件 (current *Transfer).
// 未来扩展: 多文件队列 (queue []Transfer) + 历史记录 (history []Transfer).
//
// 设计原则: Server 不直接管理文件状态, 通过 TransferManager 操作.
// 好处: 多文件/历史记录 时 Server 接口不变, 只改 TransferManager 内部.
package transfer

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Transfer 表示一次文件传输 (发送).
// 未来扩展: 加 ID/State/StartedAt 字段支持历史记录.
type Transfer struct {
	FilePath string // 绝对路径
	FileName string // 文件名 (展示用)
	FileSize int64  // 字节数
	// 未来扩展:
	// ID        string
	// State     TransferState  // pending/sending/completed/failed
	// StartedAt time.Time
	// Progress  float64        // 0.0-1.0
}

// Manager 管理当前发送文件.
// 当前: 单文件 (current). 未来: 队列 (queue []Transfer).
type Manager struct {
	mu      sync.RWMutex
	current *Transfer // nil 表示无文件
}

// NewManager 创建传输管理器.
func NewManager() *Manager {
	return &Manager{}
}

// SetCurrent 设置当前发送文件. 验证文件存在 + 提取元信息.
// 已有文件会被替换 (replace 语义, 跟现有 Server.SwapFile 一致).
func (m *Manager) SetCurrent(path string) error {
	abs, name, size, err := validateFile(path)
	if err != nil {
		return err
	}

	m.mu.Lock()
	m.current = &Transfer{
		FilePath: abs,
		FileName: name,
		FileSize: size,
	}
	m.mu.Unlock()

	return nil
}

// Current 返回当前发送文件. 无文件时返 nil.
func (m *Manager) Current() *Transfer {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.current
}

// HasCurrent 判断是否有文件在发送槽.
func (m *Manager) HasCurrent() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.current != nil
}

// Clear 清空当前发送文件.
func (m *Manager) Clear() {
	m.mu.Lock()
	m.current = nil
	m.mu.Unlock()
}

// validateFile 验证文件路径 + 提取元信息 (绝对路径 / 文件名 / 大小).
// 复用自 server.validateFile, 抽出来避免循环依赖.
func validateFile(rawPath string) (abs, name string, size int64, err error) {
	abs, err = filepath.Abs(rawPath)
	if err != nil {
		return "", "", 0, fmt.Errorf("路径转绝对路径失败: %w", err)
	}

	info, err := os.Stat(abs)
	if err != nil {
		return "", "", 0, fmt.Errorf("文件不存在或无法访问: %w", err)
	}
	if info.IsDir() {
		return "", "", 0, fmt.Errorf("不支持目录: %s", abs)
	}

	name = filepath.Base(abs)
	size = info.Size()
	return abs, name, size, nil
}
