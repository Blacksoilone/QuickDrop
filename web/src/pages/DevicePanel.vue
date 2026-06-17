<script setup lang="ts">
// 设备管理子组件 (ADR-20). 嵌入 Config.vue "设备" tab.
// 功能：搜索、筛选、分组、重命名、删除、信任管理
import { computed, onMounted, onUnmounted, ref } from "vue";
import {
  fetchDevices,
  setDeviceTrust,
  setDeviceAlias,
  deleteDevice,
  type DeviceEntry,
} from "../api";
import {
  Search,
  Check,
  Ban,
  HelpCircle,
  ChevronDown,
  ChevronRight,
  X as XIcon,
} from "lucide-vue-next";
import DeviceItem from "../components/DeviceItem.vue";

type FilterType = "all" | "trusted" | "ask" | "blocked";
type SortType = "recent" | "name";

const items = ref<DeviceEntry[]>([]);
const error = ref<string>("");
const searchQuery = ref<string>("");
const filterType = ref<FilterType>("all");
const sortType = ref<SortType>("recent");
const blockedExpanded = ref<boolean>(false);

const openMenuUuid = ref<string | null>(null);
const editingAliasUuid = ref<string | null>(null);
const editingAliasValue = ref<string>("");

let timer: number | undefined;

onMounted(async () => {
  await refresh();
  timer = window.setInterval(refresh, 3000);
  document.addEventListener("click", closeMenuOnOutsideClick);
});

onUnmounted(() => {
  if (timer !== undefined) window.clearInterval(timer);
  document.removeEventListener("click", closeMenuOnOutsideClick);
});

function closeMenuOnOutsideClick(e: MouseEvent) {
  const target = e.target as HTMLElement;
  if (!target.closest(".menu-wrapper")) {
    openMenuUuid.value = null;
  }
}

async function refresh() {
  try {
    items.value = await fetchDevices();
    error.value = "";
  } catch (e) {
    error.value = String(e);
  }
}

const filteredItems = computed(() => {
  let result = items.value;

  if (searchQuery.value.trim()) {
    const q = searchQuery.value.toLowerCase();
    result = result.filter(
      (d) =>
        d.name.toLowerCase().includes(q) ||
        d.alias.toLowerCase().includes(q) ||
        d.uuid.toLowerCase().includes(q),
    );
  }

  if (filterType.value !== "all") {
    result = result.filter((d) => d.trust === filterType.value);
  }

  if (sortType.value === "name") {
    result = [...result].sort((a, b) => {
      const aName = a.alias || a.name;
      const bName = b.alias || b.name;
      return aName.localeCompare(bName);
    });
  } else {
    result = [...result].sort((a, b) => b.lastSeen - a.lastSeen);
  }

  return result;
});

const groupedItems = computed(() => {
  if (filterType.value !== "all") {
    return null;
  }
  return {
    trusted: filteredItems.value.filter((d) => d.trust === "trusted"),
    ask: filteredItems.value.filter((d) => d.trust === "ask"),
    blocked: filteredItems.value.filter((d) => d.trust === "blocked"),
  };
});

async function change(d: DeviceEntry, trust: "ask" | "trusted" | "blocked") {
  if (d.trust === trust) return;
  openMenuUuid.value = null;
  try {
    await setDeviceTrust(d.uuid, d.name, trust);
    await refresh();
  } catch (e) {
    error.value = String(e);
  }
}

function startEditAlias(d: DeviceEntry) {
  editingAliasUuid.value = d.uuid;
  editingAliasValue.value = d.alias || "";
  openMenuUuid.value = null;
}

async function saveAlias() {
  if (!editingAliasUuid.value) return;
  const uuid = editingAliasUuid.value;
  const alias = editingAliasValue.value.trim();
  try {
    await setDeviceAlias(uuid, alias);
    editingAliasUuid.value = null;
    await refresh();
  } catch (e) {
    error.value = String(e);
  }
}

function cancelEditAlias() {
  editingAliasUuid.value = null;
  editingAliasValue.value = "";
}

async function handleDelete(d: DeviceEntry) {
  const displayName = d.alias || d.name;
  if (
    !confirm(
      `确定要删除设备 "${displayName}" 吗？\n\n` +
        `删除后该设备的信任状态将丢失。\n` +
        `如果该设备再次连接，会以"待决策"状态重新出现。`,
    )
  ) {
    return;
  }
  openMenuUuid.value = null;
  try {
    await deleteDevice(d.uuid);
    await refresh();
  } catch (e) {
    error.value = String(e);
  }
}

function toggleMenu(uuid: string) {
  openMenuUuid.value = openMenuUuid.value === uuid ? null : uuid;
}
</script>

<template>
  <div class="device-panel">
    <div class="toolbar">
      <div class="search-wrap">
        <Search :size="14" class="search-icon" />
        <input
          v-model="searchQuery"
          type="text"
          placeholder="搜索设备..."
          class="search-input"
        />
        <button
          v-if="searchQuery"
          class="search-clear"
          @click="searchQuery = ''"
          title="清空搜索"
        >
          <XIcon :size="12" />
        </button>
      </div>

      <select v-model="filterType" class="select-control">
        <option value="all">全部</option>
        <option value="trusted">仅信任</option>
        <option value="ask">仅待决策</option>
        <option value="blocked">仅黑名单</option>
      </select>

      <select v-model="sortType" class="select-control">
        <option value="recent">最近活跃</option>
        <option value="name">名称</option>
      </select>
    </div>

    <div v-if="error" class="err">{{ error }}</div>

    <div v-if="items.length === 0 && !error" class="empty">
      还没和任何设备交互过<br />
      <span class="hint-inline">PC↔PC 传输后，对方会出现在这里</span>
    </div>

    <template v-if="groupedItems">
      <div v-if="groupedItems.trusted.length > 0" class="group">
        <div class="group-header trusted">
          <Check :size="14" />
          <span>信任 ({{ groupedItems.trusted.length }})</span>
        </div>
        <ul class="list">
          <li v-for="d in groupedItems.trusted" :key="d.uuid" class="item">
            <DeviceItem
              :device="d"
              :is-editing="editingAliasUuid === d.uuid"
              :editing-value="editingAliasValue"
              :menu-open="openMenuUuid === d.uuid"
              @toggle-menu="toggleMenu(d.uuid)"
              @change-trust="(t) => change(d, t)"
              @start-edit="startEditAlias(d)"
              @update:editing-value="editingAliasValue = $event"
              @save-alias="saveAlias"
              @cancel-edit="cancelEditAlias"
              @delete="handleDelete(d)"
            />
          </li>
        </ul>
      </div>

      <div v-if="groupedItems.ask.length > 0" class="group">
        <div class="group-header ask">
          <HelpCircle :size="14" />
          <span>待决策 ({{ groupedItems.ask.length }})</span>
        </div>
        <ul class="list">
          <li v-for="d in groupedItems.ask" :key="d.uuid" class="item">
            <DeviceItem
              :device="d"
              :is-editing="editingAliasUuid === d.uuid"
              :editing-value="editingAliasValue"
              :menu-open="openMenuUuid === d.uuid"
              @toggle-menu="toggleMenu(d.uuid)"
              @change-trust="(t) => change(d, t)"
              @start-edit="startEditAlias(d)"
              @update:editing-value="editingAliasValue = $event"
              @save-alias="saveAlias"
              @cancel-edit="cancelEditAlias"
              @delete="handleDelete(d)"
            />
          </li>
        </ul>
      </div>

      <div v-if="groupedItems.blocked.length > 0" class="group">
        <button
          class="group-header blocked clickable"
          @click="blockedExpanded = !blockedExpanded"
        >
          <Ban :size="14" />
          <span>黑名单 ({{ groupedItems.blocked.length }})</span>
          <ChevronDown v-if="blockedExpanded" :size="14" class="chevron" />
          <ChevronRight v-else :size="14" class="chevron" />
        </button>
        <ul v-if="blockedExpanded" class="list">
          <li v-for="d in groupedItems.blocked" :key="d.uuid" class="item">
            <DeviceItem
              :device="d"
              :is-editing="editingAliasUuid === d.uuid"
              :editing-value="editingAliasValue"
              :menu-open="openMenuUuid === d.uuid"
              @toggle-menu="toggleMenu(d.uuid)"
              @change-trust="(t) => change(d, t)"
              @start-edit="startEditAlias(d)"
              @update:editing-value="editingAliasValue = $event"
              @save-alias="saveAlias"
              @cancel-edit="cancelEditAlias"
              @delete="handleDelete(d)"
            />
          </li>
        </ul>
      </div>
    </template>

    <ul v-else class="list">
      <li v-for="d in filteredItems" :key="d.uuid" class="item">
        <DeviceItem
          :device="d"
          :is-editing="editingAliasUuid === d.uuid"
          :editing-value="editingAliasValue"
          :menu-open="openMenuUuid === d.uuid"
          @toggle-menu="toggleMenu(d.uuid)"
          @change-trust="(t) => change(d, t)"
          @start-edit="startEditAlias(d)"
          @update:editing-value="editingAliasValue = $event"
          @save-alias="saveAlias"
          @cancel-edit="cancelEditAlias"
          @delete="handleDelete(d)"
        />
      </li>
    </ul>

    <div
      v-if="filteredItems.length === 0 && items.length > 0 && !error"
      class="empty no-match"
    >
      没有匹配的设备
    </div>
  </div>
</template>

<style scoped>
.device-panel {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.toolbar {
  display: flex;
  gap: 8px;
  align-items: center;
}
.search-wrap {
  flex: 1;
  position: relative;
  display: flex;
  align-items: center;
}
.search-icon {
  position: absolute;
  left: 10px;
  color: #888;
  pointer-events: none;
}
.search-input {
  width: 100%;
  padding: 7px 30px 7px 32px;
  border: 1px solid #ddd;
  border-radius: 6px;
  font-size: 13px;
  background: #fff;
  color: #333;
}
.search-input:focus {
  outline: none;
  border-color: #0066ff;
  box-shadow: 0 0 0 3px rgba(0, 102, 255, 0.1);
}
.search-clear {
  position: absolute;
  right: 6px;
  width: 22px;
  height: 22px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: transparent;
  border: 0;
  color: #888;
  cursor: pointer;
  border-radius: 4px;
}
.search-clear:hover {
  background: #f0f0f0;
}
.select-control {
  padding: 7px 10px;
  border: 1px solid #ddd;
  border-radius: 6px;
  font-size: 12px;
  background: #fff;
  color: #333;
  cursor: pointer;
}
.select-control:focus {
  outline: none;
  border-color: #0066ff;
}

.err {
  color: #d33;
  font-size: 12px;
  padding: 8px 12px;
  background: #fdecec;
  border-radius: 6px;
}
.empty {
  display: flex;
  flex-direction: column;
  justify-content: center;
  align-items: center;
  color: #999;
  font-size: 13px;
  text-align: center;
  line-height: 1.8;
  padding: 40px 0;
}
.empty.no-match {
  padding: 30px 0;
}
.hint-inline {
  font-size: 11px;
  color: #888;
}

.group {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.group-header {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  font-weight: 600;
  color: #666;
  padding: 4px 2px;
}
.group-header.trusted {
  color: #2a7;
}
.group-header.ask {
  color: #d80;
}
.group-header.blocked {
  color: #c33;
}
.group-header.clickable {
  cursor: pointer;
  background: transparent;
  border: 0;
  padding: 4px 6px;
  border-radius: 4px;
  width: fit-content;
}
.group-header.clickable:hover {
  background: rgba(0, 0, 0, 0.05);
}
.chevron {
  margin-left: 2px;
}

.list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.item {
  background: #fff;
  border: 1px solid #e5e5e5;
  border-radius: 8px;
  padding: 10px 12px;
  transition: border-color 0.15s ease, box-shadow 0.15s ease;
}
.item:hover {
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.06);
}

@media (prefers-color-scheme: dark) {
  .search-input,
  .select-control {
    background: #262626;
    border-color: #444;
    color: #ccc;
  }
  .search-clear:hover {
    background: #333;
  }
  .err {
    background: #3a1a1a;
    color: #f77;
  }
  .empty {
    color: #777;
  }
  .group-header {
    color: #aaa;
  }
  .group-header.clickable:hover {
    background: rgba(255, 255, 255, 0.05);
  }
  .item {
    background: #262626;
    border-color: #333;
  }
}
</style>
