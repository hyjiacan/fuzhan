<template>
  <el-dropdown trigger="click" class="theme-toggle" @command="setMode">
    <el-icon class="theme-toggle-icon" :size="18">
      <transition name="theme-icon" mode="out-in">
        <Moon v-if="isDark" key="moon" />
        <Sunny v-else key="sun" />
      </transition>
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

  &:hover {
    color: @primary-accent;
  }
}

.theme-icon-enter-active {
  transition: opacity @transition-smooth, transform @transition-bounce;
}

.theme-icon-leave-active {
  transition: opacity @transition-fast, transform @transition-fast;
}

.theme-icon-enter-from {
  opacity: 0;
  transform: rotate(-90deg) scale(0.6);
}

.theme-icon-leave-to {
  opacity: 0;
  transform: rotate(90deg) scale(0.6);
}
</style>