<template>
  <el-dialog v-model="show" :close-on-click-modal="false" title="上传状态" width="800px">
    <el-tabs v-model="activeTab">
      <el-tab-pane name="public" label="公共上传">
        <UploadTaskTable type="public" :visible="show" />
      </el-tab-pane>
      <el-tab-pane name="temp" label="临时上传">
        <UploadTaskTable type="temp" :visible="show" />
      </el-tab-pane>
      <el-tab-pane name="private" label="私有上传">
        <UploadTaskTable type="private" :visible="show" />
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
// 显式指定默认激活页签：el-tabs 无 v-model 时 currentName 默认为 "0"，
// 与各页签的 name(public/temp/private) 不匹配，会导致所有页签内容都不渲染/不可见
const activeTab = ref('public')

watch(() => props.show, (v) => { show.value = v })
watch(show, (v) => { emit('update:show', v) })
</script>
