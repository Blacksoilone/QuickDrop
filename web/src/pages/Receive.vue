<script setup lang="ts">
// 电脑端接收 dashboard. webview 弹窗加载.
// QR + 提示 + 停止接收键 + 关闭键 (ADR-17).
import { closeWindow, stopReceiving } from "../api";

// 字符串字面量绑定避开 Vite 把 "/qr-recv" 当构建资源解析.
const qrRecvUrl = "/qr-recv";

async function stop() {
  await stopReceiving();
  closeWindow();
}
</script>

<template>
  <button class="close" title="关闭窗口 (接收仍开启)" @click="closeWindow">×</button>
  <div class="wrap">
    <div class="qr"><img :src="qrRecvUrl" alt="扫码上传" /></div>
    <div class="hint">
      手机扫码上传文件<br />
      保存到 ~/Downloads/QuickDrop/
    </div>
    <button class="stop" @click="stop">停止接收</button>
  </div>
</template>

<style scoped>
.wrap {
  width: 100%;
  height: 100vh;
  display: flex;
  flex-direction: column;
  align-items: center;
  padding: 12px 12px 10px;
  overflow: hidden;
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
.stop {
  margin-top: 10px;
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
