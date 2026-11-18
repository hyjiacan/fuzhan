<template>
  <el-row :gutter="16">
    <el-col :span="12" :sm="24" class="field-col-cards">
      <el-card>
        <template #header><span>应用信息</span></template>
        <el-form autocomplete="off" label-width="120px">
          <el-form-item label="应用名称">
            <el-input autocomplete="off" v-model="props.settings.appName" :maxlength="64" placeholder="请输入应用名称" />
            <div class="field-hint">显示在页面标题和界面顶部的应用名称</div>
          </el-form-item>
        </el-form>
      </el-card>
    </el-col>

    <el-col :span="12" :sm="24" class="field-col-cards">
      <el-card>
        <template #header><span>共享目录配置</span></template>
        <div v-for="(dir, index) in props.settings.rootDirs" :key="index" class="dir-block">
          <el-form autocomplete="off" label-width="100px">
            <el-form-item label="目录路径">
              <el-input autocomplete="off" v-model="dir.path" :maxlength="1024" placeholder="目录路径">
                <template #prefix>
                  <el-select
                    class="partition-prefix-select"
                    size="small"
                    :model-value="detectPartition(dir.path)"
                    placeholder="分区"
                    :loading="partitionsLoading"
                    @change="(val) => applyPartition(index, val)"
                  >
                    <el-option
                      v-for="p in partitions"
                      :key="p.device + '|' + p.mountpoint"
                      :label="p.mountpoint"
                      :value="p.mountpoint"
                    >
                      <span :title="partitionLabel(p)">{{ p.mountpoint }}</span>
                      <span class="option-usage">剩余 {{ formatSize(p.free) }} / 共 {{ formatSize(p.total) }}</span>
                    </el-option>
                  </el-select>
                </template>
              </el-input>
              <div class="field-hint">可直接输入路径，或从左侧分区快速填入挂载点</div>
            </el-form-item>
            <el-form-item label="显示名称">
              <el-input autocomplete="off" v-model="dir.name" :maxlength="255" placeholder="显示名称" />
              <div class="field-hint">在界面上显示的目录名称</div>
            </el-form-item>
            <el-form-item label="允许上传">
              <el-switch v-model="dir.uploadEnabled" />
              <div class="field-hint">关闭后上传界面无法选择该目录，web / FTP / WebDAV / URL 上传以及移动、复制文件进入该目录都会被拒绝（仅影响写入，不影响浏览与下载）</div>
            </el-form-item>
            <el-form-item label="允许任意用户">
              <div>
                <div>
                  <div>
                    <el-switch v-model="dir.overwriteEnabled" />
                    <span>覆盖</span>
                  </div>
                  <div class="field-hint">开启后任意用户可覆盖该目录下已存在的文件；默认仅管理员或上传者本人可覆盖。对 Web / FTP / WebDAV / Open API 全部入口生效</div>
                </div>
                <div>
                  <div>
                    <el-switch v-model="dir.renameEnabled" />
                    <span>重命名</span>
                  </div>
                  <div class="field-hint">开启后任意用户可重命名该目录下已存在的文件；默认仅管理员或上传者本人可重命名。对 Web / FTP / WebDAV / Open API 全部入口生效</div>
                </div>
                <div>
                  <div>
                    <el-switch v-model="dir.moveEnabled" />
                    <span>移动</span>
                  </div>
                  <div class="field-hint">开启后任意用户可移动该目录下已存在的文件；默认仅管理员或上传者本人可移动。对 Web / FTP / WebDAV / Open API 全部入口生效</div>
                </div>
                <div>
                  <div>
                    <el-switch v-model="dir.deleteEnabled" />
                    <span>删除</span>
                  </div>
                  <div class="field-hint">开启后任意用户可删除该目录下已存在的文件；默认仅管理员或上传者本人可删除。对 Web / FTP / WebDAV / Open API 全部入口生效</div>
                </div>
              </div>
            </el-form-item>
          </el-form>
          <el-button type="danger" size="small" @click="removeDir(index)">删除</el-button>
        </div>
        <el-button plain block @click="addDir">添加共享目录</el-button>
      </el-card>
    </el-col>
  </el-row>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ConfigApi } from '@/api'
import { formatSize } from './useSettingsUtils'

const props = defineProps({
  settings: { type: Object, required: true }
})

// 分区列表（实时容量）
const partitions = ref([])
const partitionsLoading = ref(false)

const loadPartitions = async () => {
  partitionsLoading.value = true
  try {
    const resp = await ConfigApi.partitions()
    partitions.value = resp.data || []
  } catch (e) {
    partitions.value = []
  } finally {
    partitionsLoading.value = false
  }
}

onMounted(loadPartitions)

const partitionLabel = (p) => `${p.mountpoint}（${p.fsType}）剩余 ${formatSize(p.free)} / 共 ${formatSize(p.total)}`

// 比较前统一分隔符与大小写（Windows 反斜杠、盘符大小写）
const normalizePath = (p) => String(p || '').replace(/\\/g, '/').toLowerCase()
// 去掉尾部分隔符，便于按“挂载点 + 子路径”判断前缀
const trimTrailingSep = (p) => p.replace(/\/+$/, '')

// 按最长前缀匹配出当前路径所属的分区挂载点（用于回显下拉选项）
const detectPartition = (path) => {
  const target = trimTrailingSep(normalizePath(path))
  if (!target) return ''
  let best = ''
  let bestLen = -1
  for (const p of partitions.value) {
    const mp = normalizePath(p.mountpoint)
    if (!mp) continue
    const base = trimTrailingSep(mp)
    // 根分区（/）匹配所有绝对路径
    const matched = base === '' ? target.startsWith('/') : target === base || target.startsWith(base + '/')
    if (matched && mp.length > bestLen) {
      best = p.mountpoint
      bestLen = mp.length
    }
  }
  return best
}

// 选中分区后替换路径中的旧分区前缀并保留其余子路径；无匹配前缀时直接使用挂载点
const applyPartition = (index, mountpoint) => {
  const dir = props.settings.rootDirs[index]
  if (!dir || !mountpoint) return
  const current = dir.path || ''
  const matched = detectPartition(current)
  if (matched === mountpoint) return
  let rest = ''
  if (matched) {
    // 归一化仅替换分隔符与大小写，长度不变，可直接按长度截取剩余子路径
    const normCurrent = normalizePath(current)
    let consumed = normalizePath(matched).length
    if (normCurrent.charAt(consumed) === '/') consumed += 1
    rest = current.slice(consumed)
  }
  if (!rest) {
    dir.path = mountpoint
  } else if (/[\\/]$/.test(mountpoint)) {
    dir.path = mountpoint + rest
  } else {
    dir.path = mountpoint + '/' + rest
  }
}

const addDir = () => {
  props.settings.rootDirs.push({
    path: '', name: '', uploadEnabled: true,
    overwriteEnabled: false, renameEnabled: false, moveEnabled: false, deleteEnabled: false
  })
}

const removeDir = (index) => {
  props.settings.rootDirs.splice(index, 1)
}
</script>

<style lang="less" scoped>
@import '@/styles/variables.less';

.field-col-cards {
  margin-bottom: @spacing-md;
}

.dir-block {
  margin-bottom: @spacing-md;
  padding: @spacing-md;
  background: var(--el-fill-color-light);
  border-radius: @border-radius-lg;
}

.option-usage {
  float: right;
  color: @text-color-secondary;
  font-size: @font-size-xs;
  margin-left: 16px;
}

/* 目录路径输入框左侧的分区选择：去掉边框与背景，避免撑破输入框 */
.partition-prefix-select {
  width: 170px;
  margin-right: 6px;

  :deep(.el-select__wrapper) {
    background: transparent;
    box-shadow: none;
    padding-left: 0;
  }
}
</style>