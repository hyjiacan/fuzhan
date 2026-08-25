<template>
  <div class="recent-uploads-full">
    <el-table
      :data="uploads"
      row-key="id"
      stripe
    >
      <el-table-column prop="uploadTime" label="上传时间" width="140">
        <template #default="{ row }">
          <span :style="{ color: TimeUtils.isRecent24h(row.uploadTime) ? '#18a058' : '#666', fontSize: '13px' }">{{ TimeUtils.formatDateTime(row.uploadTime) }}</span>
        </template>
      </el-table-column>
      <el-table-column prop="filename" label="文件名">
        <template #default="{ row }">
          <div style="display:flex;align-items:center;gap:8px">
            <span :class="`icon-filetype icon-filetype-${getFileExt(row.filename)}`" style="font-size:16px"></span>
            <span style="color:#FFA500;cursor:pointer" @click="emit('go-to', row)">{{ row.filename }}</span>
          </div>
        </template>
      </el-table-column>
      <el-table-column prop="fileSize" label="大小" width="100">
        <template #default="{ row }">
          <span style="color:#999;font-size:13px">{{ NumberUtils.formatFileSize(row.fileSize) }}</span>
        </template>
      </el-table-column>
      <el-table-column prop="uploadType" label="上传方式" width="90">
        <template #default="{ row }">
          <el-tag :type="row.uploadType === 'remote' ? 'info' : 'success'" size="small">{{ row.uploadType === 'remote' ? 'URL' : '本地上传' }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="操作" width="120">
        <template #default="{ row }">
          <div style="display:flex;gap:8px">
            <el-button size="small" text title="定位到目录" @click="emit('go-to', row)">
              <el-icon>
                <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor" width="14" height="14">
                  <path d="M12 2C8.13 2 5 5.13 5 9c0 5.25 7 13 7 13s7-7.75 7-13c0-3.87-3.13-7-7-7zm0 9.5c-1.38 0-2.5-1.12-2.5-2.5s1.12-2.5 2.5-2.5 2.5 1.12 2.5 2.5-1.12 2.5-2.5 2.5z"/>
                </svg>
              </el-icon>
            </el-button>
            <el-button size="small" text type="primary" title="下载" @click="emit('download', row)">
              <el-icon>
                <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor" width="14" height="14">
                  <path d="M19 9h-4V3H9v6H5l7 7 7-7zM5 18v2h14v-2H5z"/>
                </svg>
              </el-icon>
            </el-button>
          </div>
        </template>
      </el-table-column>
    </el-table>
  </div>
</template>

<script setup>
import { NumberUtils, TimeUtils } from '@/utils'

const props = defineProps({
  uploads: {
    type: Array,
    default: () => []
  }
})

const emit = defineEmits(['go-to', 'download'])

// 获取文件扩展名
const getFileExt = (filename) => {
  return filename?.split('.').pop()?.toLowerCase() || 'file'
}
</script>

<style lang="less">
.recent-uploads-full {
  max-height: 60vh;
  overflow-y: auto;
}
</style>