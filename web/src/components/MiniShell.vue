<script setup lang="ts">
// 迷你窗口外壳 - 给二维码窗口用
// 整个窗口都可以拖动（除了按钮区），符合 AirDrop 风格的极简体验
// 右上角浮动关闭按钮（小型）
import { X } from "lucide-vue-next";
import { startWindowDrag } from "../api";

defineProps<{
  enableDrag?: boolean; // 是否启用拖动 (无边框模式才需要)
}>();

const emit = defineEmits<{
  close: [];
}>();

function handleClose() {
  emit('close');
}

// 鼠标按下时启动拖动. 但要排除按钮和交互元素.
function handleMouseDown(e: MouseEvent) {
  if (e.button !== 0) return;
  // 检查目标元素是否是按钮或可交互元素
  const target = e.target as HTMLElement;
  if (target.closest("button, a, input, select, textarea, [data-no-drag]")) {
    return;
  }
  // 阻止默认行为 (防止图片拖拽 / 文本选中)
  e.preventDefault();
  startWindowDrag();
}
</script>

<template>
  <div
    class="mini-shell"
    :class="{ 'shell-draggable': enableDrag }"
    @mousedown="enableDrag ? handleMouseDown($event) : undefined"
  >
    <button
      class="floating-close"
      @click="handleClose"
      @mousedown.stop
      title="关闭"
      data-no-drag
    >
      <X :size="14" />
    </button>
    <slot />
  </div>
</template>

<style scoped>
.mini-shell {
  width: 100%;
  height: 100vh;
  position: relative;
  display: flex;
  flex-direction: column;
  /* 防止用户选中文字/图片 - 整个窗口拖动时必须 */
  user-select: none;
  -webkit-user-select: none;
  /* 防止图片被拖拽 */
  -webkit-user-drag: none;
}

/* 内部图片/QR 也禁止拖拽和选中 */
.mini-shell :deep(img) {
  -webkit-user-drag: none;
  user-select: none;
  pointer-events: none;
}

.shell-draggable {
  cursor: default;
}

/* 浮动关闭按钮 - 窗口顶部内侧 */
.floating-close {
  position: absolute;
  top: 4px;
  right: 4px;
  width: 20px;
  height: 20px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(0, 0, 0, 0.04);
  border: 0;
  border-radius: 10px;
  cursor: pointer;
  color: #888;
  z-index: 1000;
  transition: background 0.15s ease, color 0.15s ease;
  /* 防止 user-select: none 阻断点击 */
  user-select: auto;
}

.floating-close:hover {
  background: #e81123;
  color: #fff;
}

@media (prefers-color-scheme: dark) {
  .floating-close {
    background: rgba(255, 255, 255, 0.06);
    color: #aaa;
  }
  .floating-close:hover {
    background: #e81123;
    color: #fff;
  }
}
</style>

