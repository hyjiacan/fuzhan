<template>
  <el-dialog v-model="show" :close-on-click-modal="false" title="上传状态" width="800px">
    <el-tabs>
      <el-tab-pane name="public" label="公共上传">
        <UploadTaskTable type="public" />
      </el-tab-pane>
      <el-tab-pane name="temp" label="临时上传">
        <UploadTaskTable type="temp" />
      </el-tab-pane>
      <el-tab-pane name="private" label="私有上传">
        <UploadTaskTable type="private" />
      </el-tab-pane>
    </el-tabs>
  </el-dialog>
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