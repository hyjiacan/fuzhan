<template>
  <div class="admin-duplicates">
    <div class="header-section">
      <div class="header-row">
        <span class="page-title">重复文件管理</span>
        <n-button @click="loadDuplicates" :loading="dupLoading" quaternary>
          刷新
        </n-button>
      </div>
      <div class="summary-text" v-if="duplicateGroups.length > 0">
        共 {{ dupTotal }} 组重复文件，按 xxh3 哈希分组
      </div>
    </div>

    <div class="content-section">
      <n-empty v-if="!dupLoading && duplicateGroups.length === 0" description="暂无重复文件" />

      <n-collapse v-else>
        <n-collapse-item
          v-for="(group, idx) in duplicateGroups"
          :key="group.xxh3Hash"
          :title="`#${idx + 1}  ${group.xxh3Hash.substring(0, 16)}...  (${group.fileCount} 个文件, ${formatSizeDup(group.totalSize)})`"
        >
          <n-data-table
            :columns="dupColumns"
            :data="group.files"
            :bordered="false"
            :single-line="true"
            striped
            size="small"
          />
        </n-collapse-item>
      </n-collapse>

      <n-space justify="center" :style="{ marginTop: '16px' }" v-if="dupTotal > dupPageSize">
        <n-button @click="dupPage++" :loading="dupLoading">加载更多</n-button>
      </n-space>
    </div>
  </div>
</template>

<script setup>
import { ref, h, onMounted } from 'vue'
import {
  NButton, NDataTable, NEmpty, NCollapse, NCollapseItem,
  NSpace, useMessage, useDialog
} from 'naive-ui'
import { IndexApi } from '@/api'
import { NumberUtils } from '@/utils'
import { formatErrorMessage } from '@/utils/error'

const message = useMessage()
const dialog = useDialog()

const duplicateGroups = ref([])
const dupLoading = ref(false)
const dupTotal = ref(0)
const dupPage = ref(1)
const dupPageSize = 20

const formatSizeDup = (bytes) => bytes === 0 ? '-' : NumberUtils.formatFileSize(bytes)

const dupColumns = [
  { title: '文件路径', key: 'fullPath', ellipsis: { tooltip: true } },
  { title: '大小', key: 'fileSize', width: 100,
    render: (row) => formatSizeDup(row.fileSize)
  },
  { title: '修改时间', key: 'modTime', width: 170 },
  {
    title: '操作',
    key: 'actions',
    width: 80,
    render(row) {
      return h(NButton, {
        size: 'small', type: 'primary', quaternary: true,
        onClick: () => handleKeepDuplicate(row)
      }, () => '保留')
    }
  }
]

async function loadDuplicates() {
  dupLoading.value = true
  try {
    const res = await IndexApi.listDuplicates({
      page: dupPage.value,
      pageSize: dupPageSize
    })
    if (res.success) {
      if (dupPage.value === 1) {
        duplicateGroups.value = res.data.groups || []
      } else {
        duplicateGroups.value = [...duplicateGroups.value, ...(res.data.groups || [])]
      }
      dupTotal.value = res.data.total || 0
    }
  } catch (err) {
    message.error(formatErrorMessage(err, '加载重复文件失败'))
  } finally {
    dupLoading.value = false
  }
}

async function handleKeepDuplicate(row) {
  dialog.warning({
    title: '确认保留',
    content: `确认保留 "${row.fileName}"，并删除其他同哈希的重复文件吗？此操作将删除磁盘文件且不可恢复。`,
    positiveText: '确认保留',
    negativeText: '取消',
    onPositiveClick: async () => {
      try {
        const res = await IndexApi.keepDuplicate(row.id)
        if (res.success) {
          message.success(`保留成功，已删除 ${res.data.deletedFiles.length} 个重复文件`)
          loadDuplicates()
        }
      } catch (err) {
        message.error(formatErrorMessage(err, '保留失败'))
      }
    }
  })
}

onMounted(() => {
  loadDuplicates()
})
</script>

<style lang="less" scoped>
@import '@/styles/variables.less';

.admin-duplicates {
  height: 100%;
  display: flex;
  flex-direction: column;
  padding: @container-padding;
  animation: slideUp 0.4s ease-out;

  .header-section {
    flex-shrink: 0;
    margin-bottom: 16px;

    .header-row {
      display: flex;
      justify-content: space-between;
      align-items: center;
      margin-bottom: 4px;

      .page-title {
        font-size: @font-size-lg;
        font-weight: 600;
        color: @text-color;
      }
    }

    .summary-text {
      color: @text-color-secondary;
      font-size: @font-size-sm;
    }
  }

  .content-section {
    flex: 1;
    overflow: auto;
    background: #fff;
    border-radius: @content-radius;
    box-shadow: @shadow-sm;
    padding: 16px;
    transition: box-shadow @transition-smooth;

    &:hover {
      box-shadow: @shadow-md;
    }
  }
}

@media @tablet {
  .admin-duplicates {
    padding: 12px;
  }
}

@media @mobile {
  .admin-duplicates {
    padding: 8px;
  }
}
</style>