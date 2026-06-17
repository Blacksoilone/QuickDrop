// /api/info 返回的 server 状态. 与 server.go ServerInfo 保持一致.
export interface ServerInfo {
  fileName: string;   // 当前发送文件名; 接收模式下空串
  fileSize: string;   // 人类可读大小; 接收模式下空串
  hasFile: boolean;   // 是否有可发送的文件
  receiving: boolean; // 是否在接收模式
}

// /api/peers 返回的对端 PC. 与 server.go Peer 保持一致.
export interface Peer {
  uuid: string;
  name: string;
  host: string;
  ipv4: string[];
  port: number;
  version: string;
  seenAt: number;
}

export async function fetchInfo(): Promise<ServerInfo> {
  const r = await fetch("/api/info", { cache: "no-store" });
  if (!r.ok) throw new Error(`/api/info ${r.status}`);
  return r.json();
}

export async function fetchPeers(): Promise<Peer[]> {
  const r = await fetch("/api/peers", { cache: "no-store" });
  if (!r.ok) throw new Error(`/api/peers ${r.status}`);
  return r.json();
}

// /api/pending 返回的待处理 incoming.
export interface PendingEntry {
  token: string;
  state: "pending" | "accepted" | "rejected" | "expired";
  from: Peer;
  fileName: string;
  fileSize: number;
  arriveAt: number; // Unix 秒
  trust: "ask" | "trusted" | "blocked"; // ADR-20 设备信任等级
}

export async function fetchPending(): Promise<PendingEntry[]> {
  const r = await fetch("/api/pending", { cache: "no-store" });
  if (!r.ok) throw new Error(`/api/pending ${r.status}`);
  return r.json();
}

// 决策一条 incoming.
// trust 可选: accept 时同时设 trust="trusted" (信任此设备),
//             reject 时同时设 trust="blocked" (永不信任).
export async function decidePeer(
  token: string,
  decision: "accept" | "reject",
  trust?: "trusted" | "blocked",
): Promise<void> {
  const body: Record<string, string> = { token, decision };
  if (trust) body.trust = trust;
  const r = await fetch("/internal/peer-decide", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  if (!r.ok) {
    const txt = await r.text();
    throw new Error(`peer-decide ${r.status}: ${txt}`);
  }
}

// /api/devices 返回的设备记录.
export interface DeviceEntry {
  uuid: string;
  name: string;
  alias: string;
  trust: "ask" | "trusted" | "blocked";
  firstSeen: number;
  lastSeen: number;
}

export async function fetchDevices(): Promise<DeviceEntry[]> {
  const r = await fetch("/api/devices", { cache: "no-store" });
  if (!r.ok) throw new Error(`/api/devices ${r.status}`);
  return r.json();
}

export async function setDeviceTrust(
  uuid: string,
  name: string,
  trust: "ask" | "trusted" | "blocked",
): Promise<void> {
  const r = await fetch("/internal/device-trust", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ uuid, name, trust }),
  });
  if (!r.ok) {
    const txt = await r.text();
    throw new Error(`device-trust ${r.status}: ${txt}`);
  }
}

// 设置设备别名 (空串清除别名, 显示 hostname)
export async function setDeviceAlias(uuid: string, alias: string): Promise<void> {
  const r = await fetch("/internal/device-alias", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ uuid, alias }),
  });
  if (!r.ok) {
    const txt = await r.text();
    throw new Error(`device-alias ${r.status}: ${txt}`);
  }
}

// 删除设备记录
export async function deleteDevice(uuid: string): Promise<void> {
  const r = await fetch("/internal/device-delete", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ uuid }),
  });
  if (!r.ok) {
    const txt = await r.text();
    throw new Error(`device-delete ${r.status}: ${txt}`);
  }
}

// ProgressEvent 与 server 端 progress.Event 保持一致.
export interface ProgressEvent {
  id: string;
  kind: "send" | "receive" | "upload" | "download";
  fileName: string;
  fileSize: number;
  bytes: number;
  done: boolean;
  err?: string;
  at: number; // Unix 毫秒
}

// subscribeProgress 连 /ws, 每帧调 onEvent. 断线自动 3 秒重连.
// 返回 unsubscribe 函数: 调了之后停止重连 + 关连接.
export function subscribeProgress(onEvent: (e: ProgressEvent) => void): () => void {
  let ws: WebSocket | null = null;
  let stopped = false;
  let reconnectTimer: number | undefined;

  function connect() {
    if (stopped) return;
    // /ws 同源, 自动用 ws:// (HTTP) 或 wss:// (HTTPS Phase 3)
    const proto = location.protocol === "https:" ? "wss:" : "ws:";
    const url = `${proto}//${location.host}/ws`;
    ws = new WebSocket(url);
    ws.onmessage = (msg) => {
      try {
        const ev = JSON.parse(msg.data) as ProgressEvent;
        onEvent(ev);
      } catch {
        /* ignore malformed */
      }
    };
    ws.onclose = () => {
      ws = null;
      if (!stopped) {
        reconnectTimer = window.setTimeout(connect, 3000);
      }
    };
    ws.onerror = () => {
      // 让 onclose 处理重连
      if (ws) ws.close();
    };
  }

  connect();

  return () => {
    stopped = true;
    if (reconnectTimer !== undefined) window.clearTimeout(reconnectTimer);
    if (ws) ws.close();
  };
}

// 给 daemon 发 IPC: 把 daemon 当前的发送文件发给指定 UUID 对端.
// daemon 自己 POST 对端 /peer/incoming, 触发对端 toast.
// filePath 不传, daemon 用自己的 s.absPath (Vue 不需要也不该知道绝对路径).
// 返回 token (本端 outgoing 句柄, 留给后续显示进度).
export async function sendToPeer(toUUID: string): Promise<{ token: string; to: string }> {
  const r = await fetch("/internal/peer-send", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ toUUID }),
  });
  if (!r.ok) {
    const txt = await r.text();
    throw new Error(`peer-send ${r.status}: ${txt}`);
  }
  return r.json();
}

// 调用 daemon 内部 IPC 关接收模式.
// 用在接收 dashboard 的 "停止接收" 按钮.
export async function stopReceiving(): Promise<void> {
  await fetch("/internal/receive", { method: "POST", body: "off" });
}

// webview 注入的全局 close 函数 (window.go 里 Bind 出来), 仅 daemon 弹窗里有.
// 浏览器里没有 → 用 window.close fallback (浏览器会忽略, 但不致命).
export function closeWindow(): void {
  const w = window as unknown as { quickdropClose?: () => void };
  if (typeof w.quickdropClose === "function") {
    w.quickdropClose();
  } else {
    window.close();
  }
}

// 最小化当前窗口 (无边框窗口专用, webview 注入的 quickdropMinimize)
export function minimizeWindow(): void {
  const w = window as unknown as { quickdropMinimize?: () => void };
  if (typeof w.quickdropMinimize === "function") {
    w.quickdropMinimize();
  }
}

// 启动窗口拖动 (无边框窗口专用)
// 在 mousedown 事件中调用, WebView2 不支持 -webkit-app-region: drag,
// 必须通过 Go 调 Win32 SendMessage 实现.
export function startWindowDrag(): void {
  const w = window as unknown as { quickdropStartDrag?: () => void };
  if (typeof w.quickdropStartDrag === "function") {
    w.quickdropStartDrag();
  }
}

// ============================================================
// 配置中心 (/c) - 与 server.go ConfigStore + internal/config.Config 对齐.
// 字段命名严格用 snake_case, 跟磁盘 JSON 一致, 直接序列化保存.
// ============================================================

export type ConflictPolicy = "rename" | "overwrite" | "reject";

export interface AppConfig {
  download: {
    dir: string; // 空串 = 默认 ~/Downloads/QuickDrop/
    conflict: ConflictPolicy;
  };
  server: {
    port: number;
    mdns_enabled: boolean;
  };
  receive: {
    max_file_size: number; // 字节, 0 = 不限
    default_on: boolean;   // 默认接收状态（启动时初值）
  };
  ui: {
    toasts_enabled: boolean;
    reveal_on_done: boolean;
    borderless_windows: boolean;
  };
  system: {
    autostart: boolean;
  };
}

export async function fetchConfig(): Promise<AppConfig> {
  const r = await fetch("/api/config", { cache: "no-store" });
  if (!r.ok) throw new Error(`/api/config ${r.status}`);
  return r.json();
}

// 整 cfg 提交给 daemon, daemon 内部 Save() 校验 + 持久化 + 热应用.
// 失败 (非法 port 等) 返 400 + 错误文本.
export async function saveConfig(cfg: AppConfig): Promise<void> {
  const r = await fetch("/internal/config-save", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(cfg),
  });
  if (!r.ok) {
    const txt = await r.text();
    throw new Error(`config-save ${r.status}: ${txt}`);
  }
}

// 弹出原生 Windows 文件夹选择器, 返回用户选择的路径.
// 用户取消返回空字符串. 用于配置页"浏览"按钮.
export async function pickFolder(): Promise<string> {
  const r = await fetch("/internal/pick-folder", { method: "POST" });
  if (!r.ok) {
    const txt = await r.text();
    throw new Error(`pick-folder ${r.status}: ${txt}`);
  }
  const data = await r.json();
  return data.path || "";
}

// ============================================================
// 系统集成 - 右键菜单 + URL scheme 注册
// ============================================================

export interface SystemStatus {
  installed: boolean;
  installed_path: string;
  url_scheme_installed: boolean;
}

// 查询系统注册状态
export async function fetchSystemStatus(): Promise<SystemStatus> {
  const r = await fetch("/internal/system-status", { cache: "no-store" });
  if (!r.ok) throw new Error(`system-status ${r.status}`);
  return r.json();
}

// 注册到系统 (注册右键菜单 + URL scheme)
export async function registerSystem(): Promise<void> {
  const r = await fetch("/internal/system-register", { method: "POST" });
  if (!r.ok) {
    const txt = await r.text();
    throw new Error(`system-register ${r.status}: ${txt}`);
  }
}

// 取消系统注册 (移除右键菜单 + URL scheme)
export async function unregisterSystem(): Promise<void> {
  const r = await fetch("/internal/system-unregister", { method: "POST" });
  if (!r.ok) {
    const txt = await r.text();
    throw new Error(`system-unregister ${r.status}: ${txt}`);
  }
}
