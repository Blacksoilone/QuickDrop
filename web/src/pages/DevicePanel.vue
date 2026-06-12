<script setup lang="ts">
// 设备管理子组件 (ADR-20). 从 Devices.vue 拆出, 供 Config.vue 的"设备"面板复用.
// 纯展示逻辑 + trust 切换, 不含外层窗口装饰 (h1 / close).
import { onMounted, onUnmounted, ref } from "vue";
import { fetchDevices, setDeviceTrust, type DeviceEntry } from "../api";

const items = ref<DeviceEntry[]>([]);
const error = ref<string>("");
let timer: number | undefined;

onMounted(async () => {
  await refresh();
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
  <div class="device-panel">
    <p class="hint">
      <b>信任</b>: 来自此设备的文件自动接受 ·
      <b>黑名单</b>: 静默拒绝, 不通知 ·
      <b>每次询问</b>: 弹 toast 等你决策
    </p>

    <div v-if="error" class="err">无法加载: {{ error }}</div>

    <div v-if="items.length === 0 && !error" class="empty">
      还没和任何设备交互过<br />
      <span class="hint-inline">收到 / 发起一次 PC↔PC 传输后, 对方会出现在这里</span>
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
            title="恢复默认: 每次发文件来都弹 toast"
            @click="change(d, 'ask')"
          >每次问</button>
          <button
            class="btn trusted"
            :disabled="d.trust === 'trusted'"
            title="信任: 自动接受文件"
            @click="change(d, 'trusted')"
          >信任</button>
          <button
            class="btn blocked"
            :disabled="d.trust === 'blocked'"
            title="黑名单: 静默拒绝, 不通知"
            @click="change(d, 'blocked')"
          >黑名单</button>
        </div>
      </li>
    </ul>
  </div>
</template>

<style scoped>
.device-panel {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.hint {
  font-size: 12px;
  color: #888;
  line-height: 1.6;
  margin-bottom: 4px;
}
.hint b {
  color: #555;
}
.hint-inline {
  font-size: 11px;
  color: #888;
}
.empty {
  display: flex;
  flex-direction: column;
  justify-content: center;
  align-items: center;
  color: #999;
  font-size: 13px;
  text-align: center;
  line-height: 1.8;
  padding: 40px 0;
}
.err {
  color: #d33;
  font-size: 12px;
  margin-bottom: 8px;
  padding: 8px 12px;
  background: #fdecec;
  border-radius: 6px;
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
  border-radius: 8px;
  padding: 12px 14px;
  display: flex;
  align-items: center;
  gap: 12px;
  transition: border-color 0.15s ease, box-shadow 0.15s ease;
}
.item:hover {
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.06);
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
  margin-top: 3px;
}
.sub code {
  background: #f0f0f0;
  padding: 0 4px;
  border-radius: 3px;
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
  padding: 5px 11px;
  border: 1px solid #ccc;
  border-radius: 5px;
  font-size: 11px;
  cursor: pointer;
  background: #fff;
  color: #333;
  transition: background 0.12s ease, border-color 0.12s ease;
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
@media (prefers-color-scheme: dark) {
  .hint {
    color: #999;
  }
  .hint b {
    color: #ccc;
  }
  .hint-inline {
    color: #888;
  }
  .item {
    background: #262626;
    border-color: #333;
  }
  .item:hover {
    box-shadow: 0 2px 8px rgba(0, 0, 0, 0.4);
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
  .err {
    background: #3a1a1a;
    color: #f77;
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
}
</style>
