<script setup lang="ts">
import { ref, watch } from "vue";

const props = defineProps<{
  message: string;
  show: boolean;
}>();

const visible = ref(false);

watch(
  () => props.show,
  (newVal) => {
    if (newVal) {
      visible.value = true;
    }
  }
);
</script>

<template>
  <Transition name="toast">
    <div v-if="visible && show" class="toast">
      {{ message }}
    </div>
  </Transition>
</template>

<style scoped>
.toast {
  position: fixed;
  bottom: 24px;
  left: 50%;
  transform: translateX(-50%);
  padding: 10px 20px;
  background: rgba(0, 0, 0, 0.85);
  color: #fff;
  border-radius: 6px;
  font-size: 13px;
  z-index: 9999;
  pointer-events: none;
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.3);
}

.toast-enter-active,
.toast-leave-active {
  transition: opacity 0.3s ease, transform 0.3s ease;
}

.toast-enter-from {
  opacity: 0;
  transform: translateX(-50%) translateY(10px);
}

.toast-leave-to {
  opacity: 0;
  transform: translateX(-50%) translateY(-10px);
}
</style>
