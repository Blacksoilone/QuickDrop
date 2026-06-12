<script setup lang="ts">
// 电脑端发送 dashboard. webview 弹窗加载.
// 双视图互斥:
//   qr     默认. QR + 文件名 + 大小 + "发送到其他设备" 按钮
//   peers  点 "发送到其他设备" 切换. 顶部 ← 返回 + 文件名/大小, 列表占主区
// 设计原则: 保持 ADR-17 极简 (默认视图不变), PC 列表是显式动作触发,
// 不混在 QR 视图里.
import { onMounted, onUnmounted, ref, watch } from "vue";
import {
  closeWindow,
  fetchInfo,
  fetchPeers,
  sendToPeer,
  type Peer,
  type ServerInfo,
} from "../api";

// 字符串字面量绑定避开 Vite 把 "/qr" 当构建资源解析.
const qrUrl = "/qr";

const view = ref<"qr" | "peers">("qr");
const info = ref<ServerInfo | null>(null);
const peers = ref<Peer[]>([]);
const error = ref<string>("");
// 状态: idle | sending | sent:<name> | fail:<msg>
const sendStatus = ref<string>("idle");

let peerTimer: number | undefined;

onMounted(async () => {
  try {
    info.value = await fetchInfo();
  } catch (e) {
    error.value = String(e);
  }
});

onUnmounted(() => {
  stopPeerPolling();
});

function startPeerPolling() {
  // 立刻拉一次, 然后每 3 秒刷新 (PC 上下线动态)
  refreshPeers();
  if (peerTimer === undefined) {
    peerTimer = window.setInterval(refreshPeers, 3000);
  }
}

function stopPeerPolling() {
  if (peerTimer !== undefined) {
    window.clearInterval(peerTimer);
    peerTimer = undefined;
  }
}

// 视图切换时管理轮询: 只在 peers 视图轮询, 省 CPU
watch(view, (v) => {
  if (v === "peers") {
    startPeerPolling();
  } else {
    stopPeerPolling();
  }
});

async function refreshPeers() {
  try {
    peers.value = await fetchPeers();
  } catch {
    // 静默, 列表保持上次值
  }
}

async function pickPeer(p: Peer) {
  sendStatus.value = "sending";
  try {
    const r = await sendToPeer(p.uuid);
    sendStatus.value = `sent:${r.to}`;
    // 发送成功 3 秒后自动切回 QR 视图, status 也清掉
    window.setTimeout(() => {
      if (sendStatus.value === `sent:${r.to}`) {
        sendStatus.value = "idle";
        view.value = "qr";
      }
    }, 3000);
  } catch (e) {
    sendStatus.value = `fail:${String(e).slice(0, 80)}`;
    // 失败留 5 秒让用户看清原因, 不自动切回
    window.setTimeout(() => {
      if (sendStatus.value.startsWith("fail:")) sendStatus.value = "idle";
    }, 5000);
  }
}
</script>

<template>
  <button class="close" :title="`关闭 (daemon 继续运行)`" @click="closeWindow">×</button>

  <!-- QR 视图 (默认) -->
  <div v-if="view === 'qr'" class="wrap">
    <div class="qr"><img :src="qrUrl" alt="扫码下载" /></div>
    <template v-if="info && info.hasFile">
      <div class="name">{{ info.fileName }}</div>
      <div class="size">{{ info.fileSize }}</div>
    </template>
    <template v-else-if="error">
      <div class="err">无法加载: {{ error }}</div>
    </template>
    <template v-else>
      <div class="size">加载中...</div>
    </template>

    <button
      v-if="info && info.hasFile"
      class="switch-btn"
      @click="view = 'peers'"
      title="发送给同 WiFi 装了 QuickDrop 的电脑"
    >
      发送到其他设备 →
    </button>
  </div>

  <!-- PC 列表视图 -->
  <div v-else class="wrap">
    <div class="header">
      <button class="back" @click="view = 'qr'" title="返回 QR 视图">←</button>
      <div class="header-meta">
        <div class="name">{{ info?.fileName }}</div>
        <div class="size">{{ info?.fileSize }}</div>
      </div>
    </div>

    <ul v-if="peers.length > 0" class="peer-list">
      <li v-for="p in peers" :key="p.uuid">
        <button
          class="peer-btn"
          :disabled="sendStatus === 'sending'"
          @click="pickPeer(p)"
        >
          <span class="peer-icon">💻</span>
          <span class="peer-name">{{ p.name }}</span>
        </button>
      </li>
    </ul>
    <div v-else class="empty">
      未发现局域网内其他设备<br />
      <span class="hint">对方需装 QuickDrop 且在同 WiFi</span>
    </div>

    <div
      v-if="sendStatus !== 'idle'"
      class="status"
      :class="{
        ok: sendStatus.startsWith('sent:'),
        fail: sendStatus.startsWith('fail:'),
      }"
    >
      <span v-if="sendStatus === 'sending'">发送中...</span>
      <span v-else-if="sendStatus.startsWith('sent:')">已发给 {{ sendStatus.slice(5) }}</span>
      <span v-else-if="sendStatus.startsWith('fail:')">失败: {{ sendStatus.slice(5) }}</span>
    </div>
  </div>
</template>

<style scoped>
html,
body,
:global(html),
:global(body) {
  width: 100%;
  height: 100%;
  overflow: hidden;
}
.wrap {
  width: 100%;
  height: 100vh;
  display: flex;
  flex-direction: column;
  align-items: center;
  padding: 12px 12px 14px;
}

/* --- QR 视图 --- */
.qr {
  width: 240px;
  height: 240px;
  background: #fff;
  padding: 6px;
  border-radius: 6px;
  flex: 0 0 auto;
}
.qr img {
  width: 100%;
  height: 100%;
  display: block;
}
.name {
  margin-top: 10px;
  font-size: 13px;
  font-weight: 600;
  max-width: 100%;
  text-align: center;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.size {
  margin-top: 2px;
  font-size: 11px;
  color: #888;
}
.err {
  margin-top: 8px;
  font-size: 12px;
  color: #d33;
  text-align: center;
}
.switch-btn {
  margin-top: auto;
  padding: 6px 12px;
  background: transparent;
  border: 1px solid #cfdcf7;
  border-radius: 4px;
  font-size: 12px;
  color: #0066ff;
  cursor: pointer;
}
.switch-btn:hover {
  background: #f0f4ff;
}

/* --- PC 列表视图 --- */
.header {
  width: 100%;
  display: flex;
  align-items: center;
  gap: 8px;
  padding-bottom: 8px;
  border-bottom: 1px solid #eee;
  margin-bottom: 8px;
  flex: 0 0 auto;
}
.back {
  width: 28px;
  height: 28px;
  font-size: 16px;
  background: transparent;
  border: 0;
  color: #555;
  cursor: pointer;
  border-radius: 4px;
  flex: 0 0 auto;
}
.back:hover {
  background: #eee;
  color: #000;
}
.header-meta {
  flex: 1 1 auto;
  min-width: 0; /* allow text-overflow */
  text-align: left;
}
.header-meta .name {
  margin: 0;
  font-size: 12px;
}
.header-meta .size {
  margin: 0;
  font-size: 10px;
}
.peer-list {
  list-style: none;
  margin: 0;
  padding: 0;
  width: 100%;
  flex: 1 1 auto;
  overflow-y: auto;
}
.peer-list li {
  margin: 4px 0;
}
.peer-btn {
  width: 100%;
  padding: 8px 10px;
  background: #f8f9fa;
  border: 1px solid #e5e5e5;
  border-radius: 4px;
  font-size: 13px;
  cursor: pointer;
  text-align: left;
  color: #1a1a1a;
  display: flex;
  align-items: center;
  gap: 8px;
}
.peer-btn:hover:not(:disabled) {
  background: #e0eaff;
  border-color: #cfdcf7;
}
.peer-btn:disabled {
  opacity: 0.5;
  cursor: wait;
}
.peer-icon {
  font-size: 14px;
}
.peer-name {
  flex: 1;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.empty {
  flex: 1 1 auto;
  display: flex;
  flex-direction: column;
  justify-content: center;
  align-items: center;
  color: #999;
  font-size: 12px;
  text-align: center;
  line-height: 1.6;
}
.empty .hint {
  font-size: 10px;
  color: #bbb;
}

/* --- 共享状态条 --- */
.status {
  margin-top: 6px;
  font-size: 11px;
  text-align: center;
  padding: 4px 8px;
  border-radius: 4px;
  max-width: 100%;
  word-break: break-word;
  flex: 0 0 auto;
}
.status.ok {
  background: #e6f6e6;
  color: #2a7;
}
.status.fail {
  background: #fde4e4;
  color: #c33;
}

/* --- 关闭按钮 --- */
.close {
  position: absolute;
  top: 4px;
  right: 6px;
  width: 22px;
  height: 22px;
  line-height: 20px;
  text-align: center;
  border: 0;
  background: transparent;
  cursor: pointer;
  font-size: 16px;
  color: #888;
  border-radius: 4px;
  z-index: 10;
}
.close:hover {
  background: #e5e5e5;
  color: #333;
}

@media (prefers-color-scheme: dark) {
  .close:hover {
    background: #333;
    color: #fff;
  }
  .switch-btn {
    color: #5a9cff;
    border-color: #2a3a5c;
  }
  .switch-btn:hover {
    background: #1a2640;
  }
  .header {
    border-color: #333;
  }
  .back:hover {
    background: #333;
    color: #fff;
  }
  .peer-btn {
    background: #262626;
    border-color: #333;
    color: #eee;
  }
  .peer-btn:hover:not(:disabled) {
    background: #2a3a5c;
    border-color: #3a4a6c;
  }
  .empty {
    color: #777;
  }
  .empty .hint {
    color: #555;
  }
  .status.ok {
    background: #1d3a1d;
    color: #7c5;
  }
  .status.fail {
    background: #3a1d1d;
    color: #f77;
  }
}
</style>
