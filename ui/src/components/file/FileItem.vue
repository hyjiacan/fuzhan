<template>
  <div class="file-item" :class="{ 'is-dir': isDir, 'is-file': !isDir }">
    <div class="file-icon">
      <span v-html="isDir ? folderIcon : fileIcon"></span>
    </div>
    <div class="file-info">
      <div class="file-name">{{ fileName }}</div>
      <div class="file-meta">
        <span class="file-size">{{ formattedSize }}</span>
        <span class="file-time">{{ formattedTime }}</span>
      </div>
    </div>
    <div class="file-actions" v-if="showActions">
      <el-button
        v-if="!isDir && canPreview"
        size="small"
        @click="$emit('preview', fileInfo)"
      >
        预览
      </el-button>
      <el-button
        size="small"
        type="primary"
        @click="$emit('download', fileInfo)"
      >
        下载
      </el-button>
    </div>
  </div>
</template>

<script setup>
import { computed, h } from 'vue'
import { FileUtils } from '@/utils'

const props = defineProps({
  fileInfo: { type: Object, required: true },
  showActions: { type: Boolean, default: true }
})

const emit = defineEmits(['preview', 'download'])

const isDir = computed(() => props.fileInfo.isDir)
const fileName = computed(() => props.fileInfo.name)
const formattedSize = computed(() => props.fileInfo.size || '-')
const formattedTime = computed(() => props.fileInfo.modTime || '')

const canPreview = computed(() => {
  if (isDir.value) return false
  return FileUtils.isTextFile(fileName.value) || FileUtils.isImageFile(fileName.value)
})

const folderIcon = '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="#FFA500" width="24" height="24"><path d="M10 4H4c-1.1 0-1.99.9-1.99 2L2 18c0 1.1.9 2 2 2h16c1.1 0 2-.9 2-2V8c0-1.1-.9-2-2-2h-8l-2-2z"/></svg>'
const fileIcon = '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="#666" width="24" height="24"><path d="M14 2H6c-1.1 0-1.99.9-1.99 2L4 20c0 1.1.89 2 1.99 2H18c1.1 0 2-.9 2-2V8l-6-6zm2 16H8v-2h8v2zm0-4H8v-2h8v2zm-3-5V3.5L18.5 9H13z"/></svg>'
</script>

<style lang="less">
.file-item {
  display: flex;
  align-items: center;
  padding: 12px 16px;
  border-bottom: 1px solid #eee;
  transition: background-color 0.2s;

  &:hover {
    background-color: #f5f7fa;
  }

  .file-icon {
    margin-right: 12px;
    display: flex;
    align-items: center;
  }

  .file-info {
    flex: 1;
    min-width: 0;

    .file-name {
      font-size: 14px;
      color: #333;
      margin-bottom: 4px;
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
    }

    .file-meta {
      display: flex;
      font-size: 12px;
      color: #999;

      .file-size {
        margin-right: 16px;
      }
    }
  }

  .file-actions {
    display: flex;
    gap: 8px;
  }
}
</style>