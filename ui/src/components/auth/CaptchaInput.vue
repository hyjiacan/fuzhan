<template>
  <div v-if="visible" class="captcha-input">
    <el-form-item label="验证码" prop="captcha">
      <div class="captcha-row">
        <el-input
          v-model="text"
          placeholder="请输入验证码"
          :maxlength="4"
          clearable
        >
          <template #prefix><el-icon><Key /></el-icon></template>
        </el-input>
        <img v-if="image" :src="image" alt="验证码" title="点击刷新" class="captcha-image" @click="refresh" />
        <div v-else class="captcha-image captcha-placeholder" @click="refresh">点击刷新</div>
      </div>
    </el-form-item>
  </div>
</template>

<script setup>
import { ref, watch } from 'vue'
import { Key } from '@element-plus/icons-vue'
import { AuthApi } from '@/api'

const props = defineProps({
  visible: { type: Boolean, default: false },
  // 外部强制刷新信号：父组件在验证码被服务端销毁（如输入错误后重试）时递增此值，
  // 组件 watch 到变化即重新拉取，避免旧图残留导致用户无法重试
  refreshNonce: { type: Number, default: 0 }
})
const emit = defineEmits(['change'])

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
    // 拉取失败：清空图片与 id，显示「点击刷新」占位，避免旧图误导（旧 id 已被服务端销毁）
    id.value = ''
    image.value = ''
    text.value = ''
    emit('change', id.value, text.value)
  } finally {
    refreshing.value = false
  }
}

// 可见即拉取一次（验证码一次性，切换登录/注册时也重新拉取）
watch(() => props.visible, (v) => {
  if (v) refresh()
}, { immediate: true })

// 外部强制刷新：父组件校验失败（captcha_invalid）后递增 refreshNonce 触发重拉
watch(() => props.refreshNonce, (v) => {
  if (v > 0) refresh()
})
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

  .captcha-placeholder {
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 0 10px;
    color: @text-color-secondary;
    font-size: 12px;
    background: var(--el-fill-color-light);
  }
}
</style>
