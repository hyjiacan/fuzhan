<template>
  <el-dropdown trigger="click" class="theme-toggle" @command="setMode">
    <el-icon class="theme-toggle-icon" :size="18">
      <Moon v-if="isDark" />
      <Sunny v-else />
    </el-icon>
    <template #dropdown>
      <el-dropdown-menu>
        <el-dropdown-item :command="'system'">
          <el-icon><Monitor /></el-icon>跟随系统
        </el-dropdown-item>
        <el-dropdown-item :command="'light'">
          <el-icon><Sunny /></el-icon>亮色
        </el-dropdown-item>
        <el-dropdown-item :command="'dark'">
          <el-icon><Moon /></el-icon>暗色
        </el-dropdown-item>
      </el-dropdown-menu>
    </template>
  </el-dropdown>
</template>

<script setup>
import { Sunny, Moon, Monitor } from '@element-plus/icons-vue'
import { useTheme } from '@/composables/useTheme'

const { isDark, setMode } = useTheme()
</script>

<style lang="less" scoped>
@import '@/styles/variables.less';

.theme-toggle-icon {
  cursor: pointer;
  transition: color @transition-fast;
}
</style>