<template>
  <n-modal v-model:show="show" :mask-closable="false" preset="card" title="上传管理" style="width: 800px; max-height: 80vh;">
    <n-tabs type="line" animated>
      <n-tab-pane name="public" tab="公共上传">
        <UploadTaskTable type="public" />
      </n-tab-pane>
      <n-tab-pane name="temp" tab="临时上传">
        <UploadTaskTable type="temp" />
      </n-tab-pane>
      <n-tab-pane name="private" tab="私有上传">
        <UploadTaskTable type="private" />
      </n-tab-pane>
    </n-tabs>
  </n-modal>
</template>

<script setup>
import { ref, watch } from 'vue'
import UploadTaskTable from './UploadTaskTable.vue'

const props = defineProps({
  show: Boolean
})
const emit = defineEmits(['update:show'])

const show = ref(props.show)
watch(() => props.show, (v) => { show.value = v })
watch(show, (v) => { emit('update:show', v) })
</script>