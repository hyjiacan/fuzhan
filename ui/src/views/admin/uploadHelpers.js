import { NumberUtils } from '@/utils'

// 格式化文件大小（0 显示为 "-"，与文件列表一致）
export const formatSize = (bytes) => NumberUtils.formatFileSize(bytes || 0)