// 上传队列/URL 下载共用的常量与纯状态映射
export const DEP_RELATION_OPTIONS = [
  { label: '依赖 (requires)', value: 'requires' },
  { label: '被引用 (referenced_by)', value: 'referenced_by' },
  { label: '关联 (related)', value: 'related' }
]

// 上传会话持久化前缀：每个会话独立 key（按 uploadId）避免多页面互相覆盖，
// localStorage 持久化保证浏览器重启后仍可自动恢复
export const SESSION_KEY_PREFIX = 'fuzhan_upload_session_'

// naive 标签类型 -> element-plus 标签类型映射
export const elTagType = (t) => ({ default: '', info: 'info', warning: 'warning', success: 'success', error: 'danger' })[t]

const UPLOAD_STATUS_TEXT = {
  pending: '等待',
  uploading: '上传中',
  paused: '已暂停',
  needFile: '需选文件',
  completed: '完成',
  failed: '失败'
}

const UPLOAD_STATUS_TAG = {
  pending: 'default',
  uploading: 'info',
  paused: 'warning',
  needFile: 'warning',
  completed: 'success',
  failed: 'error'
}

export const getStatusText = (status) => UPLOAD_STATUS_TEXT[status] || status
export const getStatusTagType = (status) => UPLOAD_STATUS_TAG[status] || 'default'