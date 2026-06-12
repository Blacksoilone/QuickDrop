<script setup lang="ts">
// QuickDrop 配置中心 (/c). v0.11.0 关键 UI.
// 左侧导航 + 右侧表单, hash 路由, 脏检测 + 保存/放弃.
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import {
  closeWindow,
  fetchConfig,
  saveConfig,
  type AppConfig,
  type ConflictPolicy,
} from "../api";
import DevicePanel from "./DevicePanel.vue";

type SectionKey =
  | "receive"
  | "send"
  | "notify"
  | "network"
  | "devices"
  | "system"
  | "about";

interface Section {
  key: SectionKey;
  label: string;
  icon: string; // unicode 字符
}

const SECTIONS: Section[] = [
  { key: "receive", label: "接收", icon: "↓" },
  { key: "send", label: "发送", icon: "↑" },
  { key: "notify", label: "通知", icon: "♪" },
  { key: "network", label: "网络", icon: "◉" },
  { key: "devices", label: "设备", icon: "▤" },
  { key: "system", label: "系统", icon: "⚙" },
  { key: "about", label: "关于", icon: "?" },
];

const VERSION = "v0.11.0";
const CONFIG_PATH = "~/.quickdrop/config.json";
const MB = 1024 * 1024;

const active = ref<SectionKey>("receive");
// 上次从 daemon 拉到的原始 cfg (JSON 序列化串, 用来比 dirty)
const baseline = ref<string>("");
// 工作中的可编辑副本
const cfg = ref<AppConfig | null>(null);
// max_file_size 在 UI 上以 MB 显示, 单独的双向绑定字段
const maxFileSizeMB = ref<number>(0);

const loadError = ref<string>("");
const saveError = ref<string>("");
const saved = ref<boolean>(false);
const saving = ref<boolean>(false);

let savedTimer: number | undefined;

const dirty = computed<boolean>(() => {
  if (!cfg.value) return false;
  return JSON.stringify(cfg.value) !== baseline.value;
});

function parseHash(): SectionKey {
  const h = (location.hash || "").replace(/^#/, "");
  const found = SECTIONS.find((s) => s.key === h);
  return found ? found.key : "receive";
}

function syncHash() {
  if (location.hash.replace(/^#/, "") !== active.value) {
    history.replaceState(null, "", `#${active.value}`);
  }
}

function onHashChange() {
  active.value = parseHash();
}

function selectSection(k: SectionKey) {
  active.value = k;
}

watch(active, () => {
  syncHash();
});

onMounted(async () => {
  active.value = parseHash();
  window.addEventListener("hashchange", onHashChange);
  await reload();
});

onUnmounted(() => {
  window.removeEventListener("hashchange", onHashChange);
  if (savedTimer !== undefined) window.clearTimeout(savedTimer);
});

async function reload() {
  loadError.value = "";
  saveError.value = "";
  try {
    const c = await fetchConfig();
    cfg.value = c;
    baseline.value = JSON.stringify(c);
    maxFileSizeMB.value = Math.round(c.receive.max_file_size / MB);
  } catch (e) {
    loadError.value = String(e);
  }
}

function discard() {
  void reload();
}

async function save() {
  if (!cfg.value) return;
  saving.value = true;
  saveError.value = "";
  // 同步 MB → 字节
  cfg.value.receive.max_file_size = Math.max(0, Math.floor(maxFileSizeMB.value)) * MB;
  try {
    await saveConfig(cfg.value);
    baseline.value = JSON.stringify(cfg.value);
    saved.value = true;
    if (savedTimer !== undefined) window.clearTimeout(savedTimer);
    savedTimer = window.setTimeout(() => {
      saved.value = false;
    }, 2000);
  } catch (e) {
    saveError.value = String(e);
  } finally {
    saving.value = false;
  }
}

function setConflict(p: ConflictPolicy) {
  if (cfg.value) cfg.value.download.conflict = p;
}
</script>

<template>
  <div class="shell">
    <header class="titlebar">
      <div class="title">
        <span class="dot" />
        QuickDrop 设置
      </div>
      <button class="close" title="关闭窗口 (daemon 继续运行)" @click="closeWindow">
        ×
      </button>
    </header>

    <div v-if="loadError" class="err global">无法加载配置: {{ loadError }}</div>

    <transition name="slide">
      <div v-if="dirty && !saved" class="dirty-bar">
        <span class="dirty-text">未保存的修改</span>
        <div class="dirty-actions">
          <button class="btn ghost" :disabled="saving" @click="discard">放弃</button>
          <button class="btn primary" :disabled="saving" @click="save">
            {{ saving ? "保存中…" : "保存" }}
          </button>
        </div>
      </div>
    </transition>

    <transition name="fade">
      <div v-if="saved" class="toast saved">已保存</div>
    </transition>

    <transition name="fade">
      <div v-if="saveError" class="toast err-toast">{{ saveError }}</div>
    </transition>

    <div class="body">
      <aside class="nav">
        <button
          v-for="s in SECTIONS"
          :key="s.key"
          :class="['nav-item', { active: active === s.key }]"
          @click="selectSection(s.key)"
        >
          <span class="nav-icon">{{ s.icon }}</span>
          <span class="nav-label">{{ s.label }}</span>
        </button>
      </aside>

      <main class="panel">
        <template v-if="cfg">
          <!-- 接收 -->
          <section v-if="active === 'receive'" class="card">
            <h2>接收</h2>
            <div class="row">
              <div class="row-label">
                <div class="row-title">保存目录</div>
                <div class="row-desc">收到的文件落到哪里</div>
              </div>
              <div class="row-control wide">
                <input
                  v-model="cfg.download.dir"
                  type="text"
                  class="text"
                  placeholder="留空表示使用默认: ~/Downloads/QuickDrop/"
                />
              </div>
            </div>
            <div class="row">
              <div class="row-label">
                <div class="row-title">同名冲突</div>
                <div class="row-desc">已经存在同名文件时怎么处理</div>
              </div>
              <div class="row-control radios">
                <label class="radio">
                  <input
                    type="radio"
                    name="conflict"
                    :checked="cfg.download.conflict === 'rename'"
                    @change="setConflict('rename')"
                  />
                  <span>
                    <b>重命名</b>
                    <small>追加 (1), (2) 后缀</small>
                  </span>
                </label>
                <label class="radio">
                  <input
                    type="radio"
                    name="conflict"
                    :checked="cfg.download.conflict === 'overwrite'"
                    @change="setConflict('overwrite')"
                  />
                  <span>
                    <b>覆盖</b>
                    <small>直接替换原文件</small>
                  </span>
                </label>
                <label class="radio">
                  <input
                    type="radio"
                    name="conflict"
                    :checked="cfg.download.conflict === 'reject'"
                    @change="setConflict('reject')"
                  />
                  <span>
                    <b>拒绝</b>
                    <small>报错, 不写入</small>
                  </span>
                </label>
              </div>
            </div>
            <div class="row">
              <div class="row-label">
                <div class="row-title">单文件大小上限</div>
                <div class="row-desc">超过此值的入站文件会被拒绝</div>
              </div>
              <div class="row-control">
                <input
                  v-model.number="maxFileSizeMB"
                  type="number"
                  min="0"
                  class="num"
                />
                <span class="unit">MB</span>
                <span class="hint-after">0 表示不限制</span>
              </div>
            </div>
          </section>

          <!-- 发送 -->
          <section v-if="active === 'send'" class="card">
            <h2>发送</h2>
            <div class="empty-panel">
              该面板尚无配置项, 后续版本会加入
            </div>
          </section>

          <!-- 通知 -->
          <section v-if="active === 'notify'" class="card">
            <h2>通知</h2>
            <div class="row">
              <div class="row-label">
                <div class="row-title">入站 Toast</div>
                <div class="row-desc">收到文件 / 请求时弹出系统通知</div>
              </div>
              <div class="row-control">
                <label class="toggle">
                  <input v-model="cfg.ui.toasts_enabled" type="checkbox" />
                  <span class="track" />
                </label>
              </div>
            </div>
            <div class="row">
              <div class="row-label">
                <div class="row-title">完成后在文件管理器中显示</div>
                <div class="row-desc">下载完成后自动打开所在文件夹</div>
              </div>
              <div class="row-control">
                <label class="toggle">
                  <input v-model="cfg.ui.reveal_on_done" type="checkbox" />
                  <span class="track" />
                </label>
              </div>
            </div>
          </section>

          <!-- 网络 -->
          <section v-if="active === 'network'" class="card">
            <h2>网络</h2>
            <div class="row">
              <div class="row-label">
                <div class="row-title">服务端口</div>
                <div class="row-desc">daemon HTTP 监听端口</div>
              </div>
              <div class="row-control">
                <input
                  v-model.number="cfg.server.port"
                  type="number"
                  min="1"
                  max="65535"
                  class="num"
                />
                <span class="hint-after">修改后需重启 daemon 生效</span>
              </div>
            </div>
            <div class="row">
              <div class="row-label">
                <div class="row-title">mDNS 广播</div>
                <div class="row-desc">向局域网公告自己</div>
              </div>
              <div class="row-control toggle-row">
                <label class="toggle">
                  <input v-model="cfg.server.mdns_enabled" type="checkbox" />
                  <span class="track" />
                </label>
                <span v-if="!cfg.server.mdns_enabled" class="hint-after stealth">
                  隐身模式: 其他设备无法主动发现你, 只能用扫码
                </span>
              </div>
            </div>
          </section>

          <!-- 设备 -->
          <section v-if="active === 'devices'" class="card">
            <h2>设备</h2>
            <DevicePanel />
          </section>

          <!-- 系统 -->
          <section v-if="active === 'system'" class="card">
            <h2>系统</h2>
            <div class="row">
              <div class="row-label">
                <div class="row-title">开机自启</div>
                <div class="row-desc">Windows 登录时自动启动接收守护</div>
              </div>
              <div class="row-control">
                <label class="toggle">
                  <input v-model="cfg.system.autostart" type="checkbox" />
                  <span class="track" />
                </label>
              </div>
            </div>
          </section>

          <!-- 关于 -->
          <section v-if="active === 'about'" class="card about">
            <h2>关于</h2>
            <div class="about-block">
              <div class="brand">QuickDrop</div>
              <div class="version">{{ VERSION }}</div>
            </div>
            <div class="about-row">
              <div class="about-key">配置文件</div>
              <code class="about-val">{{ CONFIG_PATH }}</code>
            </div>
            <p class="about-text">
              局域网点对点文件传输, 单二进制, 零依赖.<br />
              所有配置实时落盘到上方路径, 可直接编辑 JSON 后重启 daemon.
            </p>
          </section>
        </template>
      </main>
    </div>
  </div>
</template>

<style scoped>
.shell {
  /* 主题 token (本组件作用域) */
  --c-bg: #f5f5f7;
  --c-panel: #ffffff;
  --c-border: #e1e1e4;
  --c-border-strong: #cdcdd2;
  --c-text: #1d1d1f;
  --c-text-mute: #6e6e73;
  --c-text-soft: #8a8a8f;
  --c-primary: #0066ff;
  --c-primary-soft: rgba(0, 102, 255, 0.1);
  --c-danger: #d33;
  --c-danger-bg: #fdecec;
  --c-success: #1d9d52;
  --c-success-bg: #e3f6ea;
  --c-warning: #b8860b;
  --c-warning-bg: #fff4d6;
  --r-sm: 6px;
  --r-md: 8px;
  --r-lg: 12px;
  --s-1: 4px;
  --s-2: 8px;
  --s-3: 12px;
  --s-4: 16px;
  --s-5: 20px;
  --s-6: 24px;

  width: 100%;
  min-height: 100vh;
  background: var(--c-bg);
  color: var(--c-text);
  display: flex;
  flex-direction: column;
  position: relative;
}

/* 标题栏 */
.titlebar {
  height: 44px;
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 var(--s-4);
  border-bottom: 1px solid var(--c-border);
  background: var(--c-panel);
}
.title {
  font-size: 13px;
  font-weight: 600;
  display: flex;
  align-items: center;
  gap: var(--s-2);
  color: var(--c-text);
}
.dot {
  width: 10px;
  height: 10px;
  border-radius: 50%;
  background: var(--c-primary);
  box-shadow: 0 0 0 3px var(--c-primary-soft);
}
.close {
  width: 26px;
  height: 26px;
  line-height: 24px;
  text-align: center;
  border: 0;
  background: transparent;
  cursor: pointer;
  font-size: 18px;
  color: var(--c-text-mute);
  border-radius: var(--r-sm);
  transition: background 0.12s ease, color 0.12s ease;
}
.close:hover {
  background: var(--c-border);
  color: var(--c-text);
}

/* 顶部全局错误 */
.err.global {
  background: var(--c-danger-bg);
  color: var(--c-danger);
  font-size: 12px;
  padding: var(--s-2) var(--s-4);
}

/* 脏数据条 */
.dirty-bar {
  flex: 0 0 auto;
  height: 44px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 var(--s-4);
  background: var(--c-warning-bg);
  border-bottom: 1px solid rgba(184, 134, 11, 0.25);
}
.dirty-text {
  font-size: 12px;
  font-weight: 500;
  color: var(--c-warning);
}
.dirty-actions {
  display: flex;
  gap: var(--s-2);
}

/* 浮动 toast */
.toast {
  position: absolute;
  top: 54px;
  right: var(--s-4);
  padding: 8px 14px;
  border-radius: var(--r-md);
  font-size: 12px;
  font-weight: 500;
  box-shadow: 0 4px 14px rgba(0, 0, 0, 0.1);
  z-index: 5;
}
.toast.saved {
  background: var(--c-success-bg);
  color: var(--c-success);
}
.toast.err-toast {
  background: var(--c-danger-bg);
  color: var(--c-danger);
  max-width: 60%;
  word-break: break-word;
}

/* body 左右分栏 */
.body {
  flex: 1 1 auto;
  display: flex;
  min-height: 0;
}
.nav {
  width: 200px;
  flex: 0 0 200px;
  padding: var(--s-3) var(--s-2);
  background: var(--c-panel);
  border-right: 1px solid var(--c-border);
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.nav-item {
  display: flex;
  align-items: center;
  gap: var(--s-3);
  padding: 9px 12px;
  border: 0;
  background: transparent;
  cursor: pointer;
  border-radius: var(--r-md);
  font-size: 13px;
  color: var(--c-text);
  text-align: left;
  transition: background 0.12s ease, color 0.12s ease;
}
.nav-item:hover {
  background: var(--c-bg);
}
.nav-item.active {
  background: var(--c-primary);
  color: #fff;
}
.nav-icon {
  display: inline-flex;
  width: 22px;
  height: 22px;
  align-items: center;
  justify-content: center;
  border-radius: var(--r-sm);
  background: var(--c-bg);
  color: var(--c-text-mute);
  font-size: 13px;
  font-weight: 600;
  flex: 0 0 auto;
  transition: background 0.12s ease, color 0.12s ease;
}
.nav-item.active .nav-icon {
  background: rgba(255, 255, 255, 0.22);
  color: #fff;
}
.nav-label {
  flex: 1 1 auto;
}

/* 右侧面板 */
.panel {
  flex: 1 1 auto;
  padding: var(--s-6);
  overflow-y: auto;
  min-width: 0;
}
.card {
  background: var(--c-panel);
  border: 1px solid var(--c-border);
  border-radius: var(--r-lg);
  padding: var(--s-5) var(--s-5) var(--s-3);
  display: flex;
  flex-direction: column;
}
h2 {
  font-size: 15px;
  font-weight: 600;
  letter-spacing: 0.01em;
  margin-bottom: var(--s-3);
  color: var(--c-text);
}

/* 行 */
.row {
  display: flex;
  align-items: center;
  gap: var(--s-4);
  padding: var(--s-3) 0;
  border-top: 1px solid var(--c-border);
}
.row:first-of-type {
  border-top: 0;
}
.row-label {
  flex: 1 1 auto;
  min-width: 0;
}
.row-title {
  font-size: 13px;
  font-weight: 500;
  color: var(--c-text);
}
.row-desc {
  font-size: 11px;
  color: var(--c-text-soft);
  margin-top: 2px;
  line-height: 1.5;
}
.row-control {
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  gap: var(--s-2);
}
.row-control.wide {
  flex: 1 1 auto;
  max-width: 360px;
}
.row-control.radios {
  flex: 1 1 100%;
  margin-top: var(--s-2);
}
.row-control.radios {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: var(--s-2);
}
.toggle-row {
  flex-direction: row;
  flex-wrap: wrap;
  justify-content: flex-end;
  max-width: 320px;
}

/* 控件 */
.text,
.num {
  font: inherit;
  font-size: 13px;
  padding: 7px 10px;
  border: 1px solid var(--c-border-strong);
  border-radius: var(--r-sm);
  background: var(--c-panel);
  color: var(--c-text);
  outline: none;
  transition: border-color 0.12s ease, box-shadow 0.12s ease;
}
.text {
  width: 100%;
}
.num {
  width: 100px;
  text-align: right;
}
.text:focus,
.num:focus {
  border-color: var(--c-primary);
  box-shadow: 0 0 0 3px var(--c-primary-soft);
}
.unit {
  font-size: 12px;
  color: var(--c-text-mute);
}
.hint-after {
  font-size: 11px;
  color: var(--c-text-soft);
}
.hint-after.stealth {
  flex-basis: 100%;
  text-align: right;
  color: var(--c-warning);
}

/* radio 卡 */
.radio {
  display: flex;
  align-items: flex-start;
  gap: var(--s-2);
  padding: 9px 11px;
  border: 1px solid var(--c-border);
  border-radius: var(--r-md);
  cursor: pointer;
  background: var(--c-bg);
  transition: border-color 0.12s ease, background 0.12s ease;
}
.radio:hover {
  border-color: var(--c-border-strong);
}
.radio input {
  margin-top: 2px;
  accent-color: var(--c-primary);
}
.radio span {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}
.radio b {
  font-size: 12px;
  font-weight: 600;
  color: var(--c-text);
}
.radio small {
  font-size: 10.5px;
  color: var(--c-text-soft);
  line-height: 1.4;
}
.radio:has(input:checked) {
  background: var(--c-primary-soft);
  border-color: var(--c-primary);
}
.radio:has(input:checked) b {
  color: var(--c-primary);
}

/* toggle */
.toggle {
  position: relative;
  display: inline-block;
  width: 38px;
  height: 22px;
  flex: 0 0 auto;
}
.toggle input {
  opacity: 0;
  width: 0;
  height: 0;
}
.track {
  position: absolute;
  inset: 0;
  background: var(--c-border-strong);
  border-radius: 999px;
  cursor: pointer;
  transition: background 0.15s ease;
}
.track::after {
  content: "";
  position: absolute;
  top: 2px;
  left: 2px;
  width: 18px;
  height: 18px;
  background: #fff;
  border-radius: 50%;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.2);
  transition: transform 0.18s ease;
}
.toggle input:checked + .track {
  background: var(--c-primary);
}
.toggle input:checked + .track::after {
  transform: translateX(16px);
}
.toggle input:focus-visible + .track {
  box-shadow: 0 0 0 3px var(--c-primary-soft);
}

/* 按钮 */
.btn {
  font-size: 12px;
  font-weight: 500;
  padding: 7px 14px;
  border-radius: var(--r-sm);
  border: 1px solid var(--c-border-strong);
  background: var(--c-panel);
  color: var(--c-text);
  cursor: pointer;
  transition: background 0.12s ease, border-color 0.12s ease, transform 0.05s ease;
}
.btn:hover:not(:disabled) {
  background: var(--c-bg);
}
.btn:active:not(:disabled) {
  transform: translateY(1px);
}
.btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
.btn.primary {
  background: var(--c-primary);
  border-color: var(--c-primary);
  color: #fff;
}
.btn.primary:hover:not(:disabled) {
  background: #0058e0;
}
.btn.ghost {
  background: transparent;
}

/* 空面板 */
.empty-panel {
  font-size: 12px;
  color: var(--c-text-soft);
  padding: var(--s-5) 0;
  text-align: center;
}

/* 关于 */
.about-block {
  display: flex;
  align-items: baseline;
  gap: var(--s-3);
  padding: var(--s-3) 0 var(--s-4);
  border-bottom: 1px solid var(--c-border);
  margin-bottom: var(--s-3);
}
.brand {
  font-size: 24px;
  font-weight: 700;
  letter-spacing: -0.01em;
  color: var(--c-primary);
}
.version {
  font-size: 12px;
  color: var(--c-text-mute);
  font-family: ui-monospace, "SF Mono", monospace;
}
.about-row {
  display: flex;
  align-items: center;
  gap: var(--s-3);
  padding: var(--s-2) 0;
  font-size: 12px;
}
.about-key {
  flex: 0 0 80px;
  color: var(--c-text-soft);
}
.about-val {
  background: var(--c-bg);
  padding: 3px 8px;
  border-radius: var(--r-sm);
  font-family: ui-monospace, "SF Mono", monospace;
  font-size: 11.5px;
  color: var(--c-text);
}
.about-text {
  margin-top: var(--s-3);
  font-size: 12px;
  color: var(--c-text-mute);
  line-height: 1.7;
}

/* transition */
.fade-enter-from,
.fade-leave-to {
  opacity: 0;
}
.fade-enter-active,
.fade-leave-active {
  transition: opacity 0.18s ease;
}
.slide-enter-from {
  transform: translateY(-100%);
}
.slide-leave-to {
  transform: translateY(-100%);
}
.slide-enter-active,
.slide-leave-active {
  transition: transform 0.18s ease;
}

/* dark mode */
@media (prefers-color-scheme: dark) {
  .shell {
    --c-bg: #1a1a1a;
    --c-panel: #232325;
    --c-border: #333336;
    --c-border-strong: #45454a;
    --c-text: #ececec;
    --c-text-mute: #a1a1a6;
    --c-text-soft: #777;
    --c-primary: #4d8dff;
    --c-primary-soft: rgba(77, 141, 255, 0.18);
    --c-danger: #ff6b6b;
    --c-danger-bg: #3a1a1a;
    --c-success: #5dd494;
    --c-success-bg: #1a3a26;
    --c-warning: #e6b450;
    --c-warning-bg: #3a2e14;
  }
  .toast {
    box-shadow: 0 4px 14px rgba(0, 0, 0, 0.5);
  }
  .track::after {
    background: #f0f0f0;
  }
  .btn.primary:hover:not(:disabled) {
    background: #3a7eff;
  }
}
</style>
