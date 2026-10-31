<template>
  <div v-if="visible" class="captcha-input">
    <el-form-item label="验证码" prop="captcha">
      <div class="captcha-row">
        <el-input
          v-model="text"
          placeholder="请输入验证码"
          :maxlength="4"
          clearable
          @keyup.enter="onEnter"
        >
          <template #prefix><el-icon><Key /></el-icon></template>
        </el-input>
        <img v-if="image" :src="image" alt="验证码" title="点击刷新" class="captcha-image" @click="refresh" />
      </div>
    </el-form-item>
  </div>
</template>

<script setup>
import { ref, watch } from 'vue'
import { Key } from '@element-plus/icons-vue'
import { AuthApi } from '@/api'

const props = defineProps({
  visible: { type: Boolean, default: false }
})
const emit = defineEmits(['change', 'enter'])

const image = ref('')
const text = ref('')
const id = ref('')
// 点击刷新防抖，避免连点触发验证码接口限流（IP 每 30 秒/分钟 30 张）
const refreshing = ref(false)

async function refresh() {
  if (refreshing.value) return
  refreshing.value = true
  try {
    const res = await AuthApi.getCaptcha()
    id.value = res?.data?.captchaId || ''
    image.value = res?.data?.image || ''
    text.value = ''
    emit('change', id.value, text.value)
  } catch (e) {
    // 拉取失败静默处理，保留上一张图；用户可点击重试
  } finally {
    refreshing.value = false
  }
}

// 可见即拉取一次（验证码一次性，切换登录/注册时也重新拉取）
watch(() => props.visible, (v) => {
  if (v) refresh()
}, { immediate: true })

function onEnter() {
  emit('enter')
}

watch(text, () => emit('change', id.value, text.value))
</script>

<style lang="less" scoped>
@import '@/styles/variables.less';

.captcha-row {
  display: flex;
  align-items: center;
  gap: 12px;
  width: 100%;

  :deep(.el-input) {
    flex: 1;
  }

  .captcha-image {
    flex: 0 0 auto;
    height: 40px;
    border: 1px solid var(--el-border-color);
    border-radius: @border-radius-sm;
    cursor: pointer;
    user-select: none;
  }
}
</style>
