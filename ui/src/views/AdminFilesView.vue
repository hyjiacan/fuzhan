<template>
  <div class="admin-files">
    <!-- Header -->
    <div class="content-header">
      <div class="breadcrumb-actions">
        <!-- 搜索状态显示（与 HomeView 一致） -->
        <div v-if="isSearching || searchCompleted" class="search-status">
          <el-icon v-if="isSearching && searchResultCount < 0" class="is-loading" size="14"><Loading /></el-icon>
          <template v-else>
            <span v-if="isSearching">搜索中...</span>
            <span v-else>搜索完成</span>
            <span class="search-count">{{ searchResultCount }} 个结果</span>
            <span class="search-time">耗时 {{ searchTime }}ms</span>
          </template>
          <el-button v-if="!isSearching" size="small" link @click="clearSearch" title="清除搜索">
            ×
          </el-button>
        </div>
        <el-breadcrumb v-else-if="breadcrumb.length > 1" class="breadcrumb" separator="/">
          <el-breadcrumb-item v-for="(item, index) in breadcrumb" :key="index">
            <span class="breadcrumb-link" @click="navigateToBreadcrumb(index)">{{ item.name }}</span>
          </el-breadcrumb-item>
        </el-breadcrumb>
        <span v-else class="breadcrumb-root">文件管理</span>
        <div class="header-actions">
          <el-input ref="searchInputRef" v-model="searchQuery" :maxlength="200" placeholder="搜索文件..." size="small"
            class="search-input" clearable name="search-query" @keydown.enter="searchFiles" />
          <el-button size="small" @click="loadCurrentDir" :loading="loading">刷新</el-button>
          <el-button v-if="checkedRowKeys.length > 0" size="small" type="danger" @click="handleBatchDelete">
            删除选中 ({{ checkedRowKeys.length }})
          </el-button>
          <el-divider direction="vertical" class="action-divider" />
          <el-button size="small" type="primary" @click="handleScan" :loading="scanning">
            触发全量扫描
          </el-button>
          <span v-if="scanProgress.status === 'running'" class="scan-progress-text">
            扫描中: {{ scanProgress.scannedFiles }} / {{ scanProgress.totalFiles }}
          </span>
          <span v-else-if="scanProgress.status === 'completed'" class="scan-progress-text completed">
            扫描完成
          </span>
          <span v-else-if="scanProgress.status === 'failed'" class="scan-progress-text failed">
            扫描失败: {{ scanProgress.errorMessage }}
          </span>
        </div>
      </div>
      </div>

      <!-- 文件浏览 -->
    <div class="content-table">
      <div ref="tableWrapRef" class="table-v2-wrap" v-loading="loading || isSearching">
        <el-table-v2
          :columns="columns"
          :data="displayList"
          :width="tableWidth"
          :height="tableHeight"
          :row-height="32"
          row-key="path"
          @row-dblclick="handleDblClick"
        />
      </div>
    </div>

    <!-- 移动/重命名对话框（类似 Linux mv 命令） -->
    <el-dialog v-model="moveModalVisible" title="移动或重命名" :width="moveDialogWidth">
      <el-form label-width="100px">
        <el-form-item label="文件名">
          <el-input :model-value="currentFile?.name" disabled />
        </el-form-item>
        <el-form-item label="当前路径">
          <el-input :model-value="currentFile?.path" disabled />
        </el-form-item>
        <el-form-item label="目标路径">
          <div class="target-path-input">
            <el-input :model-value="targetRootName" disabled placeholder="根目录" class="root-input" />
            <span class="path-separator">/</span>
            <el-input v-model="targetSubPath" :maxlength="1024" placeholder="输入子目录/文件名" class="sub-path-input" />
          </div>
          <div class="help-text">
            <p>输入目标路径（相对于根目录），如 <code>newname.pdf</code> 或 <code>subdir/newname.pdf</code></p>
          </div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-space>
          <el-button @click="moveModalVisible = false">取消</el-button>
          <el-button type="primary" :loading="moving" @click="handleMove">确定</el-button>
        </el-space>
      </template>
    </el-dialog>

    <!-- 备注编辑弹窗（与文件页面一致） -->
    <el-dialog v-model="notesModalVisible" title="编辑备注" width="500px" :class="notesMaximized ? 'preview-maximized' : ''">
      <el-input v-model="editNotes" type="textarea" :rows="4" maxlength="4096" placeholder="输入备注内容..." />
      <template #footer>
        <div style="display: flex; justify-content: flex-end; gap: 8px;">
          <el-button @click="notesMaximized = !notesMaximized">{{ notesMaximized ? '还原' : '最大化' }}</el-button>
          <el-button @click="notesModalVisible = false">取消</el-button>
          <el-button type="primary" :loading="savingNotes" @click="saveNotes">保存</el-button>
        </div>
      </template>
    </el-dialog>

    <!-- 预览对话框 -->
    <el-dialog v-model="previewDialogVisible" title="文件预览" width="700px"
      :class="['preview-dialog', previewMaximized ? 'preview-maximized' : '']"
      :style="previewMaximized ? { width: '100vw', maxWidth: '100vw' } : {}">
      <file-preview :file="previewFileData" :maximized="previewMaximized"
        @close="previewDialogVisible = false" />
      <template #footer>
        <div style="display: flex; justify-content: flex-end; gap: 8px;">
          <el-button @click="previewMaximized = !previewMaximized">{{ previewMaximized ? '还原' : '最大化' }}</el-button>
          <el-button type="primary" @click="downloadFile(previewFileData)">下载</el-button>
          <el-button @click="previewDialogVisible = false">关闭</el-button>
        </div>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, watch, onUnmounted, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { Loading } from '@element-plus/icons-vue'
import FilePreview from '@/components/file/FilePreview.vue'
import store from '@/store'
import { FileRecordApi } from '@/api'
import { formatErrorMessage } from '@/utils/error'
import { useFilesTable } from './admin/useFilesTable'
import { useFilesSearch } from './admin/useFilesSearch'
import { useFileActions } from './admin/useFileActions'
import { buildFileColumns, isDir } from './admin/fileTableColumns'

const route = useRoute()
const router = useRouter()

const table = useFilesTable(router)
const search = useFilesSearch({
  store,
  router,
  fileList: table.fileList,
  checkedRowKeys: table.checkedRowKeys,
  loadCurrentDir: table.loadCurrentDir
})
// 勾选应基于当前显示列表（搜索时即搜索结果），此处注入 displayList 源
table.setListSource(() => search.displayList.value)

const actions = useFileActions({
  currentPath: table.currentPath,
  currentFile: table.currentFile,
  checkedRowKeys: table.checkedRowKeys,
  loadCurrentDir: table.loadCurrentDir,
  displayList: search.displayList
})

// ============ 备注编辑 ============
const notesModalVisible = ref(false)
const notesMaximized = ref(false)
const editNotes = ref('')
const editNotesRow = ref(null)
const savingNotes = ref(false)

const openNotesEditor = (row) => {
  editNotesRow.value = row
  editNotes.value = row.notes || ''
  notesModalVisible.value = true
}

const saveNotes = async () => {
  const row = editNotesRow.value
  if (!row) {
    ElMessage.warning('无法获取文件记录')
    return
  }
  savingNotes.value = true
  try {
    let recordId = row.recordId
    if (!recordId) {
      const fullPath = row.path || ''
      const rootName = row.rootName || ''
      const fileName = row.name || ''
      const indexPath = rootName ? fullPath.slice(rootName.length + 1) : fullPath
      const findRes = await FileRecordApi.findRecord(fileName, rootName, indexPath)
      if (!findRes.success || !findRes.data?.record) {
        ElMessage.warning('未找到文件索引记录，请稍后重试')
        return
      }
      recordId = findRes.data.record.id
    }
    const res = await FileRecordApi.updateNotes(recordId, editNotes.value)
    if (res.success) {
      ElMessage.success('备注已更新')
      row.notes = editNotes.value
      store.setFileNotes(row.path, editNotes.value, recordId)
      notesModalVisible.value = false
    } else {
      ElMessage.error(res.message || '更新备注失败')
    }
  } catch (e) {
    ElMessage.error(formatErrorMessage(e, '更新备注失败'))
  } finally {
    savingNotes.value = false
  }
}

// 列定义由独立模块构建，渲染回调从复合式取用
const columns = buildFileColumns({
  ...table, ...search, ...actions, router,
  openNotesEditor
})

// 解构给模板使用的绑定（<script setup> 仅顶层绑定可被模板访问）
const { loading, breadcrumb, currentFile, checkedRowKeys, tableWrapRef, tableWidth, tableHeight, loadCurrentDir, navigateToBreadcrumb } = table
const { searchQuery, searchInputRef, isSearching, searchCompleted, searchResultCount, searchTime, displayList, searchFiles, clearSearch } = search
const {
  scanning, scanProgress, handleScan,
  moveModalVisible, targetRootName, targetSubPath, moving, moveDialogWidth, handleMove,
  previewDialogVisible, previewMaximized, previewFileData, downloadFile,
  handleBatchDelete
} = actions

// 双击行
const handleDblClick = (row) => {
  if (isDir(row)) {
    table.enterDir(row)
  }
}

// 路由变化：清空搜索态并切换目录
watch(
  () => route.params.pathMatch,
  async (newPathMatch) => {
    search.resetFromRoute()
    await table.handleRouteChange(newPathMatch)
  },
  { immediate: true }
)

onMounted(() => {
  actions.updateMoveDialogWidth()
  window.addEventListener('resize', actions.updateMoveDialogWidth)
  table.setupTableResize()
})

onUnmounted(() => {
  actions.stopPollProgress()
  window.removeEventListener('resize', actions.updateMoveDialogWidth)
  table.teardownTableResize()
})
</script>

<style lang="less" scoped>
@import '@/styles/variables.less';

.admin-files {
  height: 100%;
  display: flex;
  flex-direction: column;
  padding: @container-padding;
  animation: slideUp 0.4s ease-out;

  .content-header {
    margin-bottom: 12px;
    flex-shrink: 0;

    .breadcrumb-actions {
      display: flex;
      justify-content: space-between;
      align-items: center;
      gap: 12px;

      .search-status {
        flex: 1;
        font-size: 14px;
        color: #666;
        display: flex;
        align-items: center;
        gap: 12px;

        .search-count {
          color: @primary-color;
          font-weight: 500;
        }

        .search-time {
          color: #999;
        }
      }

      .breadcrumb {
        flex: 1;

        .breadcrumb-link {
          cursor: pointer;
          color: #444;
          transition: color @transition-fast;

          &:hover {
            color: @primary-color;
          }
        }
      }

      .breadcrumb-root {
        font-size: @font-size-base;
        font-weight: 500;
        color: @text-color;
      }

      .header-actions {
        display: flex;
        gap: 8px;
        flex-shrink: 0;

        .search-input {
          width: 200px;
        }
      }
    }
  }

  .scan-progress-text {
    font-size: @font-size-sm;
    color: @text-color-secondary;

    &.completed {
      color: @success-color;
    }

    &.failed {
      color: @error-color;
    }
  }

  .action-divider {
    height: 24px;
  }

  .content-table {
    flex: 1;
    min-height: 0;
    background: #fff;
    border-radius: @content-radius;
    box-shadow: @shadow-sm;
    transition: box-shadow @transition-smooth;

    &:hover {
      box-shadow: @shadow-md;
    }

    .table-v2-wrap {
      height: 100%;
    }
  }

  
}

.file-name-cell {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
  overflow: hidden;

  &:hover {
    .icon-filetype {
      color: #e98b4c;
    }

    .path-segment,
    .file-link {
      color: #FF6600;
    }
  }

  .file-icon-wrapper {
    display: flex;
    align-items: center;
    gap: 4px;
    flex-shrink: 0;

    .icon-filetype {
      color: #888;
    }
  }

  .file-path-content {
    display: inline-flex;
    align-items: center;
    min-width: 0;
    overflow: hidden;
  }

  .file-link {
    display: block;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
}

.icon-filetype {
  color: #888;
  font-size: 20px;
}

.file-link {
  color: #444;
  cursor: pointer;
}

.dir-link {
  cursor: pointer;
  color: #444;

  &:hover {
    color: @primary-color;
  }
}

.help-text {
  font-size: 13px;
  color: @text-color-secondary;
  line-height: 1.8;
  padding: 8px 12px;
  background: @bg-color-secondary;
  border-radius: 4px;
  border-left: 3px solid @primary-color;
  margin-top: 8px;

  p {
    margin: 4px 0;
  }

  strong {
    color: @text-color;
  }

  code {
    background: @bg-color-tertiary;
    padding: 1px 6px;
    border-radius: 3px;
    font-family: monospace;
    color: @primary-color;
  }
}

.target-path-input {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;

  .root-input {
    width: 120px;
    flex-shrink: 0;
  }

  .path-separator {
    color: @text-color-secondary;
    font-size: 16px;
    flex-shrink: 0;
  }

  .sub-path-input {
    flex: 1;
  }
}

@media @tablet {
  .admin-files {
    padding: 12px;
  }

  .breadcrumb-actions {
    flex-wrap: wrap;

    .header-actions {
      width: 100%;
      .search-input {
        flex: 1;
        min-width: 0;
      }
    }
  }
}

@media @mobile {
  .admin-files {
    padding: 8px;
  }
}
</style>