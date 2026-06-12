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
