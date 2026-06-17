<script setup lang="ts">
// 自定义标题栏组件，用于无边框窗口
// 提供拖动区域 + 最小化/关闭按钮
import { X, Minus } from "lucide-vue-next";
import { startWindowDrag } from "../api";

defineProps<{
  title?: string;
  showMinimize?: boolean;
}>();

const emit = defineEmits<{
  close: [];
  minimize: [];
}>();

function handleClose() {
  emit('close');
}

function handleMinimize() {
  emit('minimize');
}

// 处理拖动: 鼠标按下时调用 Go 函数启动窗口拖动
function handleDragStart(e: MouseEvent) {
  // 只响应左键
  if (e.button !== 0) return;
  startWindowDrag();
}
</script>

<template>
  <div class="titlebar">
    <div class="titlebar-drag" @mousedown="handleDragStart">
      <span v-if="title" class="titlebar-title">{{ title }}</span>
    </div>
    <div class="titlebar-controls">
      <button
        v-if="showMinimize"
        class="titlebar-btn titlebar-minimize"
        @click="handleMinimize"
        title="最小化"
      >
        <Minus :size="14" />
      </button>
      <button
        class="titlebar-btn titlebar-close"
        @click="handleClose"
        title="关闭"
      >
        <X :size="14" />
      </button>
    </div>
  </div>
</template>

<style scoped>
.titlebar {
  display: flex;
  height: 32px;
  background: linear-gradient(to bottom, #fafafa, #f0f0f0);
  border-bottom: 1px solid #ddd;
  user-select: none;
  flex-shrink: 0;
}

.titlebar-drag {
  flex: 1;
  display: flex;
  align-items: center;
  padding-left: 12px;
  cursor: default;
}

.titlebar-title {
  font-size: 12px;
  color: #555;
  font-weight: 500;
}

.titlebar-controls {
  display: flex;
}

.titlebar-btn {
  width: 46px;
  height: 32px;
  border: none;
  background: transparent;
  cursor: pointer;
  color: #555;
  transition: background 0.15s ease;
  display: flex;
  align-items: center;
  justify-content: center;
}

.titlebar-btn:hover {
  background: rgba(0, 0, 0, 0.05);
}

.titlebar-close:hover {
  background: #e81123;
  color: #fff;
}

@media (prefers-color-scheme: dark) {
  .titlebar {
    background: linear-gradient(to bottom, #2d2d2d, #252525);
    border-bottom-color: #1a1a1a;
  }
  
  .titlebar-title {
    color: #ccc;
  }
  
  .titlebar-btn {
    color: #ccc;
  }
  
  .titlebar-btn:hover {
    background: rgba(255, 255, 255, 0.1);
  }
  
  .titlebar-close:hover {
    background: #e81123;
    color: #fff;
  }
}
</style>
