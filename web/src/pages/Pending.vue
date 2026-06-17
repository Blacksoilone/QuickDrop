<script setup lang="ts">
// 待处理 incoming 列表页 (托盘 "待处理 (N)" 点开走这里).
// 列出 pending 队列, 每条可接受/拒绝. 接受后后台服务异步 Pull 文件.
// 同时订阅 /ws 进度事件, 给"已接受"的条目显示实时进度条.
import { onMounted, onUnmounted, ref } from "vue";
import {
  closeWindow,
  decidePeer,
  fetchPending,
  subscribeProgress,
  type PendingEntry,
  type ProgressEvent,
} from "../api";

const items = ref<PendingEntry[]>([]);
const error = ref<string>("");
// token → 是否勾选了"信任此设备" (Accept 同时设 trusted, Reject 同时设 blocked)
const trustOnAccept = ref<Record<string, boolean>>({});
// token → 当前进度事件 (来自 /ws)
const progress = ref<Record<string, ProgressEvent>>({});
let timer: number | undefined;
let unsubWS: (() => void) | undefined;

onMounted(async () => {
  await refresh();
  // 每 2 秒刷新, 让状态变化 (accepted/rejected) 反映出来
  timer = window.setInterval(refresh, 2000);
  // 订阅进度
  unsubWS = subscribeProgress((e) => {
    progress.value[e.id] = e;
    // done 后保留 5 秒让用户看清最终状态再清掉
    if (e.done) {
      window.setTimeout(() => {
        delete progress.value[e.id];
      }, 5000);
    }
  });
});

onUnmounted(() => {
  if (timer !== undefined) window.clearInterval(timer);
  if (unsubWS) unsubWS();
});

async function refresh() {
  try {
    items.value = await fetchPending();
    error.value = "";
  } catch (e) {
    error.value = String(e);
  }
}

async function accept(p: PendingEntry, trustChoice: "" | "trusted") {
  try {
    await decidePeer(p.token, "accept", trustChoice || undefined);
    await refresh();
  } catch (e) {
    error.value = String(e);
  }
}

async function reject(p: PendingEntry, trustChoice: "" | "blocked") {
  try {
    await decidePeer(p.token, "reject", trustChoice || undefined);
    await refresh();
  } catch (e) {
    error.value = String(e);
  }
}

function humanSize(n: number): string {
  if (n < 1024) return `${n} B`;
  const units = ["KB", "MB", "GB", "TB"];
  let i = 0;
  let v = n / 1024;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v.toFixed(1)} ${units[i]}`;
}

function stateLabel(s: PendingEntry["state"]): string {
  switch (s) {
    case "pending":
      return "等待你决策";
    case "accepted":
      return "已接受 (正在下载)";
    case "rejected":
      return "已拒绝";
    case "expired":
      return "已超时";
  }
}

function percent(e: ProgressEvent): number {
  if (!e || e.fileSize <= 0) return 0;
  return Math.min(100, Math.floor((e.bytes / e.fileSize) * 100));
}
</script>

<template>
  <button class="close" title="关闭窗口 (后台服务继续运行)" @click="closeWindow">×</button>
  <div class="wrap">
    <h1>待处理文件传入</h1>

    <div v-if="error" class="err">无法加载: {{ error }}</div>

    <div v-if="items.length === 0 && !error" class="empty">
      当前没有待处理的传入<br />
      <span class="hint">收到新邀请时会弹 toast 通知, 也会自动出现在这里</span>
    </div>

    <ul class="list">
      <li v-for="p in items" :key="p.token" :class="['item', `state-${p.state}`]">
        <div class="meta">
          <div class="name">{{ p.fileName }}</div>
          <div class="sub">
            来自 <b>{{ p.from.name }}</b> · {{ humanSize(p.fileSize) }} ·
            <span class="state">{{ stateLabel(p.state) }}</span>
          </div>
          <!-- 进度条: state=accepted 且 ws 有最新事件时显示 -->
          <div v-if="progress[p.token] && !progress[p.token].done" class="progress">
            <div class="bar">
              <div class="fill" :style="{ width: percent(progress[p.token]) + '%' }"></div>
            </div>
            <div class="pct">{{ percent(progress[p.token]) }}% · {{ humanSize(progress[p.token].bytes) }} / {{ humanSize(p.fileSize) }}</div>
          </div>
          <div v-else-if="progress[p.token] && progress[p.token].done && !progress[p.token].err" class="progress done">
            ✓ 完成
          </div>
        </div>
        <div class="actions" v-if="p.state === 'pending'">
          <label class="trust-chk" title="勾上后, 此设备以后发文件自动接受 (可在设备管理页撤回)">
            <input
              type="checkbox"
              v-model="trustOnAccept[p.token]"
            />
            <span>信任此设备</span>
          </label>
          <button
            class="btn accept"
            @click="accept(p, trustOnAccept[p.token] ? 'trusted' : '')"
          >接受</button>
          <button
            class="btn reject"
            @click="reject(p, trustOnAccept[p.token] ? 'blocked' : '')"
            :title="trustOnAccept[p.token] ? '拒绝并永不信任此设备 (黑名单)' : '只拒绝本次'"
          >拒绝</button>
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
  margin-bottom: 12px;
  color: #444;
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
.empty .hint {
  font-size: 11px;
  color: #bbb;
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
  border-left: 3px solid #0066ff;
  border-radius: 4px;
  padding: 10px 12px;
  display: flex;
  align-items: center;
  gap: 12px;
}
.item.state-accepted {
  border-left-color: #2a7;
  opacity: 0.7;
}
.item.state-rejected,
.item.state-expired {
  border-left-color: #999;
  opacity: 0.5;
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
.sub b {
  color: #555;
  font-weight: 600;
}
.state {
  font-weight: 500;
}
.progress {
  margin-top: 6px;
  font-size: 11px;
  color: #555;
}
.progress.done {
  color: #2a7;
  font-weight: 500;
}
.progress .bar {
  width: 100%;
  height: 4px;
  background: #eee;
  border-radius: 2px;
  overflow: hidden;
  margin-bottom: 2px;
}
.progress .fill {
  height: 100%;
  background: #0066ff;
  transition: width 200ms linear;
}
.progress .pct {
  font-size: 10px;
  color: #888;
}
@media (prefers-color-scheme: dark) {
  .progress { color: #aaa; }
  .progress .bar { background: #333; }
  .progress .pct { color: #999; }
  .progress.done { color: #7c5; }
}

.actions {
  display: flex;
  gap: 6px;
  flex: 0 0 auto;
  align-items: center;
}
.trust-chk {
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: 11px;
  color: #666;
  cursor: pointer;
  user-select: none;
  margin-right: 4px;
}
.trust-chk input {
  cursor: pointer;
}
.btn {
  padding: 4px 12px;
  border: 0;
  border-radius: 3px;
  font-size: 12px;
  cursor: pointer;
  color: #fff;
}
.btn.accept {
  background: #0066ff;
}
.btn.accept:hover {
  background: #0055d4;
}
.btn.reject {
  background: #c33;
}
.btn.reject:hover {
  background: #a22;
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
  .item {
    background: #262626;
    border-color: #333;
  }
  .sub {
    color: #999;
  }
  .sub b {
    color: #ccc;
  }
  .empty {
    color: #777;
  }
  .empty .hint {
    color: #555;
  }
  .close:hover {
    background: #333;
    color: #fff;
  }
}
</style>
