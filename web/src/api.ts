// /api/info 返回的 server 状态. 与 server.go ServerInfo 保持一致.
export interface ServerInfo {
  fileName: string;   // 当前发送文件名; 接收模式下空串
  fileSize: string;   // 人类可读大小; 接收模式下空串
  hasFile: boolean;   // 是否有可发送的文件
  receiving: boolean; // 是否在接收模式
}

export async function fetchInfo(): Promise<ServerInfo> {
  const r = await fetch("/api/info", { cache: "no-store" });
  if (!r.ok) throw new Error(`/api/info ${r.status}`);
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
