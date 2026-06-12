// Package discovery 基于 mDNS 的局域网 PC 发现.
//
// 协议: _quickdrop._tcp.local
// TXT records: name=<显示名> uuid=<32hex> version=<sem ver>
//
// daemon 启动时 Register 把自己广播出去 + Browse 不断更新对端列表;
// 退出时 Close 注销.
//
// **单机限制**: grandcat/zeroconf v1.0 默认不走 loopback multicast (PR #68 待合),
// 所以同机两个 daemon 互相看不见. 这是库的限制不是代码 bug.
// 真实 PC→PC 联调需要第二台机器 (test-discovery.ps1 因此只验证注册成功 + 自己不出现在自己列表里).
//
// Phase 2.5a: 只做发现, "发送到 X" 的实际数据流走 Phase 2.5b 的 /peer/* 路由.
package discovery

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/grandcat/zeroconf"
)

const (
	// service mDNS 服务类型. 自定义 _quickdrop._tcp.
	service = "_quickdrop._tcp"
	// domain 标准 mDNS 域.
	domain = "local."
)

// Peer 发现到的对端 QuickDrop 实例.
type Peer struct {
	UUID    string   `json:"uuid"`    // 设备唯一 ID, 跨主机名变更不变
	Name    string   `json:"name"`    // 显示名 (用户起的或主机名)
	Host    string   `json:"host"`    // mDNS hostname (xxx.local.)
	IPv4    []string `json:"ipv4"`    // 对端的 LAN IPv4 列表
	Port    int      `json:"port"`    // HTTP 端口 (一般 8443)
	Version string   `json:"version"` // 软件版本 (留给将来兼容性判断)
	SeenAt  int64    `json:"seenAt"`  // 最近一次看到的 Unix 秒
}

// Discovery daemon 持有的 mDNS 句柄, 同时承担 Server (广播自己) + Browser (发现别人).
type Discovery struct {
	server    *zeroconf.Server // 广播自己
	cancelCtx context.CancelFunc

	myUUID string

	mu    sync.RWMutex
	peers map[string]*Peer // key = UUID
}

// Start 注册本机 mDNS 服务 + 启 browser 协程.
// myName/myUUID/myVersion 出现在 TXT records 里, port 是 HTTP server 监听端口.
//
// 失败返回 error, daemon 调用方应继续启动 (没有 mDNS 不影响手机扫码/右键发送).
func Start(myUUID, myName, myVersion string, port int) (*Discovery, error) {
	if myUUID == "" || myName == "" {
		return nil, fmt.Errorf("uuid 和 name 不能为空")
	}

	// Instance 名字 mDNS 上要 unique. 用 "name-uuid前6位" 避免同名碰撞.
	instance := fmt.Sprintf("%s-%s", myName, myUUID[:6])

	txt := []string{
		"name=" + myName,
		"uuid=" + myUUID,
		"version=" + myVersion,
	}

	srv, err := zeroconf.Register(instance, service, domain, port, txt, nil)
	if err != nil {
		return nil, fmt.Errorf("zeroconf.Register: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	d := &Discovery{
		server:    srv,
		cancelCtx: cancel,
		myUUID:    myUUID,
		peers:     make(map[string]*Peer),
	}

	// browser: 不断扫描 _quickdrop._tcp 实例, 推到 entries chan
	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		srv.Shutdown()
		cancel()
		return nil, fmt.Errorf("zeroconf.NewResolver: %w", err)
	}

	entries := make(chan *zeroconf.ServiceEntry, 16)
	go d.consume(entries)

	go func() {
		// Browse 阻塞到 ctx 取消, ctx cancel 后 entries 由 zeroconf 关闭
		if err := resolver.Browse(ctx, service, domain, entries); err != nil {
			log.Printf("mDNS browse: %v", err)
		}
	}()

	log.Printf("mDNS 已广播: instance=%s service=%s port=%d", instance, service, port)
	return d, nil
}

// consume 把 zeroconf 推过来的 ServiceEntry 解析成 Peer.
// 跳过自己 (同 UUID).
func (d *Discovery) consume(entries <-chan *zeroconf.ServiceEntry) {
	for e := range entries {
		txt := parseTXT(e.Text)
		uuid := txt["uuid"]
		if uuid == "" || uuid == d.myUUID {
			continue // 没有 UUID 不是 QuickDrop, 或是自己
		}
		p := &Peer{
			UUID:    uuid,
			Name:    txt["name"],
			Host:    e.HostName,
			IPv4:    ipv4Strs(e.AddrIPv4),
			Port:    e.Port,
			Version: txt["version"],
			SeenAt:  time.Now().Unix(),
		}
		d.mu.Lock()
		d.peers[uuid] = p
		d.mu.Unlock()
		log.Printf("mDNS 发现对端: %s (%s) @ %v:%d", p.Name, uuid[:6], p.IPv4, p.Port)
	}
}

// Peers 返回当前已知对端列表的快照, 按显示名排序.
// 过期清理留待 2.5b (现在先信任 zeroconf 的 entries 更新).
func (d *Discovery) Peers() []*Peer {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := make([]*Peer, 0, len(d.peers))
	for _, p := range d.peers {
		// 复制一份避免外部修改
		clone := *p
		out = append(out, &clone)
	}
	// 简单按 name 排序
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1].Name > out[j].Name; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}

// Close 注销 mDNS 广播 + 停 browser 协程.
func (d *Discovery) Close() {
	if d.server != nil {
		d.server.Shutdown()
	}
	if d.cancelCtx != nil {
		d.cancelCtx()
	}
}

// parseTXT mDNS TXT 是 []string, 每条 "key=value".
func parseTXT(txt []string) map[string]string {
	m := make(map[string]string, len(txt))
	for _, s := range txt {
		for i := 0; i < len(s); i++ {
			if s[i] == '=' {
				m[s[:i]] = s[i+1:]
				break
			}
		}
	}
	return m
}

func ipv4Strs(addrs []net.IP) []string {
	out := make([]string, 0, len(addrs))
	for _, a := range addrs {
		out = append(out, a.String())
	}
	return out
}
