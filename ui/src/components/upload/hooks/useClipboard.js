import { ref } from 'vue'

// 剪贴板上传：读取剪贴板中可用格式、选择类型、预览，并转为文件加入上传队列
export function useClipboard({
  form,
  message,
  addFileToQueue,
  startUpload,
  uploadMessage,
  uploadMessageType,
  extras
}) {
  const clipboardPreview = ref({
    type: '',     // 'image', 'text', 'other'
    data: '',     // data URL for image, or text content
    text: '',
    filename: '',
    size: 0,
    file: null    // 实际 File 对象，用于上传
  })
  const clipboardRead = ref(false)
  const clipboardReading = ref(false)
  const clipboardFilename = ref('')
  const clipboardOptions = ref([])          // 可选类型列表 [{ value, label, rawType, blob, text, filename }]
  const clipboardSelectedType = ref('')     // 用户选择的类型 value
  const clipboardTypeConfirmed = ref(false) // 已确认选择类型

  const blobToDataURL = (blob) => {
    return new Promise((resolve) => {
      const reader = new FileReader()
      reader.onload = () => resolve(reader.result)
      reader.readAsDataURL(blob)
    })
  }

  // 读取剪贴板（发现所有可用类型）
  const readClipboard = async () => {
    clipboardReading.value = true
    uploadMessage.value = '正在读取剪贴板...'
    uploadMessageType.value = 'info'
    try {
      const items = await navigator.clipboard.read()
      const options = []
      for (const item of items) {
        for (const type of item.types) {
          if (type.startsWith('image/')) {
            const blob = await item.getType(type)
            const ext = type.split('/')[1]
            const dataUrl = await blobToDataURL(blob)
            const filename = `clipboard-${Date.now()}.${ext}`
            options.push({
              value: `image-${type}`,
              label: `图片 (${ext.toUpperCase()})`,
              rawType: type,
              blob,
              text: '',
              dataUrl,
              previewType: 'image',
              filename,
              file: new File([blob], filename, { type })
            })
          } else if (type === 'text/plain') {
            const blob = await item.getType(type)
            const text = await blob.text()
            if (text.trim()) {
              const filename = `clipboard-${Date.now()}.txt`
              options.push({
                value: 'text-plain',
                label: '纯文本 (TXT)',
                rawType: type,
                blob,
                text,
                dataUrl: '',
                previewType: 'text',
                filename,
                file: new File([text], filename, { type: 'text/plain' })
              })
            }
          } else if (type === 'text/html') {
            const blob = await item.getType(type)
            const html = await blob.text()
            const plainText = html.replace(/<[^>]*>/g, '').trim()
            if (plainText) {
              const filename = `clipboard-${Date.now()}.txt`
              options.push({
                value: 'text-html',
                label: '格式化文本 (HTML)',
                rawType: type,
                blob,
                text: plainText,
                dataUrl: '',
                previewType: 'text',
                filename,
                file: new File([plainText], filename, { type: 'text/plain' })
              })
            }
          } else if (type.startsWith('text/') || type.startsWith('application/')) {
            const blob = await item.getType(type)
            const filename = `clipboard-${Date.now()}`
            options.push({
              value: `other-${type}`,
              label: `其它 (${type.split('/').pop()})`,
              rawType: type,
              blob,
              text: '',
              dataUrl: '',
              previewType: 'other',
              filename,
              file: new File([blob], filename, { type })
            })
          }
        }
      }
      if (options.length === 0) {
        message.warning('剪贴板中没有可读取的内容')
        uploadMessage.value = ''
        clipboardReading.value = false
        return
      }
      clipboardOptions.value = options
      // 只有一种类型时直接选中
      if (options.length === 1) {
        clipboardSelectedType.value = options[0].value
        applyClipboardSelection()
      } else {
        clipboardSelectedType.value = options[0].value
        clipboardRead.value = true
        clipboardTypeConfirmed.value = false
        uploadMessage.value = ''
        message.info(`检测到 ${options.length} 种格式，请选择要读取的类型`)
      }
    } catch (e) {
      message.warning('无法读取剪贴板，请尝试 Ctrl+V 粘贴')
      uploadMessage.value = ''
    } finally {
      clipboardReading.value = false
    }
  }

  // 应用用户选择的剪贴板类型
  const applyClipboardSelection = () => {
    const opt = clipboardOptions.value.find(o => o.value === clipboardSelectedType.value)
    if (!opt) return
    clipboardPreview.value = {
      type: opt.previewType,
      data: opt.dataUrl,
      text: opt.text,
      filename: opt.filename,
      size: opt.blob.size,
      file: opt.file
    }
    clipboardFilename.value = opt.filename
    clipboardRead.value = true
    clipboardTypeConfirmed.value = true
    uploadMessage.value = ''
    message.success('已选择剪贴板数据类型，确认后点击"开始上传"')
  }

  // 清除剪贴板预览
  const clearClipboard = () => {
    clipboardPreview.value = { type: '', data: '', text: '', filename: '', size: 0, file: null }
    clipboardRead.value = false
    clipboardFilename.value = ''
    clipboardOptions.value = []
    clipboardSelectedType.value = ''
    clipboardTypeConfirmed.value = false
    uploadMessage.value = ''
  }

  // 上传剪贴板内容
  const uploadClipboard = () => {
    if (!clipboardRead.value || !clipboardPreview.value.file) {
      message.warning('请先读取剪贴板')
      return
    }
    const file = clipboardPreview.value.file
    // 使用用户自定义的文件名
    const finalName = clipboardFilename.value.trim() || file.name
    addFileToQueue({
      name: finalName,
      size: file.size,
      file: new File([file], finalName, { type: file.type }),
      notes: extras.extraNotes.value,
      depFileName: extras.extraDepFileName.value,
      depFileRecordId: extras.extraDepRecordId.value,
      depRelation: extras.extraDepRelation.value,
      depDescription: extras.extraDepDescription.value
    })
    message.success(`已添加文件: ${finalName}`)
    // 切换到 local 模式并开始上传
    form.uploadMethod = 'local'
    uploadMessage.value = ''
    startUpload()
  }

  return {
    clipboardPreview,
    clipboardRead,
    clipboardReading,
    clipboardFilename,
    clipboardOptions,
    clipboardSelectedType,
    clipboardTypeConfirmed,
    readClipboard,
    applyClipboardSelection,
    clearClipboard,
    uploadClipboard
  }
}