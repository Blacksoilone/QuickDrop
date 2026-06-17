<script setup lang="ts">
// 设备列表中的单个设备行
import {
  Monitor,
  MoreVertical,
  Check,
  Ban,
  HelpCircle,
  Pencil,
  Trash2,
} from "lucide-vue-next";
import type { DeviceEntry } from "../api";

defineProps<{
  device: DeviceEntry;
  isEditing: boolean;
  editingValue: string;
  menuOpen: boolean;
}>();

defineEmits<{
  "toggle-menu": [];
  "change-trust": [trust: "ask" | "trusted" | "blocked"];
  "start-edit": [];
  "update:editing-value": [value: string];
  "save-alias": [];
  "cancel-edit": [];
  "delete": [];
}>();

function timeAgo(unixSec: number): string {
  if (unixSec === 0) return "未见过";
  const diff = Math.floor(Date.now() / 1000) - unixSec;
  if (diff < 60) return `${diff} 秒前`;
  if (diff < 3600) return `${Math.floor(diff / 60)} 分钟前`;
  if (diff < 86400) return `${Math.floor(diff / 3600)} 小时前`;
  return `${Math.floor(diff / 86400)} 天前`;
}
</script>

<template>
  <div class="device-row">
    <Monitor :size="20" class="device-icon" />
    <div class="device-info">
      <div v-if="!isEditing" class="device-name">
        {{ device.alias || device.name || "(无名设备)" }}
      </div>
      <input
        v-else
        :value="editingValue"
        @input="$emit('update:editing-value', ($event.target as HTMLInputElement).value)"
        type="text"
        class="alias-input"
        :placeholder="device.name"
        @keydown.enter="$emit('save-alias')"
        @keydown.esc="$emit('cancel-edit')"
        @blur="$emit('save-alias')"
      />
      <div class="device-sub">
        <span v-if="device.alias">{{ device.name }} · </span>
        <span>{{ timeAgo(device.lastSeen) }}</span>
      </div>
    </div>

    <div class="menu-wrapper">
      <button class="menu-btn" @click="$emit('toggle-menu')" title="操作">
        <MoreVertical :size="16" />
      </button>
      <div v-if="menuOpen" class="menu">
        <button
          v-if="device.trust !== 'trusted'"
          class="menu-item"
          @click="$emit('change-trust', 'trusted')"
        >
          <Check :size="14" />
          <span>信任</span>
        </button>
        <button
          v-if="device.trust !== 'ask'"
          class="menu-item"
          @click="$emit('change-trust', 'ask')"
        >
          <HelpCircle :size="14" />
          <span>每次询问</span>
        </button>
        <button
          v-if="device.trust !== 'blocked'"
          class="menu-item"
          @click="$emit('change-trust', 'blocked')"
        >
          <Ban :size="14" />
          <span>拉黑</span>
        </button>
        <div class="menu-divider" />
        <button class="menu-item" @click="$emit('start-edit')">
          <Pencil :size="14" />
          <span>重命名</span>
        </button>
        <button class="menu-item danger" @click="$emit('delete')">
          <Trash2 :size="14" />
          <span>删除</span>
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.device-row {
  display: flex;
  align-items: center;
  gap: 10px;
}
.device-icon {
  flex-shrink: 0;
  color: #666;
}
.device-info {
  flex: 1;
  min-width: 0;
}
.device-name {
  font-size: 13px;
  font-weight: 600;
  color: #222;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.device-sub {
  font-size: 11px;
  color: #888;
  margin-top: 2px;
}
.alias-input {
  width: 100%;
  padding: 4px 6px;
  border: 1px solid #0066ff;
  border-radius: 4px;
  font-size: 13px;
  font-weight: 600;
  background: #fff;
  color: #222;
  outline: none;
}

.menu-wrapper {
  position: relative;
}
.menu-btn {
  width: 28px;
  height: 28px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: transparent;
  border: 0;
  border-radius: 6px;
  cursor: pointer;
  color: #888;
}
.menu-btn:hover {
  background: #f0f0f0;
  color: #333;
}
.menu {
  position: absolute;
  right: 0;
  top: 100%;
  margin-top: 4px;
  min-width: 140px;
  background: #fff;
  border: 1px solid #ddd;
  border-radius: 6px;
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.1);
  z-index: 100;
  padding: 4px;
  display: flex;
  flex-direction: column;
  gap: 1px;
}
.menu-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 10px;
  background: transparent;
  border: 0;
  border-radius: 4px;
  font-size: 12px;
  color: #333;
  cursor: pointer;
  text-align: left;
}
.menu-item:hover {
  background: #f0f0f0;
}
.menu-item.danger {
  color: #d33;
}
.menu-item.danger:hover {
  background: #fdecec;
}
.menu-divider {
  height: 1px;
  background: #eee;
  margin: 2px 0;
}

@media (prefers-color-scheme: dark) {
  .device-name {
    color: #eee;
  }
  .device-sub {
    color: #888;
  }
  .device-icon {
    color: #999;
  }
  .alias-input {
    background: #1a1a1a;
    color: #eee;
  }
  .menu-btn:hover {
    background: #333;
    color: #ccc;
  }
  .menu {
    background: #2a2a2a;
    border-color: #444;
  }
  .menu-item {
    color: #ccc;
  }
  .menu-item:hover {
    background: #333;
  }
  .menu-item.danger {
    color: #f77;
  }
  .menu-item.danger:hover {
    background: #3a1a1a;
  }
  .menu-divider {
    background: #444;
  }
}
</style>
