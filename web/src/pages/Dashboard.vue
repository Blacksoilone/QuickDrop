<script setup lang="ts">
// 电脑端发送 dashboard. webview 弹窗加载.
// 只渲 QR + 文件名 + 大小 + 关闭按钮 + 可折叠的 "发送到其他 PC" (ADR-17).
import { onMounted, onUnmounted, ref } from "vue";
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
  // 立刻拉一次, 然后每 3 秒刷新对端列表 (PC 上下线动态显示)
  refreshPeers();
  peerTimer = window.setInterval(refreshPeers, 3000);
});

onUnmounted(() => {
  if (peerTimer !== undefined) window.clearInterval(peerTimer);
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
    // 3 秒后状态回 idle (允许用户连发多次)
    window.setTimeout(() => {
      if (sendStatus.value === `sent:${r.to}`) sendStatus.value = "idle";
    }, 3000);
  } catch (e) {
    sendStatus.value = `fail:${String(e).slice(0, 60)}`;
    window.setTimeout(() => {
      if (sendStatus.value.startsWith("fail:")) sendStatus.value = "idle";
    }, 5000);
  }
}
</script>

<template>
  <button class="close" :title="`关闭 (daemon 继续运行)`" @click="closeWindow">×</button>
  <div class="wrap">
    <div class="qr"><img :src="qrUrl" alt="扫码下载" /></div>
    <template v-if="info && info.hasFile">
      <div class="name">{{ info.fileName }}</div>
      <div class="size">{{ info.fileSize }}</div>
    </template>
    <template v-else-if="error">
      <div class="err">无法加载文件信息: {{ error }}</div>
    </template>
    <template v-else>
      <div class="size">加载中...</div>
    </template>

    <details class="peers" v-if="info && info.hasFile">
      <summary>发送到其他 PC ▾</summary>
      <ul v-if="peers.length > 0">
        <li v-for="p in peers" :key="p.uuid">
          <button
            class="peer-btn"
            :disabled="sendStatus === 'sending'"
            @click="pickPeer(p)"
          >
            {{ p.name }}
          </button>
        </li>
      </ul>
      <div v-else class="empty">未发现局域网内其他 PC</div>
    </details>

    <div v-if="sendStatus !== 'idle'" class="status" :class="{ ok: sendStatus.startsWith('sent:'), fail: sendStatus.startsWith('fail:') }">
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
}
.wrap {
  width: 100%;
  min-height: 100vh;
  display: flex;
  flex-direction: column;
  align-items: center;
  padding: 12px 12px 14px;
  overflow-y: auto;
}
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
}
.close:hover {
  background: #e5e5e5;
  color: #333;
}

/* 可折叠 "发送到其他 PC" */
.peers {
  margin-top: 8px;
  width: 100%;
  font-size: 12px;
}
.peers summary {
  cursor: pointer;
  color: #0066ff;
  text-align: center;
  list-style: none;
  user-select: none;
  padding: 2px 0;
}
.peers summary::-webkit-details-marker {
  display: none;
}
.peers ul {
  list-style: none;
  margin: 4px 0 0;
  padding: 0;
}
.peers li {
  margin: 4px 0;
}
.peer-btn {
  width: 100%;
  padding: 6px 8px;
  background: #f0f4ff;
  border: 1px solid #cfdcf7;
  border-radius: 4px;
  font-size: 12px;
  cursor: pointer;
  text-align: left;
  color: #1a1a1a;
}
.peer-btn:hover:not(:disabled) {
  background: #e0eaff;
}
.peer-btn:disabled {
  opacity: 0.5;
  cursor: wait;
}
.empty {
  color: #999;
  text-align: center;
  padding: 6px 0 2px;
  font-size: 11px;
}

.status {
  margin-top: 8px;
  font-size: 11px;
  text-align: center;
  padding: 4px 8px;
  border-radius: 4px;
  max-width: 100%;
  word-break: break-word;
}
.status.ok {
  background: #e6f6e6;
  color: #2a7;
}
.status.fail {
  background: #fde4e4;
  color: #c33;
}

@media (prefers-color-scheme: dark) {
  .close:hover {
    background: #333;
    color: #fff;
  }
  .peers summary {
    color: #5a9cff;
  }
  .peer-btn {
    background: #1a2640;
    border-color: #2a3a5c;
    color: #eee;
  }
  .peer-btn:hover:not(:disabled) {
    background: #243454;
  }
  .empty {
    color: #777;
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
