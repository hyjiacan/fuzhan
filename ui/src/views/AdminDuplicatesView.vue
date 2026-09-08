<template>
  <div class="admin-duplicates">
    <div class="header-section">
      <div class="header-row">
        <span class="page-title">重复文件管理</span>
        <el-button @click="loadDuplicates" :loading="dupLoading" link>
          刷新
        </el-button>
      </div>
      <div class="summary-text" v-if="duplicateGroups.length > 0">
        共 {{ dupTotal }} 组重复文件，按 xxh3 哈希分组
      </div>
    </div>

    <div class="content-section">
      <el-empty v-if="!dupLoading && duplicateGroups.length === 0" description="暂无重复文件" />

      <el-collapse v-else v-model="openGroups">
        <el-collapse-item
          v-for="(group, idx) in duplicateGroups"
          :key="group.xxh3Hash"
          :name="group.xxh3Hash"
          :title="groupTitle(group, idx)"
        >
          <!-- 表内嵌于可折叠面板中，容器高度动态变化，虚拟滚动(el-table-v2)难以稳定测量高度，故采用普通 el-table 实现 -->
          <el-table :data="group.files" size="small" stripe :border="false">
            <el-table-column label="文件路径" min-width="260">
              <template #default="{ row }">
                <div class="dup-path-cell">
                  <template v-for="(d, dirIdx) in parseDupPath(row.fullPath).dirs" :key="'d' + dirIdx">
                    <a class="path-segment" @click.prevent="goToDir(d.path)">{{ d.name }}</a>
                    <span class="path-sep">/</span>
                  </template>
                  <span class="dup-file-name" :title="parseDupPath(row.fullPath).fileName">
                    {{ parseDupPath(row.fullPath).fileName }}
                  </span>
                </div>
              </template>
            </el-table-column>
            <el-table-column label="大小" width="100">
              <template #default="{ row }">{{ formatSizeDup(row.fileSize) }}</template>
            </el-table-column>
            <el-table-column label="修改时间" width="170">
              <template #default="{ row }">{{ row.modTime ? TimeUtils.formatDateTime(row.modTime) : '-' }}</template>
            </el-table-column>
            <el-table-column label="操作" width="80">
              <template #default="{ row }">
                <el-button size="small" type="primary" link @click="handleKeepDuplicate(row)">保留</el-button>
              </template>
            </el-table-column>
          </el-table>
        </el-collapse-item>
      </el-collapse>

      <div style="display: flex; justify-content: center; margin-top: 16px;" v-if="dupTotal > dupPageSize">
        <el-button @click="dupPage++" :loading="dupLoading">加载更多</el-button>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted, computed } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { IndexApi } from '@/api'
import { NumberUtils, TimeUtils } from '@/utils'
import { formatErrorMessage } from '@/utils/error'

const router = useRouter()

const duplicateGroups = ref([])
const dupLoading = ref(false)
const dupTotal = ref(0)
const dupPage = ref(1)
const dupPageSize = 20
const openGroups = ref([])

const formatSizeDup = (bytes) => bytes === 0 ? '-' : NumberUtils.formatFileSize(bytes)

// 对路径的每段分别编码，避免斜杠被编码（与 HomeView 导航一致）
const encodePath = (path) => {
  return path.split('/').filter(Boolean).map(p => encodeURIComponent(p)).join('/')
}

// 跳转到文件页面的对应目录（/rootName/...）
const goToDir = (dirPath) => {
  router.push('/files/' + encodePath(dirPath))
}

// 把完整路径拆分为目录段（含可跳转路径）与文件名
const parseDupPath = (fullPath) => {
  const parts = String(fullPath || '').split('/').filter(Boolean)
  const fileName = parts[parts.length - 1] || ''
  const dirs = parts.slice(0, -1).map((name, idx) => ({
    name,
    path: '/' + parts.slice(0, idx + 1).join('/')
  }))
  return { dirs, fileName }
}

// 分组标题：文件名相同则追加文件名，不同则提示有 x 个文件名
const groupTitle = (group, idx) => {
  const base = `#${idx + 1}  ${group.xxh3Hash.substring(0, 16)}...  (${group.fileCount} 个文件, ${formatSizeDup(group.totalSize)})`
  const nameSet = new Set((group.files || []).map(f => f.fileName).filter(Boolean))
  if (nameSet.size === 1) return `${base} · ${[...nameSet][0]}`
  if (nameSet.size > 1) return `${base} · 有 ${nameSet.size} 个文件名`
  return base
}

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
    ElMessage.error(formatErrorMessage(err, '加载重复文件失败'))
  } finally {
    dupLoading.value = false
  }
}

async function handleKeepDuplicate(row) {
  try {
    await ElMessageBox.confirm(
      `确认保留 "${row.fileName}"，并删除其他同哈希的重复文件吗？此操作将删除磁盘文件且不可恢复。`,
      '确认保留',
      {
        confirmButtonText: '确认保留',
        cancelButtonText: '取消',
        type: 'warning',
        appendTo: document.body,
        customClass: 'dup-keep-confirm'
      }
    )
  } catch {
    return
  }
  try {
    const res = await IndexApi.keepDuplicate(row.id)
    if (res.success) {
      ElMessage.success(`保留成功，已删除 ${res.data.deletedFiles.length} 个重复文件`)
      loadDuplicates()
    }
  } catch (err) {
    ElMessage.error(formatErrorMessage(err, '保留失败'))
  }
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

  .dup-path-cell {
    display: flex;
    align-items: center;
    flex-wrap: nowrap;
    overflow: hidden;

    .path-segment {
      color: @primary-color;
      cursor: pointer;
      white-space: nowrap;

      &:hover {
        text-decoration: underline;
      }
    }

    .path-sep {
      margin: 0 2px;
      color: @text-color-placeholder;
    }

    .dup-file-name {
      margin-left: 2px;
      color: @text-color;
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
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

<!-- 保留确认弹框通过 appendTo: body 挂载到 body，需全局样式保证居中且内容清晰 -->
<style>
.dup-keep-confirm {
  .el-message-box__message {
    word-break: break-word;
    line-height: 1.6;
    color: #303133;
  }
}
</style>