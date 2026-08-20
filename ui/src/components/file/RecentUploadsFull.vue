<template>
  <div class="recent-uploads-full">
    <n-data-table
      :columns="columns"
      :data="uploads"
      :pagination="false"
      :row-key="row => row.id"
      striped
    />
  </div>
</template>

<script setup>
import { h } from 'vue'
import { NDataTable, NButton, NIcon, NTag, useMessage } from 'naive-ui'
import { NumberUtils, TimeUtils } from '@/utils'

const props = defineProps({
  uploads: {
    type: Array,
    default: () => []
  }
})

const emit = defineEmits(['go-to', 'download'])
const message = useMessage()

// 获取文件扩展名
const getFileExt = (filename) => {
  return filename?.split('.').pop()?.toLowerCase() || 'file'
}

// 下载图标
const DownloadIcon = () => h('svg', { xmlns: 'http://www.w3.org/2000/svg', viewBox: '0 0 24 24', fill: 'currentColor', width: 14, height: 14 }, [
  h('path', { d: 'M19 9h-4V3H9v6H5l7 7 7-7zM5 18v2h14v-2H5z' })
])

// 定位图标
const LocationIcon = () => h('svg', { xmlns: 'http://www.w3.org/2000/svg', viewBox: '0 0 24 24', fill: 'currentColor', width: 14, height: 14 }, [
  h('path', { d: 'M12 2C8.13 2 5 5.13 5 9c0 5.25 7 13 7 13s7-7.75 7-13c0-3.87-3.13-7-7-7zm0 9.5c-1.38 0-2.5-1.12-2.5-2.5s1.12-2.5 2.5-2.5 2.5 1.12 2.5 2.5-1.12 2.5-2.5 2.5z' })
])

// Table columns
const columns = [
  {
    title: '上传时间',
    key: 'uploadTime',
    width: 140,
    render(row) {
      const text = TimeUtils.formatDateTime(row.uploadTime)
      if (TimeUtils.isRecent24h(row.uploadTime)) {
        return h('span', { style: 'color: #18a058; font-size: 13px' }, text)
      }
      return h('span', { style: 'color: #666; font-size: 13px' }, text)
    }
  },
  {
    title: '文件名',
    key: 'filename',
    render(row) {
      const ext = getFileExt(row.filename)
      return h('div', { style: { display: 'flex', alignItems: 'center', gap: '8px' } }, [
        h('span', { class: `icon-filetype icon-filetype-${ext}`, style: { fontSize: '16px' } }),
        h('span', {
          style: { color: '#FFA500', cursor: 'pointer' },
          onClick: () => emit('go-to', row)
        }, row.filename)
      ])
    }
  },
  {
    title: '大小',
    key: 'fileSize',
    width: 100,
    render(row) {
      return h('span', { style: { color: '#999', fontSize: '13px' } }, NumberUtils.formatFileSize(row.fileSize))
    }
  },
  {
    title: '上传方式',
    key: 'uploadType',
    width: 90,
    render(row) {
      return h(NTag, { size: 'small', type: row.uploadType === 'remote' ? 'info' : 'success' }, () =>
        row.uploadType === 'remote' ? 'URL' : '本地上传'
      )
    }
  },
  {
    title: '操作',
    key: 'actions',
    width: 120,
    render(row) {
      return h('div', { style: { display: 'flex', gap: '8px' } }, [
        h(NButton, { size: 'small', quaternary: true, onClick: () => emit('go-to', row), title: '定位到目录' }, () => h(NIcon, null, () => h(LocationIcon))),
        h(NButton, { size: 'small', quaternary: true, type: 'primary', onClick: () => emit('download', row), title: '下载' }, () => h(NIcon, null, () => h(DownloadIcon)))
      ])
    }
  }
]
</script>

<style lang="less">
.recent-uploads-full {
  max-height: 60vh;
  overflow-y: auto;
}
</style>