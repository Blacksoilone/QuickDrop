<script setup lang="ts">
// 手机端上传表单. 受 server.go receiveMode 门禁保护
// (这页只在接收模式开启时可达, 关时整个 /u 404).
import { ref } from "vue";

const status = ref<"idle" | "uploading" | "done" | "error">("idle");
const count = ref(0);
const errMsg = ref("");

async function onSubmit(e: Event) {
  e.preventDefault();
  const form = e.target as HTMLFormElement;
  const data = new FormData(form);
  status.value = "uploading";
  try {
    const r = await fetch("/upload", { method: "POST", body: data });
    if (!r.ok) throw new Error(`HTTP ${r.status}`);
    // 服务器返回 uploadDoneHTMLTpl, 我们粗略解析 "已收到 N 个文件"
    const text = await r.text();
    const m = text.match(/已收到\s*(\d+)/);
    count.value = m ? parseInt(m[1], 10) : 0;
    status.value = "done";
    form.reset();
  } catch (e) {
    errMsg.value = String(e);
    status.value = "error";
  }
}
</script>

<template>
  <div class="card">
    <h1>上传到电脑</h1>
    <form v-if="status !== 'done'" @submit="onSubmit">
      <input type="file" name="file" multiple required :disabled="status === 'uploading'" />
      <button class="btn" type="submit" :disabled="status === 'uploading'">
        {{ status === "uploading" ? "上传中..." : "上传" }}
      </button>
    </form>
    <div v-else class="done">
      <p>已收到 {{ count }} 个文件</p>
      <button class="btn" @click="status = 'idle'">继续上传</button>
    </div>
    <p v-if="status === 'error'" class="err">上传失败: {{ errMsg }}</p>
    <p class="hint">保存到电脑 ~/Downloads/QuickDrop/</p>
  </div>
</template>

<style scoped>
.card {
  background: #fff;
  border: 1px solid #e5e5e5;
  border-radius: 12px;
  padding: 24px 20px;
  margin: 32px 20px;
  max-width: 420px;
}
h1 {
  font-size: 1.1em;
  margin: 0 0 16px;
}
input[type="file"] {
  width: 100%;
  margin-bottom: 16px;
  padding: 10px;
  background: #fafafa;
  border: 1px dashed #ccc;
  border-radius: 8px;
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
  cursor: pointer;
}
.btn:disabled {
  background: #888;
  cursor: wait;
}
.done {
  text-align: center;
}
.done p {
  margin-bottom: 16px;
}
.hint {
  color: #888;
  font-size: 0.85em;
  margin-top: 12px;
  text-align: center;
}
.err {
  margin-top: 12px;
  color: #d33;
  font-size: 0.9em;
}
@media (prefers-color-scheme: dark) {
  .card {
    background: #262626;
    border-color: #333;
  }
  input[type="file"] {
    background: #1a1a1a;
    border-color: #444;
    color: #eee;
  }
}
</style>
