import { ElMessageBox } from 'element-plus/es/components/message-box/index'
import { safeStorage } from '@/utils/storage'

// 简洁模式（/lite）卡顿提醒。
// 检测主线程长时间阻塞（页面卡顿）后，询问用户是否切换到简洁模式；
// 用户的选择存储到 localStorage，避免反复打扰。"yes"/"no" 即不再提醒。
const STORAGE_KEY = 'lite_mode_choice'
const JANK_THRESHOLD_MS = 5000 // 主线程阻塞超过该时长视为一次卡顿
const HEARTBEAT_MS = 500
const RECOMMEND_URL = '/lite'
const PROMPT_TITLE = '页面响应缓慢'
const PROMPT_TEXT = '检测到当前页面较为卡顿，可能是因为文件较多或设备性能较低。是否切换到简洁模式（服务器渲染的精简列表，更适合老旧或低性能设备）？'

let installed = false

export function useLiteModeSuggestion() {
  const start = (delayMs = 10000) => {
    if (installed) return
    installed = true

    setTimeout(() => {
      // 用户已明确选择过，不再干预
      const choice = safeStorage.get(STORAGE_KEY)
      if (choice === 'yes' || choice === 'no') return

      let prompted = false
      const maybePrompt = () => {
        if (prompted) return
        if (safeStorage.get(STORAGE_KEY) === 'yes' || safeStorage.get(STORAGE_KEY) === 'no') return
        prompted = true // 每会话仅询问一次，避免重复打扰
        ElMessageBox.confirm(PROMPT_TEXT, PROMPT_TITLE, {
          confirmButtonText: '切换到简洁模式',
          cancelButtonText: '暂不',
          type: 'warning',
          closeOnClickModal: false,
          closeOnPressEscape: false
        }).then(() => {
          safeStorage.set(STORAGE_KEY, 'yes')
          window.location.href = RECOMMEND_URL
        }).catch(() => {
          safeStorage.set(STORAGE_KEY, 'no')
        })
      }

      // 优先使用 Long Task API 观测长任务；不可用时退回心跳测量
      let longTaskObserver = null
      if (typeof PerformanceObserver !== 'undefined' &&
          Array.isArray(PerformanceObserver.supportedEntryTypes) &&
          PerformanceObserver.supportedEntryTypes.indexOf('longtask') !== -1) {
        try {
          longTaskObserver = new PerformanceObserver(() => maybePrompt())
          longTaskObserver.observe({ entryTypes: ['longtask'], buffered: false })
        } catch (e) {
          longTaskObserver = null
        }
      }
      if (!longTaskObserver) {
        let last = performance.now()
        setInterval(() => {
          const now = performance.now()
          const delay = now - last - HEARTBEAT_MS
          last = now
          if (delay > JANK_THRESHOLD_MS) maybePrompt()
        }, HEARTBEAT_MS)
      }
    }, delayMs)
  }

  // 允许用户/设置页重置选择（例如某个页面正常后续需要再提醒）
  const clearChoice = () => safeStorage.remove(STORAGE_KEY)

  return { start, clearChoice }
}