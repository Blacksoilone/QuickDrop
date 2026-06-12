// Package peer 管理 PC↔PC 文件传输的状态机.
//
// 角色:
//   - Sender (我是发送方): 持有 outgoing map[token]*Outgoing. 当 POST 到对端 /peer/incoming
//     成功后, 等对端 Pull /peer/file?token=xxx 或调 /peer/decision 通知决策.
//   - Receiver (我是接收方): 持有 pending map[token]*Pending. 收到 /peer/incoming 后入队,
//     等用户 (toast / Vue / CLI) 接受或拒绝.
//
// 一个 daemon 同时是两种角色 (你既能发也能收), 所以两个 map 都持有.
//
// 安全 (ADR-17 + ADR-20):
//   token 是 Sender 生成的 32 字符随机 hex, 同时也是 transferID
//   Pull 必须带正确 token, 否则 403 (不暴露存在性)
//   一次 Pull 成功后 outgoing 立即清除, token 失效 (防重放)
//   pending 30 分钟未决策自动 expire (避免内存泄漏)
package peer

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

const (
	// pendingTTL 入队但未决策的 incoming 多久过期清理.
	pendingTTL = 30 * time.Minute
	// outgoingTTL 已 POST 出去未被 Pull 的 outgoing 多久过期 (防 sender 内存堆积).
	outgoingTTL = 30 * time.Minute
)

// PeerInfo 对端身份的子集 (来自 discovery.Peer, 但 peer 包不直接引用 discovery 避免循环).
type PeerInfo struct {
	UUID string `json:"uuid"`
	Name string `json:"name"`
	Host string `json:"host"`
	IPv4 string `json:"ipv4"` // 第一个 IP 即可, 不全部带
	Port int    `json:"port"`
}

// State 决策状态.
type State string

const (
	StatePending   State = "pending"   // Bob 端: 等用户决策
	StateAccepted  State = "accepted"  // Bob 已接受, 准备 Pull
	StateRejected  State = "rejected"  // Bob 已拒绝
	StateDelivered State = "delivered" // Alice 端: Bob 已 Pull 完
	StateExpired   State = "expired"   // 30min 未决策
)

// Incoming Sender→Receiver POST /peer/incoming 的 body 结构.
// 同时也作 Pending 持久化结构在 Receiver 内存里.
type Incoming struct {
	Token    string   `json:"token"`    // 32 字符 hex, 同时是 transferID
	From     PeerInfo `json:"from"`     // 谁发的
	FileName string   `json:"fileName"` // 文件名 (展示)
	FileSize int64    `json:"fileSize"` // 字节数 (展示 + 进度)
}

// Outgoing Sender 持有的待 Pull 状态.
type Outgoing struct {
	Token    string
	To       PeerInfo // 发给谁
	AbsPath  string   // 本地文件绝对路径 (Pull 时 ServeFile 用)
	FileName string
	FileSize int64
	State    State
	StartAt  time.Time
}

// Pending Receiver 持有的待决策状态.
type Pending struct {
	Incoming
	State    State
	ArriveAt time.Time
}

// Manager 是 server 持有的 peer 状态机.
// 并发安全, 内部用 mu 保护两个 map.
type Manager struct {
	mu       sync.RWMutex
	outgoing map[string]*Outgoing // key = token
	pending  map[string]*Pending  // key = token
	onChange func()               // 任何 state 变化后触发 (add/setState/gc), tray 红点用
}

func NewManager() *Manager {
	m := &Manager{
		outgoing: make(map[string]*Outgoing),
		pending:  make(map[string]*Pending),
	}
	// 后台 GC, 30 秒扫一次清过期项
	go m.gcLoop()
	return m
}

// SetOnChange 注册 pending/outgoing 变化回调. 调用频率: 每次 add/setState/gc.
// 回调里只该读 PendingCount 等 cheap 操作, 别阻塞.
func (m *Manager) SetOnChange(fn func()) {
	m.mu.Lock()
	m.onChange = fn
	m.mu.Unlock()
}

// fireChange 内部帮手, 持锁外调.
func (m *Manager) fireChange() {
	m.mu.RLock()
	cb := m.onChange
	m.mu.RUnlock()
	if cb != nil {
		cb()
	}
}

// CreateOutgoing 由 Sender 调: 生成 token 注册 outgoing, 返回 Incoming 让调用方 POST 给对端.
func (m *Manager) CreateOutgoing(to PeerInfo, absPath, fileName string, fileSize int64) (*Outgoing, *Incoming, error) {
	token, err := newToken()
	if err != nil {
		return nil, nil, err
	}
	o := &Outgoing{
		Token:    token,
		To:       to,
		AbsPath:  absPath,
		FileName: fileName,
		FileSize: fileSize,
		State:    StatePending,
		StartAt:  time.Now(),
	}
	m.mu.Lock()
	m.outgoing[token] = o
	m.mu.Unlock()
	return o, &Incoming{
		Token:    token,
		From:     PeerInfo{}, // 调用方填 (来自 identity)
		FileName: fileName,
		FileSize: fileSize,
	}, nil
}

// LookupOutgoing 由 /peer/file handler 调: 验证 token 取出本地文件路径 + 收件方信息.
// 返回 ok=false 表示 token 错或已 Delivered (防重放).
func (m *Manager) LookupOutgoing(token string) (*Outgoing, bool) {
	m.mu.RLock()
	o, ok := m.outgoing[token]
	m.mu.RUnlock()
	if !ok || o.State == StateDelivered {
		return nil, false
	}
	return o, true
}

// MarkDelivered 由 /peer/file handler 在 ServeFile 完成后调, 标记 outgoing 已交付.
// 不立刻 delete, 留给 gc 清理, 让 Sender 端有窗口看到 "Delivered" 状态.
func (m *Manager) MarkDelivered(token string) {
	m.mu.Lock()
	if o, ok := m.outgoing[token]; ok {
		o.State = StateDelivered
	}
	m.mu.Unlock()
}

// AddPending 由 /peer/incoming handler 调: 收到对端 POST, 入待决策队列.
// 返回 error 表示 token 冲突 (理论上 32hex 不可能, 但兜底).
func (m *Manager) AddPending(inc Incoming) error {
	m.mu.Lock()
	if _, dup := m.pending[inc.Token]; dup {
		m.mu.Unlock()
		return errors.New("token 已存在")
	}
	m.pending[inc.Token] = &Pending{
		Incoming: inc,
		State:    StatePending,
		ArriveAt: time.Now(),
	}
	m.mu.Unlock()
	m.fireChange()
	return nil
}

// LookupPending 查找一条 pending. 找不到返 nil.
func (m *Manager) LookupPending(token string) *Pending {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.pending[token]
	if !ok {
		return nil
	}
	return p
}

// SetPendingState 由决策 handler 调 (accept/reject 后).
// 不删, 留给 gc, UI 端能看到 "已接受" / "已拒绝" 几秒.
func (m *Manager) SetPendingState(token string, s State) bool {
	m.mu.Lock()
	p, ok := m.pending[token]
	if ok {
		p.State = s
	}
	m.mu.Unlock()
	if ok {
		m.fireChange()
	}
	return ok
}

// PendingList 返回所有 pending 项的快照 (按到达时间, 新的在前).
// /api/pending 用.
func (m *Manager) PendingList() []*Pending {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Pending, 0, len(m.pending))
	for _, p := range m.pending {
		clone := *p
		out = append(out, &clone)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1].ArriveAt.Before(out[j].ArriveAt); j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}

// PendingCount 待决策 (StatePending) 的条数, 给托盘 tooltip / 红点用.
func (m *Manager) PendingCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	n := 0
	for _, p := range m.pending {
		if p.State == StatePending {
			n++
		}
	}
	return n
}

// gcLoop 后台扫过期项: pending > 30min 未决策 → expired, outgoing > 30min 未 Pull → 删.
// daemon 退出时 goroutine 随进程结束, 不需要显式 stop.
func (m *Manager) gcLoop() {
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	for range tick.C {
		now := time.Now()
		changed := false
		m.mu.Lock()
		for t, p := range m.pending {
			if p.State == StatePending && now.Sub(p.ArriveAt) > pendingTTL {
				p.State = StateExpired
				changed = true
			}
			// 已决策/过期的 1 小时后彻底删
			if p.State != StatePending && now.Sub(p.ArriveAt) > pendingTTL+time.Hour {
				delete(m.pending, t)
				changed = true
			}
		}
		for t, o := range m.outgoing {
			if o.State == StateDelivered && now.Sub(o.StartAt) > 5*time.Minute {
				delete(m.outgoing, t)
			} else if o.State == StatePending && now.Sub(o.StartAt) > outgoingTTL {
				delete(m.outgoing, t)
			}
		}
		m.mu.Unlock()
		if changed {
			m.fireChange()
		}
	}
}

// newToken 32 字符 hex (16 字节随机).
func newToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成 token: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
