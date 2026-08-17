<template>
	<footer class="app-footer">
		<div class="footer-left">
			<a @click="showPreferences = true" class="link-button">偏好设置</a>
			<IndexStatusBar @triggerScan="handleTriggerScan" />
		</div>
		<div class="footer-right">
			<a v-if="openApiEnabled" href="/scalar.html" target="_blank" title="OpenAPI 接口文档">API 文档</a>
			<span v-if="openApiEnabled" class="divider">•</span>
			<a href="https://gitee.com/hyjiacan/fuzhan" target="_blank">轻共享</a>
			<span class="divider">•</span>
			<a href="https://gitee.com/hyjiacan/fuzhan/issues/new" target="_blank">反馈建议</a>
			<span class="divider">•</span>
			<span>hyjiacan © 2025</span>
		</div>

		<n-modal v-model:show="showPreferences" preset="card" title="偏好设置" style="width: 400px;">
			<div class="preference-item">
				<span class="preference-label">页面宽度</span>
				<n-select
					v-model:value="pageWidth"
					:options="widthOptions"
					@update:value="savePreferences"
				/>
			</div>
			<template #footer>
				<n-button type="primary" @click="showPreferences = false">关闭</n-button>
			</template>
		</n-modal>
	</footer>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { NModal, NSelect, NButton, useMessage } from 'naive-ui'
import store from '@/store'
import IndexStatusBar from './IndexStatusBar.vue'
import { IndexApi } from '@/api'

const openApiEnabled = computed(() => store.state.config.openApiEnabled)

const message = useMessage()

const handleTriggerScan = async () => {
	try {
		await IndexApi.triggerFullScan()
		message.success('全量扫描已触发')
	} catch (e) {
		message.error('触发扫描失败: ' + (e.message || '未知错误'))
	}
}

const STORAGE_KEY = 'page-width-preference'

const showPreferences = ref(false)
const pageWidth = ref('80%')

const widthOptions = [
	{ label: '50%', value: '50%' },
	{ label: '60%', value: '60%' },
	{ label: '70%', value: '70%' },
	{ label: '80%', value: '80%' },
	{ label: '90%', value: '90%' },
	{ label: '100%', value: '100%' },
]

const loadPreferences = () => {
	const saved = localStorage.getItem(STORAGE_KEY)
	if (saved) {
		pageWidth.value = saved
	}
}

const savePreferences = (value) => {
	localStorage.setItem(STORAGE_KEY, value)
	document.body.style.setProperty('--app-width', value)
}

onMounted(() => {
	loadPreferences()
})
</script>

<style lang="less">
@import '@/styles/variables.less';

.app-footer {
	height: 40px;
	padding: 6px 20px;
	background: transparent;
	display: flex;
	justify-content: space-between;
	align-items: center;
	border-top: 1px solid @border-color-light;
	flex-shrink: 0;

	.footer-left {
		display: flex;
		align-items: center;
		gap: 12px;

		.link-button {
			cursor: pointer;
			color: @text-color-secondary;
			font-size: @font-size-sm;
			transition: color 0.2s;

			&:hover {
				color: @primary-color;
			}
		}
	}

	.footer-right {
		color: @text-color-secondary;
		font-size: @font-size-sm;
		display: flex;
		align-items: center;
		gap: 12px;

		.divider {
			margin: 0 8px;
		}
	}

	.preference-item {
		display: flex;
		align-items: center;
		justify-content: space-between;
		padding: 12px 0;

		.preference-label {
			font-weight: 500;
		}

		.n-select {
			width: 120px;
		}
	}
}
</style>
