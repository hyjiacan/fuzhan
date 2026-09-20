<template>
  <el-row :gutter="16">
    <el-col :span="12" :sm="24" class="field-col-cards">
      <el-card>
        <template #header><span>应用信息</span></template>
        <el-form label-width="120px">
          <el-form-item label="应用名称">
            <el-input v-model="props.settings.appName" :maxlength="64" placeholder="请输入应用名称" />
            <div class="field-hint">显示在页面标题和界面顶部的应用名称</div>
          </el-form-item>
        </el-form>
      </el-card>
    </el-col>

    <el-col :span="12" :sm="24" class="field-col-cards">
      <el-card>
        <template #header><span>共享目录配置</span></template>
        <div v-for="(dir, index) in props.settings.rootDirs" :key="index" class="dir-block">
          <el-form label-width="100px">
            <el-form-item label="目录路径">
              <el-input v-model="dir.path" :maxlength="1024" placeholder="目录路径" />
              <div class="field-hint">共享目录的绝对路径或相对路径</div>
            </el-form-item>
            <el-form-item label="显示名称">
              <el-input v-model="dir.name" :maxlength="255" placeholder="显示名称" />
              <div class="field-hint">在界面上显示的目录名称</div>
            </el-form-item>
            <el-form-item label="允许上传">
              <el-switch v-model="dir.uploadEnabled" />
              <div class="field-hint">关闭后上传界面无法选择该目录，web / FTP / WebDAV / URL 上传以及移动、复制文件进入该目录都会被拒绝（仅影响写入，不影响浏览与下载）</div>
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
const props = defineProps({
  settings: { type: Object, required: true }
})

const addDir = () => {
  props.settings.rootDirs.push({ path: '', name: '', uploadEnabled: true })
}

const removeDir = (index) => {
  props.settings.rootDirs.splice(index, 1)
}
</script>

<style lang="less" scoped>
@import '@/styles/variables.less';

.field-col-cards {
  margin-bottom: @spacing-md;

  :deep(.el-card) {
    transition: box-shadow @transition-smooth;

    &:hover {
      box-shadow: @shadow-md;
    }
  }
}

.dir-block {
  margin-bottom: @spacing-md;
  padding: @spacing-md;
  background: var(--el-fill-color-light);
  border-radius: @border-radius-lg;
}

.field-hint {
  color: @text-color-secondary;
  font-size: @font-size-xs;
}
</style>