import { ref, computed } from 'vue'

// 全局请求计数：>0 表示有请求在途，驱动页面顶部的全局 loading 条
const pending = ref(0)

export function beginRequest() {
  pending.value++
}

export function endRequest() {
  if (pending.value > 0) {
    pending.value--
  }
}

export const isLoading = computed(() => pending.value > 0)

export function useRequestLoading() {
  return { pending, isLoading }
}