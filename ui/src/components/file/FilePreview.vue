<template>
  <!-- 预览弹窗由组件自带，使用处只需 v-model 控制显隐 -->
  <el-dialog
    v-model="visible"
    width="900px"
    top="5vh"
    destroy-on-close
    :class="['preview-dialog', maximized ? 'preview-maximized' : '']"
    :style="maximized ? { width: '100vw', maxWidth: '100vw' } : undefined"
    @closed="maximized = false"
  >
    <template #header>
      <span class="preview-title">
        <span class="prefix">预览</span>
        <span class="filename">{{ fileName }}</span>
      </span>
    </template>
    <div class="preview-container">
      <!-- 分段导航 -->
      <div v-if="chunked && totalChunks > 1" class="chunk-nav">
        <el-button-group size="small">
          <el-button
            :disabled="currentChunk <= 0"
            @click="loadChunk(currentChunk - 1)"
          >
            <el-icon><component :is="ChevronLeftIcon" /></el-icon> 上一页
          </el-button>
          <el-button disabled>
            第 {{ currentChunk + 1 }} / {{ totalChunks }} 段
          </el-button>
          <el-button
            :disabled="currentChunk >= totalChunks - 1"
            @click="loadChunk(currentChunk + 1)"
          >
            下一页 <el-icon><component :is="ChevronRightIcon" /></el-icon>
          </el-button>
        </el-button-group>
        <span class="chunk-info">
          位置: {{ formatOffset(startOffset) }} -
          {{ formatOffset(endOffset) }} / {{ formatSize(totalSize) }}
        </span>
      </div>

      <!-- 加载状态 -->
      <div
        v-if="loading"
        style="
          display: flex;
          justify-content: center;
          align-items: center;
          height: 300px;
        "
      >
        <el-icon class="is-loading" :size="24"><Loading /></el-icon>
        <span v-if="chunked" style="margin-left: 12px"
          >加载分段 {{ currentChunk + 1 }}...</span
        >
      </div>

      <!-- Markdown 文件预览（渲染结果） -->
      <div
        v-else-if="isMarkdown && textContent"
        class="markdown-preview"
        :style="markdownContainerStyle"
      >
        <div v-html="renderedMarkdown"></div>
      </div>

      <!-- 文本文件预览 -->
      <div v-else-if="fileType === 'text'" :style="textContainerStyle">
        <el-input
          type="textarea"
          v-model="textContent"
          readonly
          :autosize="maximized ? undefined : { minRows: 20, maxRows: 30 }"
          :style="inputStyleComputed"
        />
      </div>

      <!-- 图片文件预览 -->
      <div
        v-else-if="fileType === 'image'"
        style="
          display: flex;
          justify-content: center;
          align-items: center;
          flex: 1;
          min-height: 0;
          overflow: hidden;
        "
      >
        <img
          :src="imageUrl"
          :alt="fileName"
          :style="{
            maxWidth: '100%',
            maxHeight: maximized ? 'calc(100vh - 200px)' : '500px',
            objectFit: 'contain',
          }"
        />
      </div>

      <!-- 音频文件预览 -->
      <div
        v-else-if="fileType === 'audio'"
        style="
          display: flex;
          flex-direction: column;
          justify-content: center;
          align-items: center;
          gap: 16px;
          flex: 1;
          min-height: 0;
          padding: 24px;
        "
      >
        <el-icon :size="56" style="color: var(--el-color-primary)"
          ><Headset
        /></el-icon>
        <div
          style="
            font-size: 14px;
            color: var(--el-text-color-regular);
            word-break: break-all;
            text-align: center;
          "
        >
          {{ fileName }}
        </div>
        <audio
          controls
          :src="mediaUrl"
          style="width: 100%; max-width: 560px"
          preload="metadata"
        />
      </div>

      <!-- 视频文件预览 -->
      <div
        v-else-if="fileType === 'video'"
        style="
          display: flex;
          justify-content: center;
          align-items: center;
          flex: 1;
          min-height: 0;
          overflow: hidden;
        "
      >
        <video
          controls
          :src="mediaUrl"
          preload="metadata"
          :style="{
            maxWidth: '100%',
            maxHeight: maximized ? 'calc(100vh - 200px)' : '500px',
            objectFit: 'contain',
            background: '#000',
          }"
        />
      </div>

      <!-- PDF文件预览 -->
      <div v-else-if="fileType === 'pdf'" style="flex: 1; min-height: 0">
        <iframe
          :src="pdfUrl"
          style="width: 100%; height: 100%"
          frameborder="0"
        />
      </div>

      <!-- 不支持预览的文件 -->
      <div
        v-else
        style="
          display: flex;
          flex-direction: column;
          justify-content: center;
          align-items: center;
          height: 300px;
        "
      >
        <el-icon :size="64" style="color: var(--el-text-color-placeholder)">
          <component :is="FileIcon" />
        </el-icon>
        <p
          style="
            font-size: 16px;
            color: var(--el-text-color-regular);
            margin: 20px 0;
          "
        >
          {{ errorMessage || "该文件类型不支持预览" }}
        </p>
        <el-button type="primary" @click="downloadFile">下载文件</el-button>
      </div>

      <!-- 备注：所有预览类型均展示（有则显示） -->
      <div v-if="fileNotes" class="preview-notes">
        <div class="preview-notes-label">备注</div>
        <div class="preview-notes-body">{{ fileNotes }}</div>
      </div>
    </div>

    <template #footer>
      <div style="display: flex; justify-content: flex-end; gap: 8px">
        <el-button @click="maximized = !maximized">{{
          maximized ? "还原" : "最大化"
        }}</el-button>
        <el-button type="primary" @click="downloadFile">下载</el-button>
        <el-button @click="visible = false">关闭</el-button>
      </div>
    </template>
  </el-dialog>
</template>

<script setup>
import { ref, computed, watch, h } from "vue";
import { Loading, Headset } from "@element-plus/icons-vue";
import { FileApi } from "@/api";
import { PathUtils } from "@/utils";
import { downloadWithTicket } from "@/utils/download";
import { isPreviewable, resolveMime } from "@/config/preview";
import { marked } from "marked";
import DOMPurify from "dompurify";

const props = defineProps({
  // 弹窗显隐（v-model）
  modelValue: { type: Boolean, default: false },
  file: { type: Object, default: () => ({}) },
  // 无 path 的来源（临时/私有文件）通过该函数获取下载 URL 以完成预览
  downloadApi: { type: Function, default: null },
  // 自定义下载动作（接收当前文件），用于各类来源各自的下载地址与下载后刷新
  download: { type: Function, default: null },
});

const emit = defineEmits(["update:modelValue"]);

// 弹窗显隐代理
const visible = computed({
  get: () => props.modelValue,
  set: (v) => emit("update:modelValue", v),
});
// 最大化状态由弹窗内部维护，关闭时复原
const maximized = ref(false);

const loading = ref(false);
const textContent = ref("");
const imageUrl = ref("");
const pdfUrl = ref("");
// 音频/视频内联播放地址（与图片/PDF 同走 fileUrl，依赖下载接口 Range/206 流式播放）
const mediaUrl = ref("");
const fileType = ref("unsupported");
const errorMessage = ref("");

// 分段加载状态
const chunked = ref(false);
const currentChunk = ref(0);
const totalChunks = ref(0);
const startOffset = ref(0);
const endOffset = ref(0);
const totalSize = ref(0);

// Icons
const FileIcon = () =>
  h(
    "svg",
    {
      xmlns: "http://www.w3.org/2000/svg",
      viewBox: "0 0 24 24",
      fill: "currentColor",
    },
    [
      h("path", {
        d: "M14 2H6c-1.1 0-1.99.9-1.99 2L4 20c0 1.1.89 2 1.99 2H18c1.1 0 2-.9 2-2V8l-6-6zm2 16H8v-2h8v2zm0-4H8v-2h8v2zm-3-5V3.5L18.5 9H13z",
      }),
    ],
  );
const ChevronLeftIcon = () =>
  h(
    "svg",
    {
      xmlns: "http://www.w3.org/2000/svg",
      viewBox: "0 0 24 24",
      fill: "currentColor",
      width: 16,
      height: 16,
    },
    [h("path", { d: "M15.41 7.41L14 6l-6 6 6 6 1.41-1.41L10.83 12z" })],
  );
const ChevronRightIcon = () =>
  h(
    "svg",
    {
      xmlns: "http://www.w3.org/2000/svg",
      viewBox: "0 0 24 24",
      fill: "currentColor",
      width: 16,
      height: 16,
    },
    [h("path", { d: "M10 6L8.59 7.41 13.17 12l-4.58 4.59L10 18l6-6z" })],
  );

const fileName = computed(() => props.file.name || props.file.filename || "");

// 文件备注（记录中的 notes 字段），为空则不展示
const fileNotes = computed(() => (props.file?.notes || "").toString().trim());

// 无 path 的来源（临时/私有文件）走 downloadApi 获取下载 URL 完成预览
const isDownloadBased = computed(
  () => !props.file?.path && typeof props.downloadApi === "function",
);

// 拼接预览 URL：附加 preview=true，后端据此内联返回且不触发下载即删/下载计数
const buildPreviewUrl = () => {
  const base = isDownloadBased.value ? props.downloadApi() || "" : "";
  if (!base) return "";
  return base + (base.includes("?") ? "&" : "?") + "preview=true";
};

const textContainerStyle = computed(() => {
  if (maximized.value) {
    return {
      flex: 1,
      overflow: "hidden",
      display: "flex",
      flexDirection: "column",
      minHeight: 0,
    };
  }
  return { flex: 1, overflow: "hidden" };
});

const markdownContainerStyle = computed(() => {
  if (maximized.value) {
    return { flex: 1, overflow: "auto" };
  }
  return { maxHeight: "70vh", overflow: "auto" };
});

const inputStyleComputed = computed(() => {
  const base = { fontFamily: "Courier New, monospace" };
  if (maximized.value) {
    return { ...base, flex: 1 };
  }
  return { ...base, height: "100%" };
});

// Markdown 渲染
const isMarkdown = computed(() => {
  const ext = fileName.value.toLowerCase();
  return ext.endsWith(".md") || ext.endsWith(".markdown");
});

const renderedMarkdown = computed(() => {
  if (!textContent.value) return "";
  return DOMPurify.sanitize(marked.parse(textContent.value));
});

// 判断文件类型（MIME 类型判断）
const detectFileType = (mimeType) => {
  if (!mimeType) return "unsupported";
  if (mimeType.startsWith("text/")) return "text";
  if (mimeType.startsWith("image/")) return "image";
  if (mimeType.startsWith("audio/")) return "audio";
  if (mimeType.startsWith("video/")) return "video";
  if (
    mimeType === "application/json" ||
    mimeType === "application/javascript" ||
    mimeType === "application/xml"
  )
    return "text";
  if (mimeType === "application/pdf") return "pdf";
  return "unsupported";
};

// 格式化文件大小
const formatSize = (bytes) => {
  if (!bytes) return "0 B";
  const units = ["B", "KB", "MB", "GB"];
  let i = 0;
  while (bytes >= 1024 && i < units.length - 1) {
    bytes /= 1024;
    i++;
  }
  return `${bytes.toFixed(1)} ${units[i]}`;
};

// 格式化偏移量
const formatOffset = (offset) => {
  return formatSize(offset);
};

// 加载预览内容
const loadPreviewContent = async () => {
  loading.value = true;
  errorMessage.value = "";
  textContent.value = "";
  chunked.value = false;
  imageUrl.value = "";
  pdfUrl.value = "";
  mediaUrl.value = "";

  try {
    if (isDownloadBased.value) {
      await loadDownloadBasedPreview();
      return;
    }

    if (!props.file?.path || typeof props.file.path !== "string") {
      fileType.value = "unsupported";
      errorMessage.value = "文件路径无效";
      return;
    }

    const fullPath = props.file.path;
    const result = await FileApi.preview(fullPath);

    if (!result.success) {
      errorMessage.value = result.message || "加载预览失败";
      fileType.value = "unsupported";
      return;
    }

    const data = result.data || {};
    fileType.value = detectFileType(data.mimeType);

    if (data.previewable) {
      if (data.content) {
        // 小文件，直接显示内容
        textContent.value = data.content;
        chunked.value = false;
      } else if (data.chunked) {
        // 大文件，需要分段加载
        chunked.value = true;
        totalChunks.value = data.totalChunks || 1;
        currentChunk.value = 0;
        totalSize.value = data.size || 0;
        // 自动加载第一段
        await loadChunk(0);
      } else if (data.fileUrl) {
        // 图片/PDF/音频/视频：统一使用内联预览地址
        imageUrl.value = data.fileUrl;
        pdfUrl.value = data.fileUrl;
        mediaUrl.value = data.fileUrl;
      }
    } else {
      errorMessage.value = "该文件类型不支持预览";
    }
  } catch (e) {
    console.error("预览加载失败:", e);
    errorMessage.value = "加载预览失败";
    fileType.value = "unsupported";
  } finally {
    loading.value = false;
  }
};

// 无 path 来源（临时/私有文件）的预览：文本走 fetch 拉取内容，图片/PDF 直接内联渲染
const loadDownloadBasedPreview = async () => {
  const name = fileName.value;
  const mime = props.file?.mimeType || resolveMime(name);

  if (!isPreviewable(name, mime)) {
    fileType.value = "unsupported";
    errorMessage.value = "该文件类型不支持预览";
    return;
  }

  const url = buildPreviewUrl();
  if (!url) {
    fileType.value = "unsupported";
    errorMessage.value = "无法获取文件地址";
    return;
  }

  let type = detectFileType(mime);
  // MIME 未识别但扩展名在白名单内时，按文本处理
  if (type === "unsupported") type = "text";
  fileType.value = type;

  if (type === "image") {
    imageUrl.value = url;
  } else if (type === "pdf") {
    pdfUrl.value = url;
  } else if (type === "audio" || type === "video") {
    mediaUrl.value = url;
  } else {
    const res = await fetch(url);
    if (!res.ok) {
      fileType.value = "unsupported";
      errorMessage.value =
        res.status === 410 ? "文件已过期或已被删除" : "加载预览失败";
      return;
    }
    textContent.value = await res.text();
  }
};

// 加载指定分段
const loadChunk = async (chunkIndex) => {
  if (chunkIndex < 0 || chunkIndex >= totalChunks.value) return;

  loading.value = true;
  currentChunk.value = chunkIndex;

  try {
    const fullPath = props.file.path;
    const result = await FileApi.previewChunk(fullPath, chunkIndex);

    if (result.success && result.data) {
      const data = result.data;
      if (data.content !== undefined && data.content !== null) {
        textContent.value = data.content || "";
      } else {
        errorMessage.value = "分段内容为空，请重试";
      }
      startOffset.value = data.startOffset || 0;
      endOffset.value = data.endOffset || 0;
    } else {
      errorMessage.value = result.message || "加载分段失败";
    }
  } catch (e) {
    console.error("分段加载失败:", e);
    errorMessage.value = "网络错误，请检查连接后重试";
  } finally {
    loading.value = false;
  }
};

const downloadFile = () => {
  // 使用处自定义下载动作优先（不同来源的下载地址与下载后刷新逻辑不同）
  if (typeof props.download === "function") {
    props.download(props.file);
    return;
  }
  if (props.file.path) {
    downloadWithTicket(`/download/${PathUtils.encodeFilePath(props.file.path)}`);
    return;
  }
  if (typeof props.downloadApi === "function") {
    const url = props.downloadApi();
    if (url) window.open(url, "_blank");
  }
};

// 弹窗打开或文件变化时加载内容（关闭状态下不发起请求）
watch(
  [() => props.file, visible],
  ([, open]) => {
    if (open) loadPreviewContent();
  },
  { immediate: true, deep: true },
);
</script>

<style lang="less">
.file-preview {
  .chunk-nav {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 8px 12px;
    background: var(--el-fill-color-light);
    border-bottom: 1px solid var(--el-border-color-light);
    margin-bottom: 8px;

    .chunk-info {
      font-size: 13px;
      color: var(--el-text-color-regular);
    }
  }
}
.preview-maximized {
  display: flex;
  flex-direction: column;
  height: 100vh;
  margin-top: 0 !important;

  .el-dialog__body {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }
  .el-textarea {
    flex: 1;
    min-height: 0;
    height: 100%;

    .el-textarea__inner {
      height: 100% !important;
      min-height: 0;
    }
  }
}

.preview-title {
  font-size: 14px;
  .prefix {
    background-color: #f0f0f0;
    border-radius: 3px;
    padding: 0 2px;
    margin-right: 5px;
  }
  .filename {
    font-size: small;
  }
}

.preview-container {
  height: 100%;
  min-height: 400px;
  position: relative;
  & > div {
    height: 100%;
  }
}

.preview-notes {
  flex-shrink: 0;
  margin-top: 12px;
  padding: 8px 12px;
  border-top: 1px solid var(--el-border-color-light);
  background: var(--el-fill-color-light);
  border-radius: 4px;
  max-height: 150px;
  overflow: auto;

  .preview-notes-label {
    font-size: 12px;
    color: var(--el-text-color-secondary);
    margin-bottom: 4px;
  }

  .preview-notes-body {
    font-size: 13px;
    line-height: 1.5;
    color: var(--el-text-color-regular);
    white-space: pre-wrap;
    word-break: break-word;
  }
}
</style>
