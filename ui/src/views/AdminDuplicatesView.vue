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
          <el-table :data="visibleGroupFiles(group)" size="small" stripe :border="false">
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
            <div class="dup-group-more" v-if="(group.files || []).length > GROUP_SLICE">
              <el-button link type="primary" @click="toggleGroupFull(group)">
                {{ groupExpand.get(group.xxh3Hash) ? '收起' : `展开全部 ${group.files.length} 个` }}
              </el-button>
            </div>
          </el-collapse-item>
      </el-collapse>

      <div class="dup-pagination" v-if="dupTotal > 0">
        <el-pagination
          background
          layout="total, prev, pager, next"
          :total="dupTotal"
          :page-size="dupPageSize"
          :current-page="dupPage"
          :disabled="dupLoading"
          @current-change="onPageChange"
        />
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

// 单个重复组内默认只渲染前 GROUP_SLICE 条，展开后渲染全部，避免同哈希副本很多时一次性全量渲染。
const GROUP_SLICE = 30
// 记录各哈希组是否已展开全部（xxh3Hash -> bool）
const groupExpand = ref(new Map())

const isGroupExpanded = (hash) => groupExpand.value.get(hash) === true

// 客户端切片：未展开则只取前 GROUP_SLICE 条，展开则返回全部
const visibleGroupFiles = (group) => {
  const files = group.files || []
  if (isGroupExpanded(group.xxh3Hash)) return files
  return files.slice(0, GROUP_SLICE)
}

const toggleGroupFull = (group) => {
  const next = new Map(groupExpand.value)
  next.set(group.xxh3Hash, !isGroupExpanded(group.xxh3Hash))
  groupExpand.value = next
}

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

const onPageChange = (page) => {
  dupPage.value = page
  loadDuplicates()
}

async function loadDuplicates() {
  dupLoading.value = true
  try {
    const res = await IndexApi.listDuplicates({
      page: dupPage.value,
      pageSize: dupPageSize
    })
    if (res.success) {
      // 分页模式：每次请求直接替换当前页数据
      duplicateGroups.value = res.data.groups || []
      // 切页/刷新时清空各组的展开状态
      groupExpand.value = new Map()
      openGroups.value = []
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
    background: @bg-color;
    border-radius: @content-radius;
    box-shadow: @shadow-sm;
    padding: 16px;
    transition: box-shadow @transition-smooth;

    &:hover {
      box-shadow: @shadow-md;
    }
  }

  .dup-pagination {
    display: flex;
    justify-content: center;
    margin-top: 16px;
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