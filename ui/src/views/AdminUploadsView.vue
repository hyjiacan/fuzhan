<template>
  <div class="admin-uploads">
    <div class="header-section">
      <h2>上传管理</h2>
      <p class="description">管理僵尸文件和 URL 上传任务</p>
    </div>

    <el-tabs v-model="activeTab">
      <!-- 僵尸文件标签页 -->
      <el-tab-pane name="zombie" label="僵尸文件">
        <!-- 统计卡片 -->
        <el-row :gutter="16" style="margin-bottom: 16px;">
          <el-col :span="8">
            <el-card class="stat-card" shadow="never">
              <div class="stat-content">
                <el-icon :size="32" color="#faad14"><ZombieIcon /></el-icon>
                <div class="stat-info">
                  <div class="stat-value">{{ zombieStats.zombieSessions }}</div>
                  <div class="stat-label">过期会话</div>
                </div>
              </div>
            </el-card>
          </el-col>
          <el-col :span="8">
            <el-card class="stat-card" shadow="never">
              <div class="stat-content">
                <el-icon :size="32" color="#1890ff"><FileIcon /></el-icon>
                <div class="stat-info">
                  <div class="stat-value">{{ zombieStats.totalSessions }}</div>
                  <div class="stat-label">总会话</div>
                </div>
              </div>
            </el-card>
          </el-col>
          <el-col :span="8">
            <el-card class="stat-card" shadow="never">
              <div class="stat-content">
                <el-icon :size="32" color="#f5222d"><TrashIcon /></el-icon>
                <div class="stat-info">
                  <div class="stat-value">{{ formatSize(zombieStats.totalSize) }}</div>
                  <div class="stat-label">占用空间</div>
                </div>
              </div>
            </el-card>
          </el-col>
        </el-row>

        <!-- 会话列表 -->
        <el-card header="过期会话列表" shadow="never">
          <template #header>
            <div style="display: flex; justify-content: space-between; align-items: center;">
              <span>过期会话列表</span>
              <el-space>
                <el-button size="small" type="danger" :disabled="zombieSelectedRowKeys.length === 0" @click="zombieCleanSelected" :loading="zombieCleaning">
                  清理选中 ({{ zombieSelectedRowKeys.length }})
                </el-button>
                <el-button size="small" type="warning" @click="zombieCleanAllExpired" :loading="zombieCleaning">
                  一键清理
                </el-button>
                <el-button size="small" @click="loadZombieSessions" :loading="zombieLoading">刷新</el-button>
              </el-space>
            </div>
          </template>

          <div ref="zombieWrapRef" class="table-v2-wrap" v-loading="zombieLoading">
            <el-table-v2
              :columns="zombieColumns"
              :data="zombieSessions"
              :width="zombieTableWidth"
              :height="zombieTableHeight"
              :row-height="TableConst.ROW_HEIGHT"
              row-key="id"
            />
          </div>
          <div class="pagination-wrap" v-if="zombieTotal > 0">
            <el-pagination
              v-model:current-page="zombiePage"
              v-model:page-size="zombiePageSize"
              :total="zombieTotal"
              :page-sizes="[10, 20, 50, 100]"
              layout="total, sizes, prev, pager, next"
              @current-change="onZombiePageChange"
              @size-change="onZombiePageSizeChange"
            />
          </div>
        </el-card>

        <!-- 确认清理对话框 -->
        <el-dialog v-model="zombieConfirmVisible" title="确认清理" width="400px">
          <el-alert v-if="zombieCleanMode === 'all'" type="warning" :closable="false">
            确定要一键清理全部过期/待处理/上传中的会话吗？系统将从后台重新查询，此操作将删除相关的残留文件，且不限于当前显示的这一页。
          </el-alert>
          <el-alert v-else type="warning" :closable="false">
            确定要清理这 {{ zombieSelectedSessions.length }} 个会话吗？此操作将删除相关的残留文件。
          </el-alert>
          <template #footer>
            <el-space>
              <el-button @click="zombieConfirmVisible = false">取消</el-button>
              <el-button type="danger" :loading="zombieCleaning" @click="zombieConfirmClean">确认清理</el-button>
            </el-space>
          </template>
        </el-dialog>
      </el-tab-pane>

      <!-- URL上传标签页 -->
      <el-tab-pane name="url-tasks" label="URL上传">
        <!-- 统计卡片 -->
        <el-row :gutter="16" style="margin-bottom: 16px;">
          <el-col :span="6">
            <el-card class="stat-card" shadow="never">
              <div class="stat-content">
                <div class="stat-info">
                  <div class="stat-value">{{ urlStats.total }}</div>
                  <div class="stat-label">全部任务</div>
                </div>
              </div>
            </el-card>
          </el-col>
          <el-col :span="6">
            <el-card class="stat-card" shadow="never">
              <div class="stat-content">
                <div class="stat-info">
                  <div class="stat-value" style="color: #52c41a;">{{ urlStats.completed }}</div>
                  <div class="stat-label">已完成</div>
                </div>
              </div>
            </el-card>
          </el-col>
          <el-col :span="6">
            <el-card class="stat-card" shadow="never">
              <div class="stat-content">
                <div class="stat-info">
                  <div class="stat-value" style="color: #ff4d4f;">{{ urlStats.failed }}</div>
                  <div class="stat-label">失败</div>
                </div>
              </div>
            </el-card>
          </el-col>
          <el-col :span="6">
            <el-card class="stat-card" shadow="never">
              <div class="stat-content">
                <div class="stat-info">
                  <div class="stat-value" style="color: #1890ff;">{{ urlStats.downloading }}</div>
                  <div class="stat-label">上传中/等待</div>
                </div>
              </div>
            </el-card>
          </el-col>
        </el-row>

        <!-- 过滤和操作栏 -->
        <el-card shadow="never">
          <template #header>
            <div style="display: flex; justify-content: space-between; align-items: center;">
              <span>URL 上传任务</span>
              <el-space>
                <el-button
                  size="small" type="danger"
                  :disabled="urlSelectedRowKeys.length === 0"
                  @click="batchDelete"
                >
                  删除选中 ({{ urlSelectedRowKeys.length }})
                </el-button>
                <el-button
                  size="small" type="warning"
                  :disabled="urlSelectedRowKeys.length === 0"
                  @click="batchRetry"
                >
                  重试选中 ({{ urlSelectedRowKeys.length }})
                </el-button>
                <el-button size="small" @click="loadURLTasks" :loading="urlLoading">刷新</el-button>
              </el-space>
            </div>
          </template>

          <!-- 状态过滤 -->
          <div style="margin-bottom: 12px;">
            <el-radio-group v-model="urlStatusFilter" @change="onURLStatusFilterChange">
              <el-radio-button value="">全部</el-radio-button>
              <el-radio-button value="pending">等待中</el-radio-button>
              <el-radio-button value="downloading">上传中</el-radio-button>
              <el-radio-button value="completed">已完成</el-radio-button>
              <el-radio-button value="failed">失败</el-radio-button>
            </el-radio-group>
          </div>

          <div ref="urlWrapRef" class="table-v2-wrap" v-loading="urlLoading">
            <el-table-v2
              :columns="urlColumns"
              :data="urlTasks"
              :width="urlTableWidth"
              :height="urlTableHeight"
              :row-height="TableConst.ROW_HEIGHT"
              row-key="id"
            />
          </div>
          <div class="pagination-wrap" v-if="urlTotal > urlPageSize">
            <el-pagination
              v-model:current-page="urlPage"
              v-model:page-size="urlPageSize"
              :total="urlTotal"
              :page-sizes="[10, 20, 50, 100]"
              layout="total, sizes, prev, pager, next"
              @current-change="loadURLTasks"
              @size-change="onURLPageSizeChange"
            />
          </div>
        </el-card>

        <!-- 确认删除对话框 -->
        <el-dialog v-model="deleteModalVisible" title="确认删除" width="400px">
          <el-alert type="warning" :closable="false">
            确定要删除选中的 {{ toDeleteTasks.length }} 个任务吗？相关文件也会被清理。
          </el-alert>
          <template #footer>
            <el-space>
              <el-button @click="deleteModalVisible = false">取消</el-button>
              <el-button type="danger" :loading="deleting" @click="confirmDelete">确认删除</el-button>
            </el-space>
          </template>
        </el-dialog>

        <!-- 确认重试对话框 -->
        <el-dialog v-model="retryModalVisible" title="确认重试" width="400px">
          <el-alert type="warning" :closable="false">
            确定要重试选中的 {{ toRetryTasks.length }} 个失败任务吗？
          </el-alert>
          <template #footer>
            <el-space>
              <el-button @click="retryModalVisible = false">取消</el-button>
              <el-button type="warning" :loading="retrying" @click="confirmRetry">确认重试</el-button>
            </el-space>
          </template>
        </el-dialog>
      </el-tab-pane>
    </el-tabs>
  </div>
</template>

<script setup>
import { TableConst } from '@/utils'
import { ref, h, onMounted, onUnmounted, nextTick } from 'vue'
import { useZombieSessions } from './admin/useZombieSessions'
import { useUrlTasks } from './admin/useUrlTasks'
import { formatSize } from './admin/uploadHelpers'

const activeTab = ref('zombie')

// ============ Icons ============
const FileIcon = () => h('svg', { xmlns: 'http://www.w3.org/2000/svg', viewBox: '0 0 24 24', fill: 'currentColor' }, [
  h('path', { d: 'M14 2H6c-1.1 0-2 .9-2 2v16c0 1.1.89 2 1.99 2H18c1.1 0 2-.9 2-2V8l-6-6zm4 18H6V4h7v5h5v11z' })
])
const ZombieIcon = () => h('svg', { xmlns: 'http://www.w3.org/2000/svg', viewBox: '0 0 24 24', fill: 'currentColor' }, [
  h('path', { d: 'M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm1 15h-2v-2h2v2zm0-4h-2V7h2v6z' })
])
const TrashIcon = () => h('svg', { xmlns: 'http://www.w3.org/2000/svg', viewBox: '0 0 24 24', fill: 'currentColor' }, [
  h('path', { d: 'M6 19c0 1.1.9 2 2 2h8c1.1 0 2-.9 2-2V7H6v12zM19 4h-3.5l-1-1h-5l-1 1H5v2h14V4z' })
])

// ============ 尺寸测量 ============
const zombieWrapRef = ref(null)
const urlWrapRef = ref(null)
const zombieTableWidth = ref(600)
const zombieTableHeight = ref(300)
const urlTableWidth = ref(600)
const urlTableHeight = ref(300)
let tableResizeObs = null
const updateTableSize = () => {
  const z = zombieWrapRef.value
  if (z && z.clientWidth > 0) {
    zombieTableWidth.value = z.clientWidth
    zombieTableHeight.value = z.clientHeight || 300
  }
  const u = urlWrapRef.value
  if (u && u.clientWidth > 0) {
    urlTableWidth.value = u.clientWidth
    urlTableHeight.value = u.clientHeight || 300
  }
}

// ============ 僵尸文件部分 ============
const zombie = useZombieSessions()
const {
  zombieLoading, zombieCleaning, zombieConfirmVisible,
  zombieCleanMode,
  zombieSelectedRowKeys, zombieSelectedSessions, zombieSessions, zombieStats,
  zombiePage, zombiePageSize, zombieTotal,
  zombieColumns, loadZombieSessions,
  onZombiePageChange, onZombiePageSizeChange,
  zombieCleanSelected, zombieCleanAllExpired, zombieConfirmClean
} = zombie

// ============ URL上传部分 ============
const url = useUrlTasks()
const {
  urlLoading, deleting, retrying, deleteModalVisible, retryModalVisible,
  toDeleteTasks, toRetryTasks, urlSelectedRowKeys, urlStatusFilter,
  urlPage, urlPageSize, urlTotal, urlTasks, urlStats, urlColumns,
  loadURLTasks, onURLStatusFilterChange, onURLPageSizeChange,
  batchDelete, batchRetry, confirmDelete, confirmRetry
} = url

onMounted(() => {
  updateTableSize()
  tableResizeObs = new ResizeObserver(updateTableSize)
  if (zombieWrapRef.value) tableResizeObs.observe(zombieWrapRef.value)
  if (urlWrapRef.value) tableResizeObs.observe(urlWrapRef.value)
  loadZombieSessions()
  loadURLTasks(() => nextTick(updateTableSize))
})

onUnmounted(() => {
  tableResizeObs?.disconnect()
})
</script>

<style lang="less" scoped>
@import '@/styles/variables.less';

.admin-uploads {
  display: flex;
  flex-direction: column;
  gap: 0;
  padding: @container-padding;
  animation: slideUp 0.4s ease-out;

  .header-section {
    margin-bottom: 16px;

    h2 {
      margin: 0 0 8px 0;
      font-size: @font-size-xxl;
      font-weight: 600;
    }

    .description {
      margin: 0;
      color: @text-color-secondary;
      font-size: @font-size-base;
    }
  }

  .stat-card :deep(.el-card__body) {
    padding: 16px;
  }

  .stat-content {
    display: flex;
    align-items: center;
    gap: 16px;
  }

  .stat-info {
    .stat-value {
      font-size: @font-size-xxl;
      font-weight: 600;
    }

    .stat-label {
      font-size: @font-size-base;
      color: @text-color-secondary;
    }
  }

  .table-v2-wrap {
    height: 420px;
  }

  .pagination-wrap {
    display: flex;
    justify-content: flex-end;
    padding: 16px 0 8px;
  }
}
</style>