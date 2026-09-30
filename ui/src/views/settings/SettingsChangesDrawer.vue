<template>
  <el-drawer
    :model-value="modelValue"
    title="未保存的更改"
    size="520px"
    direction="rtl"
    @update:model-value="$emit('update:modelValue', $event)"
  >
    <div v-if="groups.length === 0" class="changes-empty">
      <el-icon><CircleCheck /></el-icon>
      <span>暂无未保存的更改</span>
    </div>

    <template v-else>
      <div class="changes-summary">共 <strong>{{ changes.length }}</strong> 项改动，保存后生效</div>
      <div v-for="group in groups" :key="group.name" class="changes-group">
        <div
          class="changes-group-title"
          :class="{ 'is-link': !!group.panel }"
          :title="group.panel ? `跳转到「${group.name}」设置` : group.name"
          @click="handleNavigate(group)"
        >
          <span>{{ group.name }}</span>
          <span v-if="group.panel" class="changes-group-link">
            前往设置
            <el-icon><Right /></el-icon>
          </span>
        </div>
        <div v-for="item in group.items" :key="item.path" class="changes-item">
          <div class="changes-item-label" :title="item.path">{{ item.label }}</div>
          <div class="changes-item-values">
            <span class="val-old" :title="item.oldText">{{ item.oldText }}</span>
            <el-icon class="val-arrow"><Right /></el-icon>
            <span class="val-new" :title="item.newText">{{ item.newText }}</span>
          </div>
        </div>
      </div>
    </template>
  </el-drawer>
</template>

<script setup>
import { computed } from 'vue'
import { Right, CircleCheck } from '@element-plus/icons-vue'
import { groupChanges } from './useSettingsDiff'

const props = defineProps({
  modelValue: { type: Boolean, default: false },
  changes: { type: Array, default: () => [] }
})

const emit = defineEmits(['update:modelValue', 'navigate'])

const groups = computed(() => groupChanges(props.changes))

// 点击分组标题跳转到该分区对应的设置面板
const handleNavigate = (group) => {
  if (!group.panel) return
  emit('navigate', group.panel)
}
</script>

<style lang="less" scoped>
@import '@/styles/variables.less';

.changes-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 10px;
  padding: 64px 0;
  color: @text-color-placeholder;
  font-size: @font-size-sm;

  .el-icon {
    font-size: 32px;
    color: @success-color;
  }
}

.changes-summary {
  margin-bottom: @spacing-md;
  padding: 10px 12px;
  border-radius: @border-radius-lg;
  background: #fdf4e3;
  color: @warning-color;
  font-size: @font-size-sm;

  strong {
    font-size: @font-size-base;
  }
}

.changes-group {
  margin-bottom: @spacing-md;

  &-title {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    margin-bottom: @spacing-sm;
    padding: 4px 8px;
    border-left: 3px solid @primary-color;
    border-radius: @border-radius-sm;
    font-size: @font-size-sm;
    font-weight: 600;
    color: @text-color;
    transition: background-color @transition-fast;

    &.is-link {
      cursor: pointer;

      &:hover {
        background: @bg-color-tertiary;

        .changes-group-link {
          opacity: 1;
        }
      }
    }
  }

  &-link {
    display: inline-flex;
    align-items: center;
    gap: 2px;
    flex: 0 0 auto;
    font-size: @font-size-xs;
    font-weight: 400;
    color: @primary-color;
    opacity: 0.7;
    transition: opacity @transition-fast;
  }
}

.changes-item {
  padding: 10px 12px;
  border-radius: @border-radius;
  background: @bg-color-tertiary;

  & + & {
    margin-top: @spacing-sm;
  }

  &-label {
    margin-bottom: 6px;
    font-size: @font-size-sm;
    color: @text-color;
    word-break: break-all;
  }

  &-values {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
    font-size: @font-size-xs;
    word-break: break-all;
  }

  .val-old {
    color: @text-color-placeholder;
    text-decoration: line-through;
  }

  .val-arrow {
    flex: 0 0 auto;
    color: @text-color-placeholder;
  }

  .val-new {
    color: @success-color;
    font-weight: 500;
  }
}
</style>