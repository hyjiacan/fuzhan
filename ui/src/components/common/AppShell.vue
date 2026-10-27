<template>
  <div class="app-shell">
    <app-header v-if="!standalone" />
    <div class="app-body" :class="{ 'is-standalone': standalone }">
      <router-view v-slot="{ Component }">
        <transition name="page-fade" mode="out-in">
          <component :is="Component" />
        </transition>
      </router-view>
    </div>
    <app-footer v-if="!standalone" />
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import AppHeader from '@/components/common/AppHeader.vue'
import AppFooter from '@/components/common/AppFooter.vue'

const route = useRoute()
// 独立布局页面（如登录页）不渲染公共 header/footer，占满整个视口
const standalone = computed(() => route.meta.standalone === true)
</script>

<style lang="less">
@import '@/styles/variables.less';

.app-shell {
  height: 100%;
  width: 100%;
  position: relative;
}

.app-body {
  width: 100%;
  height: calc(100% - @header-height - @footer-height);
  position: relative;
  overflow: auto;

  // 独立布局：占满整个视口
  &.is-standalone {
    height: 100%;
  }
}
</style>
