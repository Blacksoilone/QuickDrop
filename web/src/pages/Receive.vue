<script setup lang="ts">
// 电脑端接收 dashboard. webview 弹窗加载.
// QR + 提示 + 停止接收键 (ADR-17).
// 无边框模式: 整个窗口可拖动, 右上角浮动关闭按钮.
import { onMounted, onUnmounted, ref } from "vue";
import { closeWindow, stopReceiving, fetchConfig } from "../api";
import Toast from "../components/Toast.vue";
import MiniShell from "../components/MiniShell.vue";

// 字符串字面量绑定避开 Vite 把 "/qr-recv" 当构建资源解析.
const qrRecvUrl = "/qr-recv";

const toastMessage = ref("");
const showToast = ref(false);
const borderless = ref(true);
let toastTimer: number | undefined;

onMounted(async () => {
  // 加载配置
  try {
    const cfg = await fetchConfig();
    borderless.value = cfg.ui.borderless_windows;
  } catch {
    // 失败保持默认值
  }
});

async function stop() {
  await stopReceiving();
  closeWindow();
}

function copyLink() {
  const url = window.location.origin + "/u";
  navigator.clipboard.writeText(url).then(
    () => {
      toastMessage.value = "已复制到剪贴板";
      showToast.value = true;
      if (toastTimer !== undefined) window.clearTimeout(toastTimer);
      toastTimer = window.setTimeout(() => {
        showToast.value = false;
      }, 2000);
    },
    () => {
      toastMessage.value = "复制失败";
      showToast.value = true;
      if (toastTimer !== undefined) window.clearTimeout(toastTimer);
      toastTimer = window.setTimeout(() => {
        showToast.value = false;
      }, 2000);
    }
  );
}

onUnmounted(() => {
  if (toastTimer !== undefined) window.clearTimeout(toastTimer);
});
</script>

<template>
  <MiniShell :enable-drag="borderless" @close="closeWindow">
    <div class="wrap">
      <div class="qr"><img :src="qrRecvUrl" alt="扫码上传" /></div>
      <div class="hint">
        手机扫码上传文件<br />
        保存到 C:\Users\用户名\Downloads\QuickDrop\
      </div>
      <div class="actions">
        <button class="copy-btn" @click="copyLink">复制链接</button>
        <button class="stop" @click="stop">停止接收</button>
      </div>
    </div>
    <Toast :message="toastMessage" :show="showToast" />
  </MiniShell>
</template>

<style scoped>
.page-borderless {
  display: flex;
  flex-direction: column;
  height: 100vh;
}

.wrap {
  width: 100%;
  height: 100vh;
  display: flex;
  flex-direction: column;
  align-items: center;
  padding: 12px 12px 10px;
  overflow: hidden;
}

.page-borderless .wrap {
  height: auto;
  flex: 1;
}

.qr {
  width: 220px;
  height: 220px;
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
.hint {
  margin-top: 8px;
  font-size: 12px;
  color: #555;
  text-align: center;
}
.actions {
  margin-top: 10px;
  display: flex;
  gap: 8px;
  width: 100%;
}
.copy-btn {
  flex: 1;
  padding: 6px 14px;
  background: #0066ff;
  color: #fff;
  border: 1px solid #0066ff;
  border-radius: 4px;
  font-size: 12px;
  cursor: pointer;
  transition: background 0.15s ease;
}
.copy-btn:hover {
  background: #0052cc;
  border-color: #0052cc;
}
.stop {
  flex: 1;
  padding: 6px 14px;
  background: #d33;
  color: #fff;
  border: 0;
  border-radius: 4px;
  font-size: 12px;
  cursor: pointer;
}
.stop:hover {
  background: #b22;
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
  .hint {
    color: #aaa;
  }
  .close:hover {
    background: #333;
    color: #fff;
  }
}
</style>
