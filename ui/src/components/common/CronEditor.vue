<template>
  <div class="cron-editor">
    <div class="cron-row">
      <el-select :model-value="mode" class="cron-mode" @change="onModeChange">
        <el-option v-for="opt in modeOptions" :key="opt.value" :label="opt.label" :value="opt.value" />
      </el-select>

      <el-select v-if="mode === 'weekly'" v-model="weekDay" class="cron-weekday">
        <el-option v-for="opt in weekDayOptions" :key="opt.value" :label="opt.label" :value="opt.value" />
      </el-select>

      <el-select v-if="mode === 'monthly'" v-model="monthDay" class="cron-day">
        <el-option v-for="d in 31" :key="d" :label="`每月 ${d} 日`" :value="d" />
      </el-select>

      <el-select v-if="mode === 'hourly'" v-model="minuteOfHour" class="cron-minute">
        <el-option v-for="m in 60" :key="m - 1" :label="`第 ${m - 1} 分钟`" :value="m - 1" />
      </el-select>

      <el-time-picker
        v-if="needsTime"
        v-model="time"
        class="cron-time"
        format="HH:mm"
        value-format="HH:mm"
        placeholder="选择时间"
      />

      <el-input
        v-if="mode === 'custom'"
        v-model="customText"
        class="cron-custom"
        placeholder="如 0 1 * * *（分 时 日 月 周）"
      />
    </div>

    <div class="cron-footer">
      <span class="cron-desc">{{ description }}</span>
      <span v-if="mode === 'custom'" class="cron-status" :class="isValid ? 'is-valid' : 'is-invalid'">
        {{ isValid ? '表达式有效' : '请输入 5 段 cron 表达式（分 时 日 月 周）' }}
      </span>
      <code v-else class="cron-expr">{{ expression }}</code>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, watch, onMounted } from 'vue'

const props = defineProps({
  // 5 段标准 cron 表达式（分 时 日 月 周），可能为空
  modelValue: {
    type: String,
    default: ''
  }
})

const emit = defineEmits(['update:modelValue'])

const DEFAULT_TIME = '01:00'

const modeOptions = [
  { label: '每天', value: 'daily' },
  { label: '每周', value: 'weekly' },
  { label: '每月', value: 'monthly' },
  { label: '每小时', value: 'hourly' },
  { label: '自定义', value: 'custom' }
]

const WEEK_LABELS = { 0: '日', 1: '一', 2: '二', 3: '三', 4: '四', 5: '五', 6: '六' }

const weekDayOptions = [1, 2, 3, 4, 5, 6, 0].map((value) => ({
  value,
  label: `周${WEEK_LABELS[value]}`
}))

const mode = ref('daily')
const time = ref(DEFAULT_TIME)
const weekDay = ref(1)
const monthDay = ref(1)
const minuteOfHour = ref(0)
const customText = ref('')

const needsTime = computed(() => ['daily', 'weekly', 'monthly'].includes(mode.value))

const pad2 = (n) => String(n).padStart(2, '0')

const timeParts = computed(() => {
  const [h, m] = (time.value || DEFAULT_TIME).split(':')
  return { h: Number(h) || 0, m: Number(m) || 0 }
})

// 依据当前模式生成 cron 表达式
const expression = computed(() => {
  const { h, m } = timeParts.value
  switch (mode.value) {
    case 'daily':
      return `${m} ${h} * * *`
    case 'weekly':
      return `${m} ${h} * * ${weekDay.value}`
    case 'monthly':
      return `${m} ${h} ${monthDay.value} * *`
    case 'hourly':
      return `${minuteOfHour.value} * * * *`
    default:
      return customText.value.trim()
  }
})

const description = computed(() => {
  const { h, m } = timeParts.value
  const t = `${pad2(h)}:${pad2(m)}`
  switch (mode.value) {
    case 'daily':
      return `每天 ${t} 执行`
    case 'weekly':
      return `每周${WEEK_LABELS[weekDay.value] || '一'} ${t} 执行`
    case 'monthly':
      return `每月 ${monthDay.value} 日 ${t} 执行`
    case 'hourly':
      return `每小时第 ${minuteOfHour.value} 分钟执行`
    default:
      return `自定义：${expression.value || '（空）'}`
  }
})

// 单段校验：允许 *、数字、*/n、a-b、a,b（宽松校验）
const inRange = (n, min, max) => Number.isInteger(n) && n >= min && n <= max

const checkPart = (part, min, max) => {
  if (!part) return false
  const segments = part.split('/')
  if (segments.length > 2) return false
  const [base, step] = segments
  if (step !== undefined && (!/^\d+$/.test(step) || Number(step) < 1)) return false
  if (base === '*') return true
  if (/^\d+$/.test(base)) return inRange(Number(base), min, max)
  if (/^\d+-\d+$/.test(base)) {
    const [start, end] = base.split('-').map(Number)
    return start <= end && inRange(start, min, max) && inRange(end, min, max)
  }
  return false
}

const checkSegment = (segment, min, max) =>
  segment.split(',').every((part) => checkPart(part.trim(), min, max))

const isValidCron = (expr) => {
  const value = (expr || '').trim()
  if (!value) return false
  const parts = value.split(/\s+/)
  if (parts.length !== 5) return false
  return (
    checkSegment(parts[0], 0, 59) &&
    checkSegment(parts[1], 0, 23) &&
    checkSegment(parts[2], 1, 31) &&
    checkSegment(parts[3], 1, 12) &&
    checkSegment(parts[4], 0, 7)
  )
}

// 反向解析：把已保存的表达式还原为可视化状态，无法识别的走自定义模式
const parseCron = (expr) => {
  const value = (expr || '').trim()
  if (!isValidCron(value)) return null
  const [m, h, d, mon, w] = value.split(/\s+/)
  const isNum = (s) => /^\d+$/.test(s)
  if (mon !== '*') return null
  if (isNum(m) && h === '*' && d === '*' && w === '*') {
    return { mode: 'hourly', minute: Number(m) }
  }
  if (isNum(m) && isNum(h) && d === '*' && w === '*') {
    return { mode: 'daily', time: `${pad2(Number(h))}:${pad2(Number(m))}` }
  }
  if (isNum(m) && isNum(h) && d === '*' && isNum(w)) {
    return { mode: 'weekly', time: `${pad2(Number(h))}:${pad2(Number(m))}`, weekDay: Number(w) % 7 }
  }
  if (isNum(m) && isNum(h) && isNum(d) && w === '*') {
    return { mode: 'monthly', time: `${pad2(Number(h))}:${pad2(Number(m))}`, monthDay: Number(d) }
  }
  return null
}

const isValid = computed(() => isValidCron(expression.value))

const emitIfValid = () => {
  const value = expression.value
  if (!isValidCron(value)) return
  if (value !== (props.modelValue || '').trim()) emit('update:modelValue', value)
}

const applyExpression = (expr) => {
  const parsed = parseCron(expr)
  if (!parsed) {
    mode.value = 'custom'
    customText.value = expr
    return
  }
  if (parsed.time) time.value = parsed.time
  if (parsed.weekDay !== undefined) weekDay.value = parsed.weekDay
  if (parsed.monthDay !== undefined) monthDay.value = parsed.monthDay
  if (parsed.minute !== undefined) minuteOfHour.value = parsed.minute
  mode.value = parsed.mode
  customText.value = expr
}

// 时间被清空时回落到默认时间，避免生成非法表达式
watch(time, (val) => {
  if (!val) time.value = DEFAULT_TIME
})

watch([time, weekDay, monthDay, minuteOfHour], () => {
  if (mode.value !== 'custom') emitIfValid()
})

watch(customText, () => {
  if (mode.value === 'custom') emitIfValid()
})

watch(
  () => props.modelValue,
  (val) => {
    const next = (val || '').trim()
    // 与本组件当前表达式一致时不再回写，避免循环
    if (next === expression.value) return
    applyExpression(next)
  }
)

const onModeChange = (next) => {
  if (next === mode.value) return
  const prevExpr = expression.value
  const parsed = parseCron(prevExpr)
  mode.value = next
  if (next === 'custom') {
    customText.value = parsed ? prevExpr : (props.modelValue || '').trim()
  } else if (parsed && parsed.mode === next) {
    if (parsed.time) time.value = parsed.time
    if (parsed.weekDay !== undefined) weekDay.value = parsed.weekDay
    if (parsed.monthDay !== undefined) monthDay.value = parsed.monthDay
    if (parsed.minute !== undefined) minuteOfHour.value = parsed.minute
  }
  emitIfValid()
}

onMounted(() => {
  const current = (props.modelValue || '').trim()
  if (current) {
    applyExpression(current)
  } else {
    emit('update:modelValue', expression.value)
  }
})

defineExpose({ validate: () => isValidCron(expression.value) })
</script>

<style lang="less" scoped>
@import '@/styles/variables.less';

.cron-editor {
  width: 100%;

  .cron-row {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: @spacing-sm;
  }

  .cron-mode,
  .cron-weekday {
    width: 110px;
  }

  .cron-day,
  .cron-minute,
  .cron-time {
    width: 130px;
  }

  .cron-custom {
    flex: 1 1 220px;
  }

  .cron-footer {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: @spacing-sm;
    margin-top: 4px;
    font-size: @font-size-xs;
    line-height: 1.5;
  }

  .cron-desc {
    color: @text-color-secondary;
  }

  .cron-expr {
    font-family: @font-family-mono;
    color: @text-color-placeholder;
    background: @bg-color-tertiary;
    padding: 1px 4px;
    border-radius: 3px;
  }

  .cron-status {
    &.is-valid {
      color: @success-color;
    }

    &.is-invalid {
      color: @error-color;
    }
  }
}
</style>
