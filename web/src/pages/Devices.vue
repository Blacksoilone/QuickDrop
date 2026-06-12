<script setup lang="ts">
// 设备管理页 (ADR-20). 列出所有已见过的设备 + 用户可设/撤 trust/block.
// 由托盘"设备管理"菜单 (后续) 或手动浏览器 /v 打开. 单实例.
import { onMounted, onUnmounted, ref } from "vue";
import {
  closeWindow,
  fetchDevices,
  setDeviceTrust,
  type DeviceEntry,
} from "../api";

const items = ref<DeviceEntry[]>([]);
const error = ref<string>("");
let timer: number | undefined;

onMounted(async () => {
  await refresh();
  // 3 秒刷新, 反映 daemon 端 UpsertSeen 更新的 lastSeen
  timer = window.setInterval(refresh, 3000);
});

onUnmounted(() => {
  if (timer !== undefined) window.clearInterval(timer);
});

async function refresh() {
  try {
    items.value = await fetchDevices();
    error.value = "";
  } catch (e) {
    error.value = String(e);
  }
}

async function change(d: DeviceEntry, trust: "ask" | "trusted" | "blocked") {
  if (d.trust === trust) return;
  try {
    await setDeviceTrust(d.uuid, d.name, trust);
    await refresh();
  } catch (e) {
    error.value = String(e);
  }
}

function trustLabel(t: DeviceEntry["trust"]): string {
  switch (t) {
    case "ask":
      return "每次询问";
    case "trusted":
      return "信任";
    case "blocked":
      return "黑名单";
  }
}

function timeAgo(unixSec: number): string {
  if (unixSec === 0) return "未见过";
  const diff = Math.floor(Date.now() / 1000) - unixSec;
  if (diff < 60) return `${diff}秒前`;
  if (diff < 3600) return `${Math.floor(diff / 60)}分钟前`;
  if (diff < 86400) return `${Math.floor(diff / 3600)}小时前`;
  return `${Math.floor(diff / 86400)}天前`;
}
</script>

<template>
  <button class="close" title="关闭窗口 (daemon 继续运行)" @click="closeWindow">×</button>
  <div class="wrap">
    <h1>设备管理</h1>
    <p class="hint">
      <b>信任</b>: 来自此设备的文件自动接受 ·
      <b>黑名单</b>: 静默拒绝, 不通知 ·
      <b>每次询问</b>: 弹 toast 等你决策
    </p>

    <div v-if="error" class="err">无法加载: {{ error }}</div>

    <div v-if="items.length === 0 && !error" class="empty">
      还没和任何设备交互过<br />
      <span class="hint">收到 / 发起一次 PC↔PC 传输后, 对方会出现在这里</span>
    </div>

    <ul class="list">
      <li v-for="d in items" :key="d.uuid" :class="['item', `trust-${d.trust}`]">
        <div class="meta">
          <div class="name">{{ d.name || "(无名设备)" }}</div>
          <div class="sub">
            <code>{{ d.uuid.slice(0, 12) }}…</code> ·
            上次出现: {{ timeAgo(d.lastSeen) }} ·
            当前: <b>{{ trustLabel(d.trust) }}</b>
          </div>
        </div>
        <div class="actions">
          <button
            class="btn ask"
            :disabled="d.trust === 'ask'"
            @click="change(d, 'ask')"
            title="恢复默认: 每次发文件来都弹 toast"
          >每次问</button>
          <button
            class="btn trusted"
            :disabled="d.trust === 'trusted'"
            @click="change(d, 'trusted')"
            title="信任: 自动接受文件"
          >信任</button>
          <button
            class="btn blocked"
            :disabled="d.trust === 'blocked'"
            @click="change(d, 'blocked')"
            title="黑名单: 静默拒绝, 不通知"
          >黑名单</button>
        </div>
      </li>
    </ul>
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
  padding: 14px 16px;
  display: flex;
  flex-direction: column;
}
h1 {
  font-size: 14px;
  font-weight: 600;
  color: #444;
  margin-bottom: 4px;
}
.hint {
  font-size: 11px;
  color: #888;
  margin-bottom: 12px;
  line-height: 1.6;
}
.hint b {
  color: #555;
}
.empty {
  flex: 1 1 auto;
  display: flex;
  flex-direction: column;
  justify-content: center;
  align-items: center;
  color: #999;
  font-size: 13px;
  text-align: center;
  line-height: 1.8;
}
.err {
  color: #d33;
  font-size: 12px;
  text-align: center;
  margin-bottom: 8px;
}
.list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.item {
  background: #fff;
  border: 1px solid #e5e5e5;
  border-left: 3px solid #888;
  border-radius: 4px;
  padding: 10px 12px;
  display: flex;
  align-items: center;
  gap: 12px;
}
.item.trust-trusted {
  border-left-color: #2a7;
}
.item.trust-blocked {
  border-left-color: #c33;
  opacity: 0.7;
}
.meta {
  flex: 1 1 auto;
  min-width: 0;
}
.name {
  font-size: 13px;
  font-weight: 600;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.sub {
  font-size: 11px;
  color: #888;
  margin-top: 2px;
}
.sub code {
  background: #f0f0f0;
  padding: 0 4px;
  border-radius: 2px;
  font-family: ui-monospace, "SF Mono", monospace;
}
.sub b {
  color: #555;
}
.actions {
  display: flex;
  gap: 4px;
  flex: 0 0 auto;
}
.btn {
  padding: 4px 10px;
  border: 1px solid #ccc;
  border-radius: 3px;
  font-size: 11px;
  cursor: pointer;
  background: #fff;
  color: #333;
}
.btn:hover:not(:disabled) {
  background: #f0f0f0;
}
.btn:disabled {
  background: #eef4ff;
  border-color: #88aaee;
  color: #444;
  cursor: default;
}
.btn.trusted:disabled {
  background: #e6f6e6;
  border-color: #2a7;
  color: #2a7;
}
.btn.blocked:disabled {
  background: #fde4e4;
  border-color: #c33;
  color: #c33;
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
@media (prefers-color-scheme: dark) {
  h1 {
    color: #ccc;
  }
  .hint {
    color: #999;
  }
  .hint b {
    color: #ccc;
  }
  .item {
    background: #262626;
    border-color: #333;
  }
  .sub {
    color: #999;
  }
  .sub code {
    background: #1a1a1a;
  }
  .sub b {
    color: #ccc;
  }
  .empty {
    color: #777;
  }
  .btn {
    background: #262626;
    border-color: #444;
    color: #ccc;
  }
  .btn:hover:not(:disabled) {
    background: #333;
  }
  .btn:disabled {
    background: #1a2640;
    border-color: #2a3a5c;
    color: #ccc;
  }
  .btn.trusted:disabled {
    background: #1d3a1d;
    border-color: #2a7;
    color: #7c5;
  }
  .btn.blocked:disabled {
    background: #3a1d1d;
    border-color: #c33;
    color: #f77;
  }
  .close:hover {
    background: #333;
    color: #fff;
  }
}
</style>
