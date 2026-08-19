<template>
  <div class="app-shell">
    <app-header />
    <div class="app-body">
      <router-view />
    </div>
    <app-footer />
  </div>
</template>

<script setup>
import AppHeader from '@/components/common/AppHeader.vue'
import AppFooter from '@/components/common/AppFooter.vue'
</script>

<style lang="less">
@import '@/styles/variables.less';

.app-shell {
  height: 100%;
  width: var(--app-width);
  position: relative;
}

.app-body {
  width: 100%;
  height: calc(100% - @header-height - @footer-height);
  position: relative;
  overflow: auto;
}
</style>
