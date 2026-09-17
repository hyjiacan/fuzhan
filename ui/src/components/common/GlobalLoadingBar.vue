<template>
  <transition name="fade">
    <div v-if="isLoading" class="global-loading-bar">
      <div class="loading-bar-inner" />
    </div>
  </transition>
</template>

<script setup>
import { useRequestLoading } from '@/utils/requestLoading'

const { isLoading } = useRequestLoading()
</script>

<style lang="less" scoped>
.global-loading-bar {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  height: 2px;
  z-index: 3000;
  overflow: hidden;
  pointer-events: none;

  .loading-bar-inner {
    height: 100%;
    width: 40%;
    background: var(--el-color-primary, #b25a00);
    border-radius: 2px;
    animation: global-loading-slide 1.1s ease-in-out infinite;
  }
}

@keyframes global-loading-slide {
  0% {
    transform: translateX(-100%);
  }
  100% {
    transform: translateX(350%);
  }
}

.fade-enter-active,
.fade-leave-active {
  transition: opacity 0.2s ease;
}

.fade-enter-from,
.fade-leave-to {
  opacity: 0;
}
</style>