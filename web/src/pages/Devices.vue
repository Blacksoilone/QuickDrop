<script setup lang="ts">
// /v 旧设备管理入口. 保留为薄壳, 实际渲染抽出的 <DevicePanel/>.
// 顶部 banner 引导用户去新配置中心 (/c#devices).
import { closeWindow } from "../api";
import DevicePanel from "./DevicePanel.vue";
</script>

<template>
  <button class="close" title="关闭窗口 (daemon 继续运行)" @click="closeWindow">×</button>
  <div class="wrap">
    <div class="banner">
      设备管理已合并到
      <a href="/c#devices">配置中心</a>, 后续会移除独立入口
    </div>
    <h1>设备管理</h1>
    <DevicePanel />
  </div>
</template>

<style scoped>
.wrap {
  width: 100%;
  min-height: 100vh;
  padding: 14px 16px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.banner {
  font-size: 12px;
  color: #5a4a14;
  background: #fff4d6;
  border: 1px solid rgba(184, 134, 11, 0.25);
  border-radius: 6px;
  padding: 8px 12px;
  line-height: 1.5;
}
.banner a {
  color: #0066ff;
  font-weight: 500;
  text-decoration: none;
}
.banner a:hover {
  text-decoration: underline;
}
h1 {
  font-size: 14px;
  font-weight: 600;
  color: #444;
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
  .banner {
    color: #e6b450;
    background: #3a2e14;
    border-color: rgba(230, 180, 80, 0.3);
  }
  .banner a {
    color: #4d8dff;
  }
  .close:hover {
    background: #333;
    color: #fff;
  }
}
</style>
