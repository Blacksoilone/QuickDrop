<script setup lang="ts">
// 手机端发送目标页. 手机扫 /qr 进的就是这里 (/d).
// 文件图标 + 文件名 + 大小 + 下载按钮, 没有 QR.
import { onMounted, ref } from "vue";
import { fetchInfo, type ServerInfo } from "../api";

// 字符串字面量绑定避开 Vite 把 "/file" 当构建资源解析.
const fileUrl = "/file";

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
  <div class="card">
    <div class="icon">
      <svg viewBox="0 0 24 24" stroke-linecap="round" stroke-linejoin="round">
        <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" />
        <polyline points="14 2 14 8 20 8" />
      </svg>
    </div>
    <template v-if="info && info.hasFile">
      <div class="name">{{ info.fileName }}</div>
      <div class="size">{{ info.fileSize }}</div>
      <a class="btn" :href="fileUrl">下载到本机</a>
    </template>
    <template v-else-if="error">
      <div class="err">无法加载: {{ error }}</div>
    </template>
    <template v-else>
      <div class="size">加载中...</div>
    </template>
  </div>
</template>

<style scoped>
.card {
  background: #fff;
  border: 1px solid #e5e5e5;
  border-radius: 12px;
  padding: 28px 20px;
  margin: 32px 20px;
  max-width: 420px;
  text-align: center;
}
.icon {
  width: 96px;
  height: 96px;
  margin: 0 auto 18px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: #eef3ff;
  border-radius: 16px;
}
.icon svg {
  width: 56px;
  height: 56px;
  stroke: #0066ff;
  fill: none;
  stroke-width: 2;
}
.name {
  font-size: 1.05em;
  font-weight: 600;
  word-break: break-all;
  margin-bottom: 6px;
}
.size {
  color: #888;
  font-size: 0.9em;
  margin-bottom: 20px;
}
.err {
  color: #d33;
  font-size: 0.9em;
}
.btn {
  display: block;
  width: 100%;
  padding: 14px;
  background: #0066ff;
  color: #fff;
  border: 0;
  border-radius: 8px;
  font-size: 1em;
  font-weight: 600;
  text-decoration: none;
  cursor: pointer;
}
@media (prefers-color-scheme: dark) {
  .card {
    background: #262626;
    border-color: #333;
  }
  .icon {
    background: #1a2640;
  }
}
</style>
