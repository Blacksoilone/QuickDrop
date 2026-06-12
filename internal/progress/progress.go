// Package progress 给前端推实时传输进度.
//
// 架构: 单 Hub 持订阅者列表 (Vue 各页通过 /ws 连进来), publish 时广播.
// 节流: 调用方负责 (CountingReader 内部按 200ms 或 done 触发, 避免 100Hz 刷屏).
//
// Event JSON 形状跟 web/src/api.ts 的 ProgressEvent 保持一致.
package progress

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"sync"
	"time"
)

// Kind 区分进度事件的语义.
type Kind string

const (
	KindSend     Kind = "send"     // 我发, 对端 Pull 中 (sender 视角; 当前不推, 留位)
	KindReceive  Kind = "receive"  // 我作为接收方 Pull 对端 / 接收手机 upload 中
	KindUpload   Kind = "upload"   // 手机上传到电脑 (与 receive 同义, 当前不区分)
	KindDownload Kind = "download" // 手机下载本机 (sender 视角; 当前不推)
)

// Event 推给前端的一帧进度. JSON 字段小写驼峰跟 Vue 一致.
type Event struct {
	ID       string `json:"id"`       // 传输唯一 ID (token / upload-<rand> / file-<rand>)
	Kind     Kind   `json:"kind"`
	FileName string `json:"fileName"`
	FileSize int64  `json:"fileSize"`
	Bytes    int64  `json:"bytes"`
	Done     bool   `json:"done"`
	Err      string `json:"err,omitempty"`
	At       int64  `json:"at"` // Unix 毫秒, 给前端算速率
}

// Hub 单例, 由 server 持有, 通过 /ws 路由把订阅者加进来.
type Hub struct {
	mu       sync.RWMutex
	subs     map[chan Event]struct{}
	lastEv   map[string]Event // id → 最后一帧, 新连接立刻补一个 snapshot 拿当前状态
	lastMu   sync.RWMutex
}

func NewHub() *Hub {
	return &Hub{
		subs:   make(map[chan Event]struct{}),
		lastEv: make(map[string]Event),
	}
}

// Subscribe 加一个订阅者 channel. 返回 unsubscribe 函数.
// channel buffer = 32, 满了直接 drop 那一帧 (slow client 不阻塞其他人).
func (h *Hub) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, 32)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()

	// snapshot: 把当前所有进行中传输的最后一帧重放给新订阅者
	h.lastMu.RLock()
	snap := make([]Event, 0, len(h.lastEv))
	for _, e := range h.lastEv {
		snap = append(snap, e)
	}
	h.lastMu.RUnlock()
	for _, e := range snap {
		select {
		case ch <- e:
		default:
		}
	}

	return ch, func() {
		h.mu.Lock()
		delete(h.subs, ch)
		h.mu.Unlock()
		close(ch)
	}
}

// Publish 广播一帧给所有订阅者. 顺手更新 lastEv (Done 后清理).
func (h *Hub) Publish(e Event) {
	e.At = time.Now().UnixMilli()

	h.lastMu.Lock()
	if e.Done {
		delete(h.lastEv, e.ID)
	} else {
		h.lastEv[e.ID] = e
	}
	h.lastMu.Unlock()

	h.mu.RLock()
	for ch := range h.subs {
		select {
		case ch <- e:
		default:
			// 缓冲满, drop 这一帧 (节流场景下下一帧很快会来)
		}
	}
	h.mu.RUnlock()
}

// WrapReader 包装 r 让其透传 Read 时按字节计数 + 节流推进度.
// 返回的 ProgressReader 实现 io.Reader, 额外 Done(err) 在结束时强制推最终一帧.
// 这是 server 端 ProgressPublisher 接口的入口.
func (h *Hub) WrapReader(r io.Reader, id string, kind Kind, fileName string, fileSize int64) ProgressReader {
	return NewCountingReader(r, h, id, kind, fileName, fileSize)
}

// ProgressReader 由 WrapReader 返回, 透传 io.Reader 同时支持 Done(err) 收尾.
type ProgressReader interface {
	io.Reader
	Done(err error)
}
// 用于 saveStream / pullPeerFile / handleUpload 的 io.Copy 源.
type CountingReader struct {
	r        io.Reader
	hub      *Hub
	id       string
	kind     Kind
	fileName string
	fileSize int64
	bytes    int64
	lastPub  time.Time
}

func NewCountingReader(r io.Reader, hub *Hub, id string, kind Kind, fileName string, fileSize int64) *CountingReader {
	cr := &CountingReader{
		r:        r,
		hub:      hub,
		id:       id,
		kind:     kind,
		fileName: fileName,
		fileSize: fileSize,
	}
	// 立刻发一帧 bytes=0 让前端立即看到 "开始中"
	if hub != nil {
		hub.Publish(Event{ID: id, Kind: kind, FileName: fileName, FileSize: fileSize, Bytes: 0})
	}
	return cr
}

// Read 透传 + 节流推进度.
// 调用方对 EOF 后还要不要发 Done(true) 自己决定 (Close 函数).
func (c *CountingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if n > 0 {
		c.bytes += int64(n)
		now := time.Now()
		// 200ms 节流, 防 ws 拥堵
		if c.hub != nil && now.Sub(c.lastPub) >= 200*time.Millisecond {
			c.hub.Publish(Event{
				ID: c.id, Kind: c.kind, FileName: c.fileName, FileSize: c.fileSize,
				Bytes: c.bytes,
			})
			c.lastPub = now
		}
	}
	return n, err
}

// Done 调用方完成 (成功或失败) 后调一次. 强制推最后一帧 Done=true.
// err 非空表示失败.
func (c *CountingReader) Done(err error) {
	if c.hub == nil {
		return
	}
	ev := Event{
		ID: c.id, Kind: c.kind, FileName: c.fileName, FileSize: c.fileSize,
		Bytes: c.bytes, Done: true,
	}
	if err != nil {
		ev.Err = err.Error()
	}
	c.hub.Publish(ev)
}

// CountingReader 包装 io.Reader, 每读 N 字节 (或 200ms) 推一帧.

// Conn 由 ServeWS 用. coder/websocket.Conn 实现 (在 server 包 adapter 后).
// progress 包不直接 import websocket 以保持轻量.
type Conn interface {
	Write(ctx context.Context, msg []byte) error
	Close(code int, reason string) error
}

// ServeWS 给一个 ws connection 跑订阅 → 写循环, 阻塞到客户端断开或 ctx done.
// 由 server.handleWS 调.
func (h *Hub) ServeWS(ctx context.Context, conn Conn) {
	ch, unsub := h.Subscribe()
	defer unsub()
	defer func() { _ = conn.Close(1000, "bye") }()

	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			data, err := json.Marshal(ev)
			if err != nil {
				continue
			}
			wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err = conn.Write(wctx, data)
			cancel()
			if err != nil {
				// 写失败说明客户端断了
				log.Printf("ws write err (sub will close): %v", err)
				return
			}
		}
	}
}
