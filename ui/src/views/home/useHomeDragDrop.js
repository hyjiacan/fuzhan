import { ref, watch, nextTick } from 'vue'
import { ElMessage } from 'element-plus'

// 文件页页面级拖放上传：显示提醒遮罩，松开鼠标后打开上传弹框并把文件注入 UploadManager 本地上传队列
export function useHomeDragDrop(uploadManagerRef) {
  const uploadDialogVisible = ref(false)
  const isPageDragging = ref(false)
  const dragFileCount = ref(0)
  const pendingDropFiles = ref([])
  let pageDragCounter = 0
  // 是否拖入了文件（而非普通元素拖拽）
  const hasFileDrag = (e) => Array.from(e?.dataTransfer?.types || []).includes('Files')

  const handlePageDragEnter = (e) => {
    if (!hasFileDrag(e)) return
    // 上传弹框已打开时不显示页面遮罩（由弹框内的 drop zone 接管）
    if (uploadDialogVisible.value) return
    e.dataTransfer.dropEffect = 'copy'
    pageDragCounter++
    isPageDragging.value = true
    dragFileCount.value = e.dataTransfer?.files?.length || 0
  }

  const handlePageDragOver = (e) => {
    if (!isPageDragging.value && hasFileDrag(e) && !uploadDialogVisible.value) {
      pageDragCounter = Math.max(pageDragCounter, 1)
      isPageDragging.value = true
      dragFileCount.value = e.dataTransfer?.files?.length || 0
    }
    e.dataTransfer.dropEffect = 'copy'
  }

  const handlePageDragLeave = (e) => {
    if (!hasFileDrag(e)) return
    pageDragCounter = Math.max(0, pageDragCounter - 1)
    if (pageDragCounter === 0) {
      isPageDragging.value = false
    }
  }

  const handlePageDrop = (e) => {
    pageDragCounter = 0
    isPageDragging.value = false
    const dropFiles = e.dataTransfer?.files
    if (!dropFiles || dropFiles.length === 0) return
    // 打开上传弹框，待组件挂载后把拖放文件加入本地上传队列
    pendingDropFiles.value = Array.from(dropFiles)
    uploadDialogVisible.value = true
  }

  // 上传弹框打开后，将拖放文件注入 UploadManager 的本地上传队列
  watch(uploadDialogVisible, async (visible, prev) => {
    if (visible && prev === false && pendingDropFiles.value.length > 0) {
      await nextTick()
      if (uploadManagerRef.value) {
        const files = pendingDropFiles.value
        pendingDropFiles.value = []
        files.forEach((file, idx) => {
          uploadManagerRef.value.addFile({ name: file.name, size: file.size, file })
          if (idx === files.length - 1) ElMessage.success(`已添加 ${files.length} 个文件到上传队列`)
        })
      }
    }
  })

  const showUploadDialog = () => { uploadDialogVisible.value = true }

  return {
    uploadDialogVisible,
    isPageDragging,
    dragFileCount,
    handlePageDragEnter,
    handlePageDragOver,
    handlePageDragLeave,
    handlePageDrop,
    showUploadDialog
  }
}