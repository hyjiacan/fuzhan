import { ref } from 'vue'
import { safeStorage } from '@/utils/storage'

const STORAGE_KEY = 'theme_mode'

const media = window.matchMedia('(prefers-color-scheme: dark)')

// 全局单例：避免每处引用重复监听系统主题
const mode = ref(safeStorage.get(STORAGE_KEY) || 'system') // 'light' | 'dark' | 'system'
const isDark = ref(false)

function resolveDark(m, systemDark) {
  return m === 'dark' ? true : m === 'light' ? false : systemDark
}

function apply() {
  const dark = resolveDark(mode.value, media.matches)
  document.documentElement.classList.toggle('dark', dark)
  isDark.value = dark
}

function setMode(m) {
  mode.value = m
  safeStorage.set(STORAGE_KEY, m)
  apply()
}

function init() {
  apply()
  media.addEventListener('change', () => {
    if (mode.value === 'system') apply()
  })
  // 跨标签页同步主题
  window.addEventListener('storage', (e) => {
    if (e.key === STORAGE_KEY) {
      mode.value = e.newValue || 'system'
      apply()
    }
  })
}

export function useTheme() {
  return { mode, isDark, setMode, init }
}