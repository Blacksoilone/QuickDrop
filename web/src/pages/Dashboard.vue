<script setup lang="ts">
// 电脑端发送 dashboard. webview 弹窗加载.
// 只渲 QR + 文件名 + 大小 + 关闭按钮 (ADR-17).
import { onMounted, ref } from "vue";
import { closeWindow, fetchInfo, type ServerInfo } from "../api";

// 字符串字面量绑定避开 Vite 把 "/qr" 当构建资源解析.
const qrUrl = "/qr";

const info = ref<ServerInfo | null>(null);
const error = ref<string>("");

onMounted(async () => {
  try {
    info.value = await fetchInfo();
  } catch (e) {
    error.value = String(e);
  }
});
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
@media (prefers-color-scheme: dark) {
  .close:hover {
    background: #333;
    color: #fff;
  }
}
</style>
